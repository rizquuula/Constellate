package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"
)

// AuthService is the consumer-side port for operator authentication.
// *auth.UseCase satisfies this interface.
type AuthService interface {
	HasOperator(ctx context.Context) (bool, error)
	LoginTOTP(ctx context.Context, code string) (sessionID string, err error)
	LoginRecovery(ctx context.Context, code string) (sessionID string, err error)
	ValidateSession(ctx context.Context, sessionID string) (bool, error)
	Logout(ctx context.Context, sessionID string) error
}

const sessionCookieName = "constellate_session"

func setSessionCookie(w http.ResponseWriter, sessionID string, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    sessionID,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   secure,
		MaxAge:   int((24 * time.Hour).Seconds()),
	})
}

func clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

func (s *Server) handleAuthStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	hasOp, err := s.auth.HasOperator(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	authenticated := false
	if c, err := r.Cookie(sessionCookieName); err == nil {
		ok, verr := s.auth.ValidateSession(ctx, c.Value)
		if verr == nil && ok {
			authenticated = true
		}
	}
	writeJSON(w, http.StatusOK, map[string]bool{
		"hasOperator":   hasOp,
		"authenticated": authenticated,
	})
}

func (s *Server) handleLoginTOTP(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON")
		return
	}
	sid, err := s.auth.LoginTOTP(r.Context(), req.Code)
	if err != nil {
		writeError(w, statusFor(err), "unauthorized", err.Error())
		return
	}
	setSessionCookie(w, sid, s.secureCookies)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleLoginRecovery(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON")
		return
	}
	sid, err := s.auth.LoginRecovery(r.Context(), req.Code)
	if err != nil {
		writeError(w, statusFor(err), "unauthorized", err.Error())
		return
	}
	setSessionCookie(w, sid, s.secureCookies)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	c, err := r.Cookie(sessionCookieName)
	if err == nil {
		_ = s.auth.Logout(r.Context(), c.Value)
	}
	clearSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

// authMiddlewareLogger is used for one-time nil-auth warning.
func authMiddlewareLogger(log *slog.Logger) func() {
	var warned bool
	return func() {
		if !warned {
			warned = true
			log.Warn("auth middleware: no AuthService configured, all requests pass through (dev mode)")
		}
	}
}
