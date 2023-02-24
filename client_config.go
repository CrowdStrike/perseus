package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/pflag"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	"github.com/CrowdStrike/perseus/perseusapi"
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

	if addr := os.Getenv("PERSEUS_SERVER_ADDR"); addr != "" {
		opts = append(opts, withServerAddress(addr))
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
		grpc.WithTransportCredentials(insecure.NewCredentials()), // TODO: support TLS
	}
	debugLog("connecting to Perseus server", "addr", conf.serverAddr)
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	conn, err := grpc.DialContext(ctx, conf.serverAddr, dialOpts...)
	if err != nil {
		return nil, err
	}

	// create and return the client
	return perseusapi.NewPerseusServiceClient(conn), nil
}
