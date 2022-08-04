package store

// A Dependency represents a link between specific versions of 2 Go modules
type Dependency struct {
	DependentID string `json:"dependent_id" db:"dependent_id"`
	DependeeID  string `json:"dependee_id" db:"dependee_id"`
}
