// Package http implements the REST transport adapter: DTOs, middleware,
// handlers, router, and server for the api-contracts.md Part 1 surface.
package http

import (
	"time"

	"github.com/google/uuid"
)

// ---------------------------------------------------------------------------
// Memory DTOs (api-contracts §1.2.1)
// ---------------------------------------------------------------------------

// ProvenanceInput carries the client-asserted origin. agent_id/actor are
// readOnly: the server stamps them from the request principal (F21) and any
// client-supplied values are discarded.
type ProvenanceInput struct {
	Origin string `json:"origin" validate:"required,oneof=agent_observation user_instruction file_artifact consolidation"`
}

// EmbeddingInput is a client-supplied embedding; the server computes one when
// omitted.
type EmbeddingInput struct {
	Vector []float64 `json:"vector" validate:"required"`
	Model  string    `json:"model"`
	Dims   int       `json:"dims" validate:"omitempty,min=1"`
}

type SourceRefInput struct {
	Kind string  `json:"kind" validate:"required,oneof=file url tool_call message"`
	Path *string `json:"path"`
	URI  *string `json:"uri"`
	Hash *string `json:"hash"`
}

type ValidityInput struct {
	ValidFrom *time.Time `json:"valid_from"`
}

// CreateMemoryRequest is the MemoryInput schema of POST /memories.
type CreateMemoryRequest struct {
	Type           string           `json:"type" validate:"required,oneof=episodic semantic procedural"`
	Content        string           `json:"content" validate:"required,min=1"`
	ContentFormat  string           `json:"content_format" validate:"omitempty,oneof=markdown plain json"`
	Tags           []string         `json:"tags" validate:"omitempty,dive,tag_name"`
	Entities       []string         `json:"entities" validate:"omitempty,dive,uuid"`
	ExtractEntities *bool           `json:"extract_entities"`
	Embedding      *EmbeddingInput  `json:"embedding"`
	Confidence     *float64         `json:"confidence" validate:"omitempty,min=0,max=1"`
	Provenance     ProvenanceInput  `json:"provenance" validate:"required"`
	SourceRef      *SourceRefInput  `json:"source_ref"`
	SessionID      *uuid.UUID       `json:"session_id"`
	Validity       *ValidityInput   `json:"validity"`
	TTLExpiresAt   *time.Time       `json:"ttl_expires_at"`
	AccessScope    string           `json:"access_scope" validate:"omitempty,oneof=individual shared"`
	SharedSpaceID  *uuid.UUID       `json:"shared_space_id"`
}

// UpdateMemoryRequest is the MemoryUpdate schema of PUT /memories.
type UpdateMemoryRequest struct {
	ExpectedVersion *int       `json:"expected_version" validate:"omitempty,min=1"`
	Content         *string    `json:"content" validate:"omitempty,min=1"`
	ContentFormat   *string    `json:"content_format" validate:"omitempty,oneof=markdown plain json"`
	Confidence      *float64   `json:"confidence" validate:"omitempty,min=0,max=1"`
	DecayScore      *float64   `json:"decay_score" validate:"omitempty,min=0,max=1"`
	Tags            []string   `json:"tags"`
	TTLExpiresAt    *time.Time `json:"ttl_expires_at"`
	CloseValidity   bool       `json:"close_validity"`
}

// CreateLinkRequest is the MemoryLinkInput schema of POST /memories/{id}/links.
type CreateLinkRequest struct {
	TargetID         uuid.UUID `json:"target_id" validate:"required"`
	TargetEntityID   *uuid.UUID `json:"target_entity_id"`
	RelationshipType string    `json:"relationship_type" validate:"required,oneof=derived_from supersedes similar_to co_occurs_with causal_next anchors_entity"`
	Weight           *float64  `json:"weight" validate:"omitempty,min=0,max=1"`
	Evidence         *string   `json:"evidence"`
}

// ---------------------------------------------------------------------------
// Recall DTOs (§1.2.2)
// ---------------------------------------------------------------------------

type TimeBounds struct {
	From *time.Time `json:"from"`
	To   *time.Time `json:"to"`
}

type RecallContextInput struct {
	Entities       []string    `json:"entities"`
	TaskSignature  *string     `json:"task_signature"`
	TimeBounds     *TimeBounds `json:"time_bounds"`
	MentionedFiles []string    `json:"mentioned_files"`
}

type RetrievalParamsInput struct {
	Strategies  []string  `json:"strategies" validate:"required,min=1,dive,oneof=vector bm25 graph temporal"`
	TopK        *int      `json:"top_k" validate:"omitempty,min=1,max=200"`
	Rerank      *string   `json:"rerank" validate:"omitempty,oneof=cross_encoder none"`
	MinScore    *float64  `json:"min_score" validate:"omitempty,min=0,max=1"`
	SlotBudget  *int      `json:"slot_budget" validate:"omitempty,min=0"`
	TokenBudget *int      `json:"token_budget" validate:"omitempty,min=0"`
}

