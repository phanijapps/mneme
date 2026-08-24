package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/phanijapps/mneme/internal/domain"
)

// SharedMemorySpace tools. No update_space tool exists over MCP: space-policy
// mutation is deliberately REST-admin-only (§3.2 asymmetry #1).

// ---------------------------------------------------------------------------
// create_space — SpaceInput (§1.2.4)
// ---------------------------------------------------------------------------

const createSpaceSchema = `{
  "type": "object",
  "additionalProperties": false,
  "required": ["name", "owner_type", "owner_id", "scope", "access_policy", "storage_backend"],
  "properties": {
    "name": { "type": "string", "minLength": 1 },
    "description": { "type": "string" },
    "owner_type": { "enum": ["user", "agent", "team", "organization"] },
    "owner_id": { "type": "string", "minLength": 1 },
    "scope": { "type": "string", "minLength": 1 },
    "participants": { "type": "array", "items": { "type": "string", "format": "uuid" } },
    "access_policy": {
      "type": "object",
      "required": ["default_access", "write", "promote"],
      "properties": {
        "default_access": { "enum": ["read", "write", "none"] },
        "write": { "enum": ["owner_approved", "participant_free", "proposal_only"] },
        "promote": { "enum": ["human_review", "auto"] }
      }
    },
    "storage_backend": {
      "type": "object",
      "required": ["kind"],
      "properties": {
        "kind": { "enum": ["files", "relational", "vector", "graph", "hybrid"] },
        "config_ref": { "type": "string" }
      }
    },
    "retention_policy": {
      "type": "object",
      "properties": {
        "supersede_not_delete": { "type": "boolean" },
        "ttl_days": { "type": "integer", "minimum": 0 },
        "archive_after_days": { "type": "integer", "minimum": 0 }
      }
    }
  }
}`

func createSpaceTool() mcp.Tool {
	return mcp.NewToolWithRawSchema("create_space",
		"Create a shared memory space with its access policy and storage backend.",
		json.RawMessage(createSpaceSchema))
}

func (s *Server) handleCreateSpace(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := argPayload(req.GetArguments())
	ownerType, err := domain.ParseSpaceOwnerType(argString(args, "owner_type", ""))
	if err != nil {
		return validationError("owner_type must be user|agent|team|organization"), nil
	}
	name := argString(args, "name", "")
	ownerID := argString(args, "owner_id", "")
	scope := argString(args, "scope", "")
	if name == "" || ownerID == "" || scope == "" {
		return validationError("name, owner_id, and scope are required"), nil
	}

	space := &domain.SharedMemorySpace{
		Name:        name,
		Description: argStringPtr(args, "description"),
		OwnerType:   ownerType,
		OwnerID:     ownerID,
		Scope:       scope,
	}

	if apRaw, ok := args["access_policy"].(map[string]any); ok {
		da, err := domain.ParseDefaultAccess(argString(apRaw, "default_access", "read"))
		if err != nil {
			return validationError("access_policy.default_access must be read|write|none"), nil
		}
		wp, err := domain.ParseWritePolicy(argString(apRaw, "write", "owner_approved"))
		if err != nil {
			return validationError("access_policy.write must be owner_approved|participant_free|proposal_only"), nil
		}
		pp, err := domain.ParsePromotePolicy(argString(apRaw, "promote", "human_review"))
		if err != nil {
			return validationError("access_policy.promote must be human_review|auto"), nil
		}
		space.AccessPolicy = domain.AccessPolicy{DefaultAccess: da, WritePolicy: wp, PromotePolicy: pp}
	}
	if sbRaw, ok := args["storage_backend"].(map[string]any); ok {
		kind, err := domain.ParseStorageBackendType(argString(sbRaw, "kind", ""))
		if err != nil {
			return validationError("storage_backend.kind must be files|relational|vector|graph|hybrid"), nil
		}
		space.StorageBackend = domain.StorageBackend{Kind: kind, ConfigRef: argStringPtr(sbRaw, "config_ref")}
	}
	if rpRaw, ok := args["retention_policy"].(map[string]any); ok {
		rp := &domain.RetentionPolicy{}
		if v, ok := rpRaw["supersede_not_delete"].(bool); ok {
			rp.SupersedeNotDelete = &v
		}
		if v := argIntPtr(rpRaw, "ttl_days"); v != nil {
			rp.TTLDays = v
		}
		if v := argIntPtr(rpRaw, "archive_after_days"); v != nil {
			rp.ArchiveAfterDays = v
		}
		space.RetentionPolicy = rp
	}

	created, err := s.spaces.CreateSpace(ctx, space)
	if err != nil {
		return errorResult(err), nil
	}
	return okResult(map[string]any{"space": created},
		fmt.Sprintf("created space %s (%s)", created.ID, created.Name)), nil
}

