package main

import (
	"crypto/tls"
	"os"
	"strconv"

	"connectrpc.com/connect"
	"github.com/bufbuild/httplb"
	"github.com/spf13/pflag"

	"github.com/CrowdStrike/perseus/perseusapi/perseusapiconnect"
)

// package variables to hold CLI flag values
var (
	formatAsJSON, formatAsList, formatAsDotGraph bool
	formatTemplate                               string
	maxDepth                                     int
	disableTLS                                   bool
)

// clientConfig defines the runtime options for the "client" CLI commands
type clientConfig struct {
	// the TCP host/port of the Perseus server
	serverAddr string
	// do not use TLS when connecting if true
	disableTLS bool
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

// withInsecureDial disables TLS when connecting to the server
func withInsecureDial() clientOption {
	return func(conf *clientConfig) error {
		conf.disableTLS = true
		return nil
	}
}

// readClientConfig scans the process environment vars and returns a list of 0 or more config options
func readClientConfigEnv() []clientOption {
	var opts []clientOption

	if addr := os.Getenv("PERSEUS_SERVER_ADDR"); addr != "" {
		opts = append(opts, withServerAddress(addr))
	}
	if s := os.Getenv("PERSEUS_SERVER_NO_TLS"); s != "" {
		val, err := strconv.ParseBool(s)
		if val && err != nil {
			opts = append(opts, withInsecureDial())
		}
	}

	return opts
}

// readClientConfigFlags scans the CLI flags in the provided flag set and returns a list of 0 or more
// config options
func readClientConfigFlags(fset *pflag.FlagSet) []clientOption {
	var opts []clientOption

	if addr, err := fset.GetString("server-addr"); err == nil && addr != "" {
		opts = append(opts, withServerAddress(addr))
	}
	if v, err := fset.GetBool("insecure"); err == nil && v {
		opts = append(opts, withInsecureDial())
	}

	return opts
}

func (conf *clientConfig) getClient() (client perseusapiconnect.PerseusServiceClient) {
	opts := []httplb.ClientOption{}
	if !conf.disableTLS {
		tlsc := tls.Config{
			MinVersion: tls.VersionTLS13,
		}
		opts = append(opts, httplb.WithTLSConfig(&tlsc, 0))
	}

	// we include WithGRPC() so that the CLI can hit an existing gRPC-based server instance
	// - this may be removed at some point in the future
	cc := perseusapiconnect.NewPerseusServiceClient(
		httplb.NewClient(opts...),
		conf.serverAddr,
		connect.WithGRPC())
	return cc
}
