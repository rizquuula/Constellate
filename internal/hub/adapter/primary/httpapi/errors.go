package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/rizquuula/Constellate/internal/hub/adapter/secondary/agentlink"
	appauth "github.com/rizquuula/Constellate/internal/hub/app/auth"
	"github.com/rizquuula/Constellate/internal/hub/app/enroll"
	appprojects "github.com/rizquuula/Constellate/internal/hub/app/projects"
	appsessions "github.com/rizquuula/Constellate/internal/hub/app/sessions"
	"github.com/rizquuula/Constellate/internal/hub/domain/machine"
	"github.com/rizquuula/Constellate/internal/hub/domain/project"
	"github.com/rizquuula/Constellate/internal/hub/domain/session"
)

type errorBody struct {
	Error errorDetail `json:"error"`
}

type errorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, errorBody{Error: errorDetail{Code: code, Message: msg}})
}

func statusFor(err error) int {
	if errors.Is(err, machine.ErrNotFound) {
		return http.StatusNotFound
	}
	if errors.Is(err, session.ErrNotFound) {
		return http.StatusNotFound
	}
	if errors.Is(err, appsessions.ErrSessionRunning) {
		return http.StatusConflict
	}
	if errors.Is(err, project.ErrNotFound) {
		return http.StatusNotFound
	}
	if errors.Is(err, project.ErrInvalid) {
		return http.StatusBadRequest
	}
	if errors.Is(err, project.ErrDuplicatePath) {
		return http.StatusConflict
	}
	if errors.Is(err, appprojects.ErrHasSessions) {
		return http.StatusConflict
	}
	if errors.Is(err, agentlink.ErrAgentOffline) {
		return http.StatusServiceUnavailable
	}
	var ae *agentlink.AgentError
	if errors.As(err, &ae) && ae.Code == "cwd_not_found" {
		// Recoverable: the UI offers to create the directory and retry.
		return http.StatusUnprocessableEntity
	}
	if errors.Is(err, enroll.ErrInvalidToken) {
		return http.StatusUnauthorized
	}
	if errors.Is(err, enroll.ErrUnknownMachine) {
		return http.StatusNotFound
	}
	if errors.Is(err, enroll.ErrRevoked) {
		return http.StatusForbidden
	}
	if errors.Is(err, appauth.ErrInvalidCredential) {
		return http.StatusUnauthorized
	}
	if errors.Is(err, appauth.ErrNoOperator) {
		return http.StatusForbidden
	}
	if errors.Is(err, appauth.ErrWebAuthnUnavailable) {
		return http.StatusNotImplemented
	}
	if errors.Is(err, appauth.ErrChallengeNotFound) {
		return http.StatusUnauthorized
	}
	return http.StatusInternalServerError
}