// ---------------------------------------------------------------------------
// list_spaces
// ---------------------------------------------------------------------------

const listSpacesSchema = `{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "owner_type": { "enum": ["user", "agent", "team", "organization"] },
    "scope": { "type": "array", "items": { "type": "string" } },
    "batch_size": { "type": "integer", "minimum": 1, "maximum": 50, "default": 20 },
    "cursor": { "type": "string" }
  }
}`

func listSpacesTool() mcp.Tool {
	return mcp.NewToolWithRawSchema("list_spaces",
		"List spaces visible to the connection principal. Use to discover which shared memories an agent may read before recalling from them.",
		json.RawMessage(listSpacesSchema))
}

func (s *Server) handleListSpaces(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	p := principalFor(ctx, req.Header)
	args := argPayload(req.GetArguments())

	ptype := p.Type
	if ptype == "" {
		ptype = domain.PrincipalAgent
	}
	if ot := argString(args, "owner_type", ""); ot != "" {
		t, err := domain.ParseSpaceOwnerType(ot)
		if err != nil {
			return validationError("owner_type must be user|agent|team|organization"), nil
		}
		ptype = domain.PrincipalType(t)
	}
	batch := argInt(args, "batch_size", 20)
	if batch < 1 || batch > 50 {
		return validationError("batch_size must be within 1..50 (MCP cap)"), nil
	}

	all, err := s.spaces.ListSpaces(ctx, ptype, p.ActorOrAgent())
	if err != nil {
		return errorResult(err), nil
	}
	start := 0
	if cursor := argString(args, "cursor", ""); cursor != "" {
		for i, sp := range all {
			if sp.ID.String() == cursor {
				start = i + 1
				break
			}
		}
	}
	end := start + batch
	if end > len(all) {
		end = len(all)
	}
	page := all[start:end]
	hasMore := end < len(all)
	out := map[string]any{"items": page, "has_more": hasMore}
	if hasMore {
		out["next_cursor"] = page[len(page)-1].ID
	}
	return okResult(out, fmt.Sprintf("%d spaces (has_more=%v)", len(page), hasMore)), nil
}

// ---------------------------------------------------------------------------
// get_space
// ---------------------------------------------------------------------------

const getSpaceSchema = `{
  "type": "object",
  "additionalProperties": false,
  "required": ["space_id"],
  "properties": {
    "space_id": { "type": "string", "format": "uuid" }
  }
}`

func getSpaceTool() mcp.Tool {
	return mcp.NewToolWithRawSchema("get_space",
		"Get full space details — participants, access policy, artifacts manifest, sync state.",
		json.RawMessage(getSpaceSchema))
}

func (s *Server) handleGetSpace(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	id, verr := argUUID(argPayload(req.GetArguments()), "space_id")
	if verr != nil {
		return errorResult(verr), nil
	}
	space, err := s.spaces.GetSpace(ctx, id)
	if err != nil {
		return errorResult(err), nil
	}
	return okResult(map[string]any{"space": space}, fmt.Sprintf("space %s", space.ID)), nil
}

// ---------------------------------------------------------------------------
// promote_memory
// ---------------------------------------------------------------------------

const promoteMemorySchema = `{
  "type": "object",
  "additionalProperties": false,
  "required": ["memory_id", "space_id"],
  "properties": {
    "memory_id": { "type": "string", "format": "uuid" },
    "space_id": { "type": "string", "format": "uuid" },
    "target_artifact": {
      "type": "object",
      "properties": {
        "path": { "type": "string" },
        "kind": { "enum": ["spec", "rule", "agent_doc", "memory_doc"] },
        "role": { "enum": ["procedural", "semantic", "episodic"] }
      }
    },
    "note": { "type": "string" }
  }
}`

func promoteMemoryTool() mcp.Tool {
	return mcp.NewToolWithRawSchema("promote_memory",
		"Propose promoting an individual memory into a shared space. Creates a PromotionProposal with a reviewable diff. Under promote: auto the proposal merges immediately (recorded for audit).",
		json.RawMessage(promoteMemorySchema))
}

