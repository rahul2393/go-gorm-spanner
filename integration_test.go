// Copyright 2023 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package gorm

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/api/iterator"
	"google.golang.org/grpc/codes"
	"gorm.io/gorm"

	"cloud.google.com/go/spanner"
	database "cloud.google.com/go/spanner/admin/database/apiv1"
	"cloud.google.com/go/spanner/admin/database/apiv1/databasepb"
	instance "cloud.google.com/go/spanner/admin/instance/apiv1"
	"cloud.google.com/go/spanner/admin/instance/apiv1/instancepb"
)

var projectId, instanceId string
var skipped bool

func init() {
	var ok bool

	// Get environment variables or set to default.
	if instanceId, ok = os.LookupEnv("SPANNER_TEST_INSTANCE"); !ok {
		instanceId = "test-instance"
	}
	if projectId, ok = os.LookupEnv("SPANNER_TEST_PROJECT"); !ok {
		projectId = "test-project"
	}
}

func runsOnEmulator() bool {
	if _, ok := os.LookupEnv("SPANNER_EMULATOR_HOST"); ok {
		return true
	}
	return false
}

func initTestInstance(config string) (cleanup func(), err error) {
	ctx := context.Background()
	instanceAdmin, err := instance.NewInstanceAdminClient(ctx)
	if err != nil {
		return nil, err
	}
	defer instanceAdmin.Close()
	// Check if the instance exists or not.
	_, err = instanceAdmin.GetInstance(ctx, &instancepb.GetInstanceRequest{
		Name: fmt.Sprintf("projects/%s/instances/%s", projectId, instanceId),
	})
	if err == nil {
		return func() {}, nil
	}
	if spanner.ErrCode(err) != codes.NotFound {
		return nil, err
	}

	// Instance does not exist. Create a temporary instance for this test run.
	// The instance will be deleted after the test run.
	op, err := instanceAdmin.CreateInstance(ctx, &instancepb.CreateInstanceRequest{
		Parent:     fmt.Sprintf("projects/%s", projectId),
		InstanceId: instanceId,
		Instance: &instancepb.Instance{
			Config:      fmt.Sprintf("projects/%s/instanceConfigs/%s", projectId, config),
			DisplayName: instanceId,
			NodeCount:   1,
			Labels: map[string]string{
				"gormtestinstance": "true",
				"createdate":       fmt.Sprintf("t%d", time.Now().Unix()),
			},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("could not create instance %s: %v", fmt.Sprintf("projects/%s/instances/%s", projectId, instanceId), err)
	} else {
		// Wait for the instance creation to finish.
		_, err := op.Wait(ctx)
		if err != nil {
			return nil, fmt.Errorf("waiting for instance creation to finish failed: %v", err)
		}
	}
	// Delete the instance after all tests have finished.
	// Also delete any stale test instances that might still be around on the project.
	return func() {
		instanceAdmin, err := instance.NewInstanceAdminClient(ctx)
		if err != nil {
			return
		}
		// Delete this test instance.
		instanceAdmin.DeleteInstance(ctx, &instancepb.DeleteInstanceRequest{
			Name: fmt.Sprintf("projects/%s/instances/%s", projectId, instanceId),
		})
		// Also delete any other stale test instance.
		instances := instanceAdmin.ListInstances(ctx, &instancepb.ListInstancesRequest{
			Parent: fmt.Sprintf("projects/%s", projectId),
			Filter: "label.gormtestinstance:*",
		})
		for {
			instance, err := instances.Next()
			if err == iterator.Done {
				break
			}
			if err != nil {
				log.Printf("failed to fetch instances during cleanup: %v", err)
				break
			}
			if createdAtString, ok := instance.Labels["createdate"]; ok {
				// Strip the leading 't' from the value.
				seconds, err := strconv.ParseInt(createdAtString[1:], 10, 64)
				if err != nil {
					log.Printf("failed to parse created time from string %q of instance %s: %v", createdAtString, instance.Name, err)
				} else {
					diff := time.Duration(time.Now().Unix()-seconds) * time.Second
					if diff > time.Hour*2 {
						log.Printf("deleting stale test instance %s", instance.Name)
						instanceAdmin.DeleteInstance(ctx, &instancepb.DeleteInstanceRequest{
							Name: instance.Name,
						})
					}
				}
			}
		}
	}, nil
}

func createTestDB(ctx context.Context, statements ...string) (dsn string, cleanup func(), err error) {
	databaseAdminClient, err := database.NewDatabaseAdminClient(ctx)
	if err != nil {
		return "", nil, err
	}
	defer databaseAdminClient.Close()
	prefix, ok := os.LookupEnv("SPANNER_TEST_DBID")
	if !ok {
		prefix = "gormtest"
	}
	currentTime := time.Now().UnixNano()
	databaseId := fmt.Sprintf("%s-%d", prefix, currentTime)
	opdb, err := databaseAdminClient.CreateDatabase(ctx, &databasepb.CreateDatabaseRequest{
		Parent:          fmt.Sprintf("projects/%s/instances/%s", projectId, instanceId),
		CreateStatement: fmt.Sprintf("CREATE DATABASE `%s`", databaseId),
		ExtraStatements: statements,
	})
	if err != nil {
		return "", nil, err
	} else {
		// Wait for the database creation to finish.
		_, err := opdb.Wait(ctx)
		if err != nil {
			return "", nil, fmt.Errorf("waiting for database creation to finish failed: %v", err)
		}
	}
	dsn = "projects/" + projectId + "/instances/" + instanceId + "/databases/" + databaseId
	cleanup = func() {
		databaseAdminClient, err := database.NewDatabaseAdminClient(ctx)
		if err != nil {
			return
		}
		defer databaseAdminClient.Close()
		databaseAdminClient.DropDatabase(ctx, &databasepb.DropDatabaseRequest{
			Database: fmt.Sprintf("projects/%s/instances/%s/databases/%s", projectId, instanceId, databaseId),
		})
	}
	return
}

func initIntegrationTests() (cleanup func(), err error) {
	flag.Parse() // Needed for testing.Short().
	noop := func() {}
	if testing.Short() {
		log.Println("Integration tests skipped in -short mode.")
		return noop, nil
	}
	_, hasCredentials := os.LookupEnv("GOOGLE_APPLICATION_CREDENTIALS")
	_, hasEmulator := os.LookupEnv("SPANNER_EMULATOR_HOST")
	if !(hasCredentials || hasEmulator) {
		log.Println("Skipping integration tests as no credentials and no emulator host has been set")
		skipped = true
		return noop, nil
	}

	// Automatically create test instance if necessary.
	config := "regional-us-east1"
	if _, ok := os.LookupEnv("SPANNER_EMULATOR_HOST"); ok {
		config = "emulator-config"
	}
	cleanup, err = initTestInstance(config)
	if err != nil {
		return nil, err
	}

	return cleanup, nil
}

func TestMain(m *testing.M) {
	cleanup, err := initIntegrationTests()
	if err != nil {
		log.Fatalf("could not init integration tests: %v", err)
		os.Exit(1)
	}
	res := m.Run()
	cleanup()
	os.Exit(res)
}

func skipIfShort(t *testing.T) {
	if testing.Short() {
		t.Skip("Integration tests skipped in -short mode.")
	}
	if skipped {
		t.Skip("Integration tests skipped")
	}
}

func TestDefaultValue(t *testing.T) {
	skipIfShort(t)
	t.Parallel()
	dsn, cleanup, err := createTestDB(context.Background())
	if err != nil {
		log.Fatalf("could not init integration tests while creating database: %v", err)
		os.Exit(1)
	}
	defer cleanup()
	// Open db.
	db, err := gorm.Open(New(Config{
		DriverName: "spanner",
		DSN:        dsn,
	}), &gorm.Config{PrepareStmt: true})
	if err != nil {
		log.Fatal(err)
	}

	type Harumph struct {
		gorm.Model

		Email   string    `gorm:"not null;index:,unique"`
		Name    string    `gorm:"notNull;default:foo"`
		Name2   string    `gorm:"size:233;not null;default:'foo'"`
		Name3   string    `gorm:"size:233;notNull;default:''"`
		Age     int       `gorm:"default:18"`
		Created time.Time `gorm:"default:2000-01-02"`
		Enabled bool      `gorm:"default:true"`
	}

	if err := db.AutoMigrate(&Harumph{}); err != nil {
		t.Fatalf("Failed to migrate with default value, got error: %v", err)
	}

	harumph := Harumph{Email: "hello@gorm.io"}
	if err := db.Create(&harumph).Error; err != nil {
		t.Fatalf("Failed to create data with default value, got error: %v", err)
	} else if harumph.Name != "foo" || harumph.Name2 != "foo" || harumph.Name3 != "" || harumph.Age != 18 || !harumph.Enabled {
		t.Fatalf("Failed to create data with default value, got: %+v", harumph)
	}

	var result Harumph
	if err := db.First(&result, "email = ?", "hello@gorm.io").Error; err != nil {
		t.Fatalf("Failed to find created data, got error: %v", err)
	} else if result.Name != "foo" || result.Name2 != "foo" || result.Name3 != "" || result.Age != 18 || !result.Enabled || result.Created.Format("20060102") != "20000102" {
		t.Fatalf("Failed to find created data with default data, got %+v", result)
	}
	require.Conditionf(t, func() (success bool) {
		return result.ID > 0
	}, "ID should be greater than 0")
}
