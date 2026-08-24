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

// EntityRepo implements port.EntityRepository on PostgreSQL.
type EntityRepo struct {
	pool *pgxpool.Pool
}

// NewEntityRepo returns an EntityRepository bound to pool.
func NewEntityRepo(pool *pgxpool.Pool) *EntityRepo { return &EntityRepo{pool: pool} }

var _ port.EntityRepository = (*EntityRepo)(nil)

// Save upserts an entity by primary key and returns the stored row (facts are
// written separately via SaveFact).
func (r *EntityRepo) Save(ctx context.Context, e *domain.Entity) (*domain.Entity, error) {
	if e.EntityID == uuid.Nil {
		e.EntityID = uuid.Must(uuid.NewV7())
	}
	row := querier(ctx, r.pool).QueryRow(ctx, `INSERT INTO entities
		(entity_id, name, entity_type, aliases, created_at)
		VALUES ($1,$2,$3,$4,$5)
		ON CONFLICT (entity_id) DO UPDATE SET
		  name = EXCLUDED.name, entity_type = EXCLUDED.entity_type, aliases = EXCLUDED.aliases
		RETURNING entity_id, name, entity_type, aliases, created_at`,
		e.EntityID, e.Name, e.EntityType, e.Aliases, e.CreatedAt)
	out, err := scanEntity(row)
	if err != nil {
		return nil, err
	}
	out.Facts = e.Facts
	return out, nil
}

// GetByID fetches an entity with its facts.
func (r *EntityRepo) GetByID(ctx context.Context, entityID uuid.UUID) (*domain.Entity, error) {
	e, err := scanEntity(querier(ctx, r.pool).QueryRow(ctx,
		`SELECT entity_id, name, entity_type, aliases, created_at FROM entities WHERE entity_id = $1`, entityID))
	if err != nil {
		return nil, err
	}
	facts, err := r.ListFacts(ctx, entityID)
	if err != nil {
		return nil, err
	}
	e.Facts = factsOf(facts)
	return e, nil
}

// GetByName resolves the canonical entity for an exact (non-alias) name.
func (r *EntityRepo) GetByName(ctx context.Context, name string) (*domain.Entity, error) {
	e, err := scanEntity(querier(ctx, r.pool).QueryRow(ctx,
		`SELECT entity_id, name, entity_type, aliases, created_at FROM entities WHERE name = $1`, name))
	if err != nil {
		return nil, err
	}
	facts, err := r.ListFacts(ctx, e.EntityID)
	if err != nil {
		return nil, err
	}
	e.Facts = factsOf(facts)
	return e, nil
}

// ListByMemory returns entities anchored to a memory via memory_entities.
func (r *EntityRepo) ListByMemory(ctx context.Context, memoryID uuid.UUID) ([]*domain.Entity, error) {
	rows, err := querier(ctx, r.pool).Query(ctx, `SELECT e.entity_id, e.name, e.entity_type, e.aliases, e.created_at
		FROM entities e JOIN memory_entities me ON me.entity_id = e.entity_id
		WHERE me.memory_id = $1 ORDER BY e.name`, memoryID)
	if err != nil {
		return nil, mapErr(err, nil)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (*domain.Entity, error) {
		e, err := scanEntity(row)
		if err != nil {
			return nil, err
		}
		return e, nil
	})
}

// SaveMemoryEntity records a memory↔entity tie (idempotent junction insert).
func (r *EntityRepo) SaveMemoryEntity(ctx context.Context, me *domain.MemoryEntity) error {
	if me.CreatedAt.IsZero() {
		me.CreatedAt = time.Now().UTC()
	}
	_, err := querier(ctx, r.pool).Exec(ctx, `INSERT INTO memory_entities
		(memory_id, entity_id, created_at) VALUES ($1,$2,$3)
		ON CONFLICT (memory_id, entity_id) DO NOTHING`,
		me.MemoryID, me.EntityID, me.CreatedAt)
	return mapErr(err, nil)
}

// SaveFact appends a temporal fact; supersession closes validity rather than
// deleting (Zep-style intervals).
func (r *EntityRepo) SaveFact(ctx context.Context, fact *domain.EntityFact) (*domain.EntityFact, error) {
	if fact.ID == uuid.Nil {
		fact.ID = uuid.Must(uuid.NewV7())
	}
	err := querier(ctx, r.pool).QueryRow(ctx, `INSERT INTO entity_facts
		(id, entity_id, fact, memory_id, valid_from, valid_until)
		VALUES ($1,$2,$3,$4,$5,$6)
		RETURNING id, entity_id, fact, memory_id, valid_from, valid_until`,
		fact.ID, fact.EntityID, fact.Fact, fact.MemoryID, fact.ValidFrom, fact.ValidUntil).
		Scan(&fact.ID, &fact.EntityID, &fact.Fact, &fact.MemoryID, &fact.ValidFrom, &fact.ValidUntil)
	if err != nil {
		return nil, mapErr(err, nil)
	}
	return fact, nil
}

// ListFacts returns an entity's facts ordered oldest-first.
func (r *EntityRepo) ListFacts(ctx context.Context, entityID uuid.UUID) ([]*domain.EntityFact, error) {
	rows, err := querier(ctx, r.pool).Query(ctx,
		`SELECT id, entity_id, fact, memory_id, valid_from, valid_until
		 FROM entity_facts WHERE entity_id = $1 ORDER BY valid_from`, entityID)
	if err != nil {
		return nil, mapErr(err, nil)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (*domain.EntityFact, error) {
		var f domain.EntityFact
		return &f, mapErr(row.Scan(&f.ID, &f.EntityID, &f.Fact, &f.MemoryID, &f.ValidFrom, &f.ValidUntil), nil)
	})
}

func scanEntity(row pgx.Row) (*domain.Entity, error) {
	var e domain.Entity
	if err := row.Scan(&e.EntityID, &e.Name, &e.EntityType, &e.Aliases, &e.CreatedAt); err != nil {
		return nil, mapErr(err, notFound(domain.CodeInternal, "entity"))
	}
	return &e, nil
}

// factsOf dereferences a fact slice so callers keep value semantics.
func factsOf(fs []*domain.EntityFact) []domain.EntityFact {
	if len(fs) == 0 {
		return nil
	}
	out := make([]domain.EntityFact, len(fs))
	for i, f := range fs {
		out[i] = *f
	}
	return out
}
