package httpapi

import (
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/rizquuula/Constellate/internal/hub/domain/audit"
)

// unauthenticatedPaths lists exact paths (or prefixes ending in a slash) that
// bypass the operator session gate. The WebAuthn register/* routes are
// intentionally NOT here — they require an active operator session.
//
// /ws/agent is the headless agent dial-home endpoint: it authenticates with a
// machine-signed bearer assertion validated by the wsagent endpoint itself, not
// with the operator session cookie, so it must bypass this gate (the endpoint
// enforces machine identity before accepting the connection).
var unauthenticatedPaths = []string{
	"/api/enroll",
	"/api/auth/totp",
	"/api/auth/recovery",
	"/api/auth/status",
	"/api/auth/logout",
	"/api/auth/webauthn/login/begin",
	"/api/auth/webauthn/login/finish",
	"/ws/agent",
}

// authMiddleware gates /api/* and /ws/* except the explicit allowlist above.
// When authSvc is nil, all requests pass through (dev/test mode) with a one-time warning.
func authMiddleware(authSvc AuthService, secureCookies bool, sessionTTL time.Duration, log *slog.Logger, next http.Handler) http.Handler {
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
		for _, allowed := range unauthenticatedPaths {
			if path == allowed {
				next.ServeHTTP(w, r)
				return
			}
		}

		// Check session cookie.
		c, err := r.Cookie(sessionCookieName)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized", "no session")
			return
		}
		ok, renewed, err := authSvc.ValidateSession(r.Context(), c.Value)
		if err != nil || !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized", "invalid or expired session")
			return
		}
		if renewed {
			// The session slid, so the browser's cookie Max-Age is now stale.
			// Gating on renewed (rather than re-issuing every request) keeps this
			// to at most one Set-Cookie per renewal skew window.
			setSessionCookie(w, c.Value, secureCookies, sessionTTL)
		}

		ctx := audit.ContextWithActor(r.Context(), "operator")
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
