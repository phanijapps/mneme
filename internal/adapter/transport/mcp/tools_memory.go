package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/phanijapps/mneme/internal/domain"
	"github.com/phanijapps/mneme/internal/port"
)

// argPayload is the decoded tool-argument envelope for handler bodies.
type argPayload map[string]any

// ---------------------------------------------------------------------------
// save_memory — §2.1 (provenance excluded from input: server-stamped, F21)
// ---------------------------------------------------------------------------

const saveMemorySchema = `{
  "type": "object",
  "additionalProperties": false,
  "required": ["type", "content"],
  "properties": {
    "type": { "enum": ["episodic", "semantic", "procedural"] },
    "content": { "type": "string", "minLength": 1 },
    "content_format": { "enum": ["markdown", "plain", "json"], "default": "markdown" },
    "tags": { "type": "array", "items": { "type": "string", "pattern": "^[a-z0-9][a-z0-9_-]*$" } },
    "confidence": { "type": "number", "minimum": 0, "maximum": 1 },
    "origin": { "enum": ["agent_observation", "user_instruction", "file_artifact", "consolidation"], "default": "agent_observation" },
    "source_ref": {
      "type": "object",
      "properties": { "kind": { "enum": ["file", "url", "tool_call", "message"] }, "path": { "type": "string" }, "hash": { "type": "string" } }
    },
    "access_scope": { "enum": ["individual", "shared"], "default": "individual" },
    "shared_space_id": { "type": "string", "format": "uuid" }
  }
}`

func saveMemoryTool() mcp.Tool {
	t := mcp.NewToolWithRawSchema("save_memory",
		"Encode and store a new memory. Episodic observations are cheap and should be saved liberally; semantic claims require repetition or consolidation.",
		json.RawMessage(saveMemorySchema))
	return t
}

func (s *Server) handleSaveMemory(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	p := principalFor(ctx, req.Header)
	args := argPayload(req.GetArguments())

	mType, err := domain.ParseMemoryType(argString(args, "type", ""))
	if err != nil {
		return validationError("type must be episodic|semantic|procedural"), nil
	}
	content := argString(args, "content", "")
	if content == "" {
		return validationError("content is required (minLength 1)"), nil
	}
	format, err := domain.ParseContentFormat(argString(args, "content_format", "markdown"))
	if err != nil {
		return validationError("content_format must be markdown|plain|json"), nil
	}
	origin, err := domain.ParseOrigin(argString(args, "origin", "agent_observation"))
	if err != nil {
		return validationError("origin must be agent_observation|user_instruction|file_artifact|consolidation"), nil
	}
	scope, err := domain.ParseAccessScope(argString(args, "access_scope", "individual"))
	if err != nil {
		return validationError("access_scope must be individual|shared"), nil
	}
	spaceID, err := argUUIDPtr(args, "shared_space_id")
	if err != nil {
		return errorResult(err), nil
	}

	mem := &domain.Memory{
		Type:               mType,
		Content:            content,
		ContentFormat:      format,
		Tags:               argStrings(args, "tags"),
		Origin:             origin,
		AccessScope:        scope,
		SharedSpaceID:      spaceID,
		Confidence:         argFloatPtr(args, "confidence"),
		OwnerPrincipalType: domain.PrincipalAgent,
		OwnerPrincipalID:   p.AgentID,
	}
	if ref := args["source_ref"]; ref != nil {
		if refMap, ok := ref.(map[string]any); ok {
			sr := &domain.SourceRef{Kind: argString(refMap, "kind", "message")}
			if v := argStringPtr(refMap, "path"); v != nil {
				sr.Path = v
			}
			if v := argStringPtr(refMap, "hash"); v != nil {
				sr.Hash = v
			}
			mem.SourceRef = sr
		}
	}
	if p.UserID != "" {
		mem.OwnerPrincipalType = domain.PrincipalUser
		mem.OwnerPrincipalID = p.UserID
	}

	stored, err := s.memories.SaveMemory(withServicePrincipal(ctx, p), mem)
	if err != nil {
		return errorResult(err), nil
	}
	return okResult(map[string]any{"memory": stored},
		fmt.Sprintf("saved memory %s (v%d)", stored.ID, stored.Version)), nil
}

