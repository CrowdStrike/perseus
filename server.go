package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/soheilhy/cmux"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"

	"github.com/CrowdStrike/perseus/internal/server"
	"github.com/CrowdStrike/perseus/internal/store"
	"github.com/CrowdStrike/perseus/perseusapi"
)

// TODO: define and implement the 'perseus server ...' CLI command

func createServerCommand() *cobra.Command {
	cmd := cobra.Command{
		Use:          "server",
		Short:        "Starts the Perseus server",
		RunE:         runServerCmd,
		SilenceUsage: true,
	}
	// fset := cmd.Flags()
	// fset
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

type serverConfig struct {
	listenAddr string
}

type serverOption func(*serverConfig) error

func withAddress(addr string) serverOption {
	return func(o *serverConfig) error {
		o.listenAddr = addr
		return nil
	}
}

func readServerConfigEnv() []serverOption {
	var opts []serverOption

	// TODO

	return opts
}

func readServerConfigFlags(fset *pflag.FlagSet) []serverOption {
	var opts []serverOption

	// TODO

	return opts
}

func runServer(opts ...serverOption) error {
	conf := serverConfig{
		listenAddr: "localhost:31138",
	}
	for _, fn := range opts {
		if err := fn(&conf); err != nil {
			return fmt.Errorf("could not apply service config option: %w", err)
		}
	}

	log.Println("starting the server")
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

	connStr := "postgres://postgres:postgres@localhost:5432/perseus"
	db, err := store.NewPostgresClient(ctx, connStr)
	if err != nil {
		return fmt.Errorf("could not connect to the database: %w", err)
	}
	log.Println("connected to the database at", connStr)
	grpcSrv := newGRPCServer()
	apiSrv := server.NewGRPCServer(db)

	perseusapi.RegisterPerseusServiceServer(grpcSrv, apiSrv)

	httpSrv := newHTTPServer(ctx, conf.listenAddr)

	eg, ctx := errgroup.WithContext(ctx)

	// start gRPC server
	eg.Go(func() error {
		log.Println("serving gRPC")
		defer log.Println("gRPC server closed")
		return grpcSrv.Serve(grpcLis)
	})

	// start HTTP server
	eg.Go(func() error {
		log.Println("serving HTTP/REST")
		defer log.Println("HTTP/REST server closed")
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
					log.Printf("Got SIGNUP signal, TODO - reload config\n")
				default:
					log.Printf("Got [%s] signal, shutting down\n", sig)
					return nil
				}
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	})

	// spin up the cmux
	go func() { _ = mux.Serve() }()
	log.Println("Server listening at", conf.listenAddr)
	defer log.Println("Server exited")
	// wait for shutdown
	if err := eg.Wait(); err != nil && err != context.Canceled {
		return err
	}

	return nil
}

func newGRPCServer() *grpc.Server {
	s := grpc.NewServer()
	// TODO: apply interceptors, etc.
	return s
}

func newHTTPServer(ctx context.Context, grpcAddr string) http.Server {
	mux := http.NewServeMux()
	mux.Handle("/", server.HandleGrpcGateway(ctx, grpcAddr))
	mux.Handle("/ui/", server.HandleUX(ctx))
	return http.Server{
		Handler:           mux,
		ReadHeaderTimeout: time.Second,
	}
}
