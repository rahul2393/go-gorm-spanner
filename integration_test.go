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
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"cloud.google.com/go/spanner"
	database "cloud.google.com/go/spanner/admin/database/apiv1"
	"cloud.google.com/go/spanner/admin/database/apiv1/databasepb"
	instance "cloud.google.com/go/spanner/admin/instance/apiv1"
	"cloud.google.com/go/spanner/admin/instance/apiv1/instancepb"
	"github.com/googleapis/go-gorm/testutil"
	"google.golang.org/api/iterator"
	"google.golang.org/grpc/codes"
	"gorm.io/gorm"
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
		prefix = "gotest"
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

func isEmulatorEnvSet() bool {
	return os.Getenv("SPANNER_EMULATOR_HOST") != ""
}

func skipEmulatorTest(t *testing.T) {
	if isEmulatorEnvSet() {
		t.Skip("Skipping testing against the emulator.")
	}
}
func TestDefaultValue(t *testing.T) {
	skipIfShort(t)
	skipEmulatorTest(t)
	t.Parallel()
	dsn, cleanup, err := createTestDB(context.Background(), []string{`CREATE SEQUENCE seqT OPTIONS (sequence_kind = "bit_reversed_positive")`}...)
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
		testutil.BaseModel
		Email   string    `gorm:"not null;index:,unique"`
		Name    string    `gorm:"notNull;default:foo"`
		Name2   string    `gorm:"size:233;not null;default:'foo'"`
		Name3   string    `gorm:"size:233;notNull;default:''"`
		Age     int       `gorm:"default:18"`
		Created time.Time `gorm:"default:2000-01-02"`
		Enabled bool      `gorm:"default:true"`
	}

	db.Migrator().DropIndex(&Harumph{}, "idx_harumphs_email")
	db.Migrator().DropTable(&Harumph{})

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
}

func TestForeignKeyConstraints(t *testing.T) {
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
	db.Migrator().DropTable(&testutil.Profile{}, &testutil.Member{})
	if err := db.AutoMigrate(&testutil.Profile{}, &testutil.Member{}); err != nil {
		t.Fatalf("Failed to migrate, got error: %v", err)
	}
	member := testutil.Member{Refer: 1, Name: "foreign_key_constraints", Profile: testutil.Profile{Name: "my_profile"}}

	db.Create(&member)

	var profile testutil.Profile
	if err := db.First(&profile, "id = ?", member.Profile.ID).Error; err != nil {
		t.Fatalf("failed to find profile, got error: %v", err)
	} else if profile.MemberID != member.Refer {
		t.Fatalf("member id is not equal: expects: %v, got: %v", member.ID, profile.MemberID)
	}
}

