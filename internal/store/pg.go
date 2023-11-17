package store

import (
	"context"
	"database/sql"
	"fmt"
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

	psql = sq.StatementBuilder.PlaceholderFormat(sq.Dollar)
)

// PostgresClient performs store-related operations against a postgres backend
// database.
type PostgresClient struct {
	db  *sqlx.DB
	log Logger
}

// ensure the PG client satisfies the Store interface
var _ Store = (*PostgresClient)(nil)

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
	p := &PostgresClient{
		db: db,
	}
	for _, fn := range opts {
		if err = fn(p); err != nil {
			return nil, err
		}
	}
	if p.log == nil {
		p.log = nopLogger{}
	}
	return p, nil
}

// Ping verifies that the database connection is available
func (p *PostgresClient) Ping(ctx context.Context) error {
	return p.db.PingContext(ctx)
}

// SaveModule upserts module metadata. If there is an existing module with the provided name the
// description will be updated.  Otherwise, a new module will be inserted.
func (p *PostgresClient) SaveModule(ctx context.Context, name, description string, versions ...string) (err error) {
	if name == "" {
		return fmt.Errorf("module name must be provided")
	}

	var txn *sql.Tx
	txn, err = p.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("unable to start a database transaction: %w", err)
	}

	defer func() {
		if err == nil {
			err = txn.Commit()
		} else {
			_ = txn.Rollback()
		}
	}()

	var moduleID int32
	moduleID, err = writeModule(ctx, txn, name, description)
	if err != nil {
		return err
	}

	if _, err = writeModuleVersions(ctx, txn, moduleID, versions...); err != nil {
		return err
	}
	return nil
}

// SaveModuleDependencies writes the specified set of direct dependencies of mod to the database.
func (p *PostgresClient) SaveModuleDependencies(ctx context.Context, mod Version, deps ...Version) (err error) {
	if mod.ModuleID == "" || mod.SemVer == "" {
		return fmt.Errorf("invalid module, both the module name and version must be specified")
	}
	var txn *sql.Tx
	txn, err = p.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("unable to start a database transaction: %w", err)
	}
	defer func() {
		if err == nil {
			err = txn.Commit()
		} else {
			if e2 := txn.Rollback(); e2 != nil {
				p.log.Error(e2, "error rolling back transaction after error")
			}
		}
	}()

	p.log.Debug("saving module", "moduleName", mod.ModuleID, "version", mod.SemVer)
	pkey, err := writeModule(ctx, txn, mod.ModuleID, "")
	if err != nil {
		return err
	}
	versionIDs, err := writeModuleVersions(ctx, txn, pkey, mod.SemVer)
	if err != nil {
		return err
	}
	if len(deps) == 0 {
		return nil
	}
	cmd := psql.
		Insert("module_dependency").
		Columns("dependent_id", "dependee_id")
	// it's possible for a given dependency to appear in a module's go.mod more than once if it hasn't
	// been 'go mod tidy'-ed, so we skip any duplicates here to avoid updating the same row in the
	// database multiple times in a single command
	uniqueDeps := map[string]struct{}{}
	for _, d := range deps {
		p.log.Debug("saving dependency", "moduleName", d.ModuleID, "version", d.SemVer)
		pkey, err := writeModule(ctx, txn, d.ModuleID, "")
		if err != nil {
			return err
		}
		vids, err := writeModuleVersions(ctx, txn, pkey, d.SemVer)
		if err != nil {
			return err
		}
		k := fmt.Sprintf("%d-%d", versionIDs[0], vids[0])
		if _, found := uniqueDeps[k]; found {
			p.log.Debug("skipping duplicate dependency", "dependency", d.ModuleID+"@"+d.SemVer)
			continue
		}
		cmd = cmd.Values(versionIDs[0], vids[0])
		uniqueDeps[k] = struct{}{}
	}
	sql, args, err := cmd.Suffix("ON CONFLICT (dependent_id, dependee_id) DO UPDATE SET dependent_id = EXCLUDED.dependent_id").ToSql()
	if err != nil {
		return fmt.Errorf("error constructing SQL query: %w", err)
	}
	p.log.Debug("upsert module dependencies", "sql", sql, "args", args)
	if _, err = txn.ExecContext(ctx, sql, args...); err != nil {
		return fmt.Errorf("database error saving new module dependency: %w", err)
	}
	return nil
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

