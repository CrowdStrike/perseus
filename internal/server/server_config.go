package server

import (
	"os"

	"github.com/spf13/pflag"
)

const defaultDbName = "perseus"

type serverConfig struct {
	listenAddr string

	dbAddr, dbUser, dbPwd, dbName string
}

type serverOption func(*serverConfig) error

func withListenAddress(addr string) serverOption {
	return func(o *serverConfig) error {
		o.listenAddr = addr
		return nil
	}
}

func withDBAddress(addr string) serverOption {
	return func(conf *serverConfig) error {
		conf.dbAddr = addr
		return nil
	}
}

func withDBUser(user string) serverOption {
	return func(conf *serverConfig) error {
		conf.dbUser = user
		return nil
	}
}

func withDBPass(pass string) serverOption {
	return func(conf *serverConfig) error {
		conf.dbPwd = pass
		return nil
	}
}

func withDBName(db string) serverOption {
	return func(conf *serverConfig) error {
		if db == "" {
			db = defaultDbName
		}
		conf.dbName = db
		return nil
	}
}

func readServerConfigEnv() []serverOption {
	var opts []serverOption

	if addr := os.Getenv("LISTEN_ADDR"); addr != "" {
		opts = append(opts, withListenAddress(addr))
	}

	if addr := os.Getenv("DB_ADDR"); addr != "" {
		opts = append(opts, withDBAddress(addr))
	}
	if user := os.Getenv("DB_USER"); user != "" {
		opts = append(opts, withDBUser(user))
	}
	if pwd := os.Getenv("DB_PASS"); pwd != "" {
		opts = append(opts, withDBPass(pwd))
	}
	if db := os.Getenv("DB_NAME"); db != "" {
		opts = append(opts, withDBName(db))
	}

	return opts
}

func readServerConfigFlags(fset *pflag.FlagSet) []serverOption {
	var opts []serverOption

	// TODO
	if addr, err := fset.GetString("listen-addr"); err == nil && addr != "" {
		opts = append(opts, withListenAddress(addr))
	}

	if addr, err := fset.GetString("db-addr"); err == nil && addr != "" {
		opts = append(opts, withDBAddress(addr))
	}
	if user, err := fset.GetString("db-user"); err == nil && user != "" {
		opts = append(opts, withDBUser(user))
	}
	if pwd, err := fset.GetString("db-pass"); err == nil && pwd != "" {
		opts = append(opts, withDBPass(pwd))
	}
	if db, err := fset.GetString("db-name"); err != nil && db != "" {
		opts = append(opts, withDBName(db))
	}

	return opts
}