// RecallRequestDTO is the RecallSubmit schema of POST /recall.
type RecallRequestDTO struct {
	Query           string                `json:"query" validate:"required,min=1"`
	Context         RecallContextInput    `json:"context"`
	Trigger         string                `json:"trigger" validate:"required,oneof=task_context user_query temporal associative session_start"`
	AgentID         string                `json:"agent_id" validate:"required"`
	SessionID       uuid.UUID             `json:"session_id" validate:"required"`
	RetrievalParams RetrievalParamsInput  `json:"retrieval_params" validate:"required"`
	Mode            string                `json:"mode" validate:"omitempty,oneof=sync async"`
}

// ---------------------------------------------------------------------------
// Session DTOs (§1.2.3)
// ---------------------------------------------------------------------------

type ContextWindowInput struct {
	Model                 string `json:"model" validate:"required"`
	MaxTokens             int    `json:"max_tokens" validate:"required,min=1"`
	InstructionSlotBudget int    `json:"instruction_slot_budget" validate:"min=0"`
}

// CreateSessionRequest is the SessionInput schema of POST /sessions.
type CreateSessionRequest struct {
	AgentType     string             `json:"agent_type" validate:"required,oneof=claude-code codex cursor letta custom"`
	UserID        string             `json:"user_id" validate:"required,min=1"`
	SharedSpaceID *uuid.UUID         `json:"shared_space_id"`
	ContextWindow ContextWindowInput `json:"context_window" validate:"required"`
	Bootstrap     *bool              `json:"bootstrap"`
}

// ActivateMemoryRequest is the body of POST /sessions/{id}/memories.
type ActivateMemoryRequest struct {
	MemoryID uuid.UUID `json:"memory_id" validate:"required"`
	Position *int      `json:"position"`
	Force    bool      `json:"force"`
}

// EndSessionRequest is the body of POST /sessions/{id}/end.
type EndSessionRequest struct {
	Summary     *string `json:"summary"`
	Consolidate *bool   `json:"consolidate"`
}

// ---------------------------------------------------------------------------
// Space DTOs (§1.2.4)
// ---------------------------------------------------------------------------

type ParticipantInput struct {
	PrincipalType string `json:"principal_type" validate:"required,oneof=user agent session group"`
	PrincipalID   string `json:"principal_id" validate:"required,min=1"`
	AccessLevel   string `json:"access_level" validate:"required,oneof=read write promote admin"`
}

type AccessPolicyInput struct {
	DefaultAccess string `json:"default_access" validate:"required,oneof=read write none"`
	Write         string `json:"write" validate:"required,oneof=owner_approved participant_free proposal_only"`
	Promote       string `json:"promote" validate:"required,oneof=human_review auto"`
}

type StorageBackendInput struct {
	Kind      string  `json:"kind" validate:"required,oneof=files relational vector graph hybrid"`
	ConfigRef *string `json:"config_ref"`
}

type RetentionPolicyInput struct {
	SupersedeNotDelete *bool `json:"supersede_not_delete"`
	TTLDays            *int  `json:"ttl_days" validate:"omitempty,min=1"`
	ArchiveAfterDays   *int  `json:"archive_after_days" validate:"omitempty,min=1"`
}

// CreateSpaceRequest is the SpaceInput schema of POST /spaces.
type CreateSpaceRequest struct {
	Name            string                 `json:"name" validate:"required,min=1"`
	Description     *string                `json:"description"`
	OwnerType       string                 `json:"owner_type" validate:"required,oneof=user agent team organization"`
	OwnerID         string                 `json:"owner_id" validate:"required,min=1"`
	Scope           string                 `json:"scope" validate:"required,min=1"`
	Participants    []ParticipantInput     `json:"participants"`
	AccessPolicy    *AccessPolicyInput     `json:"access_policy"`
	StorageBackend  *StorageBackendInput   `json:"storage_backend"`
	RetentionPolicy *RetentionPolicyInput  `json:"retention_policy"`
}

// UpdateSpaceRequest is the SpaceUpdate schema of PUT /spaces/{id}.
type UpdateSpaceRequest struct {
	Description        *string             `json:"description"`
	AccessPolicy       *AccessPolicyInput  `json:"access_policy"`
	RetentionPolicy    *RetentionPolicyInput `json:"retention_policy"`
	ParticipantsAppend []ParticipantInput  `json:"participants_append"`
	ParticipantsRemove []string            `json:"participants_remove"`
}

