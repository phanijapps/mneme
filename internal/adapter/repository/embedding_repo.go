package repository

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/phanijapps/mneme/internal/domain"
	"github.com/phanijapps/mneme/internal/port"
)

// EmbeddingModelRepo implements port.EmbeddingModelRepository on PostgreSQL.
type EmbeddingModelRepo struct {
	pool *pgxpool.Pool
}

// NewEmbeddingRepo returns an EmbeddingModelRepository bound to pool.
func NewEmbeddingRepo(pool *pgxpool.Pool) *EmbeddingModelRepo {
	return &EmbeddingModelRepo{pool: pool}
}

var _ port.EmbeddingModelRepository = (*EmbeddingModelRepo)(nil)

// Save upserts a model-registry row (review F11).
func (r *EmbeddingModelRepo) Save(ctx context.Context, m *domain.EmbeddingModel) (*domain.EmbeddingModel, error) {
	if m.DistanceMetric == "" {
		m.DistanceMetric = domain.MetricCosine
	}
	err := querier(ctx, r.pool).QueryRow(ctx, `INSERT INTO embedding_models
		(model_id, provider, dims, distance_metric, is_active, created_at)
		VALUES ($1,$2,$3,$4,$5,$6)
		ON CONFLICT (model_id) DO UPDATE SET
		  provider = EXCLUDED.provider, dims = EXCLUDED.dims,
		  distance_metric = EXCLUDED.distance_metric, is_active = EXCLUDED.is_active
		RETURNING model_id, provider, dims, distance_metric, is_active, created_at`,
		m.ModelID, m.Provider, m.Dimensions, m.DistanceMetric, m.IsActive, m.CreatedAt).
		Scan(&m.ModelID, &m.Provider, &m.Dimensions, &m.DistanceMetric, &m.IsActive, &m.CreatedAt)
	if err != nil {
		return nil, mapErr(err, nil)
	}
	return m, nil
}

// GetByID fetches one registry row by model id.
func (r *EmbeddingModelRepo) GetByID(ctx context.Context, modelID string) (*domain.EmbeddingModel, error) {
	var m domain.EmbeddingModel
	err := querier(ctx, r.pool).QueryRow(ctx,
		`SELECT model_id, provider, dims, distance_metric, is_active, created_at
		 FROM embedding_models WHERE model_id = $1`, modelID).
		Scan(&m.ModelID, &m.Provider, &m.Dimensions, &m.DistanceMetric, &m.IsActive, &m.CreatedAt)
	if err != nil {
		return nil, mapErr(err, notFound(domain.CodeInternal, "embedding model"))
	}
	return &m, nil
}

// GetByName resolves a model by registry name; an alias for GetByID since
// model_id *is* the name.
func (r *EmbeddingModelRepo) GetByName(ctx context.Context, name string) (*domain.EmbeddingModel, error) {
	return r.GetByID(ctx, name)
}

// List returns registry rows, optionally only the active ones.
func (r *EmbeddingModelRepo) List(ctx context.Context, activeOnly bool) ([]*domain.EmbeddingModel, error) {
	rows, err := querier(ctx, r.pool).Query(ctx, `SELECT model_id, provider, dims,
		distance_metric, is_active, created_at FROM embedding_models
		WHERE ($1 OR is_active) ORDER BY model_id`, activeOnly)
	if err != nil {
		return nil, mapErr(err, nil)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (*domain.EmbeddingModel, error) {
		var m domain.EmbeddingModel
		return &m, mapErr(row.Scan(&m.ModelID, &m.Provider, &m.Dimensions,
			&m.DistanceMetric, &m.IsActive, &m.CreatedAt), nil)
	})
}

// MemoryEmbeddingRepo implements port.MemoryEmbeddingRepository on PostgreSQL.
type MemoryEmbeddingRepo struct {
	pool *pgxpool.Pool
}

// NewMemoryEmbeddingRepo returns a MemoryEmbeddingRepository bound to pool.
func NewMemoryEmbeddingRepo(pool *pgxpool.Pool) *MemoryEmbeddingRepo {
	return &MemoryEmbeddingRepo{pool: pool}
}

var _ port.MemoryEmbeddingRepository = (*MemoryEmbeddingRepo)(nil)

// Save inserts or refreshes an alternate-model vector (composite PK upsert).
// Exactly one of vec_1536/vec_768 must be populated (CHECK).
func (r *MemoryEmbeddingRepo) Save(ctx context.Context, e *domain.MemoryEmbedding) error {
	_, err := querier(ctx, r.pool).Exec(ctx, `INSERT INTO memory_embeddings
		(memory_id, model_id, vec_1536, vec_768, created_at)
		VALUES ($1,$2,$3,$4,$5)
		ON CONFLICT (memory_id, model_id) DO UPDATE SET
		  vec_1536 = EXCLUDED.vec_1536, vec_768 = EXCLUDED.vec_768`,
		e.MemoryID, e.ModelID, e.Vec1536, e.Vec768, e.CreatedAt)
	return mapErr(err, nil)
}

// GetByMemoryID returns every alternate-model vector of a memory.
func (r *MemoryEmbeddingRepo) GetByMemoryID(ctx context.Context, memoryID uuid.UUID) ([]*domain.MemoryEmbedding, error) {
	rows, err := querier(ctx, r.pool).Query(ctx, `SELECT memory_id, model_id,
		vec_1536, vec_768, created_at FROM memory_embeddings
		WHERE memory_id = $1 ORDER BY model_id`, memoryID)
	if err != nil {
		return nil, mapErr(err, nil)
	}
	return pgx.CollectRows(rows, scanEmbedding)
}

// ListByModel returns vectors stored under one model, newest first.
func (r *MemoryEmbeddingRepo) ListByModel(ctx context.Context, modelID string, limit int) ([]*domain.MemoryEmbedding, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := querier(ctx, r.pool).Query(ctx, `SELECT memory_id, model_id,
		vec_1536, vec_768, created_at FROM memory_embeddings
		WHERE model_id = $1 ORDER BY created_at DESC LIMIT $2`, modelID, limit)
	if err != nil {
		return nil, mapErr(err, nil)
	}
	return pgx.CollectRows(rows, scanEmbedding)
}

func scanEmbedding(row pgx.CollectableRow) (*domain.MemoryEmbedding, error) {
	var e domain.MemoryEmbedding
	return &e, mapErr(row.Scan(&e.MemoryID, &e.ModelID, &e.Vec1536, &e.Vec768, &e.CreatedAt), nil)
}
