package server

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"net/http"
	"time"

	"github.com/CrowdStrike/perseus/internal/store"
)

//go:embed web
var webContent embed.FS

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
func handleHealthz(db store.Store, timeout time.Duration) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), timeout)
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