// PromoteMemoryRequest is the PromotionInput schema of POST /spaces/{id}/memories.
type PromoteMemoryRequest struct {
	MemoryID        uuid.UUID          `json:"memory_id" validate:"required"`
	TargetPath      string             `json:"target_path"`
	TargetKind      string             `json:"target_kind" validate:"omitempty,oneof=spec rule agent_doc memory_doc"`
	TargetRole      string             `json:"target_role" validate:"omitempty,oneof=procedural semantic episodic"`
	Note            *string            `json:"note"`
}

// ReviewProposalRequest is the approve/reject body.
type ReviewProposalRequest struct {
	Reviewer string  `json:"reviewer" validate:"omitempty,min=1"`
	Note     *string `json:"note"`
	Reason   *string `json:"reason"`
}

// SyncSpaceRequest is the body of POST /spaces/{id}/sync.
type SyncSpaceRequest struct {
	Direction string `json:"direction" validate:"omitempty,oneof=pull push both"`
}

// ---------------------------------------------------------------------------
// Lifecycle DTOs (§1.2.5)
// ---------------------------------------------------------------------------

type JobScope struct {
	Kind string  `json:"kind" validate:"required,oneof=session user space all"`
	ID   *string `json:"id"`
}

// ConsolidateRequest is the ConsolidateInput schema.
type ConsolidateRequest struct {
	Scope       JobScope  `json:"scope" validate:"required"`
	MemoryTypes []string  `json:"memory_types" validate:"omitempty,dive,oneof=episodic semantic procedural"`
	DryRun      bool      `json:"dry_run"`
}

// DecayRequest is the decay trigger body.
type DecayRequest struct {
	Scope        JobScope `json:"scope" validate:"required"`
	DecayProfile string   `json:"decay_profile" validate:"omitempty,oneof=standard aggressive"`
	MinAgeDays   *int     `json:"min_age_days" validate:"omitempty,min=0"`
}

// ---------------------------------------------------------------------------
// Response envelopes
// ---------------------------------------------------------------------------

// PageResponse is the §1.1 pagination envelope shared by all list endpoints.
type PageResponse struct {
	Items         []any  `json:"items"`
	NextCursor    string `json:"next_cursor"`
	HasMore       bool   `json:"has_more"`
	TotalEstimate *int   `json:"total_estimate"`
}

// JobRef is the async-job pointer returned by 202 responses.
type JobRef struct {
	JobID   uuid.UUID `json:"job_id"`
	Kind    string    `json:"kind"`
	Status  string    `json:"status"`
	PollURL string    `json:"poll_url"`
}

// CreateSessionResponse is the POST /sessions 201 body.
type CreateSessionResponse struct {
	Session       any                   `json:"session"`
	BootstrapPlan []InjectionPlanItemDTO `json:"bootstrap_plan"`
}

type InjectionPlanItemDTO struct {
	MemoryID uuid.UUID `json:"memory_id"`
	Position int       `json:"position"`
	SlotCost int       `json:"slot_cost"`
}

// SessionUsage is the GET /sessions/{id} usage block.
type SessionUsage struct {
	TokensRemaining int `json:"tokens_remaining"`
	SlotsRemaining  int `json:"slots_remaining"`
}

// GetSessionResponse is the GET /sessions/{id} 200 body.
type GetSessionResponse struct {
	Session any          `json:"session"`
	Usage   SessionUsage `json:"usage"`
}

// ActivateMemoryResponse is the POST /sessions/{id}/memories 200 body.
type ActivateMemoryResponse struct {
	Session   any                 `json:"session"`
	Injection InjectionPlanItemDTO `json:"injection"`
}

// EndSessionResponse is the POST /sessions/{id}/end 202 body.
type EndSessionResponse struct {
	Session          any    `json:"session"`
	ConsolidationJob *JobRef `json:"consolidation_job"`
}

// SyncSpaceResponse is the POST /spaces/{id}/sync 202 body.
type SyncSpaceResponse struct {
	Job     JobRef   `json:"job"`
	SpaceID uuid.UUID `json:"space_id"`
}

// JobResponse is the POST /lifecycle/{consolidate,decay} 202 body.
type JobResponse struct {
	Job JobRef `json:"job"`
}

// RecallSyncResponse is the POST /recall sync 200 body.
type RecallSyncResponse struct {
	Request any `json:"request"`
	Result  any `json:"result"`
}

// RecallAsyncResponse is the POST /recall async 202 body.
type RecallAsyncResponse struct {
	Request     any    `json:"request"`
	Result      any    `json:"result"`
	Status      string `json:"status"`
	PollAfterMS int    `json:"poll_after_ms"`
}

// RecallStatusResponse is the GET /recall/{request_id} 200 body.
type RecallStatusResponse struct {
	Request any    `json:"request"`
	Status  string `json:"status"`
	Result  any    `json:"result"`
	Error   any    `json:"error"`
}
