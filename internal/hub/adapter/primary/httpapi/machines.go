package httpapi

import "net/http"

func (s *Server) handleListMachines(w http.ResponseWriter, r *http.Request) {
	views, err := s.machines.List(r.Context())
	if err != nil {
		writeError(w, statusFor(err), "list_failed", err.Error())
		return
	}

	dtos := make([]MachineDTO, len(views))
	for i, v := range views {
		dtos[i] = machineToDTO(v)
	}

	writeJSON(w, http.StatusOK, dtos)
}

func (s *Server) handleRevokeMachine(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.enroll.Revoke(r.Context(), id); err != nil {
		writeError(w, statusFor(err), "revoke_failed", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleUnrevokeMachine(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.enroll.Unrevoke(r.Context(), id); err != nil {
		writeError(w, statusFor(err), "unrevoke_failed", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleDeleteMachine(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.enroll.Delete(r.Context(), id); err != nil {
		writeError(w, statusFor(err), "delete_failed", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
