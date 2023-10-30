package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/googleapis/go-gorm-spanner/samples/interleave"
)

// Run with `go run sample.go` to run the sample application.
func main() {
	// TODO(developer): Uncomment if your environment does not already have default credentials set.
	// os.Setenv("GOOGLE_APPLICATION_CREDENTIALS", "/path/to/credentials.json")

	// TODO(developer): Replace defaults with your project, instance and database if you want to run the sample
	//                  without having to specify any command line arguments.
	project := flag.String("project", "my-project", "The Google Cloud project of the Cloud Spanner instance")
	instance := flag.String("instance", "my-instance", "The Cloud Spanner instance to connect to")
	database := flag.String("database", "my-database", "The Cloud Spanner database to connect to")

	flag.Parse()
	if err := samples.RunSample(os.Stdout, "projects/"+*project+"/instances/"+*instance+"/databases/"+*database); err != nil {
		fmt.Printf("Failed to run sample: %v\n", err)
		os.Exit(1)
	}
}
