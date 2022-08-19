package main

import (
	"os"

	"github.com/spf13/pflag"
)

// clientConfig defines the runtime options for the "client" CLI commands
type clientConfig struct {
	// the TCP host/port of the Perseus server
	serverAddr string
}

// clientOption defines a functional option that configures a particular "client" CLI runtime option
type clientOption func(*clientConfig) error

// withServerAddress assigns the TCP host/port of the Perseus server
func withServerAddress(addr string) clientOption {
	return func(conf *clientConfig) error {
		conf.serverAddr = addr
		return nil
	}
}

// readClientConfig scans the process environment vars and returns a list of 0 or more config options
func readClientConfigEnv() []clientOption {
	var opts []clientOption

	if addr := os.Getenv("SERVER_ADDR"); addr != "" {
		opts = append(opts, withServerAddress(addr))
	}

	return opts
}

// readClientConfigFlags scans the CLi flags in the provided flag set and returns a list of 0 or more
// config options
func readClientConfigFlags(fset *pflag.FlagSet) []clientOption {
	var opts []clientOption

	if addr, err := fset.GetString("server-addr"); err == nil && addr != "" {
		opts = append(opts, withServerAddress(addr))
	}

	return opts
}
