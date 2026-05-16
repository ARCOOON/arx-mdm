package api

import (
	"encoding/json"
	"net/http"
)

// ticketsJSONError is the standard dashboard JSON error envelope (legacy name retained for callers).
type ticketsJSONError struct {
	Error string `json:"error"`
}

func writeTicketsJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeTicketsError(w http.ResponseWriter, status int, msg string) {
	writeTicketsJSON(w, status, ticketsJSONError{Error: msg})
}
