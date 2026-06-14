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
