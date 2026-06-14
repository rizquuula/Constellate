package httpapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"

	"github.com/rizquuula/Constellate/internal/hub/app/enroll"
)

// EnrollService is the consumer-side port for machine enrollment.
// *enroll.UseCase satisfies this interface.
type EnrollService interface {
	Enroll(ctx context.Context, in enroll.EnrollInput) (string, error)
}

type enrollRequest struct {
	Token     string `json:"token"`
	PublicKey string `json:"publicKey"` // base64 std encoding
	Name      string `json:"name"`
	OS        string `json:"os"`
	Arch      string `json:"arch"`
}

type enrollResponse struct {
	MachineID string `json:"machineID"`
}

// handleEnroll handles POST /api/enroll.
// This route MUST remain unauthenticated — it is the enrollment bootstrap, protected
// only by the one-time token. A later worker adding operator-auth middleware MUST
// allowlist /api/enroll.
func (s *Server) handleEnroll(w http.ResponseWriter, r *http.Request) {
	var req enrollRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	if req.Token == "" {
		writeError(w, http.StatusBadRequest, "missing_token", "token is required")
		return
	}
	if req.PublicKey == "" {
		writeError(w, http.StatusBadRequest, "missing_public_key", "publicKey is required")
		return
	}

	pubKeyBytes, err := base64.StdEncoding.DecodeString(req.PublicKey)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_public_key", "publicKey must be base64 std encoded")
		return
	}

	machineID, err := s.enroll.Enroll(r.Context(), enroll.EnrollInput{
		Token:     []byte(req.Token),
		PublicKey: pubKeyBytes,
		Name:      req.Name,
		OS:        req.OS,
		Arch:      req.Arch,
	})
	if err != nil {
		writeError(w, statusFor(err), "enroll_failed", err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, enrollResponse{MachineID: machineID})
}
