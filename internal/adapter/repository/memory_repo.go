package repository

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/phanijapps/mneme/internal/domain"
	"github.com/phanijapps/mneme/internal/port"
)

// MemoryRepo implements port.MemoryRepository on PostgreSQL.
type MemoryRepo struct {
	pool *pgxpool.Pool
}

// NewMemoryRepo returns a MemoryRepository bound to pool.
func NewMemoryRepo(pool *pgxpool.Pool) *MemoryRepo { return &MemoryRepo{pool: pool} }

var _ port.MemoryRepository = (*MemoryRepo)(nil)

const memoryCols = `id, type, content, content_format, tags, embedding, embedding_model,
	origin, session_id, agent_id, actor, owner_principal_type, owner_principal_id,
	source_ref, created_at, updated_at, confidence, decay_score,
	valid_from, valid_until, superseded_by, ttl_expires_at, version,
	access_scope, shared_space_id, deleted_at`

// Save inserts a new memory (idempotent by primary key) and returns the
// stored row.
func (r *MemoryRepo) Save(ctx context.Context, m *domain.Memory) (*domain.Memory, error) {
	if m.ID == uuid.Nil {
		m.ID = uuid.Must(uuid.NewV7())
	}
	// tags is tag_name[] — bind the Go slice natively; jsonb() would send JSON
	// bytes that Postgres rejects as a malformed array literal.
	tags := m.Tags
	if tags == nil {
		tags = []string{}
	}
	srcRef, err := jsonb(m.SourceRef)
	if err != nil {
		return nil, err
	}
	sql := `INSERT INTO memories (` + memoryCols + `)
	VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26)
	ON CONFLICT (id) DO UPDATE SET
	  content = EXCLUDED.content, content_format = EXCLUDED.content_format,
	  tags = EXCLUDED.tags, embedding = EXCLUDED.embedding,
	  embedding_model = EXCLUDED.embedding_model, updated_at = EXCLUDED.updated_at,
	  confidence = EXCLUDED.confidence, decay_score = EXCLUDED.decay_score,
	  ttl_expires_at = EXCLUDED.ttl_expires_at, deleted_at = EXCLUDED.deleted_at
	RETURNING ` + memoryCols
	args := []any{m.ID, m.Type, m.Content, m.ContentFormat, tags, m.Embedding,
		m.EmbeddingModel, m.Origin, m.SessionID, m.AgentID, m.Actor,
		m.OwnerPrincipalType, m.OwnerPrincipalID, srcRef,
		m.CreatedAt, m.UpdatedAt, m.Confidence, m.DecayScore,
		m.ValidFrom, m.ValidUntil, m.SupersededBy, m.TTLExpiresAt, m.Version,
		m.AccessScope, m.SharedSpaceID, m.DeletedAt}
	row := querier(ctx, r.pool).QueryRow(ctx, sql, args...)
	return scanMemory(row)
}