// ---------------------------------------------------------------------------
// get_memory — §2.1
// ---------------------------------------------------------------------------

const getMemorySchema = `{
  "type": "object",
  "additionalProperties": false,
  "required": ["memory_id"],
  "properties": {
    "memory_id": { "type": "string", "format": "uuid" },
    "include_links": { "type": "boolean", "default": false }
  }
}`

func getMemoryTool() mcp.Tool {
	return mcp.NewToolWithRawSchema("get_memory",
		"Retrieve one memory by ID with full metadata. Use when a recall result or link references an ID whose content you need.",
		json.RawMessage(getMemorySchema))
}

func (s *Server) handleGetMemory(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := argPayload(req.GetArguments())
	id, verr := argUUID(args, "memory_id")
	if verr != nil {
		return errorResult(verr), nil
	}
	mem, err := s.memories.GetMemory(ctx, id)
	if err != nil {
		return errorResult(err), nil
	}
	out := map[string]any{"memory": mem}
	if argBool(args, "include_links", false) {
		if s.links != nil {
			links, err := s.links(ctx, id)
			if err != nil {
				return errorResult(err), nil
			}
			out["links"] = links
		} else {
			out["links"] = []any{}
		}
	}
	return okResult(out, fmt.Sprintf("memory %s", mem.ID)), nil
}

// ---------------------------------------------------------------------------
// list_memories — §2.1 (batch_size capped at 50, §3.2 asymmetry #5)
// ---------------------------------------------------------------------------

const listMemoriesSchema = `{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "type": { "enum": ["episodic", "semantic", "procedural"] },
    "tags": { "type": "array", "items": { "type": "string" } },
    "tags_match": { "enum": ["any", "all"], "default": "any" },
    "entity_id": { "type": "string", "format": "uuid" },
    "access_scope": { "enum": ["individual", "shared"] },
    "shared_space_id": { "type": "string", "format": "uuid" },
    "created_after": { "type": "string", "format": "date-time" },
    "created_before": { "type": "string", "format": "date-time" },
    "min_confidence": { "type": "number", "minimum": 0, "maximum": 1 },
    "batch_size": { "type": "integer", "minimum": 1, "maximum": 50, "default": 20 },
    "cursor": { "type": "string", "description": "opaque; pass next_cursor from a prior call" }
  }
}`

func listMemoriesTool() mcp.Tool {
	return mcp.NewToolWithRawSchema("list_memories",
		"Filter and list memories deterministically (no relevance scoring — use recall for that). batch_size is capped at 50.",
		json.RawMessage(listMemoriesSchema))
}