// QueryModuleVersions returns a list of 0 or more module versions for the specified module,
// along with a paging token.
//
// The pageToken argument, if provided, should be the return value from a prior call to this method
// with the same filter.  It will be decoded to determine the next "page" of results.  An invalid page
// token will result in an error being returned.
func (p *PostgresClient) QueryModuleVersions(ctx context.Context, query ModuleVersionQuery) (results []ModuleVersionQueryResult, nextPageToken string, err error) {
	offset := 0
	if query.PageToken != "" {
		var err error
		offset, err = decodePageToken(query.PageToken, query.pageTokenString())
		if err != nil {
			return nil, "", fmt.Errorf("invalid page token: %w", err)
		}
	}

	if query.ModuleFilter == "" {
		return nil, "", fmt.Errorf("the module name must be specified")
	}
	var columnList []string
	if query.LatestOnly {
		columnList = []string{"m.name", "MAX(mv.version) AS version"}
	} else {
		columnList = []string{"m.name", "mv.version AS version"}
	}
	q := psql.
		Select(columnList...).
		From(tableModuleVersions + " mv").
		Join(tableModules + " m ON (m.id = mv.module_id)")
	if strings.ContainsAny(query.ModuleFilter, "*?") {
		q = q.Where(sq.Like{"m.name": globToLike(query.ModuleFilter)})
	} else {
		q = q.Where(sq.Eq{"m.name": query.ModuleFilter})
	}
	if query.VersionFilter != "" {
		if strings.ContainsAny(query.VersionFilter, "*?") {
			q = q.Where(sq.Like{"mv.version::text": globToLike(query.VersionFilter)})
		} else {
			q = q.Where(sq.Eq{"mv.version": query.VersionFilter})
		}
	}
	if !query.IncludePrerelease {
		q = q.Where(sq.Eq{"get_semver_prerelease(mv.version)": ""})
	}
	if query.LatestOnly {
		q = q.GroupBy("m.name")
	}
	q = q.OrderBy("1, 2 DESC")
	if offset > 0 {
		q = q.Offset(uint64(offset))
	}
	if query.Count > 0 {
		q = q.Limit(uint64(query.Count))
	}

	var (
		sql  string
		args []interface{}
	)
	sql, args, err = q.ToSql()
	if err != nil {
		return nil, "", err
	}

	type queryResult struct {
		ID       int32  `db:"id"`
		ModuleID string `db:"module_id"`
		Module   string `db:"name"`
		SemVer   string `db:"version"`
	}
	var rows []queryResult
	p.log.Debug("QueryModuleVersions", "sql", sql, "args", args)
	err = p.db.SelectContext(ctx, &rows, sql, args...)
	if err != nil {
		return nil, "", err
	}

	for _, row := range rows {
		results = append(results, ModuleVersionQueryResult{Module: row.Module, Version: row.SemVer})
	}

	return results, encodePageToken(query.pageTokenString(), len(results), offset, query.Count), nil
}

// GetDependents retrieves all known module versions that depend on the given
// module id and version pair.
func (p *PostgresClient) GetDependents(ctx context.Context, id, version string, pageToken string, count int) ([]Version, string, error) {
	return getDependx(ctx, p.db, id, version, joinTargetDependents, pageToken, count, p.log)
}

// GetDependees retrieves all known module versions that the given module id
// and version pair depend on.
func (p *PostgresClient) GetDependees(ctx context.Context, id, version string, pageToken string, count int) ([]Version, string, error) {
	return getDependx(ctx, p.db, id, version, joinTargetDependees, pageToken, count, p.log)
}

// getModuleVersionID executes a database query to translate the specified module and version to the
// corresponding PKEY in the module_version table, creating the module and/or version if necessary
func getModuleVersionID(ctx context.Context, db database, mod, ver string, log func(string, ...any)) (int32, error) { //nolint: unused // not calling this but hanging onto it for now
	q := psql.
		Select("mv.id").
		From("module_version mv").
		Join("module m ON (m.id = mv.module_id)").
		Where(sq.Eq{"mv.version": ver}).
		Where(sq.Eq{"m.name": mod})
	sql, args, err := q.ToSql()
	log("translate module name/version to ID", "sql", sql, "args", args, "err", err)
	if err != nil {
		return 0, fmt.Errorf("error constructing SQL query: %w", err)
	}

	rows, err := db.QueryContext(ctx, sql, args...)
	if err != nil {
		return 0, err
	}
	defer func() {
		if e := rows.Close(); e != nil {
			log("error closing sql.Rows", "err", e)
		}
	}()
	var id int32
	for rows.Next() {
		if id != 0 {
			return 0, fmt.Errorf("module/version lookup returned >1 row")
		}
		if err := rows.Scan(&id); err != nil {
			return 0, err
		}
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("error processing database query results: %w", err)
	}
	return id, nil
}

