package store

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"

	sq "github.com/Masterminds/squirrel"
	_ "github.com/jackc/pgx/v4/stdlib" //nolint: revive // intentional blank import b/c that's how pgx works
	"github.com/jmoiron/sqlx"
)

const (
	tableModules            = "module"
	tableModuleVersions     = "module_version"
	tableModuleDependencies = "module_dependency"

	joinTargetDependents = `dependee_id`
	joinTargetDependees  = `dependent_id`
)

var (
	columnsModules = []string{"id", "name", "description"}
	//columnsModuleVersions = []string{"id", "module_id", "version"}
	//colummsModuleDependencies = []string{"dependent_id", "dependee_id"}

	psql = sq.StatementBuilder.PlaceholderFormat(sq.Dollar)
)

// PostgresClient performs store-related operations against a postgres backend
// database.
type PostgresClient struct {
	db *sqlx.DB
}

// ensure the PG client satisfies the Store interface
var _ Store = (*PostgresClient)(nil)

// SaveModule upserts module metadata. If there is an existing module with the provided name the
// description will be updated.  Otherwise, a new module will be inserted.
func (p *PostgresClient) SaveModule(ctx context.Context, name, description string) (int32, error) {
	if name == "" {
		return -1, fmt.Errorf("module name must be provided")
	}

	sql, args, err := psql.
		Insert(tableModules).
		Columns(columnsModules[1:]...). // don't provide ID on an insert
		Values(name, description).
		Suffix(`ON CONFLICT (name) DO UPDATE SET description = ? RETURNING id`, description).
		ToSql()
	if err != nil {
		return 0, fmt.Errorf("error constructing database command: %w", err)
	}
	res, err := p.db.QueryContext(ctx, sql, args...)
	if err != nil {
		return 0, fmt.Errorf("error executing database command: %w", err)
	}
	defer func() { _ = res.Close() }()

	if !res.Next() {
		return 0, fmt.Errorf("database insert modified 0 rows")
	}
	var moduleID int32
	if err = res.Scan(&moduleID); err != nil {
		return 0, fmt.Errorf("error processing database command result: %w", err)
	}
	if res.Next() {
		return 0, fmt.Errorf("database insert modified more than 1 row")
	}
	return moduleID, err
}

// GetModules retrieves modules by name or ID, where if no keys are provided, all modules are returned.
func (p *PostgresClient) GetModules(ctx context.Context, namesOrIDs ...string) ([]Module, error) {
	q := psql.
		Select(columnsModules...).
		From(tableModules)
	if len(namesOrIDs) != 0 {
		if _, parseErr := strconv.ParseInt(namesOrIDs[0], 10, 32); parseErr == nil {
			q = q.Where(sq.Eq{"id": namesOrIDs})
		} else {
			q = q.Where(sq.Eq{"name": namesOrIDs})
		}
	}
	sql, args, err := q.ToSql()
	if err != nil {
		return nil, err
	}

	var modules []Module
	err = p.db.SelectContext(ctx, &modules, sql, args...)
	if err != nil {
		return nil, err
	}
	return modules, nil
}

// QueryModules returns a list of 0 to count modules that match the specified name filter (glob format),
// along with a paging token.
//
// The pageToken argument, if provided, should be the return value from a prior call to this method
// with the same filter.  It will be decoded to determine the next "page" of results.  An invalid page
// token will result in an error being returned.
func (p *PostgresClient) QueryModules(ctx context.Context, nameFilter string, pageToken string, count int) ([]Module, string, error) {
	offset := 0
	if pageToken != "" {
		var err error
		offset, err = decodePageToken(pageToken, nameFilter)
		if err != nil {
			return nil, "", fmt.Errorf("invalid page token: %w", err)
		}
	}
	q := psql.
		Select(columnsModules...).
		From(tableModules)
	q = applyNameFilter(q, nameFilter)
	q = q.OrderBy("name")
	if offset > 0 {
		q = q.Offset(uint64(offset))
	}
	if count > 0 {
		q = q.Limit(uint64(count))
	}

	sql, args, err := q.ToSql()
	if err != nil {
		return nil, "", err
	}

	var results []Module
	err = p.db.SelectContext(ctx, &results, sql, args...)
	if err != nil {
		return nil, "", err
	}

	return results, encodePageToken(nameFilter, len(results), offset, count), nil
}

