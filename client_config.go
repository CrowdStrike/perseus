package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/spf13/pflag"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	"github.com/CrowdStrike/perseus/perseusapi"
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

func (conf *clientConfig) dialServer() (client perseusapi.PerseusServiceClient, err error) {
	// translate RPC errors to human-friendly ones on return
	defer func() {
		switch err {
		case context.DeadlineExceeded:
			err = fmt.Errorf("timed out trying to connect to the Perseus server")
		default:
			if err != nil {
				switch status.Code(err) {
				case codes.Unavailable:
					err = fmt.Errorf("unable to connect to the Perseus server")
				default:
				}
			}
		}
	}()

	// setup gRPC connection options and connect
	dialOpts := []grpc.DialOption{
		grpc.WithBlock(),
	}
	if conf.disableTLS {
		dialOpts = append(dialOpts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	} else {
		dialOpts = append(dialOpts, grpc.WithTransportCredentials(credentials.NewTLS(nil)))
	}
	logger.Debug("connecting to Perseus server", "addr", conf.serverAddr, "useTLS", !conf.disableTLS)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, err := grpc.DialContext(ctx, conf.serverAddr, dialOpts...)
	if err != nil {
		return nil, err
	}

	// create and return the client
	return perseusapi.NewPerseusServiceClient(conn), nil
}
