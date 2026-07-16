package httpapi_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rizquuula/Constellate/internal/hub/adapter/primary/httpapi"
	"github.com/rizquuula/Constellate/internal/hub/adapter/secondary/memory"
	appauth "github.com/rizquuula/Constellate/internal/hub/app/auth"
	"github.com/rizquuula/Constellate/internal/hub/app/enroll"
	"github.com/rizquuula/Constellate/internal/hub/domain/machine"
	platlog "github.com/rizquuula/Constellate/internal/platform/log"
)

// fakeEnrollSvc satisfies httpapi.EnrollService with per-method canned errors.
type fakeEnrollSvc struct {
	revokeErr   error
	unrevokeErr error
	deleteErr   error
}

func (f fakeEnrollSvc) Enroll(_ context.Context, _ enroll.EnrollInput) (string, error) {
	return "m1", nil
}
func (f fakeEnrollSvc) Revoke(_ context.Context, _ string) error   { return f.revokeErr }
func (f fakeEnrollSvc) Unrevoke(_ context.Context, _ string) error { return f.unrevokeErr }
func (f fakeEnrollSvc) Delete(_ context.Context, _ string) error   { return f.deleteErr }

// buildMachinesTestServer wires a server with the given EnrollService behind the
// real auth middleware and returns a live test server plus a valid session id.
func buildMachinesTestServer(t *testing.T, enrollSvc httpapi.EnrollService) (*httptest.Server, string) {
	t.Helper()
	logger := platlog.New("error", "text")

	ops := memory.NewOperatorStore()
	sess := memory.NewOperatorSessionStore()
	uc := appauth.New(ops, sess, fakeTOTPMW{}, fakeAuditMW{}, appauth.SystemClock{}, func() string { return "tok" }, 24*time.Hour, nil, nil, nil)
	if _, _, _, err := uc.BootstrapTOTP(context.Background(), "Constellate", "operator"); err != nil {
		t.Fatalf("BootstrapTOTP: %v", err)
	}
	sid, err := uc.LoginTOTP(context.Background(), "000000")
	if err != nil {
		t.Fatalf("LoginTOTP: %v", err)
	}

	srv := httpapi.NewServer(
		"127.0.0.1:0",
		stubMachineSvcMW{},
		stubSessionSvcMW{},
		stubProjectSvcMW{},
		enrollSvc,
		nil, nil, nil,
		uc,
		nil,
		false,
		logger,
	)
	ts := httptest.NewServer(srv.Handler())
	return ts, sid
}

func doAuthed(t *testing.T, ts *httptest.Server, sid, method, path string) int {
	t.Helper()
	req, err := http.NewRequest(method, ts.URL+path, nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.AddCookie(&http.Cookie{Name: "constellate_session", Value: sid})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	return resp.StatusCode
}

func TestHandleRevokeMachine_NoContent(t *testing.T) {
	ts, sid := buildMachinesTestServer(t, fakeEnrollSvc{})
	defer ts.Close()

	if got := doAuthed(t, ts, sid, http.MethodPost, "/api/machines/m1/revoke"); got != http.StatusNoContent {
		t.Errorf("revoke: got %d, want 204", got)
	}
}

func TestHandleUnrevokeMachine_NoContent(t *testing.T) {
	ts, sid := buildMachinesTestServer(t, fakeEnrollSvc{})
	defer ts.Close()

	if got := doAuthed(t, ts, sid, http.MethodPost, "/api/machines/m1/unrevoke"); got != http.StatusNoContent {
		t.Errorf("unrevoke: got %d, want 204", got)
	}
}

func TestHandleDeleteMachine_NotRevoked_Conflict(t *testing.T) {
	ts, sid := buildMachinesTestServer(t, fakeEnrollSvc{deleteErr: enroll.ErrNotRevoked})
	defer ts.Close()

	if got := doAuthed(t, ts, sid, http.MethodDelete, "/api/machines/m1"); got != http.StatusConflict {
		t.Errorf("delete non-revoked: got %d, want 409", got)
	}
}

func TestHandleDeleteMachine_Revoked_NoContent(t *testing.T) {
	ts, sid := buildMachinesTestServer(t, fakeEnrollSvc{})
	defer ts.Close()

	if got := doAuthed(t, ts, sid, http.MethodDelete, "/api/machines/m1"); got != http.StatusNoContent {
		t.Errorf("delete revoked: got %d, want 204", got)
	}
}

func TestHandleDeleteMachine_Unknown_NotFound(t *testing.T) {
	ts, sid := buildMachinesTestServer(t, fakeEnrollSvc{deleteErr: machine.ErrNotFound})
	defer ts.Close()

	if got := doAuthed(t, ts, sid, http.MethodDelete, "/api/machines/m1"); got != http.StatusNotFound {
		t.Errorf("delete unknown: got %d, want 404", got)
	}
}
