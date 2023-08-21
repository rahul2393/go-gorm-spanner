package clause

import (
	"gorm.io/gorm/clause"
)

type Returning struct {
	Columns []clause.Column
}

// Name where clause name
func (returning Returning) Name() string {
	return "THEN RETURN"
}

// Build returning clause
func (returning Returning) Build(builder clause.Builder) {
	if len(returning.Columns) > 0 {
		for idx, column := range returning.Columns {
			if idx > 0 {
				builder.WriteByte(',')
			}

			builder.WriteQuoted(column)
		}
	} else {
		builder.WriteByte('*')
	}
}

// MergeClause merge order by clauses
func (returning Returning) MergeClause(clause *clause.Clause) {
	if v, ok := clause.Expression.(Returning); ok {
		returning.Columns = append(v.Columns, returning.Columns...)
	}

	clause.Expression = returning
}
