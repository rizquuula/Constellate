package httpapi

import (
	"log/slog"
	"net/http"
	"strings"
	"sync"

	"github.com/rizquuula/Constellate/internal/hub/domain/audit"
)

// authMiddleware gates /api/* and /ws/* except /api/auth/* and /api/enroll.
// When authSvc is nil, all requests pass through (dev/test mode) with a one-time warning.
func authMiddleware(authSvc AuthService, secureCookies bool, log *slog.Logger, next http.Handler) http.Handler {
	var warnOnce sync.Once
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if authSvc == nil {
			warnOnce.Do(func() {
				log.Warn("auth middleware: no AuthService configured, all requests pass through (dev mode)")
			})
			next.ServeHTTP(w, r)
			return
		}

		path := r.URL.Path

		// Determine whether this path requires auth.
		needsAuth := strings.HasPrefix(path, "/api/") || strings.HasPrefix(path, "/ws/")
		if !needsAuth {
			next.ServeHTTP(w, r)
			return
		}

		// Allowlisted paths that bypass auth.
		if strings.HasPrefix(path, "/api/auth/") || path == "/api/enroll" {
			next.ServeHTTP(w, r)
			return
		}

		// Check session cookie.
		c, err := r.Cookie(sessionCookieName)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized", "no session")
			return
		}
		ok, err := authSvc.ValidateSession(r.Context(), c.Value)
		if err != nil || !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized", "invalid or expired session")
			return
		}

		ctx := audit.ContextWithActor(r.Context(), "operator")
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
