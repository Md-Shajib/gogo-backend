package router

import (
	"net/http"

	"github.com/md-shajib/gogo-backend/pkg/middleware"
	"github.com/md-shajib/gogo-backend/pkg/response"
)

// New returns the fully wired HTTP handler with all middleware applied.
// All module routes are registered here as phases are completed.
func New() http.Handler {
	mux := http.NewServeMux()

	// health check
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		response.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	// middleware chain — outermost first
	var handler http.Handler = mux
	handler = middleware.CORS(handler)
	handler = middleware.Logger(handler)
	handler = middleware.Recover(handler)

	return handler
}
