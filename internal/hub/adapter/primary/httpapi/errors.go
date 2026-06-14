package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/rizquuula/Constellate/internal/hub/domain/machine"
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
	return http.StatusInternalServerError
}
