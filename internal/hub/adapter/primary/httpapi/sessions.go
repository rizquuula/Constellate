package httpapi

import (
	"context"
	"encoding/json"
	"net/http"

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
}

type openSessionRequest struct {
	MachineID string `json:"machineID"`
	ProjectID string `json:"projectID"`
	Title     string `json:"title"`
	Cwd       string `json:"cwd"`
	Shell     string `json:"shell"`
	Cols      int    `json:"cols"`
	Rows      int    `json:"rows"`
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
	})
	if err != nil {
		writeError(w, statusFor(err), "open_failed", err.Error())
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
