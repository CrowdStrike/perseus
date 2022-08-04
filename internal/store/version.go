package store

// A Version represents a specific version of a Go module
type Version struct {
	ID       int32  `json:"id" db:"id"`
	ModuleID string `json:"module_id" db:"module_id"`
	SemVer   string `json:"semver" db:"version"`
}
