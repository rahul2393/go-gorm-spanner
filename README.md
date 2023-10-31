# go-gorm-spanner

[![go.dev reference](https://img.shields.io/badge/go.dev-reference-007d9c?logo=go&logoColor=white&style=flat-square)](https://pkg.go.dev/github.com/googleapis/go-gorm-spanner)

[Google Cloud Spanner](https://cloud.google.com/spanner) ORM for
Go's [GORM](https://gorm.io/) implementation.

``` go
import (
    "gorm.io/gorm"
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

// Print singers with more than 500 likes.
type Singer struct {
    gorm.Model
    Text         string
    Likes        int
}
var singers []Singer
if err := db.Where("likes > ?", 500).Find(&singers).Error; err != nil {
    log.Fatal(err)
}
for s := range singers {
    fmt.Println(s.ID, s.Text)
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

- Go 1.21
- Go 1.20
- Go 1.19

## Data Types
Cloud Spanner supports the following data types in combination with `gorm`.

| Cloud Spanner Type       | gorm / go type             |
|--------------------------|----------------------------|
| bool                     | bool, sql.NullBool         |
| int64                    | uint, int64, sql.NullInt64 |
| string                   | string, sql.NullString     |
| json                     | string, sql.NullString     |
| float64                  | float64, sql.NullFloat64   |
| numeric                  | decimal.NullDecimal        |
| timestamp with time zone | time.Time, sql.NullTime    |
| date                     | datatypes.Date             |
| bytes                    | []byte                     |


## Limitations
The following limitations are currently known:

| Limitation                                                                                     | Workaround                                                                                                                                                                                                |
|------------------------------------------------------------------------------------------------|-----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| OnConflict                                                                                     | OnConflict clauses are not supported                                                                                                                                                                      |
| Nested transactions                                                                            | Nested transactions and savepoints are not supported. It is therefore recommended to set the configuration option `DisableNestedTransaction: true,`                                                       |
| Locking                                                                                        | Lock clauses (e.g. `clause.Locking{Strength: "UPDATE"}`) are not supported. These are generally speaking also not required, as the default isolation level that is used by Cloud Spanner is serializable. |
| Auto-save associations                                                                         | Auto saved associations are not supported, as these will automatically use an OnConflict clause                                                                                                           |
| [gorm.Automigrate](https://gorm.io/docs/migration.html#Auto-Migration) with interleaved tables | Interleaved tables are not supported by gorm. It is therefore recommended to setup interleave tables separately.                                                                                          |
| [Cloud Spanner stale reads](https://cloud.google.com/spanner/docs/reads#go)                    | Stale reads are not supported by gorm.                                                                                                                                                                    |    

For complete list of the limitations, see the [Cloud Spanner GORM limitations](https://github.com/googleapis/go-gorm-spanner/blob/main/docs/limitations.md).

### OnConflict Clauses
`OnConflict` clauses are not supported by Cloud Spanner and should not be used. The following will
therefore not work.

```go
user := User{
    ID:   1,
    Name: "User Name",
}
// OnConflict is not supported and this will return an error.
db.Clauses(clause.OnConflict{DoNothing: true}).Create(&user)
```

### Auto-save Associations
Auto-saving associations will automatically use an `OnConflict` clause in gorm. These are not
supported. Instead, the parent entity of the association must be created before the child entity is
created.

```go
blog := Blog{
    ID:     1,
    Name:   "",
    UserID: 1,
    User: User{
        ID:   1,
        Name: "User Name",
    },
}
// This will fail, as the insert statement for User will use an OnConflict clause.
db.Create(&blog).Error
```

Instead, do the following:

```go
user := User{
    ID:   1,
    Name: "User Name",
    Age:  20,
}
blog := Blog{
    ID:     1,
    Name:   "",
    UserID: 1,
}
db.Create(&user)
db.Create(&blog)
```

### Nested Transactions
`gorm` uses savepoints for nested transactions. Savepoints are currently not supported by Cloud Spanner. Nested
transactions can therefore not be used with PGAdapter. It is recommended to set the configuration option
`DisableNestedTransactions: true` to be sure that `gorm` does not try to use a nested transaction.

```go
db, err := gorm.Open(postgres.Open(connectionString), &gorm.Config{
    DisableNestedTransaction: true,
})
```

### Locking
Locking clauses, like `clause.Locking{Strength: "UPDATE"}`, are not supported. These are generally speaking also not
required, as Cloud Spanner uses isolation level `serializable` for read/write transactions.

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
