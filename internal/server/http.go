package server

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"net/http"
	"time"

	gwruntime "github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/CrowdStrike/perseus/internal/store"
	"github.com/CrowdStrike/perseus/perseusapi"
)

//go:embed web
var webContent embed.FS

// handleGrpcGateway registers the REST->gRPC gateway handler
func handleGrpcGateway(ctx context.Context, grpcAddr string) *gwruntime.ServeMux {
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

// handleUX serves the web UI for the service
func handleUX() http.Handler {
	content, err := fs.Sub(webContent, "web")
	if err != nil {
		// panic so we don't clutter the code with handling unrecoverable errors
		panic(fmt.Errorf("unable to resolve 'web/' directory in embedded content: %w", err))
	}
	return http.StripPrefix("/ui/", http.FileServer(http.FS(content)))
}

// handleHealthz exposes an HTTP health check endpoint that responds with '200 OK' if the service is
// healthy (can connect to the Perseus database) and '500 Internal Server Error' if not
func handleHealthz(db store.Store) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 300*time.Millisecond)
		defer cancel()
		if err := db.Ping(ctx); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintf(w, "a connection to the database is unavailable")
			return
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "Krakens beware!")
	})
}
