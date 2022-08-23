package server

import (
	"context"
	"fmt"
	"net"
	"net/http"
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

type LogFunc func(string, ...any)

var debugLog LogFunc = func(string, ...any) { /* no-op by default */ }

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
	return &cmd
}

func runServerCmd(cmd *cobra.Command, _ []string) error {
	var opts []serverOption
	opts = append(opts, readServerConfigEnv()...)
	opts = append(opts, readServerConfigFlags(cmd.Flags())...)

	if err := runServer(opts...); err != nil && err != cmux.ErrListenerClosed {
		return err
	}
	return nil
}

func runServer(opts ...serverOption) error {
	// set a default login/password for local dev in Docker.  prod deployment should always override
	// these.
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
		_ = lis.Close()
	}()

	mux := cmux.New(lis)

	// route gRPC connections
	grpcLis := mux.MatchWithWriters(cmux.HTTP2MatchHeaderFieldSendSettings("content-type", "application/grpc"))
	defer func() {
		_ = grpcLis.Close()
	}()

	// route HTTP connections
	httpLis := mux.Match(cmux.HTTP1Fast(http.MethodPatch))
	defer func() {
		_ = httpLis.Close()
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	connStr := fmt.Sprintf("postgres://%s:%s@%s/perseus", conf.dbUser, conf.dbPwd, conf.dbAddr)
	db, err := store.NewPostgresClient(ctx, connStr)
	if err != nil {
		return fmt.Errorf("could not connect to the database: %w", err)
	}
	debugLog("connected to the database at %s", connStr)

	grpcSrv := grpc.NewServer()
	// TODO: apply interceptors, etc.
	apiSrv := newGRPCServer(db)
	perseusapi.RegisterPerseusServiceServer(grpcSrv, apiSrv)

	httpSrv := newHTTPServer(ctx, conf.listenAddr)

	eg, ctx := errgroup.WithContext(ctx)

	// start gRPC server
	eg.Go(func() error {
		debugLog("serving gRPC")
		defer debugLog("gRPC server closed")
		return grpcSrv.Serve(grpcLis)
	})

	// start HTTP server
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

func newHTTPServer(ctx context.Context, grpcAddr string) http.Server {
	mux := http.NewServeMux()
	mux.Handle("/", handleGrpcGateway(ctx, grpcAddr))
	mux.Handle("/ui/", handleUX())
	return http.Server{
		Handler:           mux,
		ReadHeaderTimeout: time.Second,
	}
}