func TestFind(t *testing.T) {
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

	if err := db.AutoMigrate(&testutil.User{}, &testutil.Account{}, &testutil.Pet{}, &testutil.Toy{}, &testutil.Company{}, &testutil.Language{}); err != nil {
		t.Fatalf("Failed to migrate, got error: %v", err)
	}

	users := []testutil.User{
		*testutil.GetUser("find", "1", testutil.Config{}),
		*testutil.GetUser("find", "2", testutil.Config{}),
		*testutil.GetUser("find", "3", testutil.Config{}),
	}

	if err := db.Create(&users).Error; err != nil {
		t.Fatalf("errors happened when create users: %v", err)
	}

	t.Run("First", func(t *testing.T) {
		var first testutil.User
		if err := db.Where("name = ?", "find").First(&first).Error; err != nil {
			t.Errorf("errors happened when query first: %v", err)
		} else {
			testutil.CheckUser(t, db, first, users[0])
		}
	})

	t.Run("Last", func(t *testing.T) {
		var last testutil.User
		if err := db.Where("name = ?", "find").Last(&last).Error; err != nil {
			t.Errorf("errors happened when query last: %v", err)
		} else {
			testutil.CheckUser(t, db, last, users[2])
		}
	})

	var all []testutil.User
	if err := db.Where("name = ?", "find").Find(&all).Error; err != nil || len(all) != 3 {
		t.Errorf("errors happened when query find: %v, length: %v", err, len(all))
	} else {
		for idx, user := range users {
			t.Run("FindAll#"+strconv.Itoa(idx+1), func(t *testing.T) {
				testutil.CheckUser(t, db, all[idx], user)
			})
		}
	}

	t.Run("FirstMap", func(t *testing.T) {
		first := map[string]interface{}{}
		if err := db.Model(&testutil.User{}).Where("name = ?", "find").First(first).Error; err != nil {
			t.Errorf("errors happened when query first: %v", err)
		} else {
			for _, name := range []string{"Name", "Age", "Birthday"} {
				t.Run(name, func(t *testing.T) {
					dbName := db.NamingStrategy.ColumnName("", name)

					switch name {
					case "Name":
						if _, ok := first[dbName].(string); !ok {
							t.Errorf("invalid data type for %v, got %#v", dbName, first[dbName])
						}
					case "Age":
						if _, ok := first[dbName].(int64); !ok {
							t.Errorf("invalid data type for %v, got %#v", dbName, first[dbName])
						}
					case "Birthday":
						if _, ok := first[dbName].(time.Time); !ok {
							t.Errorf("invalid data type for %v, got %#v", dbName, first[dbName])
						}
					}

					reflectValue := reflect.Indirect(reflect.ValueOf(users[0]))
					testutil.AssertEqual(t, first[dbName], reflectValue.FieldByName(name).Interface())
				})
			}
		}
	})

	t.Run("FirstMapWithTable", func(t *testing.T) {
		first := map[string]interface{}{}
		if err := db.Table("users").Where("name = ?", "find").Find(first).Error; err != nil {
			t.Errorf("errors happened when query first: %v", err)
		} else {
			for _, name := range []string{"Name", "Age", "Birthday"} {
				t.Run(name, func(t *testing.T) {
					dbName := db.NamingStrategy.ColumnName("", name)
					resultType := reflect.ValueOf(first[dbName]).Type().Name()

					switch name {
					case "Name":
						if !strings.Contains(resultType, "string") {
							t.Errorf("invalid data type for %v, got %v %#v", dbName, resultType, first[dbName])
						}
					case "Age":
						if !strings.Contains(resultType, "int") {
							t.Errorf("invalid data type for %v, got %v %#v", dbName, resultType, first[dbName])
						}
					case "Birthday":
						if !strings.Contains(resultType, "Time") {
							t.Errorf("invalid data type for %v, got %v %#v", dbName, resultType, first[dbName])
						}
					}

					reflectValue := reflect.Indirect(reflect.ValueOf(users[0]))
					testutil.AssertEqual(t, first[dbName], reflectValue.FieldByName(name).Interface())
				})
			}
		}
	})

	t.Run("FirstPtrMap", func(t *testing.T) {
		first := map[string]interface{}{}
		if err := db.Model(&testutil.User{}).Where("name = ?", "find").First(&first).Error; err != nil {
			t.Errorf("errors happened when query first: %v", err)
		} else {
			for _, name := range []string{"Name", "Age", "Birthday"} {
				t.Run(name, func(t *testing.T) {
					dbName := db.NamingStrategy.ColumnName("", name)
					reflectValue := reflect.Indirect(reflect.ValueOf(users[0]))
					testutil.AssertEqual(t, first[dbName], reflectValue.FieldByName(name).Interface())
				})
			}
		}
	})

	t.Run("FirstSliceOfMap", func(t *testing.T) {
		allMap := []map[string]interface{}{}
		if err := db.Model(&testutil.User{}).Where("name = ?", "find").Find(&allMap).Error; err != nil {
			t.Errorf("errors happened when query find: %v", err)
		} else {
			for idx, user := range users {
				t.Run("FindAllMap#"+strconv.Itoa(idx+1), func(t *testing.T) {
					for _, name := range []string{"Name", "Age", "Birthday"} {
						t.Run(name, func(t *testing.T) {
							dbName := db.NamingStrategy.ColumnName("", name)

							switch name {
							case "Name":
								if _, ok := allMap[idx][dbName].(string); !ok {
									t.Errorf("invalid data type for %v, got %#v", dbName, allMap[idx][dbName])
								}
							case "Age":
								if _, ok := allMap[idx][dbName].(int64); !ok {
									t.Errorf("invalid data type for %v, got %#v", dbName, allMap[idx][dbName])
								}
							case "Birthday":
								if _, ok := allMap[idx][dbName].(time.Time); !ok {
									t.Errorf("invalid data type for %v, got %#v", dbName, allMap[idx][dbName])
								}
							}

							reflectValue := reflect.Indirect(reflect.ValueOf(user))
							testutil.AssertEqual(t, allMap[idx][dbName], reflectValue.FieldByName(name).Interface())
						})
					}
				})
			}
		}
	})

	t.Run("FindSliceOfMapWithTable", func(t *testing.T) {
		allMap := []map[string]interface{}{}
		if err := db.Table("users").Where("name = ?", "find").Find(&allMap).Error; err != nil {
			t.Errorf("errors happened when query find: %v", err)
		} else {
			for idx, user := range users {
				t.Run("FindAllMap#"+strconv.Itoa(idx+1), func(t *testing.T) {
					for _, name := range []string{"Name", "Age", "Birthday"} {
						t.Run(name, func(t *testing.T) {
							dbName := db.NamingStrategy.ColumnName("", name)
							resultType := reflect.ValueOf(allMap[idx][dbName]).Type().Name()

							switch name {
							case "Name":
								if !strings.Contains(resultType, "string") {
									t.Errorf("invalid data type for %v, got %v %#v", dbName, resultType, allMap[idx][dbName])
								}
							case "Age":
								if !strings.Contains(resultType, "int") {
									t.Errorf("invalid data type for %v, got %v %#v", dbName, resultType, allMap[idx][dbName])
								}
							case "Birthday":
								if !strings.Contains(resultType, "Time") {
									t.Errorf("invalid data type for %v, got %v %#v", dbName, resultType, allMap[idx][dbName])
								}
							}

							reflectValue := reflect.Indirect(reflect.ValueOf(user))
							testutil.AssertEqual(t, allMap[idx][dbName], reflectValue.FieldByName(name).Interface())
						})
					}
				})
			}
		}
	})

	var models []testutil.User
	if err := db.Where("name in (?)", []string{"find"}).Find(&models).Error; err != nil || len(models) != 3 {
		t.Errorf("errors happened when query find with in clause: %v, length: %v", err, len(models))
	} else {
		for idx, user := range users {
			t.Run("FindWithInClause#"+strconv.Itoa(idx+1), func(t *testing.T) {
				testutil.CheckUser(t, db, models[idx], user)
			})
		}
	}

	// test array
	var models2 [3]testutil.User
	if err := db.Where("name in (?)", []string{"find"}).Find(&models2).Error; err != nil || len(models2) != 3 {
		t.Errorf("errors happened when query find with in clause: %v, length: %v", err, len(models2))
	} else {
		for idx, user := range users {
			t.Run("FindWithInClause#"+strconv.Itoa(idx+1), func(t *testing.T) {
				testutil.CheckUser(t, db, models2[idx], user)
			})
		}
	}

	// test smaller array
	var models3 [2]testutil.User
	if err := db.Where("name in (?)", []string{"find"}).Find(&models3).Error; err != nil || len(models3) != 2 {
		t.Errorf("errors happened when query find with in clause: %v, length: %v", err, len(models3))
	} else {
		for idx, user := range users[:2] {
			t.Run("FindWithInClause#"+strconv.Itoa(idx+1), func(t *testing.T) {
				testutil.CheckUser(t, db, models3[idx], user)
			})
		}
	}

	var none []testutil.User
	if err := db.Where("name in (?)", []string{}).Find(&none).Error; err != nil || len(none) != 0 {
		t.Errorf("errors happened when query find with in clause and zero length parameter: %v, length: %v", err, len(none))
	}
}
