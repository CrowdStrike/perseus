package server

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/soheilhy/cmux"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"

	"github.com/CrowdStrike/perseus/internal/store"
	"github.com/CrowdStrike/perseus/perseusapi"
)

// LogFunc defines a callback function for logging.  This type is defined here so that the server
// implementation is not tied to any specified logging library
type LogFunc func(string, ...any)

// debugLog is the logging function for the server.
var debugLog LogFunc = func(string, ...any) { /* no-op by default */ }

// CreateServerCommand initializes and returns a *cobra.Command that implements the 'server' CLI sub-command
func CreateServerCommand(logFn LogFunc) *cobra.Command {
	if logFn != nil {
		debugLog = logFn
	}

	cmd := cobra.Command{
		Use:          "server",
		Short:        "Starts the Perseus server",
		RunE:         runServerCmd,
		SilenceUsage: true,
	}
	fset := cmd.Flags()
	fset.String("listen-addr", ":31138", "the TCP address to listen on")
	fset.String("db-addr", "", "the TCP host and port of the Perseus DB")
	fset.String("db-user", "", "the login to be used when connecting to the Perseus DB")
	fset.String("db-pass", "", "the password to be used when connecting to the Perseus DB")
	fset.String("db-name", defaultDbName, "the name of the Perseus DB to connect to")
	return &cmd
}

// runServerCmd implements the logic for the 'server' CLI sub-command
func runServerCmd(cmd *cobra.Command, _ []string) error {
	var opts []serverOption
	opts = append(opts, readServerConfigEnv()...)
	opts = append(opts, readServerConfigFlags(cmd.Flags())...)

	if err := runServer(opts...); err != nil && err != cmux.ErrListenerClosed {
		return err
	}
	return nil
}

// runServer starts the server with the specified runtime options.
func runServer(opts ...serverOption) error {
	// apply and validate runtime options
	var conf serverConfig
	for _, fn := range opts {
		if err := fn(&conf); err != nil {
			return fmt.Errorf("could not apply service config option: %w", err)
		}
	}
	if conf.dbAddr == "" || conf.dbUser == "" || conf.dbPwd == "" {
		return fmt.Errorf("the host, user name, and password for the Perseus database must be specified")
	}

	debugLog("starting the server")
	// create the root listener for cmux
	lis, err := net.Listen("tcp", conf.listenAddr)
	if err != nil {
		return fmt.Errorf("could not create TCP listener: %w", err)
	}
	defer func() {
		if err := lis.Close(); err != nil {
			debugLog("unexpected error closing TCP listener: %v", err)
		}
	}()

	// create a muxer and configure gRPC and HTTP routes
	mux := cmux.New(lis)
	grpcLis := mux.MatchWithWriters(cmux.HTTP2MatchHeaderFieldSendSettings("content-type", "application/grpc"))
	defer func() {
		if err := grpcLis.Close(); err != nil {
			debugLog("unexpected error closing gRPC mux listener: %v", err)
		}
	}()
	httpLis := mux.Match(cmux.HTTP1Fast(http.MethodPatch))
	defer func() {
		if err := httpLis.Close(); err != nil {
			debugLog("unexpected error closing HTTP mux listener: %v", err)
		}
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// connect to the database
	connStr := fmt.Sprintf("postgres://%s:%s@%s/%s", url.PathEscape(conf.dbUser), url.PathEscape(conf.dbPwd), url.PathEscape(conf.dbAddr), url.PathEscape(conf.dbName))
	db, err := store.NewPostgresClient(ctx, connStr, store.WithLog(debugLog))
	if err != nil {
		return fmt.Errorf("could not connect to the database: %w", err)
	}
	debugLog("connected to the database at %s", connStr)

	// spin up gRPC and HTTP servers
	grpcSrv := grpc.NewServer()
	// TODO: apply interceptors, etc.
	apiSrv := newGRPCServer(db)
	perseusapi.RegisterPerseusServiceServer(grpcSrv, apiSrv)
	httpSrv := newHTTPServer(ctx, conf.listenAddr)

	// start services
	// . use x/sync/errgroup so we can stop everything at once via the context
	eg, ctx := errgroup.WithContext(ctx)
	eg.Go(func() error {
		debugLog("serving gRPC")
		defer debugLog("gRPC server closed")
		return grpcSrv.Serve(grpcLis)
	})
	eg.Go(func() error {
		debugLog("serving HTTP/REST")
		defer debugLog("HTTP/REST server closed")
		return httpSrv.Serve(httpLis)
	})

	// handle shutdown
	eg.Go(func() (err error) {
		defer func() {
			cancel()
			grpcSrv.GracefulStop()
			err = httpSrv.Shutdown(ctx)
		}()

		sigs := make(chan os.Signal, 1)
		signal.Notify(sigs, syscall.SIGINT, syscall.SIGQUIT, syscall.SIGTERM, syscall.SIGHUP)
		for {
			select {
			case sig := <-sigs:
				switch sig {
				case syscall.SIGHUP:
					debugLog("Got SIGNUP signal, TODO - reload config\n")
				default:
					debugLog("Got [%s] signal, shutting down\n", sig)
					return nil
				}
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	})

	// spin up the cmux
	go func() { _ = mux.Serve() }()
	debugLog("Server listening at %s", conf.listenAddr)
	defer debugLog("Server exited")
	// wait for shutdown
	if err := eg.Wait(); err != nil && err != context.Canceled {
		return err
	}

	return nil
}

// newHTTPServer initializes and configures a new http.ServerMux that serves the gRPC Gateway REST
// API and the UI
func newHTTPServer(ctx context.Context, grpcAddr string) http.Server {
	mux := http.NewServeMux()
	mux.Handle("/", handleGrpcGateway(ctx, grpcAddr))
	mux.Handle("/ui/", handleUX())
	return http.Server{
		Handler:           mux,
		ReadHeaderTimeout: time.Second,
	}
}
