package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/mark3labs/mcp-go/mcp"
)

// ---------------------------------------------------------------------------
// consolidate
// ---------------------------------------------------------------------------

const consolidateSchema = `{
  "type": "object",
  "additionalProperties": false,
  "required": ["scope"],
  "properties": {
    "scope": {
      "type": "object",
      "required": ["kind"],
      "properties": {
        "kind": { "enum": ["session", "user", "space", "all"] },
        "id": { "type": "string" }
      }
    },
    "memory_types": { "type": "array", "items": { "enum": ["episodic", "semantic", "procedural"] } },
    "dry_run": { "type": "boolean", "default": false },
    "job_id": { "type": "string", "format": "uuid", "description": "poll mode: pass a previously returned job id" }
  }
}`

func consolidateTool() mcp.Tool {
	return mcp.NewToolWithRawSchema("consolidate",
		"Trigger consolidation for a scope. Use after session ends or periodically for a user/space. dry_run returns planned mutations without applying.",
		json.RawMessage(consolidateSchema))
}

func (s *Server) handleConsolidate(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := argPayload(req.GetArguments())
	if job, _ := s.pollJob(ctx, args); job != nil {
		return job, nil
	}
	kind, scopeID, verr := parseScope(args)
	if verr != nil {
		return verr, nil
	}
	job, err := s.lifecycle.Consolidate(ctx, kind, scopeID)
	if err != nil {
		return errorResult(err), nil
	}
	return okResult(map[string]any{"job": job}, fmt.Sprintf("consolidation job %s: %s", job.JobID, job.Status)), nil
}

// ---------------------------------------------------------------------------
// decay
// ---------------------------------------------------------------------------

const decaySchema = `{
  "type": "object",
  "additionalProperties": false,
  "required": ["scope"],
  "properties": {
    "scope": {
      "type": "object",
      "required": ["kind"],
      "properties": {
        "kind": { "enum": ["session", "user", "space", "all"] },
        "id": { "type": "string" }
      }
    },
    "decay_profile": { "enum": ["standard", "aggressive"] },
    "min_age_days": { "type": "integer", "minimum": 0 },
    "job_id": { "type": "string", "format": "uuid", "description": "poll mode: pass a previously returned job id" }
  }
}`

func decayTool() mcp.Tool {
	return mcp.NewToolWithRawSchema("decay",
		"Trigger a decay pass — reduce decay/confidence on stale memories, expire TTL'd records. Use on a schedule or after long idle periods.",
		json.RawMessage(decaySchema))
}

func (s *Server) handleDecay(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := argPayload(req.GetArguments())
	if job, _ := s.pollJob(ctx, args); job != nil {
		return job, nil
	}
	if profile := argString(args, "decay_profile", "standard"); profile != "standard" && profile != "aggressive" {
		return validationError("decay_profile must be standard|aggressive"), nil
	}
	kind, scopeID, verr := parseScope(args)
	if verr != nil {
		return verr, nil
	}
	job, err := s.lifecycle.Decay(ctx, kind, scopeID)
	if err != nil {
		return errorResult(err), nil
	}
	return okResult(map[string]any{"job": job}, fmt.Sprintf("decay job %s: %s", job.JobID, job.Status)), nil
}

// ---------------------------------------------------------------------------
// memory_stats
// ---------------------------------------------------------------------------

const memoryStatsSchema = `{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "scope": { "type": "string", "description": "global | user:<id> | space:<uuid>", "default": "global" },
    "include_index_health": { "type": "boolean", "default": false }
  }
}`

func memoryStatsTool() mcp.Tool {
	return mcp.NewToolWithRawSchema("memory_stats",
		"Get memory statistics. Use for monitoring/dashboards or an agent self-reporting its memory health.",
		json.RawMessage(memoryStatsSchema))
}

func (s *Server) handleMemoryStats(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := argPayload(req.GetArguments())
	scope := argString(args, "scope", "global")
	if scope == "" {
		scope = "global"
	}
	if scope != "global" && !strings.HasPrefix(scope, "user:") && !strings.HasPrefix(scope, "space:") {
		return validationError("scope must be global, user:<id>, or space:<uuid>"), nil
	}
	stats, err := s.lifecycle.GetStats(ctx, scope)
	if err != nil {
		return errorResult(err), nil
	}
	if !argBool(args, "include_index_health", false) {
		stats.IndexHealth = nil
	}
	return okResult(stats, fmt.Sprintf("stats %s: %d memories", scope, stats.Counts.Total)), nil
}

// ---------------------------------------------------------------------------
// shared helpers
// ---------------------------------------------------------------------------

// parseScope decodes the {kind, id} scope object; "all" maps to the service's
// global scope ("global", empty id).
func parseScope(args argPayload) (string, string, *mcp.CallToolResult) {
	raw, ok := args["scope"].(map[string]any)
	if !ok {
		return "", "", validationError("scope {kind} is required")
	}
	kind := argString(raw, "kind", "")
	switch kind {
	case "session", "user", "space":
		id := argString(raw, "id", "")
		if id == "" {
			return "", "", validationError("scope.id is required for kind " + kind)
		}
		if kind == "space" {
			if _, err := uuid.Parse(id); err != nil {
				return "", "", validationError("scope.id must be a uuid for kind space")
			}
		}
		return kind, id, nil
	case "all", "global":
		return "global", "", nil
	default:
		return "", "", validationError("scope.kind must be session|user|space|all")
	}
}

// pollJob handles the in-tool job_id poll shared by consolidate/decay.
func (s *Server) pollJob(ctx context.Context, args argPayload) (*mcp.CallToolResult, *mcp.CallToolResult) {
	raw := argString(args, "job_id", "")
	if raw == "" {
		return nil, nil
	}
	jobID, err := uuid.Parse(raw)
	if err != nil {
		return nil, validationError("job_id: not a uuid")
	}
	job, err := s.lifecycle.GetJob(ctx, jobID)
	if err != nil {
		return nil, errorResult(err)
	}
	return okResult(map[string]any{"job": job}, fmt.Sprintf("job %s: %s", jobID, job.Status)), nil
}
