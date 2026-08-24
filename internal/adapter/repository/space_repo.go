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

// SpaceRepo implements port.SharedMemorySpaceRepository on PostgreSQL.
type SpaceRepo struct {
	pool *pgxpool.Pool
}

// NewSpaceRepo returns a SharedMemorySpaceRepository bound to pool.
func NewSpaceRepo(pool *pgxpool.Pool) *SpaceRepo { return &SpaceRepo{pool: pool} }

var _ port.SharedMemorySpaceRepository = (*SpaceRepo)(nil)

const spaceCols = `id, name, description, owner_type, owner_id, scope, default_access,
	write_policy, promote_policy, backend_kind, backend_config, artifacts,
	sync_status, pending_proposals, sync_revision, last_synced_at,
	retention_policy, created_at, updated_at`

// Create inserts a space and returns the stored row.
func (r *SpaceRepo) Create(ctx context.Context, s *domain.SharedMemorySpace) (*domain.SharedMemorySpace, error) {
	if s.ID == uuid.Nil {
		s.ID = uuid.Must(uuid.NewV7())
	}
	if s.CreatedAt.IsZero() {
		s.CreatedAt = time.Now().UTC()
	}
	if s.UpdatedAt.IsZero() {
		s.UpdatedAt = s.CreatedAt
	}
	backend, err := jsonb(s.StorageBackend)
	if err != nil {
		return nil, err
	}
	artifacts, err := jsonb(s.Artifacts)
	if err != nil {
		return nil, err
	}
	retention, err := jsonb(s.RetentionPolicy)
	if err != nil {
		return nil, err
	}
	row := querier(ctx, r.pool).QueryRow(ctx, `INSERT INTO shared_memory_spaces
		(id, name, description, owner_type, owner_id, scope, default_access,
		 write_policy, promote_policy, backend_kind, backend_config, artifacts,
		 sync_status, pending_proposals, sync_revision, last_synced_at,
		 retention_policy, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19)
		RETURNING `+spaceCols,
		s.ID, s.Name, s.Description, s.OwnerType, s.OwnerID, s.Scope,
		s.AccessPolicy.DefaultAccess, s.AccessPolicy.WritePolicy, s.AccessPolicy.PromotePolicy,
		s.StorageBackend.Kind, backend, artifacts,
		s.SyncState.Status, s.SyncState.PendingProposals, s.SyncState.Revision, s.LastSyncedAt,
		retention, s.CreatedAt, s.UpdatedAt)
	return scanSpace(row)
}

// GetByID fetches one space.
func (r *SpaceRepo) GetByID(ctx context.Context, spaceID uuid.UUID) (*domain.SharedMemorySpace, error) {
	return scanSpace(querier(ctx, r.pool).QueryRow(ctx,
		`SELECT `+spaceCols+` FROM shared_memory_spaces WHERE id = $1`, spaceID))
}

// List returns spaces a principal can read: their own plus membership-scoped
// (Q7 visibility path).
func (r *SpaceRepo) List(ctx context.Context, principalType domain.PrincipalType, principalID string) ([]*domain.SharedMemorySpace, error) {
	rows, err := querier(ctx, r.pool).Query(ctx, `SELECT `+spaceCols+`
		FROM shared_memory_spaces s
		WHERE (s.owner_type = $1 AND s.owner_id = $2)
		   OR EXISTS (SELECT 1 FROM space_memberships m
		               WHERE m.space_id = s.id AND m.principal_type = $1
		                 AND m.principal_id = $2)
		ORDER BY s.created_at DESC`, principalType, principalID)
	if err != nil {
		return nil, mapErr(err, nil)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (*domain.SharedMemorySpace, error) {
		return scanSpace(row)
	})
}

