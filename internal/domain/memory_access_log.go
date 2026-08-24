package domain

import (
	"time"

	"github.com/google/uuid"
)

// MemoryAccessLog is the append-only access log decoupled from the memories
// table (review F8): reads append here instead of updating the hottest table.
type MemoryAccessLog struct {
	ID         int64      `json:"id"` // bigserial
	MemoryID   uuid.UUID  `json:"memory_id" validate:"required"`
	AccessedBy uuid.UUID  `json:"accessed_by" validate:"required"`
	AccessType AccessType `json:"access_type" validate:"required"`
	AccessedAt time.Time  `json:"accessed_at"`
}

// Validate checks the access_type enum.
func (l *MemoryAccessLog) Validate() error {
	if !l.AccessType.Valid() {
		return NewValidationError("MemoryAccessLog.AccessType", l.AccessType.String(),
			"recall|direct_get|session_activate")
	}
	return nil
}
