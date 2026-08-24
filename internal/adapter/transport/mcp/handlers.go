// Package mcp implements the mneme MCP server transport (api-contracts
// Part 2): 24 tools, 3 resources, and connection-context principal stamping.
package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/phanijapps/mneme/internal/domain"
	"github.com/phanijapps/mneme/internal/service"
)

// ---------------------------------------------------------------------------
// Connection principal (F21)
// ---------------------------------------------------------------------------

// Principal is the connection-scoped identity established once at
// initialization (client info / handshake on stdio, OAuth or x-principal on
// http). Every tool call derives provenance from it — agents cannot forge
// provenance fields (api-contracts §2.0).
type Principal struct {
	AgentID string
	Actor   string // human operator behind the agent, when known
	UserID  string
	Type    domain.PrincipalType
}

type connPrincipalKey struct{}

// WithConnectionPrincipal stores the connection principal in a context.
func WithConnectionPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, connPrincipalKey{}, p)
}

// ConnectionPrincipalFromContext extracts the connection principal.
func ConnectionPrincipalFromContext(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(connPrincipalKey{}).(Principal)
	return p, ok
}

// principalFor resolves the principal for one tool call: connection context
// first, then x-principal-* request headers (http transport fallback). The
// zero Principal means unauthenticated; tool input is never a source.
func principalFor(ctx context.Context, header http.Header) Principal {
	if p, ok := ConnectionPrincipalFromContext(ctx); ok {
		return p
	}
	p := Principal{Type: domain.PrincipalAgent}
	if header != nil {
		if v := header.Get("X-Principal-Agent-Id"); v != "" {
			p.AgentID = v
		}
		if v := header.Get("X-Principal-Actor"); v != "" {
			p.Actor = v
		}
		if v := header.Get("X-Principal-User-Id"); v != "" {
			p.UserID = v
		}
	}
	if p.AgentID == "" {
		p.AgentID = "mneme-agent"
	}
	if p.Type == "" {
		p.Type = domain.PrincipalAgent
	}
	return p
}

// withServicePrincipal re-stamps ctx so services overwrite any
// client-supplied provenance (service.stampMemoryProvenance, F21).
func withServicePrincipal(ctx context.Context, p Principal) context.Context {
	return service.WithPrincipal(ctx, service.Principal{AgentID: p.AgentID, Actor: p.Actor})
}

// ---------------------------------------------------------------------------
// Active-session registry (connection-scoped session binding, §2.3)
// ---------------------------------------------------------------------------

// sessionRegistry binds an active session per connection principal. The
// session_id parameter is omitted from every session tool; it is resolved
// here, keyed by the unforgeable connection principal.
type sessionRegistry struct {
	mu    sync.RWMutex
	bound map[string]uuid.UUID
}

func newSessionRegistry() *sessionRegistry {
	return &sessionRegistry{bound: make(map[string]uuid.UUID)}
}

func (r *sessionRegistry) bind(agentKey string, sessionID uuid.UUID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.bound[agentKey] = sessionID
}

func (r *sessionRegistry) unbind(agentKey string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.bound, agentKey)
}

func (r *sessionRegistry) active(agentKey string) (uuid.UUID, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	id, ok := r.bound[agentKey]
	return id, ok
}

func (p Principal) key() string { return p.AgentID + "\x00" + p.Actor }

// ---------------------------------------------------------------------------
// Argument helpers
// ---------------------------------------------------------------------------

func argString(args map[string]any, key, def string) string {
	if v, ok := args[key].(string); ok && v != "" {
		return v
	}
	return def
}

func argStringPtr(args map[string]any, key string) *string {
	if v, ok := args[key].(string); ok && v != "" {
		return &v
	}
	return nil
}

func argBool(args map[string]any, key string, def bool) bool {
	if v, ok := args[key].(bool); ok {
		return v
	}
	return def
}

func argInt(args map[string]any, key string, def int) int {
	switch v := args[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case json.Number:
		if n, err := v.Int64(); err == nil {
			return int(n)
		}
	}
	return def
}

