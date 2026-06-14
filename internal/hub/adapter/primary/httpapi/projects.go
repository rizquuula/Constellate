package httpapi

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/rizquuula/Constellate/internal/hub/app/projects"
	"github.com/rizquuula/Constellate/internal/hub/domain/project"
)

// ProjectService is the consumer-side port for project management.
// *projects.UseCase satisfies this interface.
type ProjectService interface {
	Create(ctx context.Context, in projects.CreateInput) (project.Project, error)
	List(ctx context.Context) ([]project.Project, error)
}

type createProjectRequest struct {
	MachineID string `json:"machineID"`
	Name      string `json:"name"`
	Path      string `json:"path"`
	Color     string `json:"color"`
}

func (s *Server) handleListProjects(w http.ResponseWriter, r *http.Request) {
	list, err := s.projects.List(r.Context())
	if err != nil {
		writeError(w, statusFor(err), "list_failed", err.Error())
		return
	}

	dtos := make([]ProjectDTO, len(list))
	for i, p := range list {
		dtos[i] = projectToDTO(p)
	}
	writeJSON(w, http.StatusOK, dtos)
}

func (s *Server) handleCreateProject(w http.ResponseWriter, r *http.Request) {
	var req createProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	if req.MachineID == "" {
		writeError(w, http.StatusBadRequest, "missing_machine_id", "machineID is required")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "missing_name", "name is required")
		return
	}
	if req.Path == "" {
		writeError(w, http.StatusBadRequest, "missing_path", "path is required")
		return
	}

	p, err := s.projects.Create(r.Context(), projects.CreateInput{
		MachineID: req.MachineID,
		Name:      req.Name,
		Path:      req.Path,
		Color:     req.Color,
	})
	if err != nil {
		writeError(w, statusFor(err), "create_failed", err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, projectToDTO(p))
}
