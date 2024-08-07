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

	"connectrpc.com/connect"
	"connectrpc.com/otelconnect"
	"connectrpc.com/vanguard"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/spf13/cobra"
	"go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/sdk/metric"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
	"golang.org/x/sync/errgroup"

	"github.com/CrowdStrike/perseus/internal/store"
	"github.com/CrowdStrike/perseus/perseusapi/perseusapiconnect"
)

// Logger defines the required behavior for the service's logger.  This type is defined here so that the server
// implementation is not tied to any specified logging library.
type Logger interface {
	// Info generates a log entry at INFO level with the specified message and key/value attributes
	Info(msg string, kvs ...any)
	// Debug generates a log entry at DEBUG level with the specified message and key/value attributes
	Debug(msg string, kvs ...any)
	// Error generates a log entry at ERROR level with the specified error, message, and key/value attributes
	Error(err error, msg string, kvs ...any)
}

// nopLogger is a [Logger] that does nothing.  This is used as a fallback/default if [CreateServerCommand]
// is passed a nil.
type nopLogger struct{}

func (nopLogger) Info(string, ...any) { /* no-op */ }

func (nopLogger) Debug(string, ...any) { /* no-op */ }

func (nopLogger) Error(error, string, ...any) { /* no-op */ }

// log is the logging implementation for the server.  The default is a no-op logger, potentially
// overridden by [CreateServerCommand]
var log Logger = nopLogger{}

// CreateServerCommand initializes and returns a *cobra.Command that implements the 'server' CLI sub-command
func CreateServerCommand(logger Logger) *cobra.Command {
	if logger != nil {
		log = logger
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

	if err := runServer(opts...); err != nil {
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

	log.Debug("starting the server")
	// create the root listener
	lis, err := net.Listen("tcp", conf.listenAddr)
	if err != nil {
		return fmt.Errorf("could not create TCP listener: %w", err)
	}
	defer func() {
		if err := lis.Close(); err != nil {
			log.Error(err, "unexpected error closing TCP listener")
		}
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// connect to the database
	connStr := fmt.Sprintf("postgres://%s:%s@%s/%s", url.PathEscape(conf.dbUser), url.PathEscape(conf.dbPwd), url.PathEscape(conf.dbAddr), url.PathEscape(conf.dbName))
	db, err := store.NewPostgresClient(ctx, connStr, store.WithLog(log))
	if err != nil {
		return fmt.Errorf("could not connect to the database %q at %q: %w", conf.dbName, conf.dbAddr, err)
	}
	log.Debug("connected to the database", "addr", conf.dbAddr, "database", conf.dbName, "user", conf.dbUser)

	// spin up the Connect server
	svr := &connectServer{
		store: db,
	}
	exporter, err := prometheus.New()
	if err != nil {
		return fmt.Errorf("unable to initialize Prometheus metrics exporter: %w", err)
	}
	metricsInterceptor, err := otelconnect.NewInterceptor(
		otelconnect.WithMeterProvider(
			metric.NewMeterProvider(metric.WithReader(exporter)),
		),
		otelconnect.WithTrustRemote(),
		otelconnect.WithoutServerPeerAttributes(),
	)
	if err != nil {
		return fmt.Errorf("unable to initialize metrics interceptor: %w", err)
	}
	path, ch := perseusapiconnect.NewPerseusServiceHandler(
		svr,
		connect.WithInterceptors(metricsInterceptor),
	)
	// spin up the Vanguard server and transcoder for JSON/REST mappings
	vs := vanguard.NewService(path, ch)
	vt, err := vanguard.NewTranscoder([]*vanguard.Service{vs})
	if err != nil {
		return fmt.Errorf("unable to initialize Vanguard transcoder: %w", err)
	}

	// spin up HTTP server
	// The supported paths are:
	//   - /api/v1/* - Vanguard REST mappings for the Connect endpoints
	//   - /ui/ - web UI
	//   - /healthz/ - server health checks
	//   - /metrics/ - Prometheus server metrics
	//   - /debug/pprof/* - pprof runtime profiles
	mux := http.NewServeMux()
	mux.Handle("/", vt)
	mux.Handle("/ui/", handleUX())
	mux.Handle("/healthz", handleHealthz(db, conf.healthzTimeout, log))
	mux.Handle("/metrics", promhttp.Handler())
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	httpSrv := http.Server{
		Handler:           h2c.NewHandler(mux, &http2.Server{}),
		ReadHeaderTimeout: time.Second,
	}

	// start services
	// . use x/sync/errgroup so we can stop everything at once via the context
	eg, ctx := errgroup.WithContext(ctx)
	eg.Go(func() error {
		log.Debug("serving HTTP/REST")
		defer log.Debug("HTTP/REST server closed")
		return httpSrv.Serve(lis)
	})

	// handle shutdown
	eg.Go(func() (err error) {
		defer func() {
			cancel()
			err = httpSrv.Shutdown(ctx)
		}()

		sigs := make(chan os.Signal, 1)
		signal.Notify(sigs, syscall.SIGINT, syscall.SIGQUIT, syscall.SIGTERM, syscall.SIGHUP)
		for {
			select {
			case sig := <-sigs:
				switch sig {
				case syscall.SIGHUP:
					log.Debug("Got SIGHUP signal, TODO - reload config")
				default:
					log.Debug("Got stop signal, shutting down", "signal", sig.String())
					return nil
				}
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	})

	log.Info("Server listening", "addr", conf.listenAddr)
	defer log.Info("Server exited")
	// wait for shutdown
	if err := eg.Wait(); err != nil && err != context.Canceled {
		return err
	}

	return nil
}