func (s *Server) handlePromoteMemory(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	p := principalFor(ctx, req.Header)
	args := argPayload(req.GetArguments())
	memoryID, verr := argUUID(args, "memory_id")
	if verr != nil {
		return errorResult(verr), nil
	}
	spaceID, verr := argUUID(args, "space_id")
	if verr != nil {
		return errorResult(verr), nil
	}

	proposal := &domain.PromotionProposal{
		CandidateMemoryID: memoryID,
		SharedSpaceID:     spaceID,
		TargetPath:        "memories/" + memoryID.String() + ".md",
		TargetKind:        domain.ProposalTargetMemoryDoc,
		TargetRole:        domain.ProposalRoleSemantic,
		Note:              argStringPtr(args, "note"),
	}
	if taRaw, ok := args["target_artifact"].(map[string]any); ok {
		if path := argString(taRaw, "path", ""); path != "" {
			proposal.TargetPath = path
		}
		if k := argString(taRaw, "kind", ""); k != "" {
			tk, err := domain.ParseProposalTargetKind(k)
			if err != nil {
				return validationError("target_artifact.kind must be spec|rule|agent_doc|memory_doc"), nil
			}
			proposal.TargetKind = tk
		}
		if r := argString(taRaw, "role", ""); r != "" {
			tr, err := domain.ParseProposalTargetRole(r)
			if err != nil {
				return validationError("target_artifact.role must be procedural|semantic|episodic"), nil
			}
			proposal.TargetRole = tr
		}
	}
	proposal.Diff = "+++ " + proposal.TargetPath + "\n+ promoted memory " + memoryID.String() + "\n"

	created, err := s.spaces.PromoteMemory(withServicePrincipal(ctx, p), proposal)
	if err != nil {
		return errorResult(err), nil
	}
	return okResult(map[string]any{"proposal": created},
		fmt.Sprintf("proposal %s (%s)", created.ID, created.Status)), nil
}

// ---------------------------------------------------------------------------
// review_proposals
// ---------------------------------------------------------------------------

const reviewProposalsSchema = `{
  "type": "object",
  "additionalProperties": false,
  "required": ["space_id"],
  "properties": {
    "space_id": { "type": "string", "format": "uuid" },
    "status": { "enum": ["draft", "in_review", "merged", "rejected"], "default": "in_review" },
    "batch_size": { "type": "integer", "minimum": 1, "maximum": 50, "default": 20 },
    "cursor": { "type": "string" }
  }
}`

func reviewProposalsTool() mcp.Tool {
	return mcp.NewToolWithRawSchema("review_proposals",
		"List proposals for a space (default: pending). Use by reviewing agents/humans to see what awaits decision.",
		json.RawMessage(reviewProposalsSchema))
}

func (s *Server) handleReviewProposals(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := argPayload(req.GetArguments())
	spaceID, verr := argUUID(args, "space_id")
	if verr != nil {
		return errorResult(verr), nil
	}
	batch := argInt(args, "batch_size", 20)
	if batch < 1 || batch > 50 {
		return validationError("batch_size must be within 1..50 (MCP cap)"), nil
	}

	var status *domain.ProposalStatus
	if st := argString(args, "status", "in_review"); st != "" {
		ps, err := domain.ParseProposalStatus(st)
		if err != nil {
			return validationError("status must be draft|in_review|merged|rejected"), nil
		}
		status = &ps
	}
	proposals, err := s.spaces.ReviewProposals(ctx, spaceID, status)
	if err != nil {
		return errorResult(err), nil
	}
	start := 0
	if cursor := argString(args, "cursor", ""); cursor != "" {
		for i, pr := range proposals {
			if pr.ID.String() == cursor {
				start = i + 1
				break
			}
		}
	}
	end := start + batch
	if end > len(proposals) {
		end = len(proposals)
	}
	page := proposals[start:end]
	hasMore := end < len(proposals)
	out := map[string]any{"items": page, "has_more": hasMore}
	if hasMore {
		out["next_cursor"] = page[len(page)-1].ID
	}
	return okResult(out, fmt.Sprintf("%d proposals (has_more=%v)", len(page), hasMore)), nil
}

// ---------------------------------------------------------------------------
// approve_proposal / reject_proposal — reviewer comes from connection
// context, never from input (F21).
// ---------------------------------------------------------------------------

const approveProposalSchema = `{
  "type": "object",
  "additionalProperties": false,
  "required": ["proposal_id"],
  "properties": {
    "proposal_id": { "type": "string", "format": "uuid" },
    "space_id": { "type": "string", "format": "uuid", "description": "optional: space inferred from the proposal when omitted" },
    "note": { "type": "string" }
  }
}`

func approveProposalTool() mcp.Tool {
	return mcp.NewToolWithRawSchema("approve_proposal",
		"Approve a promotion: merges the diff, flips the memory to access_scope: shared, bumps the space revision, notifies active agents. Requires promote/admin in the connection context.",
		json.RawMessage(approveProposalSchema))
}

