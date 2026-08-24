package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/phanijapps/mneme/internal/domain"
)

// ---------------------------------------------------------------------------
// recall — §2.2: sync, hydrates memory content inline (asymmetry #4).
// agent_id/session_id/mode excluded from input: connection-context-bound.
// ---------------------------------------------------------------------------

const recallSchema = `{
  "type": "object",
  "additionalProperties": false,
  "required": ["query", "trigger"],
  "properties": {
    "query": { "type": "string", "minLength": 1 },
    "context": {
      "type": "object",
      "properties": {
        "entities": { "type": "array", "items": { "type": "string", "format": "uuid" } },
        "task_signature": { "type": "string" },
        "time_bounds": { "type": "object", "properties": { "from": { "type": "string", "format": "date-time" }, "to": { "type": "string", "format": "date-time" } } },
        "mentioned_files": { "type": "array", "items": { "type": "string" } }
      }
    },
    "trigger": { "enum": ["task_context", "user_query", "temporal", "associative", "session_start"] },
    "retrieval_params": {
      "type": "object",
      "properties": {
        "strategies": { "type": "array", "minItems": 1, "items": { "enum": ["vector", "bm25", "graph", "temporal"] }, "default": ["vector", "bm25", "graph", "temporal"] },
        "top_k": { "type": "integer", "minimum": 1, "maximum": 200, "default": 50 },
        "rerank": { "enum": ["cross_encoder", "none"], "default": "cross_encoder" },
        "min_score": { "type": "number", "minimum": 0, "maximum": 1 },
        "slot_budget": { "type": "integer", "minimum": 0 },
        "token_budget": { "type": "integer", "minimum": 0 }
      }
    }
  }
}`

func recallTool() mcp.Tool {
	return mcp.NewToolWithRawSchema("recall",
		"Submit a recall request and get relevant memories with content inlined. Hybrid retrieval (vector ∥ bm25 ∥ graph ∥ temporal) with cross-encoder re-ranking, sized to the session's slot/token budgets.",
		json.RawMessage(recallSchema))
}

// parseRecallInput decodes the shared recall/recall_async input.
func (s *Server) parseRecallInput(args argPayload, p Principal, mode domain.RecallMode) (*domain.RecallRequest, *mcp.CallToolResult) {
	query := argString(args, "query", "")
	trigger, err := domain.ParseTriggerType(argString(args, "trigger", ""))
	if query == "" || err != nil {
		return nil, validationError("query (minLength 1) and trigger are required")
	}
	req := &domain.RecallRequest{
		Query:   query,
		Trigger: trigger,
		AgentID: p.AgentID,
		Mode:    mode,
	}
	if sessionID, ok := s.sessions.active(p.key()); ok {
		req.SessionID = sessionID
	} else {
		req.SessionID = uuid.Nil
	}
	if c := args["context"]; c != nil {
		if cm, ok := c.(map[string]any); ok {
			rc := domain.RecallContext{Entities: argStrings(cm, "entities"), MentionedFiles: argStrings(cm, "mentioned_files")}
			if sig := argStringPtr(cm, "task_signature"); sig != nil {
				rc.TaskSignature = sig
			}
			if tb := cm["time_bounds"]; tb != nil {
				if tm, ok := tb.(map[string]any); ok {
					from, err1 := argTimePtr(tm, "from")
					to, err2 := argTimePtr(tm, "to")
					if err1 != nil || err2 != nil {
						return nil, errorResult(err1)
					}
					if err2 != nil {
						return nil, errorResult(err2)
					}
					if from != nil && to != nil {
						bounds := [2]time.Time{*from, *to}
						rc.TimeBounds = &bounds
					}
				}
			}
			req.Context = rc
		}
	}
	rp := domain.RetrievalParams{}
	if raw := args["retrieval_params"]; raw != nil {
		if pm, ok := raw.(map[string]any); ok {
			strategies := make([]domain.StrategyType, 0, 4)
			for _, sv := range argStrings(pm, "strategies") {
				st, err := domain.ParseStrategyType(sv)
				if err != nil {
					return nil, validationError("strategies items must be vector|bm25|graph|temporal")
				}
				strategies = append(strategies, st)
			}
			if len(strategies) == 0 {
				strategies = []domain.StrategyType{domain.StrategyVector, domain.StrategyBM25, domain.StrategyGraph, domain.StrategyTemporal}
			}
			rp.Strategies = strategies
			rp.TopK = argInt(pm, "top_k", 50)
			rr, err := domain.ParseRerankType(argString(pm, "rerank", "cross_encoder"))
			if err != nil {
				return nil, validationError("rerank must be cross_encoder|none")
			}
			rp.Rerank = rr
			if m := argFloatPtr(pm, "min_score"); m != nil {
				rp.MinScore = *m
			}
			rp.SlotBudget = argIntPtr(pm, "slot_budget")
			rp.TokenBudget = argIntPtr(pm, "token_budget")
		}
	}
	req.RetrievalParams = rp
	return req, nil
}