// applyNameFilter parses the specified filter string and appends an appropriate WHERE clause to the
// provided sq.SelectBuilder.
//
// The filter string should be a glob pattern ('*' and '?' for wildcards).  If the filter doesn't contain
// any wildcards it is treated as a substring match.
func applyNameFilter(q sq.SelectBuilder, nameFilter string) sq.SelectBuilder {
	if nameFilter == "" {
		return q
	}
	// translate glob ? and * wildcards to SQL equivalents
	hasWildcards := strings.ContainsAny(nameFilter, "*?")
	where := globToLike(nameFilter)
	// treat a filter with no wildards as a "contains" substring match
	if !hasWildcards {
		where = "%" + where + "%"
	}
	return q.Where(sq.Like{"name": where})
}

// database defines a type that can execute SQL commands against a database.
//
// We define this interface so that we can write code that treats sql.DB and sql.Tx interchangeably
type database interface {
	QueryContext(context.Context, string, ...interface{}) (*sql.Rows, error)
	ExecContext(context.Context, string, ...interface{}) (sql.Result, error)
}

// writeModule upserts a module into the database
func writeModule(ctx context.Context, db database, name, description string) (int32, error) {
	var desc interface{}
	if description != "" {
		desc = description
	}
	sql, args, err := psql.
		Insert(tableModules).
		Columns(columnsModules[1:]...). // don't provide ID on an insert
		Values(name, desc).
		Suffix(`ON CONFLICT (name) DO UPDATE SET description = ? RETURNING id`, desc).
		ToSql()
	if err != nil {
		return 0, fmt.Errorf("error constructing database command: %w", err)
	}
	res, err := db.QueryContext(ctx, sql, args...)
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

// writeModuleVersions upserts module versions into the database
func writeModuleVersions(ctx context.Context, db database, moduleID int32, versions ...string) (ids []int32, err error) {
	for i, ver := range versions {
		cmd, args, err := psql.
			Insert(tableModuleVersions).
			Columns("module_id", "version").
			Values(moduleID, strings.TrimPrefix(ver, "v")).
			Suffix("ON CONFLICT ON CONSTRAINT uc_module_version_module_id_version DO UPDATE SET module_id = ? RETURNING id", moduleID).
			ToSql()
		if err != nil {
			return nil, fmt.Errorf("error constructing SQL operation for versions[%d] (%v): %w", i, ver, err)
		}

		res, err := db.QueryContext(ctx, cmd, args...)
		if err != nil {
			return nil, fmt.Errorf("error executing database operation for versions[%d] (%v): %w", i, ver, err)
		}
		defer func() { _ = res.Close() }()

		if !res.Next() {
			return nil, fmt.Errorf("database insert modified 0 rows")
		}
		var versionID int32
		if err = res.Scan(&versionID); err != nil {
			return nil, fmt.Errorf("error processing database command result: %w", err)
		}
		if res.Next() {
			return nil, fmt.Errorf("database insert modified more than 1 row")
		}
		ids = append(ids, versionID)
	}

	return ids, nil
}

// getDependx is a shared query for dependency gathering in either direction,
// dependent on the joinType.
func getDependx(ctx context.Context, db *sqlx.DB, module, version, joinType string, pageToken string, count int, log Logger) ([]Version, string, error) {
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
	log.Debug("getDependx()", "sql", sql, "args", args)
	var dependents []Version
	err = db.SelectContext(ctx, &dependents, sql, args...)
	if err != nil {
		return nil, "", err
	}

	return dependents, encodePageToken(pageTokenKey, len(dependents), offset, count), nil
}

// globToLike converts a string containing a glob pattern to a SQL LIKE clause.
func globToLike(glob string) string {
	var res strings.Builder
	for _, c := range glob {
		switch c {
		case '%', '_':
			// need to escape LIKE pattern tokens
			res.WriteRune('\\')
			res.WriteRune(c)
		case '*':
			res.WriteRune('%')
		case '?':
			res.WriteRune('_')
		default:
			res.WriteRune(c)
		}
	}
	return res.String()
}