// SaveModuleVersions ...
func (p *PostgresClient) SaveModuleVersions(ctx context.Context, moduleID int32, versions ...string) error {
	if moduleID == 0 {
		return fmt.Errorf("moduleID must be provided")
	}
	var (
		cmd  string
		args []interface{}
		err  error
	)

	for i, ver := range versions {
		cmd, args, err = psql.
			Insert(tableModuleVersions).
			Columns("module_id", "version").
			Values(moduleID, strings.TrimPrefix(ver, "v")).
			Suffix("ON CONFLICT ON CONSTRAINT uc_module_version_module_id_version DO NOTHING").
			ToSql()
		if err != nil {
			return fmt.Errorf("error constructing SQL operation for versions[%d] (%v): %w", i, ver, err)
		}

		_, err = p.db.ExecContext(ctx, cmd, args...)
		if err != nil {
			return fmt.Errorf("error executing database operation for versions[%d] (%v): %w", i, ver, err)
		}
	}

	return nil
}

// GetVersions retrieves all known versions of a given module.  The provided ID can be either a module
// name or an integer module ID.
func (p *PostgresClient) GetVersions(ctx context.Context, nameOrID string) ([]Version, error) {
	if nameOrID == "" {
		return nil, fmt.Errorf("nameOrID must be provided")
	}
	var (
		sql  string
		args []interface{}
		err  error
	)
	if ival, parseErr := strconv.ParseInt(nameOrID, 10, 32); parseErr == nil {
		sql, args, err = psql.
			Select("mv.id", "mv.module_id", "'v' || mv.version AS version").
			From(tableModuleVersions + " mv").
			Where(sq.Eq{"mv.module_id": ival}).
			OrderBy("mv.version DESC").
			ToSql()
	} else {
		sql, args, err = psql.
			Select("mv.id", "mv.module_id", "'v' || mv.version AS version").
			From(tableModuleVersions + " mv").
			Join(tableModules + " m ON (m.id = mv.module_id)").
			Where(sq.Eq{"m.name": nameOrID}).
			OrderBy("mv.version DESC").
			ToSql()
	}

	if err != nil {
		return nil, err
	}

	var versions []Version
	err = p.db.SelectContext(ctx, &versions, sql, args...)
	if err != nil {
		return nil, err
	}

	return versions, nil
}

// QueryModuleVersions returns a list of 0 or more module versions for the specified module,
// along with a paging token.
//
// The pageToken argument, if provided, should be the return value from a prior call to this method
// with the same filter.  It will be decoded to determine the next "page" of results.  An invalid page
// token will result in an error being returned.
func (p *PostgresClient) QueryModuleVersions(ctx context.Context, module string, pageToken string, count int) (results []Version, nextPageToken string, err error) {
	offset := 0
	if pageToken != "" {
		var err error
		offset, err = decodePageToken(pageToken, "moduleversions:"+module)
		if err != nil {
			return nil, "", fmt.Errorf("invalid page token: %w", err)
		}
	}

	if module == "" {
		return nil, "", fmt.Errorf("the module name must be specified")
	}
	q := psql.
		Select("mv.id", "mv.module_id", "'v' || mv.version AS version").
		From(tableModuleVersions + " mv").
		Join(tableModules + " m ON (m.id = mv.module_id)").
		Where(sq.Eq{"m.name": module}).
		OrderBy("mv.version DESC")
	if offset > 0 {
		q = q.Offset(uint64(offset))
	}
	if count > 0 {
		q = q.Limit(uint64(count))
	}

	var (
		sql  string
		args []interface{}
	)
	sql, args, err = q.ToSql()
	if err != nil {
		return nil, "", err
	}

	err = p.db.SelectContext(ctx, &results, sql, args...)
	if err != nil {
		return nil, "", err
	}

	return results, encodePageToken("moduleversions:"+module, len(results), offset, count), nil
}