func argIntPtr(args map[string]any, key string) *int {
	if _, ok := args[key]; !ok {
		return nil
	}
	v := argInt(args, key, 0)
	return &v
}

func argFloatPtr(args map[string]any, key string) *float64 {
	if v, ok := args[key].(float64); ok {
		return &v
	}
	return nil
}

func argUUID(args map[string]any, key string) (uuid.UUID, error) {
	raw, ok := args[key].(string)
	if !ok || raw == "" {
		return uuid.Nil, &domain.Error{
			Code:    domain.CodeValidationErr,
			Message: fmt.Sprintf("missing required parameter %q", key),
		}
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, &domain.Error{
			Code:    domain.CodeValidationErr,
			Message: fmt.Sprintf("%s: not a uuid: %s", key, raw),
		}
	}
	return id, nil
}

func argUUIDPtr(args map[string]any, key string) (*uuid.UUID, error) {
	raw, ok := args[key].(string)
	if !ok || raw == "" {
		return nil, nil
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return nil, &domain.Error{
			Code:    domain.CodeValidationErr,
			Message: fmt.Sprintf("%s: not a uuid: %s", key, raw),
		}
	}
	return &id, nil
}

func argTimePtr(args map[string]any, key string) (*time.Time, error) {
	raw, ok := args[key].(string)
	if !ok || raw == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return nil, &domain.Error{
			Code:    domain.CodeValidationErr,
			Message: fmt.Sprintf("%s: not RFC3339 date-time: %s", key, raw),
		}
	}
	return &t, nil
}

func argStrings(args map[string]any, key string) []string {
	raw, ok := args[key].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Result construction (§2.0 result shape, §1.3 error taxonomy)
// ---------------------------------------------------------------------------

// okResult builds the standard tool result: structured content plus a
// compact text summary for clients without structured-content support.
func okResult(structured any, summary string) *mcp.CallToolResult {
	r := mcp.NewToolResultStructured(structured, summary)
	return r
}

// errorResult maps any error onto the MCP tool-error shape carrying the REST
// error code as structuredContent.error.code — one taxonomy across protocols.
func errorResult(err error) *mcp.CallToolResult {
	de := domain.FromError(err)
	payload := map[string]any{
		"error": map[string]any{
			"code":    string(de.Code),
			"message": de.Message,
		},
	}
	if len(de.Details) > 0 {
		payload["error"].(map[string]any)["details"] = de.Details
	}
	r := mcp.NewToolResultStructured(payload, string(de.Code)+": "+de.Message)
	r.IsError = true
	return r
}

// validationError is the shorthand for input-schema-level failures.
func validationError(msg string) *mcp.CallToolResult {
	return errorResult(&domain.Error{Code: domain.CodeValidationErr, Message: msg})
}

var errNoActiveSession = &domain.Error{
	Code:    domain.CodeSessionNotFound,
	Message: "no active session bound to this connection; call start_session first",
}

// requireActiveSession resolves the connection-bound session (§2.3).
func requireActiveSession(p Principal, reg *sessionRegistry) (uuid.UUID, *mcp.CallToolResult) {
	id, ok := reg.active(p.key())
	if !ok {
		return uuid.Nil, errorResult(errNoActiveSession)
	}
	return id, nil
}

// jsonMarshalText renders v compactly for resource contents.
func jsonMarshalText(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("marshal resource: %w", err)
	}
	return string(b), nil
}

// asDomainError normalizes an error to *domain.Error (mirrors http.domainError).
func asDomainError(err error) *domain.Error {
	var de *domain.Error
	if errors.As(err, &de) {
		return de
	}
	return &domain.Error{Code: domain.CodeInternal, Message: err.Error()}
}

// trimPrefixUUID extracts a trailing uuid path segment of a resource URI.
func trimPrefixUUID(uri, prefix string) (uuid.UUID, error) {
	raw := strings.TrimPrefix(uri, prefix)
	raw = strings.Trim(raw, "/")
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, &domain.Error{
			Code:    domain.CodeValidationErr,
			Message: fmt.Sprintf("resource uri %q: %s must end in a uuid", uri, prefix),
		}
	}
	return id, nil
}
