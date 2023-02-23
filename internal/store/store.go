package store

import (
	"context"
	"fmt"
)

// Store defines the operations available on a Perseus data store
type Store interface {
	Ping(ctx context.Context) error

	SaveModule(ctx context.Context, name, description string, versions ...string) error
	SaveModuleDependencies(ctx context.Context, mod Version, deps ...Version) error

	QueryModules(ctx context.Context, nameFilter string, pageToken string, count int) ([]Module, string, error)
	QueryModuleVersions(ctx context.Context, query ModuleVersionQuery) (results []ModuleVersionQueryResult, nextPageToken string, err error)

	GetDependents(ctx context.Context, id, version string, pageToken string, count int) ([]Version, string, error)
	GetDependees(ctx context.Context, id, version string, pageToken string, count int) ([]Version, string, error)
}

// ModuleVersionQuery encapsulates the available parameters for querying for module versions.
//
// A zero value will return all stable versions for all modules.
type ModuleVersionQuery struct {
	// a glob pattern specifying which module(s) should be returned
	ModuleFilter string
	// a glob pattern specifying which version(s) should be returned
	VersionFilter string
	// if true, the query will also return pre-release versions
	IncludePrerelease bool
	// if true, the query will only return the most current version
	LatestOnly bool

	PageToken string
	Count     int
}

// pageTokenString returns the string that should be used to construct the page token returned to the
// API client for this request.
//
// The result is a concatenation of the four user-provided filters so that the generated token will be
// specific to this particular query.
func (q *ModuleVersionQuery) pageTokenString() string {
	return fmt.Sprintf("moduleversions:%s+%s+%v+%v", q.ModuleFilter, q.VersionFilter, q.IncludePrerelease, q.LatestOnly)
}

// ModuleVersionQueryResult is represents a set of modules each having a list of versions
type ModuleVersionQueryResult struct {
	Module, Version string
}
