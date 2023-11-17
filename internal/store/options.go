package store

// PGOption defines a configuration option to be used when constructing the database connection.
type PGOption func(*PostgresClient) error

type Logger interface {
	Debug(string, ...any)
	Error(error, string, ...any)
}

type nopLogger struct{}

func (nopLogger) Debug(string, ...any)        { /*no-op*/ }
func (nopLogger) Error(error, string, ...any) { /*no-op*/ }

// WithLog returns a PGOption that attaches the provided debug logging callback function
func WithLog(l Logger) PGOption {
	return func(c *PostgresClient) error {
		if l == nil {
			l = nopLogger{}
		}
		c.log = l
		return nil
	}
}
