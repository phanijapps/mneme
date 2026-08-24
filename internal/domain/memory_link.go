package domain

import (
	"time"

	"github.com/google/uuid"
)

// MemoryLink is a directed, weighted edge between two Memory records (or
// Memory→Entity via relationship_type=anchors_entity). The graph retrieval
// path traverses these edges (data-models §3.2).
type MemoryLink struct {
	ID               uuid.UUID        `json:"id" validate:"required"`
	SourceID         uuid.UUID        `json:"source_id" validate:"required"`
	TargetID         uuid.UUID        `json:"target_id" validate:"required"`
	RelationshipType RelationshipType `json:"relationship_type" validate:"required"`
	Weight           float64          `json:"weight" validate:"min=0,max=1"` // default 1.0
	Evidence         *string          `json:"evidence,omitempty"`            // why the link exists (audit)
	CreatedAt        time.Time        `json:"created_at" validate:"required"`
}

// Validate checks enum validity, weight range, and self-link prevention.
func (l *MemoryLink) Validate() error {
	if !l.RelationshipType.Valid() {
		return NewValidationError("MemoryLink.RelationshipType", l.RelationshipType.String(),
			"derived_from|supersedes|similar_to|co_occurs_with|causal_next|anchors_entity")
	}
	if l.Weight < 0 || l.Weight > 1 {
		return NewValidationError("MemoryLink.Weight", floatString(l.Weight), "[0,1]")
	}
	if l.SourceID == l.TargetID {
		return NewValidationError("MemoryLink.TargetID", l.TargetID.String(), "must differ from source_id")
	}
	return nil
}