// Update rewrites the mutable fields of a space and returns the stored row.
func (r *SpaceRepo) Update(ctx context.Context, s *domain.SharedMemorySpace) (*domain.SharedMemorySpace, error) {
	backend, err := jsonb(s.StorageBackend)
	if err != nil {
		return nil, err
	}
	artifacts, err := jsonb(s.Artifacts)
	if err != nil {
		return nil, err
	}
	retention, err := jsonb(s.RetentionPolicy)
	if err != nil {
		return nil, err
	}
	row := querier(ctx, r.pool).QueryRow(ctx, `UPDATE shared_memory_spaces SET
		name = $2, description = $3, scope = $4, default_access = $5,
		write_policy = $6, promote_policy = $7, backend_kind = $8,
		backend_config = $9, artifacts = $10, sync_status = $11,
		pending_proposals = $12, sync_revision = $13, last_synced_at = $14,
		retention_policy = $15, updated_at = now()
		WHERE id = $1
		RETURNING `+spaceCols,
		s.ID, s.Name, s.Description, s.Scope,
		s.AccessPolicy.DefaultAccess, s.AccessPolicy.WritePolicy, s.AccessPolicy.PromotePolicy,
		s.StorageBackend.Kind, backend, artifacts, s.SyncState.Status,
		s.SyncState.PendingProposals, s.SyncState.Revision, s.LastSyncedAt, retention)
	return scanSpace(row)
}

// ListMemberships returns every membership row of a space.
func (r *SpaceRepo) ListMemberships(ctx context.Context, spaceID uuid.UUID) ([]*domain.SpaceMembership, error) {
	rows, err := querier(ctx, r.pool).Query(ctx, `SELECT space_id, principal_type,
		principal_id, access_level, granted_at
		FROM space_memberships WHERE space_id = $1 ORDER BY granted_at`, spaceID)
	if err != nil {
		return nil, mapErr(err, nil)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (*domain.SpaceMembership, error) {
		var m domain.SpaceMembership
		return &m, mapErr(row.Scan(&m.SpaceID, &m.PrincipalType, &m.PrincipalID,
			&m.AccessLevel, &m.GrantedAt), nil)
	})
}

// AddMembership grants a principal access (idempotent by PK).
func (r *SpaceRepo) AddMembership(ctx context.Context, m *domain.SpaceMembership) error {
	if m.GrantedAt.IsZero() {
		m.GrantedAt = time.Now().UTC()
	}
	_, err := querier(ctx, r.pool).Exec(ctx, `INSERT INTO space_memberships
		(space_id, principal_type, principal_id, access_level, granted_at)
		VALUES ($1,$2,$3,$4,$5)
		ON CONFLICT (space_id, principal_type, principal_id) DO UPDATE
		  SET access_level = EXCLUDED.access_level, granted_at = EXCLUDED.granted_at`,
		m.SpaceID, m.PrincipalType, m.PrincipalID, m.AccessLevel, m.GrantedAt)
	return mapErr(err, nil)
}

// RemoveMembership revokes one principal's access from a space.
func (r *SpaceRepo) RemoveMembership(ctx context.Context, spaceID uuid.UUID, principalType domain.PrincipalType, principalID string) error {
	res, err := querier(ctx, r.pool).Exec(ctx, `DELETE FROM space_memberships
		WHERE space_id = $1 AND principal_type = $2 AND principal_id = $3`,
		spaceID, principalType, principalID)
	if err != nil {
		return mapErr(err, nil)
	}
	if res.RowsAffected() == 0 {
		return notFound(domain.CodeSpaceNotFound, "membership")
	}
	return nil
}

func scanSpace(row pgx.Row) (*domain.SharedMemorySpace, error) {
	var (
		s         domain.SharedMemorySpace
		backend   []byte
		artifacts []byte
		retention []byte
	)
	if err := row.Scan(&s.ID, &s.Name, &s.Description, &s.OwnerType, &s.OwnerID, &s.Scope,
		&s.AccessPolicy.DefaultAccess, &s.AccessPolicy.WritePolicy, &s.AccessPolicy.PromotePolicy,
		&s.StorageBackend.Kind, &backend, &artifacts, &s.SyncState.Status,
		&s.SyncState.PendingProposals, &s.SyncState.Revision, &s.LastSyncedAt,
		&retention, &s.CreatedAt, &s.UpdatedAt); err != nil {
		return nil, mapErr(err, notFound(domain.CodeSpaceNotFound, "space"))
	}
	if err := unmarshalJSONB(backend, &s.StorageBackend); err != nil {
		return nil, err
	}
	if len(artifacts) > 0 {
		s.Artifacts = []domain.Artifact{}
		if err := unmarshalJSONB(artifacts, &s.Artifacts); err != nil {
			return nil, err
		}
	}
	if len(retention) > 0 {
		s.RetentionPolicy = &domain.RetentionPolicy{}
		if err := unmarshalJSONB(retention, s.RetentionPolicy); err != nil {
			return nil, err
		}
	}
	return &s, nil
}
