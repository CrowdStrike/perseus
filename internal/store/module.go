package store

import "database/sql"

// A Module represents a particular Go module known by the system.
type Module struct {
	ID          int32          `json:"id" db:"id"`
	Name        string         `json:"name,omitempty" db:"name"`
	Description sql.NullString `json:"description,omitempty" db:"description"`
}
