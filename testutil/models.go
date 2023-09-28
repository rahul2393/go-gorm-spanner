package testutil

import (
	"time"

	"cloud.google.com/go/spanner"
)

// BaseModel is embedded in all other models to add common database fields.
type BaseModel struct {
	// ID is the primary key of each model.
	// Adding the `primaryKey` annotation is redundant for most models, as gorm will assume that the column with name ID
	// is the primary key. This is however not redundant for models that add additional primary key columns, such as
	// child tables in interleaved table hierarchies, as a missing primary key annotation here would then cause the
	// primary key column defined on the child table to be the only primary key column.
	ID uint `gorm:"primarykey;default:GET_NEXT_SEQUENCE_VALUE(Sequence seqT)"`
	// CreatedAt and UpdatedAt are managed automatically by gorm.
	CreatedAt time.Time
	UpdatedAt time.Time
}

// User has one `Account` (has one), many `Pets` (has many) and `Toys` (has many - polymorphic)
// He works in a Company (belongs to), he has a Manager (belongs to - single-table), and also managed a Team (has many - single-table)
// He speaks many languages (many to many) and has many friends (many to many - single-table)
// His pet also has one Toy (has one - polymorphic)
// NamedPet is a reference to a Named `Pets` (has many)
type User struct {
	BaseModel
	Name      string
	Age       int64
	Birthday  spanner.NullTime
	Account   Account
	Pets      []*Pet
	NamedPet  *Pet
	Toys      []Toy `gorm:"polymorphic:Owner"`
	CompanyID spanner.NullInt64
	Company   Company
	ManagerID spanner.NullString
	Manager   *User
	Team      []User     `gorm:"foreignkey:ManagerID"`
	Languages []Language `gorm:"many2many:UserSpeak;"`
	Friends   []*User    `gorm:"many2many:user_friends;"`
	Active    bool
}

type Account struct {
	BaseModel
	UserID spanner.NullString
	Number string
}

type Pet struct {
	BaseModel
	UserID spanner.NullString
	Name   string
	Toy    Toy `gorm:"polymorphic:Owner;"`
}

type Toy struct {
	BaseModel
	Name      string
	OwnerID   string
	OwnerType string
}

type Company struct {
	ID   int
	Name string
}

type Language struct {
	Code string `gorm:"primarykey"`
	Name string
}
