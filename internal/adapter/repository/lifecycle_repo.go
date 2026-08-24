package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/phanijapps/mneme/internal/domain"
	"github.com/phanijapps/mneme/internal/port"
)

// LifecycleRepo implements both port.LifecycleJobRepository and
// port.MemoryAccessLogRepository on PostgreSQL.
type LifecycleRepo struct {
	pool *pgxpool.Pool
}

// NewLifecycleRepo returns a repository bound to pool implementing both the
// job-ledger and access-log interfaces.
func NewLifecycleRepo(pool *pgxpool.Pool) *LifecycleRepo { return &LifecycleRepo{pool: pool} }

var (
	_ port.LifecycleJobRepository    = (*LifecycleRepo)(nil)
	_ port.MemoryAccessLogRepository = (*LifecycleRepo)(nil)
)

const jobCols = `job_id, kind, status, scope_kind, scope_id, result, error, created_at, finished_at`

// Create inserts a job ledger row and returns the stored row.
func (r *LifecycleRepo) Create(ctx context.Context, j *domain.LifecycleJob) (*domain.LifecycleJob, error) {
	if j.JobID == uuid.Nil {
		j.JobID = uuid.Must(uuid.NewV7())
	}
	if j.Status == "" {
		j.Status = domain.JobStatusQueued
	}
	if j.CreatedAt.IsZero() {
		j.CreatedAt = time.Now().UTC()
	}
	resJSON, err := jsonb(j.Result)
	if err != nil {
		return nil, err
	}
	errJSON, err := jsonb(j.Error)
	if err != nil {
		return nil, err
	}
	row := querier(ctx, r.pool).QueryRow(ctx, `INSERT INTO lifecycle_jobs
		(job_id, kind, status, scope_kind, scope_id, result, error, created_at, finished_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		RETURNING `+jobCols,
		j.JobID, j.Kind, j.Status, j.ScopeKind, j.ScopeID, resJSON, errJSON, j.CreatedAt, j.FinishedAt)
	return scanJob(row)
}

// GetByID fetches one job.
func (r *LifecycleRepo) GetByID(ctx context.Context, jobID uuid.UUID) (*domain.LifecycleJob, error) {
	return scanJob(querier(ctx, r.pool).QueryRow(ctx,
		`SELECT `+jobCols+` FROM lifecycle_jobs WHERE job_id = $1`, jobID))
}

// UpdateStatus advances queued→running→completed|failed; terminal statuses
// stamp finished_at and carry result/error payload.
func (r *LifecycleRepo) UpdateStatus(ctx context.Context, jobID uuid.UUID, status domain.JobStatus, result any, failure *domain.Error) error {
	resJSON, err := jsonb(result)
	if err != nil {
		return err
	}
	errJSON, err := jsonb(failure)
	if err != nil {
		return err
	}
	var finishedAt any // NULL for non-terminal, now() for terminal
	if status == domain.JobStatusCompleted || status == domain.JobStatusFailed {
		finishedAt = time.Now().UTC()
	}
	res, err := querier(ctx, r.pool).Exec(ctx, `UPDATE lifecycle_jobs
		SET status = $2, result = $3, error = $4, finished_at = $5
		WHERE job_id = $1`, jobID, status, resJSON, errJSON, finishedAt)
	if err != nil {
		return mapErr(err, nil)
	}
	if res.RowsAffected() == 0 {
		return notFound(domain.CodeInternal, "lifecycle job")
	}
	return nil
}

// ListPending returns queued/running jobs, optionally narrowed by kind.
func (r *LifecycleRepo) ListPending(ctx context.Context, kind *domain.JobKind, limit int) ([]*domain.LifecycleJob, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := querier(ctx, r.pool).Query(ctx, `SELECT `+jobCols+`
		FROM lifecycle_jobs
		WHERE status IN ('queued', 'running') AND ($1::text IS NULL OR kind = $1)
		ORDER BY created_at LIMIT $2`, kind, limit)
	if err != nil {
		return nil, mapErr(err, nil)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (*domain.LifecycleJob, error) {
		return scanJob(row)
	})
}

// Append writes one access-log row (review F8: reads never touch memories).
func (r *LifecycleRepo) Append(ctx context.Context, entry *domain.MemoryAccessLog) error {
	if entry.AccessedAt.IsZero() {
		entry.AccessedAt = time.Now().UTC()
	}
	_, err := querier(ctx, r.pool).Exec(ctx, `INSERT INTO memory_access_log
		(memory_id, accessed_by, access_type, accessed_at)
		VALUES ($1,$2,$3,$4)`,
		entry.MemoryID, entry.AccessedBy, entry.AccessType, entry.AccessedAt)
	return mapErr(err, nil)
}

// ListByMemory returns the most recent access rows of a memory.
func (r *LifecycleRepo) ListByMemory(ctx context.Context, memoryID uuid.UUID, limit int) ([]*domain.MemoryAccessLog, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := querier(ctx, r.pool).Query(ctx, `SELECT id, memory_id, accessed_by,
		access_type, accessed_at FROM memory_access_log
		WHERE memory_id = $1 ORDER BY accessed_at DESC, id DESC LIMIT $2`, memoryID, limit)
	if err != nil {
		return nil, mapErr(err, nil)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (*domain.MemoryAccessLog, error) {
		var e domain.MemoryAccessLog
		return &e, mapErr(row.Scan(&e.ID, &e.MemoryID, &e.AccessedBy, &e.AccessType, &e.AccessedAt), nil)
	})
}

func scanJob(row pgx.Row) (*domain.LifecycleJob, error) {
	var (
		j       domain.LifecycleJob
		resJSON []byte
		errJSON []byte
	)
	if err := row.Scan(&j.JobID, &j.Kind, &j.Status, &j.ScopeKind, &j.ScopeID,
		&resJSON, &errJSON, &j.CreatedAt, &j.FinishedAt); err != nil {
		return nil, mapErr(err, notFound(domain.CodeInternal, "lifecycle job"))
	}
	if len(resJSON) > 0 {
		var out any
		if err := unmarshalJSONB(resJSON, &out); err != nil {
			return nil, err
		}
		j.Result = out
	}
	if len(errJSON) > 0 {
		j.Error = &domain.Error{}
		if err := unmarshalJSONB(errJSON, j.Error); err != nil {
			return nil, err
		}
	}
	return &j, nil
}
