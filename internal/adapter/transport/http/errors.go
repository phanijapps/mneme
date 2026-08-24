package http

import (
	"net/http"

	"github.com/go-chi/chi/v5/middleware"
)

// ErrorEnvelope is the §1.1 standard error body; every non-2xx uses it.
type ErrorEnvelope struct {
	Error ErrorBody `json:"error"`
}

type ErrorBody struct {
	Code      string         `json:"code"`
	Message   string         `json:"message"`
	Details   map[string]any `json:"details,omitempty"`
	RequestID string         `json:"request_id"`
	DocURL    string         `json:"doc_url,omitempty"`
}

// WriteError maps err to its §1.3 status and writes the error envelope.
// domain errors carry their code; anything else is INTERNAL.
func WriteError(w http.ResponseWriter, r *http.Request, err error) {
	de := domainError(err)
	status := de.Code.HTTPStatus()
	body := ErrorEnvelope{Error: ErrorBody{
		Code:      string(de.Code),
		Message:   de.Message,
		Details:   de.Details,
		RequestID: middleware.GetReqID(r.Context()),
		DocURL:    "https://memory.example.org/docs/errors/" + string(de.Code),
	}}
	writeJSON(w, status, body)
}
