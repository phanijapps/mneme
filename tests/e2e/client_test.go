//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// apiClient drives the real HTTP server over TCP: full middleware chain,
// auth header, JSON envelopes.
type apiClient struct {
	t    *testing.T
	base string
	http *http.Client
}

// serve starts the handler on a random local port and returns a client.
func serve(t *testing.T, h http.Handler) *apiClient {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := &http.Server{Handler: h, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Shutdown(context.Background()) })
	return &apiClient{
		t:    t,
		base: "http://" + ln.Addr().String(),
		http: &http.Client{Timeout: 30 * time.Second},
	}
}

// do executes a JSON request (bearer-authenticated as agent:e2e-agent) and
// decodes the response into out when non-nil. Returns status and raw body.
func (c *apiClient) do(method, path string, in any, out any) (int, []byte) {
	c.t.Helper()
	var body io.Reader
	if in != nil {
		raw, err := json.Marshal(in)
		if err != nil {
			c.t.Fatalf("marshal request: %v", err)
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, c.base+path, body)
	if err != nil {
		c.t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer agent:e2e-agent")
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		c.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		c.t.Fatalf("read response: %v", err)
	}
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			c.t.Fatalf("%s %s: decode %d-byte response: %v\nbody: %s", method, path, len(raw), err, raw)
		}
	}
	return resp.StatusCode, raw
}

// errBody is the §1.3 error envelope.
type errBody struct {
	Error struct {
		Code    string         `json:"code"`
		Message string         `json:"message"`
		Details map[string]any `json:"details"`
	} `json:"error"`
}

// createTestMemory saves a memory via POST /api/v1/memories.
func (c *apiClient) createTestMemory(t *testing.T, memType, content string, tags []string) map[string]any {
	t.Helper()
	body := map[string]any{
		"type":       memType,
		"content":    content,
		"provenance": map[string]any{"origin": "agent_observation"},
	}
	if tags != nil {
		body["tags"] = tags
	}
	var out map[string]any
	if code, raw := c.do("POST", "/api/v1/memories", body, &out); code != http.StatusCreated {
		t.Fatalf("create memory: status %d: %s", code, raw)
	}
	return out
}

// createTestSession creates a session via POST /api/v1/sessions.
func (c *apiClient) createTestSession(t *testing.T, userID string, slotBudget int) map[string]any {
	t.Helper()
	body := map[string]any{
		"agent_type": "claude-code",
		"user_id":    userID,
		"context_window": map[string]any{
			"model":                   "gpt-test",
			"max_tokens":              128000,
			"instruction_slot_budget": slotBudget,
		},
	}
	var out map[string]any
	if code, raw := c.do("POST", "/api/v1/sessions", body, &out); code != http.StatusCreated {
		t.Fatalf("create session: status %d: %s", code, raw)
	}
	return out
}

// createTestSpace creates a shared space with human_review promotion.
func (c *apiClient) createTestSpace(t *testing.T, name string) map[string]any {
	t.Helper()
	body := map[string]any{
		"name":       name,
		"owner_type": "user",
		"owner_id":   "user-e2e",
		"scope":      "team",
		"access_policy": map[string]any{
			"default_access": "read",
			"write":          "owner_approved",
			"promote":        "human_review",
		},
	}
	var out map[string]any
	if code, raw := c.do("POST", "/api/v1/spaces", body, &out); code != http.StatusCreated {
		t.Fatalf("create space: status %d: %s", code, raw)
	}
	return out
}

func mustUUID(t *testing.T, m map[string]any, key string) uuid.UUID {
	t.Helper()
	raw, ok := m[key].(string)
	if !ok {
		t.Fatalf("field %q missing or not a string: %v", key, m)
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		t.Fatalf("field %q %q: %v", key, raw, err)
	}
	return id
}

// sessionMap extracts the nested AgentSession object from a POST /sessions
// response envelope: { "session": {...}, "bootstrap_plan": [...] }.
func sessionMap(t *testing.T, envelope map[string]any) map[string]any {
	t.Helper()
	s, ok := envelope["session"].(map[string]any)
	if !ok {
		t.Fatalf("response envelope has no session object: %v", envelope)
	}
	return s
}

func requireOK(t *testing.T, code int, raw []byte, want int) {
	t.Helper()
	require.Equal(t, want, code, "body: %s", raw)
}
