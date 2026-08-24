package domain

import (
	"time"

	"github.com/google/uuid"
)

// SharedMemorySpace is an access-controlled shared space (data-models §3.5).
// artifacts is a manifest of externalized reviewable representations, not a
// membership list; membership lives in SpaceMembership rows.
type SharedMemorySpace struct {
	ID              uuid.UUID        `json:"id" validate:"required"`
	Name            string           `json:"name" validate:"required,min=1"`
	Description     *string          `json:"description,omitempty"`
	OwnerType       SpaceOwnerType   `json:"owner_type" validate:"required"`
	OwnerID         string           `json:"owner_id" validate:"required"`
	Scope           string           `json:"scope" validate:"required"`
	AccessPolicy    AccessPolicy     `json:"access_policy"`
	StorageBackend  StorageBackend   `json:"storage_backend"`
	Artifacts       []Artifact       `json:"artifacts,omitempty"`
	SyncState       SyncState        `json:"sync_state"`
	LastSyncedAt    time.Time        `json:"last_synced_at"`
	RetentionPolicy *RetentionPolicy `json:"retention_policy,omitempty"`
	CreatedAt       time.Time        `json:"created_at" validate:"required"`
	UpdatedAt       time.Time        `json:"updated_at" validate:"required"`
}

// AccessPolicy is the generic access control of a space; shared memory is
// review-gated by default.
type AccessPolicy struct {
	DefaultAccess DefaultAccess `json:"default_access"`
	WritePolicy   WritePolicy   `json:"write"`
	PromotePolicy PromotePolicy `json:"promote"`
}

// StorageBackend names the backend class realizing the space plus optional
// config reference (backend_kind enum column + backend_config JSONB).
type StorageBackend struct {
	Kind      StorageBackendType `json:"kind" validate:"required"`
	ConfigRef *string            `json:"config_ref,omitempty"`
}

// SyncState is the backend-neutral replication state of a space.
type SyncState struct {
	Status           SyncStatus `json:"status"`
	PendingProposals int        `json:"pending_proposals"`
	Revision         *string    `json:"revision,omitempty"` // sync_revision
}

// Artifact is one externalized reviewable representation, populated only
// when the backend includes files.
type Artifact struct {
	URI      string  `json:"uri" validate:"required"`
	Kind     string  `json:"kind" validate:"required"`
	Role     string  `json:"role" validate:"required"`
	Revision *string `json:"revision,omitempty"`
	Hash     *string `json:"hash,omitempty"`
}

// RetentionPolicy mirrors the lifecycle rules: supersession-first by default.
type RetentionPolicy struct {
	SupersedeNotDelete *bool `json:"supersede_not_delete,omitempty"` // default true
	TTLDays            *int  `json:"ttl_days,omitempty"`
	ArchiveAfterDays   *int  `json:"archive_after_days,omitempty"`
}

// Validate checks name, enums, sub-structs, and cross-field policy rules.
func (s *SharedMemorySpace) Validate() error {
	if s.Name == "" {
		return NewValidationError("SharedMemorySpace.Name", s.Name, "non-empty")
	}
	if !s.OwnerType.Valid() {
		return NewValidationError("SharedMemorySpace.OwnerType", s.OwnerType.String(),
			"user|agent|team|organization")
	}
	if s.OwnerID == "" {
		return NewValidationError("SharedMemorySpace.OwnerID", s.OwnerID, "non-empty")
	}
	if s.Scope == "" {
		return NewValidationError("SharedMemorySpace.Scope", s.Scope, "non-empty")
	}
	ap := s.AccessPolicy
	if !ap.DefaultAccess.Valid() {
		return NewValidationError("AccessPolicy.DefaultAccess", ap.DefaultAccess.String(), "read|write|none")
	}
	if !ap.WritePolicy.Valid() {
		return NewValidationError("AccessPolicy.WritePolicy", ap.WritePolicy.String(),
			"owner_approved|participant_free|proposal_only")
	}
	if !ap.PromotePolicy.Valid() {
		return NewValidationError("AccessPolicy.PromotePolicy", ap.PromotePolicy.String(),
			"human_review|auto")
	}
	if !s.StorageBackend.Kind.Valid() {
		return NewValidationError("StorageBackend.Kind", s.StorageBackend.Kind.String(),
			"files|relational|vector|graph|hybrid")
	}
	if !s.SyncState.Status.Valid() {
		return NewValidationError("SyncState.Status", s.SyncState.Status.String(),
			"in_sync|pending_review|diverged|offline")
	}
	if s.SyncState.PendingProposals < 0 {
		return NewValidationError("SyncState.PendingProposals",
			intString(s.SyncState.PendingProposals), ">= 0")
	}
	// SPACE_POLICY_INVALID: promote=auto with write=proposal_only and no
	// reviewers configured is contradictory (api-contracts §4.6).
	if ap.PromotePolicy == PromoteAuto && ap.WritePolicy == WriteProposalOnly {
		return NewValidationError("AccessPolicy", "promote=auto, write=proposal_only",
			"auto-promotion requires a write policy other than proposal_only")
	}
	return nil
}

// SpaceMembership is a junction row granting a principal access to a space
// (pgvector-data-model §3.5). principal_id has no FK — principals are external.
type SpaceMembership struct {
	SpaceID       uuid.UUID     `json:"space_id" validate:"required"`
	PrincipalType PrincipalType `json:"principal_type" validate:"required"`
	PrincipalID   string        `json:"principal_id" validate:"required"`
	AccessLevel   AccessLevel   `json:"access_level" validate:"required"`
	GrantedAt     time.Time     `json:"granted_at"`
}

// Validate checks the membership enums.
func (m *SpaceMembership) Validate() error {
	if !m.PrincipalType.Valid() {
		return NewValidationError("SpaceMembership.PrincipalType", m.PrincipalType.String(),
			"user|agent|session|group")
	}
	if !m.AccessLevel.Valid() {
		return NewValidationError("SpaceMembership.AccessLevel", m.AccessLevel.String(),
			"read|write|promote|admin")
	}
	return nil
}
