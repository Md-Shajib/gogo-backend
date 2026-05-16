package middleware

import (
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/md-shajib/gogo-backend/pkg/apperr"
	"github.com/md-shajib/gogo-backend/pkg/response"
)

// Uses runtime/debug.Stack() — captures the full goroutine stack trace, not just the panic value. Essential for debugging production panics.
// Recover catches any panic in downstream handlers, logs it with a stack trace,
// and responds with a 500 — server stays alive.
func Recover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				slog.Error("panic recovered",
					"error", rec,
					"stack", string(debug.Stack()),
				)
				response.Error(w, apperr.ErrInternal)
			}
		}()

		next.ServeHTTP(w, r)
	})
}
