package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/rizquuula/Constellate/internal/hub/adapter/secondary/agentlink"
	"github.com/rizquuula/Constellate/internal/hub/app/sessions"
	"github.com/rizquuula/Constellate/internal/hub/domain/session"
)

// SessionService is the consumer-side port for session management.
// *sessions.UseCase satisfies this interface.
type SessionService interface {
	Open(ctx context.Context, in sessions.OpenInput) (session.Session, error)
	List(ctx context.Context) ([]session.Session, error)
	ListByMachine(ctx context.Context, machineID string) ([]session.Session, error)
	Close(ctx context.Context, id string) error
	Rename(ctx context.Context, id, title string) error
}

type openSessionRequest struct {
	MachineID string `json:"machineID"`
	ProjectID string `json:"projectID"`
	Title     string `json:"title"`
	Cwd       string `json:"cwd"`
	Shell     string `json:"shell"`
	Cols      int    `json:"cols"`
	Rows      int    `json:"rows"`
	CreateDir bool   `json:"createDir"`
}

func (s *Server) handleOpenSession(w http.ResponseWriter, r *http.Request) {
	var req openSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	if req.MachineID == "" {
		writeError(w, http.StatusBadRequest, "missing_machine_id", "machineID is required")
		return
	}

	sess, err := s.sessions.Open(r.Context(), sessions.OpenInput{
		MachineID: req.MachineID,
		ProjectID: req.ProjectID,
		Title:     req.Title,
		Cwd:       req.Cwd,
		Shell:     req.Shell,
		Cols:      req.Cols,
		Rows:      req.Rows,
		CreateDir: req.CreateDir,
	})
	if err != nil {
		// Preserve the agent-supplied code (e.g. "cwd_not_found") so the UI can
		// branch on it; fall back to "open_failed" for anything else.
		code := "open_failed"
		var ae *agentlink.AgentError
		if errors.As(err, &ae) && ae.Code != "" {
			code = ae.Code
		}
		writeError(w, statusFor(err), code, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, sessionToDTO(sess))
}

func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	list, err := s.sessions.List(r.Context())
	if err != nil {
		writeError(w, statusFor(err), "list_failed", err.Error())
		return
	}

	dtos := make([]SessionDTO, len(list))
	for i, ss := range list {
		dtos[i] = sessionToDTO(ss)
	}
	writeJSON(w, http.StatusOK, dtos)
}

func (s *Server) handleListSessionsByMachine(w http.ResponseWriter, r *http.Request) {
	machineID := r.PathValue("id")
	list, err := s.sessions.ListByMachine(r.Context(), machineID)
	if err != nil {
		writeError(w, statusFor(err), "list_failed", err.Error())
		return
	}

	dtos := make([]SessionDTO, len(list))
	for i, ss := range list {
		dtos[i] = sessionToDTO(ss)
	}
	writeJSON(w, http.StatusOK, dtos)
}

func (s *Server) handleCloseSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.sessions.Close(r.Context(), id); err != nil {
		writeError(w, statusFor(err), "close_failed", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type renameSessionRequest struct {
	Title string `json:"title"`
}

func (s *Server) handleRenameSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req renameSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		writeError(w, http.StatusBadRequest, "empty_title", "title must not be empty")
		return
	}
	if err := s.sessions.Rename(r.Context(), id, title); err != nil {
		writeError(w, statusFor(err), "rename_failed", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