func (s *Server) handleListMemories(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := argPayload(req.GetArguments())
	batch := argInt(args, "batch_size", 20)
	if batch < 1 || batch > 50 {
		return validationError("batch_size must be within 1..50 (MCP cap)"), nil
	}

	filter := port.NewMemoryFilter(port.WithPagination(batch, argString(args, "cursor", "")))
	if t := argString(args, "type", ""); t != "" {
		mt, err := domain.ParseMemoryType(t)
		if err != nil {
			return validationError("type must be episodic|semantic|procedural"), nil
		}
		filter.Types = []domain.MemoryType{mt}
	}
	if tags := argStrings(args, "tags"); len(tags) > 0 {
		match := port.TagsMatchAny
		if argString(args, "tags_match", "any") == "all" {
			match = port.TagsMatchAll
		}
		filter.Tags, filter.TagsMatch = tags, match
	}
	if id, err := argUUIDPtr(args, "entity_id"); err != nil {
		return errorResult(err), nil
	} else if id != nil {
		filter.EntityID = id
	}
	if sc := argString(args, "access_scope", ""); sc != "" {
		scope, err := domain.ParseAccessScope(sc)
		if err != nil {
			return validationError("access_scope must be individual|shared"), nil
		}
		filter.AccessScope = &scope
	}
	if id, err := argUUIDPtr(args, "shared_space_id"); err != nil {
		return errorResult(err), nil
	} else if id != nil {
		filter.SharedSpaceID = id
	}
	if t, err := argTimePtr(args, "created_after"); err != nil {
		return errorResult(err), nil
	} else if t != nil {
		filter.CreatedFrom = t
	}
	if t, err := argTimePtr(args, "created_before"); err != nil {
		return errorResult(err), nil
	} else if t != nil {
		filter.CreatedTo = t
	}
	if c := argFloatPtr(args, "min_confidence"); c != nil {
		filter.MinConfidence = c
	}

	page, err := s.memories.ListMemories(ctx, filter)
	if err != nil {
		return errorResult(err), nil
	}
	return okResult(map[string]any{
		"items":       page.Items,
		"has_more":    page.HasMore,
		"next_cursor": page.NextCursor,
		"batch_size":  batch,
	}, fmt.Sprintf("%d memories (has_more=%v)", len(page.Items), page.HasMore)), nil
}

// ---------------------------------------------------------------------------
// update_memory — §2.1 (expected_version required, §3.1)
// ---------------------------------------------------------------------------

const updateMemorySchema = `{
  "type": "object",
  "additionalProperties": false,
  "required": ["memory_id", "expected_version"],
  "properties": {
    "memory_id": { "type": "string", "format": "uuid" },
    "expected_version": { "type": "integer", "minimum": 1 },
    "confidence": { "type": "number", "minimum": 0, "maximum": 1 },
    "decay_score": { "type": "number", "minimum": 0, "maximum": 1 },
    "tags": { "type": "array", "items": { "type": "string" } },
    "content": { "type": "string", "minLength": 1 },
    "ttl_expires_at": { "type": "string", "format": "date-time" },
    "close_validity": { "type": "boolean", "default": false }
  }
}`

func updateMemoryTool() mcp.Tool {
	return mcp.NewToolWithRawSchema("update_memory",
		"Update mutable attributes. Content edits trigger supersession (new version, old validity closed). Prefer saving a new memory and linking supersedes when lineage matters.",
		json.RawMessage(updateMemorySchema))
}

func (s *Server) handleUpdateMemory(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := argPayload(req.GetArguments())
	id, verr := argUUID(args, "memory_id")
	if verr != nil {
		return errorResult(verr), nil
	}
	expected := argInt(args, "expected_version", 0)
	if expected < 1 {
		return validationError("expected_version is required (>= 1)"), nil
	}

	current, err := s.memories.GetMemory(ctx, id)
	if err != nil {
		return errorResult(err), nil
	}
	if current.Version != expected {
		return errorResult(&domain.Error{
			Code:    domain.CodeVersionConflict,
			Message: fmt.Sprintf("memory %s is at version %d, expected_version was %d", id, current.Version, expected),
		}), nil
	}
	if v := argFloatPtr(args, "confidence"); v != nil {
		current.Confidence = v
	}
	if v := argFloatPtr(args, "decay_score"); v != nil {
		current.DecayScore = v
	}
	if tags := argStrings(args, "tags"); tags != nil {
		current.Tags = tags
	}
	if c := argString(args, "content", ""); c != "" {
		current.Content = c
	}
	if t, err := argTimePtr(args, "ttl_expires_at"); err != nil {
		return errorResult(err), nil
	} else if t != nil {
		current.TTLExpiresAt = t
	}
	if argBool(args, "close_validity", false) {
		now := current.UpdatedAt
		current.ValidUntil = &now
	}

	updated, err := s.memories.UpdateMemory(withServicePrincipal(ctx, principalFor(ctx, req.Header)), current)
	if err != nil {
		return errorResult(err), nil
	}
	return okResult(map[string]any{"memory": updated},
		fmt.Sprintf("updated memory %s (v%d)", updated.ID, updated.Version)), nil
}

