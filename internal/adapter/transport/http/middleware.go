package http

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"

	"github.com/phanijapps/mneme/internal/domain"
)

// Principal is the access-control subject resolved from the bearer token or
// API key. It feeds provenance server-stamping on writes (api-contracts §1.1).
type Principal struct {
	Type domain.PrincipalType
	ID   string
}

type principalKey struct{}

// PrincipalFromContext returns the authenticated principal, if any.
func PrincipalFromContext(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(principalKey{}).(Principal)
	return p, ok
}

// WithPrincipal stores the principal in the request context.
func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, p)
}

// AuthMiddleware extracts the principal from Authorization: Bearer or
// X-API-Key. v1 is a stub resolver: any well-formed token maps to a principal
// whose ID is the token-derived subject; a deployment swaps in a real IdP.
func AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		principal, ok := resolvePrincipal(r)
		if !ok {
			WriteError(w, r, &domain.Error{
				Code:    domain.CodeUnauthenticated,
				Message: "missing or invalid credentials",
			})
			return
		}
		next.ServeHTTP(w, r.WithContext(WithPrincipal(r.Context(), principal)))
	})
}

// resolvePrincipal maps a bearer token / API key to a principal (stub v1).
func resolvePrincipal(r *http.Request) (Principal, bool) {
	var token string
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		token = strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
	} else if k := r.Header.Get("X-API-Key"); k != "" {
		token = strings.TrimSpace(k)
	}
	if token == "" {
		return Principal{}, false
	}
	// Stub scheme "agent:<id>" / "user:<id>" / "service:<id>"; bare tokens are agents.
	for _, prefix := range []string{"user", "agent", "service", "group"} {
		if strings.HasPrefix(token, prefix+":") {
			return Principal{Type: domain.PrincipalType(prefix), ID: strings.TrimPrefix(token, prefix+":")}, true
		}
	}
	return Principal{Type: domain.PrincipalAgent, ID: token}, true
}

// RequestIDMiddleware propagates X-Request-Id or generates one, exposing it
// via the chi context key and the response header.
func RequestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-Id")
		if id == "" {
			id = "req_" + uuid.NewString()
		}
		w.Header().Set("X-Request-Id", id)
		// Expose via the chi context key so WriteError can stamp request_id.
		ctx := context.WithValue(r.Context(), middleware.RequestIDKey, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// LoggingMiddleware logs method, path, status, latency, and principal.
func LoggingMiddleware(logger *slog.Logger) func(http.Handler) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)
			principal := ""
			if p, ok := PrincipalFromContext(r.Context()); ok {
				principal = p.ID
			}
			logger.Info("http",
				"principal_id", principal,
				"method", r.Method,
				"path", r.URL.Path,
				"status", ww.Status(),
				"latency_ms", time.Since(start).Milliseconds(),
				"request_id", middleware.GetReqID(r.Context()),
			)
		})
	}
}

// ErrorMiddleware is the terminal recoverer: it maps panics to 500 INTERNAL
// envelopes. Domain errors are handled inside handlers via WriteError.
func ErrorMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				WriteError(w, r, &domain.Error{
					Code:    domain.CodeInternal,
					Message: fmt.Sprintf("panic: %v", rec),
				})
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// CORSMiddleware sets permissive v1 CORS headers.
func CORSMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-API-Key, X-Request-Id, Idempotency-Key, If-Match")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// decodeJSON strictly decodes a request body into dst, rejecting unknown
// fields (api-contracts strict mode) and wrong content types.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	if r.Body == nil || r.ContentLength == 0 {
		WriteError(w, r, &domain.Error{
			Code:    domain.CodeValidationErr,
			Message: "request body required",
		})
		return false
	}
	if ct := r.Header.Get("Content-Type"); ct != "" && !strings.HasPrefix(ct, "application/json") {
		WriteError(w, r, &domain.Error{
			Code:    domain.CodeUnsupportedMediaType,
			Message: "Content-Type must be application/json",
		})
		return false
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		WriteError(w, r, &domain.Error{
			Code:    domain.CodeValidationErr,
			Message: "invalid JSON body: " + err.Error(),
		})
		return false
	}
	return true
}

// writeJSON writes a JSON response with the given status.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if v == nil {
		return
	}
	_ = json.NewEncoder(w).Encode(v)
}

// pathUUID extracts and parses a chi URL param as a UUID.
func pathUUID(w http.ResponseWriter, r *http.Request, name string) (uuid.UUID, bool) {
	raw := chi.URLParam(r, name)
	id, err := uuid.Parse(raw)
	if err != nil {
		WriteError(w, r, &domain.Error{
			Code:    domain.CodeValidationErr,
			Message: fmt.Sprintf("path param %s: invalid uuid %q", name, raw),
			Details: map[string]any{"field": name},
		})
		return uuid.Nil, false
	}
	return id, true
}

// domainError converts any error into a *domain.Error for the envelope.
func domainError(err error) *domain.Error {
	var de *domain.Error
	if errors.As(err, &de) {
		return de
	}
	return &domain.Error{Code: domain.CodeInternal, Message: err.Error()}
}
