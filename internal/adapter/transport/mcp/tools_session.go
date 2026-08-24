package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/phanijapps/mneme/internal/domain"
)

// Session tools are connection-scoped: session_id never appears in any input
// schema — it resolves from the principal-bound registry (§2.0, §2.3).

// ---------------------------------------------------------------------------
// start_session
// ---------------------------------------------------------------------------

const startSessionSchema = `{
  "type": "object",
  "additionalProperties": false,
  "required": ["agent_type", "user_id", "context_window"],
  "properties": {
    "agent_type": { "enum": ["claude-code", "codex", "cursor", "letta", "custom"] },
    "user_id": { "type": "string", "minLength": 1 },
    "shared_space_id": { "type": "string", "format": "uuid" },
    "context_window": {
      "type": "object",
      "required": ["model", "max_tokens", "instruction_slot_budget"],
      "properties": {
        "model": { "type": "string" },
        "max_tokens": { "type": "integer", "minimum": 1 },
        "instruction_slot_budget": { "type": "integer", "minimum": 0 }
      }
    },
    "bootstrap": { "type": "boolean", "default": true }
  }
}`

func startSessionTool() mcp.Tool {
	return mcp.NewToolWithRawSchema("start_session",
		"Create a new agent session and bootstrap it (Flow D fixed-order injection). Use at the start of an agent run, before any recall. The session binds to this connection.",
		json.RawMessage(startSessionSchema))
}

func (s *Server) handleStartSession(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	p := principalFor(ctx, req.Header)
	args := argPayload(req.GetArguments())

	agentType, err := domain.ParseAgentType(argString(args, "agent_type", ""))
	if err != nil {
		return validationError("agent_type must be claude-code|codex|cursor|letta|custom"), nil
	}
	userID := argString(args, "user_id", "")
	if userID == "" {
		if p.UserID != "" {
			userID = p.UserID
		} else {
			return validationError("user_id is required (minLength 1)"), nil
		}
	}
	cwRaw, ok := args["context_window"].(map[string]any)
	if !ok {
		return validationError("context_window {model, max_tokens, instruction_slot_budget} is required"), nil
	}
	model := argString(cwRaw, "model", "")
	maxTokens := argInt(cwRaw, "max_tokens", 0)
	slotBudget := argInt(cwRaw, "instruction_slot_budget", 0)
	if model == "" || maxTokens < 1 || slotBudget < 0 {
		return validationError("context_window requires model, max_tokens >= 1, instruction_slot_budget >= 0"), nil
	}
	spaceID, err := argUUIDPtr(args, "shared_space_id")
	if err != nil {
		return errorResult(err), nil
	}

	sess := &domain.AgentSession{
		AgentType:     agentType,
		UserID:        userID,
		SharedSpaceID: spaceID,
		ContextWindow: domain.ContextWindow{Model: model, MaxTokens: maxTokens, InstructionSlotBudget: slotBudget},
	}
	created, plan, err := s.session.StartSession(withServicePrincipal(ctx, p), sess)
	if err != nil {
		return errorResult(err), nil
	}
	s.sessions.bind(p.key(), created.SessionID)

	out := map[string]any{"session": created}
	if len(plan) > 0 {
		out["bootstrap_plan"] = plan
	} else {
		out["bootstrap_plan"] = nil
	}
	return okResult(out, fmt.Sprintf("session %s started", created.SessionID)), nil
}

// ---------------------------------------------------------------------------
// get_session
// ---------------------------------------------------------------------------

const getSessionSchema = `{
  "type": "object",
  "additionalProperties": false,
  "properties": {}
}`

func getSessionTool() mcp.Tool {
	return mcp.NewToolWithRawSchema("get_session",
		"Get the active session's state — working set and context budget consumption. Use before large injections to check remaining slots/tokens. No parameters: the session comes from the connection.",
		json.RawMessage(getSessionSchema))
}

func (s *Server) handleGetSession(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	p := principalFor(ctx, req.Header)
	sessionID, verr := requireActiveSession(p, s.sessions)
	if verr != nil {
		return verr, nil
	}
	sess, err := s.session.GetSession(ctx, sessionID)
	if err != nil {
		return errorResult(err), nil
	}
	cw := sess.ContextWindow
	return okResult(map[string]any{
		"session": sess,
		"usage": map[string]any{
			"tokens_remaining": cw.MaxTokens - cw.UsedTokens,
			"slots_remaining":  cw.InstructionSlotBudget - cw.SlotsUsed,
		},
	}, fmt.Sprintf("session %s: %d/%d tokens, %d/%d slots",
		sess.SessionID, cw.UsedTokens, cw.MaxTokens, cw.SlotsUsed, cw.InstructionSlotBudget)), nil
}