// GetByID returns the live (non-deleted) memory and appends a
// direct_get row to memory_access_log. A closed (superseded or
// TTL-expired) row is MEMORY_EXPIRED per api-contracts §GET /memories/{id};
// only a missing or soft-deleted row is MEMORY_NOT_FOUND.
func (r *MemoryRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.Memory, error) {
	m, err := scanMemory(querier(ctx, r.pool).QueryRow(ctx,
		`SELECT `+memoryCols+` FROM memories WHERE id = $1 AND deleted_at IS NULL`, id))
	if err != nil {
		return nil, err // scanMemory already maps ErrNoRows to MEMORY_NOT_FOUND
	}
	err = inTx(ctx, r.pool, func(tctx context.Context) error {
		var closed bool
		var supersededBy *uuid.UUID
		if err := querier(tctx, r.pool).QueryRow(tctx,
			`SELECT (valid_until IS NOT NULL AND valid_until <= now())
				OR superseded_by IS NOT NULL, superseded_by
			   FROM memories WHERE id = $1 AND deleted_at IS NULL`,
			id).Scan(&closed, &supersededBy); err != nil {
			return mapErr(err, notFound(domain.CodeMemoryNotFound, "memory"))
		}
		if closed {
			details := map[string]any{}
			if supersededBy != nil {
				details["superseded_by"] = supersededBy.String()
			}
			return &domain.Error{
				Code:    domain.CodeMemoryExpired,
				Message: "TTL passed or superseded",
				Details: details,
			}
		}
		if _, err := querier(tctx, r.pool).Exec(tctx,
			`INSERT INTO memory_access_log (memory_id, accessed_by, access_type)
			 VALUES ($1, $2, $3)`, id, id, domain.AccessTypeDirectGet); err != nil {
			return mapErr(err, nil)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return m, nil
}

// GetByVersion returns one specific version row of a memory chain.
func (r *MemoryRepo) GetByVersion(ctx context.Context, id uuid.UUID, version int) (*domain.Memory, error) {
	row := querier(ctx, r.pool).QueryRow(ctx,
		`SELECT `+memoryCols+` FROM memories WHERE id = $1 AND version = $2`, id, version)
	return scanMemory(row)
}

// sortExpr maps a filter sort field to its ORDER BY expression; last_accessed
// is served from the access-log aggregate (review F8).
var sortExpr = map[port.MemorySortField]string{
	port.SortByCreatedAt:    "m.created_at",
	port.SortByUpdatedAt:    "m.updated_at",
	port.SortByLastAccessed: "(SELECT max(accessed_at) FROM memory_access_log a WHERE a.memory_id = m.id)",
	port.SortByConfidence:   "m.confidence",
	port.SortByDecayScore:   "m.decay_score",
}

// sortExprAsc maps the same fields to tie-break-safe ascending expressions:
// NULLS FIRST keeps ASC cursors stable where NULLS LAST would break them.
var sortExprAsc = map[port.MemorySortField]string{
	port.SortByCreatedAt:    "m.created_at",
	port.SortByUpdatedAt:    "m.updated_at",
	port.SortByLastAccessed: "(SELECT max(accessed_at) FROM memory_access_log a WHERE a.memory_id = m.id)",
	port.SortByConfidence:   "m.confidence",
	port.SortByDecayScore:   "m.decay_score",
}

// List applies every GET /memories filter with cursor pagination. See
// port.MemoryFilter for semantics; zero-value fields are ignored.
func (r *MemoryRepo) List(ctx context.Context, filter port.MemoryFilter) (port.Page[*domain.Memory], error) {
	if filter.Limit <= 0 {
		filter.Limit = 50
	}
	where, args := buildMemoryWhere(filter)

	sortField := filter.Sort
	if _, ok := sortExpr[sortField]; !ok {
		sortField = port.SortByCreatedAt
	}
	dir := "DESC"
	expr := sortExpr[sortField]
	nulls := "NULLS LAST"
	if filter.Order == port.SortAsc {
		dir = "ASC"
		expr = sortExprAsc[sortField]
		nulls = "NULLS FIRST"
	}
	keyExpr := fmt.Sprintf("(%s, m.id)", expr)

	// fetch limit+1 so HasMore/NextCursor fall out without a count query
	if cur, ok := decodeCursor(filter.Cursor); ok {
		have := len(args) + 1
		args = append(args, cur.Key, cur.ID)
		op := ">"
		if filter.Order == port.SortAsc {
			op = "<"
		}
		where += fmt.Sprintf(" AND %s %s ($%d, $%d)", keyExpr, op, have, have+1)
	}
	args = append(args, filter.Limit+1)

	sql := fmt.Sprintf(`SELECT %s FROM memories m WHERE %s
		ORDER BY %s %s %s, m.id %s LIMIT $%d`,
		prefixCols(memoryCols, "m"), where, expr, dir, nulls, dir, len(args))
	rows, err := querier(ctx, r.pool).Query(ctx, sql, args...)
	if err != nil {
		return port.Page[*domain.Memory]{}, mapErr(err, nil)
	}
	items, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (*domain.Memory, error) {
		return scanMemory(row)
	})
	if err != nil {
		return port.Page[*domain.Memory]{}, mapErr(err, nil)
	}

	page := port.Page[*domain.Memory]{Items: items}
	if len(items) > filter.Limit {
		page.HasMore = true
		items = items[:filter.Limit]
		page.Items = items
		last := items[len(items)-1]
		key, err := sortKeyOf(sortField, last)
		if err != nil {
			return port.Page[*domain.Memory]{}, err
		}
		page.NextCursor = encodeCursor(cursor{Key: key, ID: last.ID.String()})
	}
	return page, nil
}

// sortKeyOf extracts the raw cursor key for the sort field; last_accessed is
// computed from the log aggregate in Go (same value as the SQL expression).
func sortKeyOf(field port.MemorySortField, m *domain.Memory) (any, error) {
	switch field {
	case port.SortByCreatedAt:
		return m.CreatedAt, nil
	case port.SortByUpdatedAt:
		return m.UpdatedAt, nil
	case port.SortByConfidence:
		return m.Confidence, nil
	case port.SortByDecayScore:
		return m.DecayScore, nil
	case port.SortByLastAccessed:
		return nil, &domain.Error{Code: domain.CodeInternal,
			Message: "last_accessed_at cursor key is not representable; sort on the access-log view"}
	}
	return nil, &domain.Error{Code: domain.CodeInternal, Message: "unsupported sort field"}
}

// buildMemoryWhere renders the filter predicates and positional args.
func buildMemoryWhere(f port.MemoryFilter) (string, []any) {
	var conds []string
	var args []any
	add := func(cond string, val any) {
		args = append(args, val)
		conds = append(conds, fmt.Sprintf(cond, len(args)))
	}

	conds = append(conds, "m.deleted_at IS NULL")
	if len(f.Types) > 0 {
		args = append(args, typesToStrings(f.Types))
		conds = append(conds, fmt.Sprintf("m.type = ANY($%d::text[])", len(args)))
	}
	if f.AccessScope != nil {
		add("m.access_scope = $%d", *f.AccessScope)
	}
	if f.SharedSpaceID != nil {
		add("m.shared_space_id = $%d", *f.SharedSpaceID)
	}
	if len(f.Tags) > 0 {
		args = append(args, f.Tags)
		// tag_name[] (DOMAIN over text) has no implicit cross-type array
		// operators — cast the parameter to the domain type so @>/&& resolve
		// and the GIN index stays usable.
		if f.TagsMatch == port.TagsMatchAll {
			conds = append(conds, fmt.Sprintf("m.tags @> $%d::tag_name[]", len(args)))
		} else {
			conds = append(conds, fmt.Sprintf("m.tags && $%d::tag_name[]", len(args)))
		}
	}
	if f.EntityID != nil {
		add(`EXISTS (SELECT 1 FROM memory_entities me WHERE me.memory_id = m.id AND me.entity_id = $%d)`, *f.EntityID)
	}
	if f.Query != "" {
		args = append(args, f.Query)
		conds = append(conds, fmt.Sprintf("m.search_tsv @@ websearch_to_tsquery('english', $%d)", len(args)))
	}
	if f.CreatedFrom != nil {
		add("m.created_at >= $%d", *f.CreatedFrom)
	}
	if f.CreatedTo != nil {
		add("m.created_at < $%d", *f.CreatedTo)
	}
	if f.UpdatedFrom != nil {
		add("m.updated_at >= $%d", *f.UpdatedFrom)
	}
	if f.UpdatedTo != nil {
		add("m.updated_at < $%d", *f.UpdatedTo)
	}
	if f.MinConfidence != nil {
		add("(m.confidence IS NULL OR m.confidence >= $%d)", *f.MinConfidence)
	}
	if f.MinDecayScore != nil {
		add("(m.decay_score IS NULL OR m.decay_score >= $%d)", *f.MinDecayScore)
	}
	if f.ValidAt != nil {
		add("(m.validity_range @> $%d OR (m.valid_from IS NULL AND m.valid_until IS NULL))", *f.ValidAt)
	}
	if !f.IncludeExpired {
		conds = append(conds, "(m.ttl_expires_at IS NULL OR m.ttl_expires_at > now())",
			"m.superseded_by IS NULL")
	}
	return strings.Join(conds, " AND "), args
}

func typesToStrings[T ~string](ts []T) []string {
	out := make([]string, len(ts))
	for i, t := range ts {
		out[i] = string(t)
	}
	return out
}

// Update persists changes with optimistic concurrency on expectedVersion.
// Supersession-first: any content/embedding change closes the current row's
// validity and inserts the next version as a new row (superseded_by chain),
// returning the new row; metadata-only changes update in place.
func (r *MemoryRepo) Update(ctx context.Context, m *domain.Memory, expectedVersion int) (*domain.Memory, error) {
	if expectedVersion == 0 {
		expectedVersion = m.Version
	}
	var out *domain.Memory
	err := inTx(ctx, r.pool, func(tctx context.Context) error {
		q := querier(tctx, r.pool)
		row := q.QueryRow(tctx, `SELECT `+memoryCols+` FROM memories
			WHERE id = $1 AND deleted_at IS NULL FOR UPDATE`, m.ID)
		current, err := scanMemory(row)
		if err != nil {
			if nf := (*domain.Error)(nil); err == nf { // unreachable; keeps linters calm
				return err
			}
			return err
		}
		if current.Version != expectedVersion {
			return &domain.Error{Code: domain.CodeVersionConflict,
				Message: fmt.Sprintf("expected version %d, found %d", expectedVersion, current.Version),
				Details: map[string]any{"expected": expectedVersion, "actual": current.Version}}
		}

		contentChanged := current.Content != m.Content ||
			current.ContentFormat != m.ContentFormat ||
			(current.Embedding == nil) != (m.Embedding == nil)
		now := time.Now().UTC()

		if contentChanged {
			// supersede: the new row must exist before the old one can point
			// at it (memories_superseded_by_fkey), so insert first, close after
			newID := uuid.Must(uuid.NewV7())
			next := *m
			next.ID = newID
			next.Version = expectedVersion + 1
			next.CreatedAt = current.CreatedAt
			next.UpdatedAt = now
			next.ValidFrom = &now
			next.ValidUntil = nil
			next.SupersededBy = nil
			next.Embedding = m.Embedding
			if next.SourceRef == nil {
				next.SourceRef = current.SourceRef
			}
			saved, err := r.Save(tctx, &next)
			if err != nil {
				return err
			}
			if _, err := q.Exec(tctx, `UPDATE memories
				SET valid_until = $2, superseded_by = $3, updated_at = $2
				WHERE id = $1 AND deleted_at IS NULL`,
				current.ID, now, newID); err != nil {
				return mapErr(err, nil)
			}
			out = saved
			return nil
		}

		// metadata-only update in place
		res, err := q.Exec(tctx, `UPDATE memories SET
			tags = $2, confidence = $3, decay_score = $4, ttl_expires_at = $5,
			updated_at = $6, deleted_at = $7
			WHERE id = $1 AND version = $8 AND deleted_at IS NULL`,
			m.ID, m.Tags, m.Confidence, m.DecayScore, m.TTLExpiresAt, now, m.DeletedAt, expectedVersion)
		if err != nil {
			return mapErr(err, nil)
		}
		if res.RowsAffected() == 0 {
			return &domain.Error{Code: domain.CodeVersionConflict,
				Message: "memory changed concurrently", Details: map[string]any{"version": expectedVersion}}
		}
		row = q.QueryRow(tctx, `SELECT `+memoryCols+` FROM memories WHERE id = $1`, m.ID)
		out, err = scanMemory(row)
		return mapErr(err, nil)
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// SoftDelete closes the memory: deleted_at and valid_until are stamped, the
// row is never destroyed (supersession-first retention).
func (r *MemoryRepo) SoftDelete(ctx context.Context, id uuid.UUID) error {
	res, err := querier(ctx, r.pool).Exec(ctx, `UPDATE memories
		SET deleted_at = now(), valid_until = now(), updated_at = now()
		WHERE id = $1 AND deleted_at IS NULL`, id)
	if err != nil {
		return mapErr(err, nil)
	}
	if res.RowsAffected() == 0 {
		return notFound(domain.CodeMemoryNotFound, "memory")
	}
	return nil
}

// Purge hard-deletes the memory row; cascades remove dependents (admin only).
func (r *MemoryRepo) Purge(ctx context.Context, id uuid.UUID) error {
	res, err := querier(ctx, r.pool).Exec(ctx, `DELETE FROM memories WHERE id = $1`, id)
	if err != nil {
		return mapErr(err, nil)
	}
	if res.RowsAffected() == 0 {
		return notFound(domain.CodeMemoryNotFound, "memory")
	}
	return nil
}

// SaveLink inserts a directed, weighted edge; upsert on the natural key.
func (r *MemoryRepo) SaveLink(ctx context.Context, l *domain.MemoryLink) (*domain.MemoryLink, error) {
	if l.ID == uuid.Nil {
		l.ID = uuid.Must(uuid.NewV7())
	}
	err := querier(ctx, r.pool).QueryRow(ctx, `INSERT INTO memory_links
		(id, source_id, target_id, relationship_type, weight, evidence, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
		ON CONFLICT (source_id, target_id, relationship_type) DO UPDATE
		  SET weight = EXCLUDED.weight, evidence = EXCLUDED.evidence
		RETURNING id, source_id, target_id, relationship_type, weight, evidence, created_at`,
		l.ID, l.SourceID, l.TargetID, l.RelationshipType, l.Weight, l.Evidence, l.CreatedAt).
		Scan(&l.ID, &l.SourceID, &l.TargetID, &l.RelationshipType, &l.Weight, &l.Evidence, &l.CreatedAt)
	if err != nil {
		return nil, mapErr(err, nil)
	}
	return l, nil
}

// ListLinks returns edges touching a memory filtered by direction and type.
func (r *MemoryRepo) ListLinks(ctx context.Context, memoryID uuid.UUID, opts port.LinkFilter) ([]*domain.MemoryLink, error) {
	if opts.Limit <= 0 {
		opts.Limit = 100
	}
	where, args := "($1 = source_id OR $1 = target_id)", []any{memoryID}
	switch strings.ToLower(opts.Direction) {
	case "outgoing":
		where = "source_id = $1"
	case "incoming":
		where = "target_id = $1"
	}
	if len(opts.RelationshipTypes) > 0 {
		args = append(args, typesToStrings(opts.RelationshipTypes))
		where += fmt.Sprintf(" AND relationship_type = ANY($%d::text[])", len(args))
	}
	if opts.MinWeight != nil {
		args = append(args, *opts.MinWeight)
		where += fmt.Sprintf(" AND weight >= $%d", len(args))
	}
	args = append(args, opts.Limit)
	rows, err := querier(ctx, r.pool).Query(ctx, fmt.Sprintf(
		`SELECT id, source_id, target_id, relationship_type, weight, evidence, created_at
		 FROM memory_links WHERE %s ORDER BY created_at DESC LIMIT $%d`, where, len(args)), args...)
	if err != nil {
		return nil, mapErr(err, nil)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (*domain.MemoryLink, error) {
		var l domain.MemoryLink
		return &l, mapErr(row.Scan(&l.ID, &l.SourceID, &l.TargetID,
			&l.RelationshipType, &l.Weight, &l.Evidence, &l.CreatedAt), nil)
	})
}

// scanMemory maps one memories row onto the domain struct.
func scanMemory(row pgx.Row) (*domain.Memory, error) {
	var (
		m         domain.Memory
		sourceRef []byte
	)
	if err := row.Scan(&m.ID, &m.Type, &m.Content, &m.ContentFormat, &m.Tags,
		&m.Embedding, &m.EmbeddingModel, &m.Origin, &m.SessionID, &m.AgentID, &m.Actor,
		&m.OwnerPrincipalType, &m.OwnerPrincipalID, &sourceRef,
		&m.CreatedAt, &m.UpdatedAt, &m.Confidence, &m.DecayScore,
		&m.ValidFrom, &m.ValidUntil, &m.SupersededBy, &m.TTLExpiresAt, &m.Version,
		&m.AccessScope, &m.SharedSpaceID, &m.DeletedAt); err != nil {
		return nil, mapErr(err, notFound(domain.CodeMemoryNotFound, "memory"))
	}
	if len(sourceRef) > 0 {
		m.SourceRef = &domain.SourceRef{}
		if err := unmarshalJSONB(sourceRef, m.SourceRef); err != nil {
			return nil, err
		}
	}
	return &m, nil
}

// prefixCols qualifies every column list with an alias for joins.
func prefixCols(cols, alias string) string {
	parts := strings.Split(cols, ",")
	for i, p := range parts {
		parts[i] = alias + "." + strings.TrimSpace(p)
	}
	return strings.Join(parts, ", ")
}

// ---------------------------------------------------------------------------
// Cursor codec (api-contracts §1.1: opaque base64 JSON)
// ---------------------------------------------------------------------------

type cursor struct {
	Key any    `json:"k"`
	ID  string `json:"id"`
}

func encodeCursor(c cursor) string {
	b, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(b)
}

func decodeCursor(s string) (cursor, bool) {
	if s == "" {
		return cursor{}, false
	}
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return cursor{}, false
	}
	var c cursor
	if err := json.Unmarshal(b, &c); err != nil {
		return cursor{}, false
	}
	return c, true
}