// GetDependents retrieves all known module versions that depend on the given
// module id and version pair.
func (p *PostgresClient) GetDependents(ctx context.Context, id, version string, pageToken string, count int) ([]Version, string, error) {
	return p.getDependx(ctx, id, version, joinTargetDependents, pageToken, count)
}

// GetDependees retrieves all known module versions that the given module id
// and version pair depend on.
func (p *PostgresClient) GetDependees(ctx context.Context, id, version string, pageToken string, count int) ([]Version, string, error) {
	return p.getDependx(ctx, id, version, joinTargetDependees, pageToken, count)
}

// getDependx is a shared query for dependency gathering in either direction,
// dependent on the joinType.
func (p *PostgresClient) getDependx(ctx context.Context, module, version, joinType string, pageToken string, count int) ([]Version, string, error) {
	pageTokenKey := "moduleversions:" + module + version + ":" + joinType
	offset := 0
	if pageToken != "" {
		var err error
		offset, err = decodePageToken(pageToken, pageTokenKey)
		if err != nil {
			return nil, "", fmt.Errorf("invalid page token: %w", err)
		}
	}
	if module == "" {
		return nil, "", fmt.Errorf("module must not be blank")
	}
	if version == "" {
		return nil, "", fmt.Errorf("version mut not be blank")
	}
	version = strings.TrimPrefix(version, "v")

	q := psql.
		Select("rhs.version_id id", "rhs.name module_id", "rhs.version").
		Prefix(`WITH mvs AS (SELECT m.id, m.name, mv.version, mv.id version_id FROM module m JOIN module_version mv ON (mv.module_id = m.id))`).
		From(tableModuleDependencies + " md")
	if joinType == joinTargetDependents {
		q = q.
			Join("mvs lhs ON (lhs.version_id = md." + joinType + ")").
			Join("mvs rhs ON (rhs.version_id = md." + joinTargetDependees + ")")
	} else {
		q = q.
			Join("mvs lhs ON (lhs.version_id = md." + joinType + ")").
			Join("mvs rhs ON (rhs.version_id = md." + joinTargetDependents + ")")
	}
	q = q.
		Where(sq.Eq{"lhs.name": module}).
		Where(sq.Eq{"lhs.version": version}).
		OrderBy("2", "3 DESC")
	if offset > 0 {
		q = q.Offset(uint64(offset))
	}
	if count > 0 {
		q = q.Limit(uint64(count))
	}
	sql, args, err := q.ToSql()
	if err != nil {
		return nil, "", err
	}
	log.Printf("getDependx():\n\tsql: %s\n\targs: %v\n", sql, args)
	var dependents []Version
	err = p.db.SelectContext(ctx, &dependents, sql, args...)
	if err != nil {
		return nil, "", err
	}

	return dependents, encodePageToken(pageTokenKey, len(dependents), offset, count), nil
}

// NewPostgresClient initializes a store client for interacting with a
// PostgreSQL backend. If it can not immediately reach the target database, an
// error is returned.
func NewPostgresClient(ctx context.Context, url string, opts ...PGOption) (*PostgresClient, error) {
	db, err := sqlx.ConnectContext(ctx, "pgx", url)
	if err != nil {
		return nil, err
	}
	err = db.PingContext(ctx)
	if err != nil {
		return nil, err
	}
	return &PostgresClient{
		db: db,
	}, nil
}

func applyNameFilter(q sq.SelectBuilder, nameFilter string) sq.SelectBuilder {
	if nameFilter == "" {
		return q
	}
	// translate glob ? and * wildcards to SQL equivalents
	where := strings.Map(func(c rune) rune {
		switch c {
		case '?':
			return '_'
		case '*':
			return '%'
		default:
			return c
		}
	}, nameFilter)
	// treat a filter with no wildards as a "contains" substring match
	if !strings.ContainsAny(where, "%_") {
		where = "%" + where + "%"
	}
	return q.Where(sq.Like{"name": where})
}