// ---------------------------------------------------------------------------
// delete_memory — §2.1 + §4.3: expire-only. purge is rejected (asymmetry #2).
// ---------------------------------------------------------------------------

const deleteMemorySchema = `{
  "type": "object",
  "additionalProperties": false,
  "required": ["memory_id"],
  "properties": {
    "memory_id": { "type": "string", "format": "uuid" },
    "mode": { "enum": ["expire"], "default": "expire" },
    "reason": { "type": "string" }
  }
}`

func deleteMemoryTool() mcp.Tool {
	return mcp.NewToolWithRawSchema("delete_memory",
		"Soft-expire a memory (closes its validity interval; retained for audit). Hard purge is a REST-admin-only operation, never available over MCP.",
		json.RawMessage(deleteMemorySchema))
}

func (s *Server) handleDeleteMemory(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := argPayload(req.GetArguments())
	if mode := argString(args, "mode", "expire"); mode != "expire" {
		return errorResult(&domain.Error{
			Code:    domain.CodePurgeForbidden,
			Message: "MCP delete_memory is expire-only; mode=purge requires elevated REST admin context (§4.3)",
		}), nil
	}
	id, verr := argUUID(args, "memory_id")
	if verr != nil {
		return errorResult(verr), nil
	}
	if err := s.memories.DeleteMemory(ctx, id); err != nil {
		return errorResult(err), nil
	}
	return okResult(map[string]any{
		"memory_id": id,
		"mode":      "expire",
		"purged":    false,
	}, "expired memory "+id.String()), nil
}

// ---------------------------------------------------------------------------
// link_memories — §2.1
// ---------------------------------------------------------------------------

const linkMemoriesSchema = `{
  "type": "object",
  "additionalProperties": false,
  "required": ["source_id", "target_id", "relationship_type"],
  "properties": {
    "source_id": { "type": "string", "format": "uuid" },
    "target_id": { "type": "string", "format": "uuid" },
    "target_entity_id": { "type": "string", "format": "uuid" },
    "relationship_type": { "enum": ["derived_from", "supersedes", "similar_to", "co_occurs_with", "causal_next", "anchors_entity"] },
    "weight": { "type": "number", "minimum": 0, "maximum": 1, "default": 1.0 },
    "evidence": { "type": "string" }
  }
}`

func linkMemoriesTool() mcp.Tool {
	return mcp.NewToolWithRawSchema("link_memories",
		"Create a directed relationship between two memories (or memory → entity). Edges feed the graph retrieval path.",
		json.RawMessage(linkMemoriesSchema))
}

func (s *Server) handleLinkMemories(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := argPayload(req.GetArguments())
	sourceID, verr := argUUID(args, "source_id")
	if verr != nil {
		return errorResult(verr), nil
	}
	targetID, verr := argUUID(args, "target_id")
	if verr != nil {
		return errorResult(verr), nil
	}
	rel, err := domain.ParseRelationshipType(argString(args, "relationship_type", ""))
	if err != nil {
		return validationError("relationship_type must be derived_from|supersedes|similar_to|co_occurs_with|causal_next|anchors_entity"), nil
	}
	weight := 1.0
	if w := argFloatPtr(args, "weight"); w != nil {
		weight = *w
	}

	link := &domain.MemoryLink{
		SourceID:         sourceID,
		TargetID:         targetID,
		RelationshipType: rel,
		Weight:           weight,
		Evidence:         argStringPtr(args, "evidence"),
	}
	created, err := s.memories.LinkMemories(ctx, link)
	if err != nil {
		return errorResult(err), nil
	}
	return okResult(map[string]any{"link": created},
		fmt.Sprintf("linked %s → %s (%s)", sourceID, targetID, rel)), nil
}