func (s *Server) handleApproveProposal(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	p := principalFor(ctx, req.Header)
	args := argPayload(req.GetArguments())
	proposalID, verr := argUUID(args, "proposal_id")
	if verr != nil {
		return errorResult(verr), nil
	}
	spaceID, err := argUUIDPtr(args, "space_id")
	if err != nil {
		return errorResult(err), nil
	}
	if spaceID == nil {
		id, err := s.resolveProposalSpace(ctx, proposalID)
		if err != nil {
			return errorResult(err), nil
		}
		spaceID = &id
	}

	// Reviewer is server-stamped from the connection principal (unforgeable).
	reviewer := p.Actor
	if reviewer == "" {
		reviewer = p.AgentID
	}
	proposal, err := s.spaces.ApproveProposal(ctx, *spaceID, proposalID, reviewer)
	if err != nil {
		return errorResult(err), nil
	}
	return okResult(map[string]any{"proposal": proposal},
		fmt.Sprintf("proposal %s merged", proposalID)), nil
}

const rejectProposalSchema = `{
  "type": "object",
  "additionalProperties": false,
  "required": ["proposal_id", "reason"],
  "properties": {
    "proposal_id": { "type": "string", "format": "uuid" },
    "space_id": { "type": "string", "format": "uuid", "description": "optional: space inferred from the proposal when omitted" },
    "reason": { "type": "string", "minLength": 1 }
  }
}`

func rejectProposalTool() mcp.Tool {
	return mcp.NewToolWithRawSchema("reject_proposal",
		"Reject a promotion with a mandatory reason (audit trail).",
		json.RawMessage(rejectProposalSchema))
}

func (s *Server) handleRejectProposal(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	p := principalFor(ctx, req.Header)
	args := argPayload(req.GetArguments())
	proposalID, verr := argUUID(args, "proposal_id")
	if verr != nil {
		return errorResult(verr), nil
	}
	reason := argString(args, "reason", "")
	if reason == "" {
		return validationError("reason is required (minLength 1)"), nil
	}
	spaceID, err := argUUIDPtr(args, "space_id")
	if err != nil {
		return errorResult(err), nil
	}
	if spaceID == nil {
		id, err := s.resolveProposalSpace(ctx, proposalID)
		if err != nil {
			return errorResult(err), nil
		}
		spaceID = &id
	}

	reviewer := p.Actor
	if reviewer == "" {
		reviewer = p.AgentID
	}
	proposal, err := s.spaces.RejectProposal(ctx, *spaceID, proposalID, reviewer, reason)
	if err != nil {
		return errorResult(err), nil
	}
	return okResult(map[string]any{"proposal": proposal},
		fmt.Sprintf("proposal %s rejected", proposalID)), nil
}

// resolveProposalSpace infers space_id from the proposal when the caller
// omits it (§2.4: "space_id inferred from the proposal"). It uses the
// review_proposals listing of the space-agnostic path; when no proposal
// repo view is available it reports PROPOSAL_NOT_FOUND.
func (s *Server) resolveProposalSpace(ctx context.Context, proposalID uuid.UUID) (uuid.UUID, error) {
	if s.findProposal != nil {
		if sp, ok := s.findProposal(ctx, proposalID); ok {
			return sp, nil
		}
	}
	return uuid.Nil, &domain.Error{
		Code:    domain.CodeProposalNotFound,
		Message: "proposal " + proposalID.String() + " not found; pass space_id explicitly",
	}
}

// ---------------------------------------------------------------------------
// sync_space
// ---------------------------------------------------------------------------

const syncSpaceSchema = `{
  "type": "object",
  "additionalProperties": false,
  "required": ["space_id"],
  "properties": {
    "space_id": { "type": "string", "format": "uuid" },
    "direction": { "enum": ["pull", "push", "both"], "default": "both" },
    "job_id": { "type": "string", "format": "uuid", "description": "poll mode: pass a previously returned job id" }
  }
}`

func syncSpaceTool() mcp.Tool {
	return mcp.NewToolWithRawSchema("sync_space",
		"Trigger backend sync for a space (git or graph/vector epoch). Async: returns a job; call sync_space again with job_id to poll.",
		json.RawMessage(syncSpaceSchema))
}

func (s *Server) handleSyncSpace(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := argPayload(req.GetArguments())

	// Poll mode: job_id given without submitting a new sync.
	if raw := argString(args, "job_id", ""); raw != "" {
		jobID, err := uuid.Parse(raw)
		if err != nil {
			return validationError("job_id: not a uuid"), nil
		}
		job, err := s.lifecycle.GetJob(ctx, jobID)
		if err != nil {
			return errorResult(err), nil
		}
		return okResult(map[string]any{"job": job, "space_id": job.ScopeID}, fmt.Sprintf("job %s: %s", jobID, job.Status)), nil
	}

	spaceID, verr := argUUID(args, "space_id")
	if verr != nil {
		return errorResult(verr), nil
	}
	job, err := s.spaces.SyncSpace(ctx, spaceID)
	if err != nil {
		return errorResult(err), nil
	}
	return okResult(map[string]any{"job": job, "space_id": spaceID},
		fmt.Sprintf("sync job %s queued for space %s", job.JobID, spaceID)), nil
}