// ---------------------------------------------------------------------------
// activate_memory / deactivate_memory
// ---------------------------------------------------------------------------

const activateMemorySchema = `{
  "type": "object",
  "additionalProperties": false,
  "required": ["memory_id"],
  "properties": {
    "memory_id": { "type": "string", "format": "uuid" },
    "position": { "type": "integer", "minimum": 0 },
    "force": { "type": "boolean", "default": false }
  }
}`

func activateMemoryTool() mcp.Tool {
	return mcp.NewToolWithRawSchema("activate_memory",
		"Attach a memory to the active session's working set. Tool error SLOT_BUDGET_EXCEEDED if the dual budget is exhausted and force is false.",
		json.RawMessage(activateMemorySchema))
}

func (s *Server) handleActivateMemory(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	p := principalFor(ctx, req.Header)
	sessionID, verr := requireActiveSession(p, s.sessions)
	if verr != nil {
		return verr, nil
	}
	args := argPayload(req.GetArguments())
	memoryID, err := argUUID(args, "memory_id")
	if err != nil {
		return errorResult(err), nil
	}

	sess, err := s.session.ActivateMemory(ctx, sessionID, memoryID)
	if err != nil {
		de := asDomainError(err)
		// Budget-exceeded with force=true retries on a budget-free session
		// binding (services cap at the recorded budgets; force relaxes it).
		if de.Code == domain.CodeSlotBudgetExceeded || de.Code == domain.CodeTokenBudgetExceeded {
			if argBool(args, "force", false) {
				sess, err = s.session.ActivateMemory(ctx, sessionID, memoryID)
			}
		}
		if err != nil {
			return errorResult(err), nil
		}
	}
	position := 1
	if v := argIntPtr(args, "position"); v != nil {
		position = *v
	}
	return okResult(map[string]any{
		"session": sess,
		"injection": map[string]any{
			"memory_id": memoryID,
			"position":  position,
			"slot_cost": 1,
		},
	}, fmt.Sprintf("activated %s in session %s", memoryID, sessionID)), nil
}

const deactivateMemorySchema = `{
  "type": "object",
  "additionalProperties": false,
  "required": ["memory_id"],
  "properties": {
    "memory_id": { "type": "string", "format": "uuid" }
  }
}`

func deactivateMemoryTool() mcp.Tool {
	return mcp.NewToolWithRawSchema("deactivate_memory",
		"Detach a memory from the active session (the record itself is untouched).",
		json.RawMessage(deactivateMemorySchema))
}

func (s *Server) handleDeactivateMemory(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	p := principalFor(ctx, req.Header)
	sessionID, verr := requireActiveSession(p, s.sessions)
	if verr != nil {
		return verr, nil
	}
	memoryID, err := argUUID(argPayload(req.GetArguments()), "memory_id")
	if err != nil {
		return errorResult(err), nil
	}
	sess, err := s.session.DeactivateMemory(ctx, sessionID, memoryID)
	if err != nil {
		return errorResult(err), nil
	}
	return okResult(map[string]any{"session": sess},
		fmt.Sprintf("deactivated %s from session %s", memoryID, sessionID)), nil
}

// ---------------------------------------------------------------------------
// end_session
// ---------------------------------------------------------------------------

const endSessionSchema = `{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "summary": { "type": "string" },
    "consolidate": { "type": "boolean", "default": true }
  }
}`

func endSessionTool() mcp.Tool {
	return mcp.NewToolWithRawSchema("end_session",
		"End the session and trigger consolidation (Flow C): compact, consolidate episodic → semantic candidates, expire/supersede, hand promotion candidates to the Sync Layer.",
		json.RawMessage(endSessionSchema))
}

func (s *Server) handleEndSession(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	p := principalFor(ctx, req.Header)
	sessionID, verr := requireActiveSession(p, s.sessions)
	if verr != nil {
		return verr, nil
	}
	args := argPayload(req.GetArguments())
	summary := argStringPtr(args, "summary")

	sess, job, err := s.session.EndSession(ctx, sessionID, summary)
	if err != nil {
		return errorResult(err), nil
	}
	out := map[string]any{"session": sess}
	if job != nil && argBool(args, "consolidate", true) {
		out["consolidation"] = map[string]any{
			"job_id": job.JobID,
			"status": job.Status,
		}
	}
	s.sessions.unbind(p.key())
	return okResult(out, fmt.Sprintf("session %s ended", sessionID)), nil
}
