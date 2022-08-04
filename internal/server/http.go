package server

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"net/http"

	gwruntime "github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/CrowdStrike/perseus/perseusapi"
)

//go:embed web
var webContent embed.FS

// HandleGrpcGateway registers the REST->gRPC gateway handler
func HandleGrpcGateway(ctx context.Context, grpcAddr string) *gwruntime.ServeMux {
	mux := gwruntime.NewServeMux()
	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	}
	if err := perseusapi.RegisterPerseusServiceHandlerFromEndpoint(ctx, mux, grpcAddr, opts); err != nil {
		// panic so we don't clutter the code with handling unrecoverable errors
		panic(fmt.Errorf("unable to register the gRPC gateway handler: %w", err))
	}
	return mux
}

// HandleUX serves the web UI for the service
func HandleUX(ctx context.Context) http.Handler {
	content, err := fs.Sub(webContent, "web")
	if err != nil {
		// panic so we don't clutter the code with handling unrecoverable errors
		panic(fmt.Errorf("unable to resolve 'web/' directory in embedded content: %w", err))
	}
	return http.StripPrefix("/ui/", http.FileServer(http.FS(content)))
}
