package domain

import (
	"time"

	"github.com/google/uuid"
)

// Entity is a named anchor for entity-centric retrieval
// (data-models §3.3): person, project, repository, tool, organization, concept.
type Entity struct {
	EntityID   uuid.UUID    `json:"entity_id" validate:"required"`
	Name       string       `json:"name" validate:"required,min=1"`
	EntityType EntityType   `json:"entity_type" validate:"required"`
	Aliases    []string     `json:"aliases,omitempty"`
	Facts      []EntityFact `json:"facts,omitempty"`
	CreatedAt  time.Time    `json:"created_at" validate:"required"`
}

// EntityFact is a temporal fact attached to an Entity; Zep-style validity
// intervals with supersession (valid_until closes a fact, nothing is deleted).
type EntityFact struct {
	ID         uuid.UUID  `json:"id" validate:"required"`
	EntityID   uuid.UUID  `json:"entity_id" validate:"required"`
	Fact       string     `json:"fact" validate:"required,min=1"`
	MemoryID   *uuid.UUID `json:"memory_id,omitempty"` // supporting memory
	ValidFrom  time.Time  `json:"valid_from" validate:"required"`
	ValidUntil *time.Time `json:"valid_until,omitempty"`
}

// Validate checks the Entity name and enum, then each fact's range.
func (e *Entity) Validate() error {
	if e.Name == "" {
		return NewValidationError("Entity.Name", e.Name, "non-empty")
	}
	if !e.EntityType.Valid() {
		return NewValidationError("Entity.EntityType", e.EntityType.String(),
			"person|project|repository|tool|organization|concept")
	}
	for i := range e.Facts {
		f := &e.Facts[i]
		if f.ValidUntil != nil && !f.ValidUntil.After(f.ValidFrom) {
			return NewValidationError("EntityFact.ValidUntil", f.ValidUntil.String(), "> valid_from")
		}
	}
	return nil
}

// MemoryEntity is the M:N junction between Memory.entities[] and the registry.
type MemoryEntity struct {
	MemoryID  uuid.UUID `json:"memory_id" validate:"required"`
	EntityID  uuid.UUID `json:"entity_id" validate:"required"`
	CreatedAt time.Time `json:"created_at"`
}
