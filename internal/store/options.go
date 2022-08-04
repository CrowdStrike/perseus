package store

// PGOption defines a configuration option to be used when constructing the database connection.
type PGOption func() error
