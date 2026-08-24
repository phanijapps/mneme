package domain

import (
	"time"

	"github.com/google/uuid"
)

// LifecycleJob is the async job ledger for lifecycle/sync operations
// (review F3). Backs every JobRef poll target in api-contracts.
type LifecycleJob struct {
	JobID      uuid.UUID  `json:"job_id" validate:"required"`
	Kind       JobKind    `json:"kind" validate:"required"`
	Status     JobStatus  `json:"status" validate:"required"`
	ScopeKind  *string    `json:"scope_kind,omitempty"`
	ScopeID    *string    `json:"scope_id,omitempty"`
	Result     any        `json:"result,omitempty"`
	Error      *Error     `json:"error,omitempty"`
	CreatedAt  time.Time  `json:"created_at" validate:"required"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
}

// Validate checks enums plus the terminal/error pairing CHECKs.
func (j *LifecycleJob) Validate() error {
	if !j.Kind.Valid() {
		return NewValidationError("LifecycleJob.Kind", j.Kind.String(),
			"consolidation|decay|space_sync|session_end")
	}
	if !j.Status.Valid() {
		return NewValidationError("LifecycleJob.Status", j.Status.String(),
			"queued|running|completed|failed")
	}
	terminal := j.Status == JobStatusCompleted || j.Status == JobStatusFailed
	if terminal != (j.FinishedAt != nil) {
		return NewValidationError("LifecycleJob.FinishedAt", "<nil?>",
			"finished_at set iff status is completed|failed")
	}
	if j.Status == JobStatusFailed && j.Error == nil {
		return NewValidationError("LifecycleJob.Error", "<nil?>", "required when status=failed")
	}
	return nil
}
