package store

import "context"

// Store defines the operations available on a Perseus data store
type Store interface {
	SaveModule(ctx context.Context, name, description string) (int32, error)
	GetModules(ctx context.Context, namesOrIDs ...string) ([]Module, error)
	QueryModules(ctx context.Context, nameFilter string, pageToken string, count int) ([]Module, string, error)

	SaveModuleVersions(ctx context.Context, moduleID int32, versions ...string) error
	GetVersions(ctx context.Context, nameOrID string) ([]Version, error)
	QueryModuleVersions(ctx context.Context, module string, pageToken string, count int) (results []Version, nextPageToken string, err error)

	GetDependents(ctx context.Context, id, version string, pageToken string, count int) ([]Version, string, error)
	GetDependees(ctx context.Context, id, version string, pageToken string, count int) ([]Version, string, error)

	SaveModuleDependencies(ctx context.Context, mod Version, deps ...Version) error
}
