package server

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/pprof"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	grpc_prometheus "github.com/grpc-ecosystem/go-grpc-prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
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
	if conf.healthzTimeout <= 0 {
		conf.healthzTimeout = 300 * time.Millisecond
	}

	debugLog("starting the server")
	// create the root listener for cmux
	lis, err := net.Listen("tcp", conf.listenAddr)
	if err != nil {
		return fmt.Errorf("could not create TCP listener: %w", err)
	}
	defer func() {
		if err := lis.Close(); err != nil {
			debugLog("unexpected error closing TCP listener", "err", err)
		}
	}()

	// create a muxer and configure gRPC and HTTP routes
	mux := cmux.New(lis)
	grpcLis := mux.MatchWithWriters(cmux.HTTP2MatchHeaderFieldSendSettings("content-type", "application/grpc"))
	defer func() {
		if err := grpcLis.Close(); err != nil {
			debugLog("unexpected error closing gRPC mux listener", "err", err)
		}
	}()
	httpLis := mux.Match(cmux.HTTP1Fast(http.MethodPatch))
	defer func() {
		if err := httpLis.Close(); err != nil {
			debugLog("unexpected error closing HTTP mux listener", "err", err)
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
	debugLog("connected to the database", "connectionString", connStr)

	// spin up gRPC server
	grpcOpts := []grpc.ServerOption{
		grpc.UnaryInterceptor(grpc_prometheus.UnaryServerInterceptor),
	}
	grpcSrv := grpc.NewServer(grpcOpts...)
	apiSrv := newGRPCServer(db)
	perseusapi.RegisterPerseusServiceServer(grpcSrv, apiSrv)
	grpc_prometheus.Register(grpcSrv)

	// spin up HTTP server
	httpSrv := newHTTPServer(ctx, conf.listenAddr, db, conf.healthzTimeout)

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
					debugLog("Got SIGNUP signal, TODO - reload config")
				default:
					debugLog("Got stop signal, shutting down", "signal", sig.String())
					return nil
				}
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	})

	// spin up the cmux
	go func() { _ = mux.Serve() }()
	debugLog("Server listening", "addr", conf.listenAddr)
	defer debugLog("Server exited")
	// wait for shutdown
	if err := eg.Wait(); err != nil && err != context.Canceled {
		return err
	}

	return nil
}

// newHTTPServer initializes and configures a new http.ServerMux that serves various endpoints.
//
// The supported paths are:
//   - /api/v1/* - gRPC Gateway REST mappings for the gRPC endpoints
//   - /ui/ - web UI
//   - /healthz/ - server health checks
//   - /metrics/ - Prometheus server metrics
//   - /debug/pprof/* - pprof runtime profiles
func newHTTPServer(ctx context.Context, grpcAddr string, db store.Store, healthzTimeout time.Duration) http.Server {
	mux := http.NewServeMux()
	mux.Handle("/", handleGrpcGateway(ctx, grpcAddr))
	mux.Handle("/ui/", handleUX())
	mux.Handle("/healthz", handleHealthz(db, healthzTimeout))
	mux.Handle("/metrics", promhttp.Handler())
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	return http.Server{
		Handler:           mux,
		ReadHeaderTimeout: time.Second,
	}
}
