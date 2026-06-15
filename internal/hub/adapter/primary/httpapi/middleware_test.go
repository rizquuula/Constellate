package httpapi_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rizquuula/Constellate/internal/hub/adapter/primary/httpapi"
	"github.com/rizquuula/Constellate/internal/hub/adapter/secondary/memory"
	"github.com/rizquuula/Constellate/internal/hub/app/projects"
	appauth "github.com/rizquuula/Constellate/internal/hub/app/auth"
	"github.com/rizquuula/Constellate/internal/hub/app/sessions"
	"github.com/rizquuula/Constellate/internal/hub/app/registry"
	"github.com/rizquuula/Constellate/internal/hub/domain/audit"
	"github.com/rizquuula/Constellate/internal/hub/domain/project"
	"github.com/rizquuula/Constellate/internal/hub/domain/session"
	platlog "github.com/rizquuula/Constellate/internal/platform/log"
)

// fakeTOTPMW always accepts "000000".
type fakeTOTPMW struct{}

func (fakeTOTPMW) Generate(issuer, account string) (string, string, error) {
	return "SECRET", "otpauth://totp/test", nil
}
func (fakeTOTPMW) Verify(secret, code string, now int64) bool {
	return code == "000000"
}
func (fakeTOTPMW) Matches(secret, code string, now int64) (step int64, ok bool) {
	if code == "000000" {
		return now / 30, true
	}
	return 0, false
}

// fakeAuditMW discards records.
type fakeAuditMW struct{}

func (fakeAuditMW) Record(_ context.Context, _ audit.Action, _, _, _ string) error { return nil }

// stubMachineSvcMW satisfies httpapi.MachineService.
type stubMachineSvcMW struct{}

func (s stubMachineSvcMW) List(_ context.Context) ([]registry.MachineView, error) {
	return nil, nil
}

// stubSessionSvcMW satisfies httpapi.SessionService.
type stubSessionSvcMW struct{}

func (s stubSessionSvcMW) Open(_ context.Context, _ sessions.OpenInput) (session.Session, error) {
	return session.Session{}, nil
}
func (s stubSessionSvcMW) List(_ context.Context) ([]session.Session, error) {
	return nil, nil
}
func (s stubSessionSvcMW) ListByMachine(_ context.Context, _ string) ([]session.Session, error) {
	return nil, nil
}
func (s stubSessionSvcMW) Close(_ context.Context, _ string) error      { return nil }
func (s stubSessionSvcMW) Delete(_ context.Context, _ string) error     { return nil }
func (s stubSessionSvcMW) Rename(_ context.Context, _, _ string) error  { return nil }

// stubProjectSvcMW satisfies httpapi.ProjectService.
type stubProjectSvcMW struct{}

func (s stubProjectSvcMW) Create(_ context.Context, _ projects.CreateInput) (project.Project, error) {
	return project.Project{}, nil
}
func (s stubProjectSvcMW) List(_ context.Context) ([]project.Project, error) { return nil, nil }

func buildMiddlewareTestServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	logger := platlog.New("error", "text")

	ops := memory.NewOperatorStore()
	sess := memory.NewOperatorSessionStore()
	uc := appauth.New(ops, sess, fakeTOTPMW{}, fakeAuditMW{}, appauth.SystemClock{}, func() string { return "tok" }, 24*time.Hour, nil, nil, nil)
	_, _, _, err := uc.BootstrapTOTP(context.Background(), "Constellate", "operator")
	if err != nil {
		t.Fatalf("BootstrapTOTP: %v", err)
	}

	// Log in to get a valid session ID.
	sid, err := uc.LoginTOTP(context.Background(), "000000")
	if err != nil {
		t.Fatalf("LoginTOTP: %v", err)
	}

	srv := httpapi.NewServer(
		"127.0.0.1:0",
		stubMachineSvcMW{},
		stubSessionSvcMW{},
		stubProjectSvcMW{},
		nil,
		nil, nil, nil,
		uc,
		nil,
		false,
		logger,
	)
	ts := httptest.NewServer(srv.Handler())
	return ts, sid
}

func TestMiddleware_NoCookie_Returns401(t *testing.T) {
	ts, _ := buildMiddlewareTestServer(t)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/machines")
	if err != nil {
		t.Fatalf("GET /api/machines: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", resp.StatusCode)
	}
}

func TestMiddleware_NoCookie_WS_Returns401(t *testing.T) {
	ts, _ := buildMiddlewareTestServer(t)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/ws/term")
	if err != nil {
		t.Fatalf("GET /ws/term: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", resp.StatusCode)
	}
}

func TestMiddleware_NoCookie_WSAgent_BypassesGate(t *testing.T) {
	ts, _ := buildMiddlewareTestServer(t)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/ws/agent")
	if err != nil {
		t.Fatalf("GET /ws/agent: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	// /ws/agent authenticates via the machine-signed bearer assertion handled by
	// the wsagent endpoint, not the operator session cookie, so it must bypass
	// the operator gate. No agent handler is wired in this test, so the request
	// falls through to the SPA handler — the point is only that the middleware
	// does not 401 it before the endpoint can run its own machine auth.
	if resp.StatusCode == http.StatusUnauthorized {
		t.Errorf("expected /ws/agent to bypass operator auth gate, got 401")
	}
}

func TestMiddleware_ValidCookie_Passes(t *testing.T) {
	ts, sid := buildMiddlewareTestServer(t)
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/machines", nil)
	req.AddCookie(&http.Cookie{Name: "constellate_session", Value: sid})

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /api/machines: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusUnauthorized {
		t.Errorf("expected non-401 with valid session, got %d", resp.StatusCode)
	}
}

func TestMiddleware_AuthStatus_AllowedWithoutCookie(t *testing.T) {
	ts, _ := buildMiddlewareTestServer(t)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/auth/status")
	if err != nil {
		t.Fatalf("GET /api/auth/status: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestMiddleware_Enroll_AllowedWithoutCookie(t *testing.T) {
	ts, _ := buildMiddlewareTestServer(t)
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/api/enroll", "application/json", nil)
	if err != nil {
		t.Fatalf("POST /api/enroll: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	// Enroll handler exists but we have no token — may 400/401 from enroll logic
	// but NOT 401 from auth middleware.
	if resp.StatusCode == http.StatusUnauthorized {
		t.Errorf("expected enroll to bypass auth gate, got 401")
	}
}

func TestMiddleware_WebAuthnLoginBegin_AllowedWithoutCookie(t *testing.T) {
	ts, _ := buildMiddlewareTestServer(t)
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/api/auth/webauthn/login/begin", "application/json", nil)
	if err != nil {
		t.Fatalf("POST /api/auth/webauthn/login/begin: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	// Handler will return 501 (WebAuthn unavailable) or another handler error — not 401 from auth middleware.
	if resp.StatusCode == http.StatusUnauthorized {
		t.Errorf("expected webauthn/login/begin to bypass auth gate, got 401")
	}
}

func TestMiddleware_WebAuthnRegisterBegin_RequiresCookie(t *testing.T) {
	ts, _ := buildMiddlewareTestServer(t)
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/api/auth/webauthn/register/begin", "application/json", nil)
	if err != nil {
		t.Fatalf("POST /api/auth/webauthn/register/begin: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected register/begin to require session, got %d", resp.StatusCode)
	}
}
