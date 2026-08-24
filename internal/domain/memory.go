package domain

import (
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/pgvector/pgvector-go"
)

// Memory is the system of record for one version of a memory record
// (data-models §3.1; pgvector-data-model §3.3). One row per version;
// supersession-first retention closes validity intervals instead of deleting.
type Memory struct {
	ID                 uuid.UUID        `json:"id" validate:"required"`
	Type               MemoryType       `json:"type" validate:"required"`
	Content            string           `json:"content" validate:"required,min=1"`
	ContentFormat      ContentFormat    `json:"content_format"`
	Tags               []string         `json:"tags,omitempty" validate:"omitempty,dive,tag_name"`
	Embedding          *pgvector.Vector `json:"-"`                         // nullable; primary-model vector
	EmbeddingModel     *string          `json:"embedding_model,omitempty"` // FK to embedding_models(model_id)
	Origin             Origin           `json:"origin" validate:"required"`
	SessionID          *uuid.UUID       `json:"session_id,omitempty"` // provenance session
	AgentID            *string          `json:"agent_id,omitempty"`   // server-stamped (F21)
	Actor              *string          `json:"actor,omitempty"`      // server-stamped (F21)
	OwnerPrincipalType PrincipalType    `json:"owner_principal_type"`
	OwnerPrincipalID   string           `json:"owner_principal_id" validate:"required"`
	SourceRef          *SourceRef       `json:"source_ref,omitempty"`
	CreatedAt          time.Time        `json:"created_at" validate:"required"`
	UpdatedAt          time.Time        `json:"updated_at" validate:"required"`
	Confidence         *float64         `json:"confidence,omitempty" validate:"omitempty,min=0,max=1"`
	DecayScore         *float64         `json:"decay_score,omitempty" validate:"omitempty,min=0,max=1"`
	ValidFrom          *time.Time       `json:"valid_from,omitempty"`
	ValidUntil         *time.Time       `json:"valid_until,omitempty"`
	SupersededBy       *uuid.UUID       `json:"superseded_by,omitempty"`
	TTLExpiresAt       *time.Time       `json:"ttl_expires_at,omitempty"`
	Version            int              `json:"version" validate:"required,min=1"`
	AccessScope        AccessScope      `json:"access_scope" validate:"required"`
	SharedSpaceID      *uuid.UUID       `json:"shared_space_id,omitempty"` // required iff scope=shared
	DeletedAt          *time.Time       `json:"deleted_at,omitempty"`      // soft delete
}

// SourceRef is file/URI provenance for memories of origin file_artifact
// (data-models §3.1 source_ref; JSONB {kind, path/uri, hash}).
type SourceRef struct {
	Kind string  `json:"kind" validate:"required"` // e.g. file, uri
	Path *string `json:"path,omitempty"`           // path XOR uri
	URI  *string `json:"uri,omitempty"`
	Hash *string `json:"hash,omitempty"` // read by sync change detection
}

// Validate checks field-level and cross-field constraints of a Memory:
// enum validity, scope↔space pairing, validity-range ordering, and TTL ordering.
func (m *Memory) Validate() error {
	if !m.Type.Valid() {
		return NewValidationError("Memory.Type", m.Type.String(), "episodic|semantic|procedural")
	}
	if !m.ContentFormat.Valid() {
		return NewValidationError("Memory.ContentFormat", m.ContentFormat.String(), "markdown|plain|json")
	}
	if !m.Origin.Valid() {
		return NewValidationError("Memory.Origin", m.Origin.String(),
			"agent_observation|user_instruction|file_artifact|consolidation")
	}
	if !m.AccessScope.Valid() {
		return NewValidationError("Memory.AccessScope", m.AccessScope.String(), "individual|shared")
	}
	if !m.OwnerPrincipalType.Valid() {
		return NewValidationError("Memory.OwnerPrincipalType", m.OwnerPrincipalType.String(),
			"user|agent|session|group")
	}
	if m.OwnerPrincipalID == "" {
		return NewValidationError("Memory.OwnerPrincipalID", m.OwnerPrincipalID, "non-empty")
	}
	if m.Version < 1 {
		return NewValidationError("Memory.Version", fmt.Sprint(m.Version), ">= 1")
	}
	if m.Content == "" || isBlank(m.Content) {
		return NewValidationError("Memory.Content", m.Content, "non-blank") // CHECK length(btrim(content)) > 0
	}
	if (m.AccessScope == AccessScopeShared) != (m.SharedSpaceID != nil) {
		return NewValidationError("Memory.SharedSpaceID", "<nil?>",
			"shared_space_id is required iff access_scope=shared")
	}
	if m.ValidFrom != nil && m.ValidUntil != nil && !m.ValidUntil.After(*m.ValidFrom) {
		return NewValidationError("Memory.ValidUntil", m.ValidUntil.String(), "> valid_from")
	}
	if m.TTLExpiresAt != nil && m.CreatedAt.IsZero() {
		return NewValidationError("Memory.TTLExpiresAt", m.TTLExpiresAt.String(), "requires created_at")
	}
	if m.TTLExpiresAt != nil && !m.TTLExpiresAt.After(m.CreatedAt) {
		return NewValidationError("Memory.TTLExpiresAt", m.TTLExpiresAt.String(), "> created_at")
	}
	for _, tag := range m.Tags {
		if !ValidTagName(tag) {
			return NewValidationError("Memory.Tags", tag, "^[a-z0-9][a-z0-9_-]*$")
		}
	}
	if m.Confidence != nil && (*m.Confidence < 0 || *m.Confidence > 1) {
		return NewValidationError("Memory.Confidence", fmt.Sprint(*m.Confidence), "[0,1]")
	}
	if m.DecayScore != nil && (*m.DecayScore < 0 || *m.DecayScore > 1) {
		return NewValidationError("Memory.DecayScore", fmt.Sprint(*m.DecayScore), "[0,1]")
	}
	return nil
}

// ValidityRange returns the half-open [ValidFrom, ValidUntil) interval. Zero
// bounds map to ±infinity, mirroring the tstzrange generated column. A nil
// return means the interval is empty (invalid ordering).
func (m *Memory) ValidityRange() *Interval {
	start, end := time.Time{}, time.Time{}
	if m.ValidFrom != nil {
		start = *m.ValidFrom
	}
	if m.ValidUntil != nil {
		end = *m.ValidUntil
	}
	if !start.IsZero() && !end.IsZero() && !end.After(start) {
		return nil
	}
	return &Interval{Start: start, End: end}
}

// Interval is a half-open time interval with zero bounds meaning unbounded.
type Interval struct {
	Start time.Time
	End   time.Time
}

// Contains reports whether t falls within the interval.
func (i *Interval) Contains(t time.Time) bool {
	if i == nil {
		return false
	}
	if !i.Start.IsZero() && t.Before(i.Start) {
		return false
	}
	if !i.End.IsZero() && !t.Before(i.End) {
		return false
	}
	return true
}

// isBlank mirrors SQL btrim: whitespace-only content is empty for the CHECK.
func isBlank(s string) bool {
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case ' ', '\t', '\n', '\r', '\v', '\f':
		default:
			return false
		}
	}
	return true
}

// TagPattern constrains tag_name values (pgvector-data-model §3.0 DOMAIN).
const TagPattern = `^[a-z0-9][a-z0-9_-]*$`

// ValidTagName reports whether tag matches the tag_name domain pattern.
func ValidTagName(tag string) bool {
	if tag == "" {
		return false
	}
	c := tag[0]
	if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9') {
		return false
	}
	for i := 1; i < len(tag); i++ {
		c := tag[i]
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}