// hydrateCandidates inlines full memory records into each candidate, so the
// agent consumes one tool result instead of N get_memory calls (§4.2).
func (s *Server) hydrateCandidates(ctx context.Context, result *domain.RecallResult) []map[string]any {
	out := make([]map[string]any, 0, len(result.Candidates))
	for _, c := range result.Candidates {
		entry := map[string]any{
			"memory":            c.MemoryID,
			"score":             c.Score,
			"source_strategies": c.SourceStrategies,
		}
		if c.RerankScore != nil {
			entry["rerank_score"] = *c.RerankScore
		}
		if mem, err := s.memories.GetMemory(ctx, c.MemoryID); err == nil {
			entry["memory"] = mem // embedding omitted by the json:"-" tag
		}
		out = append(out, entry)
	}
	return out
}

func (s *Server) recallStructured(ctx context.Context, result *domain.RecallResult) map[string]any {
	structured := map[string]any{
		"request_id":     result.RequestID,
		"candidates":     s.hydrateCandidates(ctx, result),
		"injection_plan": result.InjectionPlan,
		"slots_used":     result.SlotsUsed,
		"tokens_used":    result.TokensUsed,
	}
	if result.LatencyMS != nil {
		structured["latency_ms"] = *result.LatencyMS
	}
	return structured
}

func (s *Server) handleRecall(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	p := principalFor(ctx, req.Header)
	r, verr := s.parseRecallInput(argPayload(req.GetArguments()), p, domain.RecallModeSync)
	if verr != nil {
		return verr, nil
	}
	result, err := s.recall.Recall(ctx, r)
	if err != nil {
		return errorResult(err), nil
	}
	structured := s.recallStructured(ctx, result)
	return okResult(structured, fmt.Sprintf("recall %s: %d candidates", result.RequestID, len(result.Candidates))), nil
}

// ---------------------------------------------------------------------------
// recall_async — §2.2: submit + self-poll by request_id in one tool.
// ---------------------------------------------------------------------------

const recallAsyncSchema = `{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "request_id": { "type": "string", "format": "uuid", "description": "poll mode: pass a previously returned request id" },
    "query": { "type": "string", "minLength": 1 },
    "context": {
      "type": "object",
      "properties": {
        "entities": { "type": "array", "items": { "type": "string", "format": "uuid" } },
        "task_signature": { "type": "string" },
        "time_bounds": { "type": "object", "properties": { "from": { "type": "string", "format": "date-time" }, "to": { "type": "string", "format": "date-time" } } },
        "mentioned_files": { "type": "array", "items": { "type": "string" } }
      }
    },
    "trigger": { "enum": ["task_context", "user_query", "temporal", "associative", "session_start"] },
    "retrieval_params": {
      "type": "object",
      "properties": {
        "strategies": { "type": "array", "minItems": 1, "items": { "enum": ["vector", "bm25", "graph", "temporal"] } },
        "top_k": { "type": "integer", "minimum": 1, "maximum": 200, "default": 50 },
        "rerank": { "enum": ["cross_encoder", "none"], "default": "cross_encoder" },
        "min_score": { "type": "number", "minimum": 0, "maximum": 1 },
        "slot_budget": { "type": "integer", "minimum": 0 },
        "token_budget": { "type": "integer", "minimum": 0 }
      }
    },
    "poll_hint_ms": { "type": "integer", "minimum": 0 }
  }
}`

func recallAsyncTool() mcp.Tool {
	return mcp.NewToolWithRawSchema("recall_async",
		"Submit a recall request for complex retrieval without blocking (deep graph expansion, corpus-wide temporal sweeps) or poll a previously submitted request by request_id.",
		json.RawMessage(recallAsyncSchema))
}

func (s *Server) handleRecallAsync(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := argPayload(req.GetArguments())

	// Poll mode: request_id alone (or with poll_hint) fetches prior status.
	if raw := argString(args, "request_id", ""); raw != "" && argString(args, "query", "") == "" {
		requestID, err := uuid.Parse(raw)
		if err != nil {
			return validationError("request_id: not a uuid"), nil
		}
		request, result, err := s.recall.GetRecallStatus(ctx, requestID)
		if err != nil {
			return errorResult(err), nil
		}
		structured := map[string]any{"request_id": requestID, "status": request.Status}
		switch request.Status {
		case domain.RecallStatusCompleted:
			if result != nil {
				structured["result"] = s.recallStructured(ctx, result)
			}
		case domain.RecallStatusFailed:
			if request.Error != nil {
				structured["error"] = map[string]any{"code": string(request.Error.Code), "message": request.Error.Message}
			}
		default:
			structured["poll_after_ms"] = argInt(args, "poll_hint_ms", 250)
		}
		return okResult(structured, fmt.Sprintf("recall %s: %s", requestID, request.Status)), nil
	}

	// Submit mode.
	p := principalFor(ctx, req.Header)
	r, verr := s.parseRecallInput(args, p, domain.RecallModeAsync)
	if verr != nil {
		return verr, nil
	}
	if _, err := s.recall.Recall(ctx, r); err != nil { // async returns nil result, nil error
		return errorResult(err), nil
	}
	return okResult(map[string]any{
		"request_id":    r.RequestID,
		"status":        domain.RecallStatusQueued,
		"poll_after_ms": argInt(args, "poll_hint_ms", 250),
	}, fmt.Sprintf("recall %s queued", r.RequestID)), nil
}
