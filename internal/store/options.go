package store

// PGOption defines a configuration option to be used when constructing the database connection.
type PGOption func(*PostgresClient) error

// WithLog returns a PGOption that attaches the provided debug logging callback function
func WithLog(fn func(string, ...any)) PGOption {
	return func(c *PostgresClient) error {
		if fn == nil {
			fn = func(string, ...any) { /* write to /dev/null as a fallback */ }
		}
		c.log = fn
		return nil
	}
}
