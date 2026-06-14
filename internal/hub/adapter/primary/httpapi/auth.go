package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
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
	BeginPasskeyRegistration(ctx context.Context) (optionsJSON []byte, challengeKey string, err error)
	FinishPasskeyRegistration(ctx context.Context, challengeKey string, body io.Reader) error
	BeginPasskeyLogin(ctx context.Context) (optionsJSON []byte, challengeKey string, err error)
	FinishPasskeyLogin(ctx context.Context, challengeKey string, body io.Reader) (sessionID string, err error)
}

const sessionCookieName = "constellate_session"
const waChallengesCookieName = "constellate_wa_challenge"

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
	if denied := s.checkLoginRateLimit(w, r); denied {
		return
	}
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
	if denied := s.checkLoginRateLimit(w, r); denied {
		return
	}
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

// checkLoginRateLimit checks both the per-IP and global rate limiters.
// It writes a 429 response and returns true if either limiter denies the
// request. The caller should return immediately when denied is true.
func (s *Server) checkLoginRateLimit(w http.ResponseWriter, r *http.Request) (denied bool) {
	now := time.Now()

	if ok, ra := s.loginGlobalLimiter.allow("operator-login", now); !ok {
		w.Header().Set("Retry-After", fmt.Sprintf("%d", int(ra.Seconds())+1))
		writeError(w, http.StatusTooManyRequests, "rate_limited", "too many attempts, slow down")
		return true
	}

	if ok, ra := s.loginIPLimiter.allow(clientIP(r), now); !ok {
		w.Header().Set("Retry-After", fmt.Sprintf("%d", int(ra.Seconds())+1))
		writeError(w, http.StatusTooManyRequests, "rate_limited", "too many attempts, slow down")
		return true
	}

	return false
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	c, err := r.Cookie(sessionCookieName)
	if err == nil {
		_ = s.auth.Logout(r.Context(), c.Value)
	}
	clearSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

func setWaChallengeCookie(w http.ResponseWriter, key string, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     waChallengesCookieName,
		Value:    key,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   secure,
		MaxAge:   300,
	})
}

func clearWaChallengeCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     waChallengesCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

func (s *Server) handleWebAuthnLoginBegin(w http.ResponseWriter, r *http.Request) {
	options, key, err := s.auth.BeginPasskeyLogin(r.Context())
	if err != nil {
		writeError(w, statusFor(err), "webauthn_error", err.Error())
		return
	}
	setWaChallengeCookie(w, key, s.secureCookies)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(options)
}

func (s *Server) handleWebAuthnLoginFinish(w http.ResponseWriter, r *http.Request) {
	c, err := r.Cookie(waChallengesCookieName)
	if err != nil {
		writeError(w, http.StatusBadRequest, "missing_challenge", "no challenge cookie")
		return
	}
	sid, err := s.auth.FinishPasskeyLogin(r.Context(), c.Value, r.Body)
	if err != nil {
		writeError(w, statusFor(err), "webauthn_error", err.Error())
		return
	}
	setSessionCookie(w, sid, s.secureCookies)
	clearWaChallengeCookie(w)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleWebAuthnRegisterBegin(w http.ResponseWriter, r *http.Request) {
	options, key, err := s.auth.BeginPasskeyRegistration(r.Context())
	if err != nil {
		writeError(w, statusFor(err), "webauthn_error", err.Error())
		return
	}
	setWaChallengeCookie(w, key, s.secureCookies)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(options)
}

func (s *Server) handleWebAuthnRegisterFinish(w http.ResponseWriter, r *http.Request) {
	c, err := r.Cookie(waChallengesCookieName)
	if err != nil {
		writeError(w, http.StatusBadRequest, "missing_challenge", "no challenge cookie")
		return
	}
	if err := s.auth.FinishPasskeyRegistration(r.Context(), c.Value, r.Body); err != nil {
		writeError(w, statusFor(err), "webauthn_error", err.Error())
		return
	}
	clearWaChallengeCookie(w)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
