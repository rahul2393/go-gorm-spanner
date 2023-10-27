# go-gorm

[![go.dev reference](https://img.shields.io/badge/go.dev-reference-007d9c?logo=go&logoColor=white&style=flat-square)](https://pkg.go.dev/github.com/googleapis/go-gorm-spanner)

[Google Cloud Spanner](https://cloud.google.com/spanner) ORM for
Go's [GORM](https://gorm.io/) implementation.

``` go
import (
	_ "github.com/googleapis/go-sql-spanner"

	spannergorm "github.com/googleapis/go-gorm-spanner"
)

db, err := gorm.Open(spannergorm.New(spannergorm.Config{
    DriverName: "spanner",
    DSN:        "projects/PROJECT/instances/INSTANCE/databases/DATABASE",
}), &gorm.Config{PrepareStmt: true})
if err != nil {
    log.Fatal(err)
}

// Print tweets with more than 500 likes.
type Tweet struct {
    ID           int `gorm:"primarykey;autoIncrement:false"`
    Text         string
    Likes        int
}
var tweets []Tweet
if err := db.Where("likes > ?", 500).Find(&tweets).Error; err != nil {
    log.Fatal(err)
}
for t := range tweets {
    fmt.Println(t.ID, t.Text)
}
```

## Emulator

See the [Google Cloud Spanner Emulator](https://cloud.google.com/spanner/docs/emulator) support to learn how to start the emulator.
Once the emulator is started and the host environmental flag is set, the driver should just work.

```
$ gcloud beta emulators spanner start
$ export SPANNER_EMULATOR_HOST=localhost:9010
```

## [Go Versions Supported](#supported-versions)

Our libraries are compatible with at least the three most recent, major Go
releases. They are currently compatible with:

- Go 1.20
- Go 1.19
- Go 1.18
- Go 1.17

## Authorization

By default, each API will use [Google Application Default Credentials](https://developers.google.com/identity/protocols/application-default-credentials)
for authorization credentials used in calling the API endpoints. This will allow your
application to run in many environments without requiring explicit configuration.

## Contributing

Contributions are welcome. Please, see the
[CONTRIBUTING](https://github.com/googleapis/go-sql-spanner/blob/main/CONTRIBUTING.md)
document for details.

Please note that this project is released with a Contributor Code of Conduct.
By participating in this project you agree to abide by its terms.
See [Contributor Code of Conduct](https://github.com/googleapis/go-sql-spanner/blob/main/CODE_OF_CONDUCT.md)
for more information.
