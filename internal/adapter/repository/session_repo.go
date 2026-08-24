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

// SessionRepo implements port.AgentSessionRepository on PostgreSQL. Budget
// enforcement lives in the CHECK constraints; activation maps violations to
// domain.ErrSlotBudgetExceeded.
type SessionRepo struct {
	pool *pgxpool.Pool
}

// NewSessionRepo returns an AgentSessionRepository bound to pool.
func NewSessionRepo(pool *pgxpool.Pool) *SessionRepo { return &SessionRepo{pool: pool} }

var _ port.AgentSessionRepository = (*SessionRepo)(nil)

const sessionCols = `session_id, agent_type, user_id, shared_space_id, model, max_tokens,
	used_tokens, instruction_slot_budget, slots_used, active_memories,
	injection_order, created_at, ended_at, summary`

// Create inserts a session and returns the stored row.
func (r *SessionRepo) Create(ctx context.Context, s *domain.AgentSession) (*domain.AgentSession, error) {
	if s.SessionID == uuid.Nil {
		s.SessionID = uuid.Must(uuid.NewV7())
	}
	if s.CreatedAt.IsZero() {
		s.CreatedAt = time.Now().UTC()
	}
	row := querier(ctx, r.pool).QueryRow(ctx, `INSERT INTO agent_sessions
		(session_id, agent_type, user_id, shared_space_id, model, max_tokens,
		 used_tokens, instruction_slot_budget, slots_used, active_memories,
		 injection_order, created_at, ended_at, summary)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
		RETURNING `+sessionCols,
		s.SessionID, s.AgentType, s.UserID, s.SharedSpaceID, s.ContextWindow.Model,
		s.ContextWindow.MaxTokens, s.ContextWindow.UsedTokens,
		s.ContextWindow.InstructionSlotBudget, s.ContextWindow.SlotsUsed,
		uuidsToSlice(s.ActiveMemories), s.InjectionOrder, s.CreatedAt, s.EndedAt, s.Summary)
	return scanSession(row)
}

// GetByID fetches one session.
func (r *SessionRepo) GetByID(ctx context.Context, sessionID uuid.UUID) (*domain.AgentSession, error) {
	return scanSession(querier(ctx, r.pool).QueryRow(ctx,
		`SELECT `+sessionCols+` FROM agent_sessions WHERE session_id = $1`, sessionID))
}

// Update rewrites the mutable context-window fields of a session.
func (r *SessionRepo) Update(ctx context.Context, s *domain.AgentSession) (*domain.AgentSession, error) {
	row := querier(ctx, r.pool).QueryRow(ctx, `UPDATE agent_sessions SET
		shared_space_id = $2, max_tokens = $3, used_tokens = $4,
		instruction_slot_budget = $5, slots_used = $6, active_memories = $7,
		injection_order = $8, summary = $9
		WHERE session_id = $1
		RETURNING `+sessionCols,
		s.SessionID, s.SharedSpaceID, s.ContextWindow.MaxTokens, s.ContextWindow.UsedTokens,
		s.ContextWindow.InstructionSlotBudget, s.ContextWindow.SlotsUsed,
		uuidsToSlice(s.ActiveMemories), s.InjectionOrder, s.Summary)
	return scanSession(row)
}

// EndSession stamps ended_at and the closing summary; a second end returns
// SESSION_ALREADY_ENDED (idempotent guard).
func (r *SessionRepo) EndSession(ctx context.Context, sessionID uuid.UUID, summary *string) (*domain.AgentSession, error) {
	now := time.Now().UTC()
	row := querier(ctx, r.pool).QueryRow(ctx, `UPDATE agent_sessions
		SET ended_at = $2, summary = COALESCE($3, summary)
		WHERE session_id = $1 AND ended_at IS NULL
		RETURNING `+sessionCols, sessionID, now, summary)
	s, err := scanSession(row)
	if err != nil {
		if de, ok := err.(*domain.Error); ok && de.Code == domain.CodeSessionNotFound {
			// distinguish already-ended from missing: re-read
			if _, gerr := r.GetByID(ctx, sessionID); gerr == nil {
				return nil, &domain.Error{Code: domain.CodeSessionAlreadyEnded,
					Message: "session " + sessionID.String() + " is already ended"}
			}
		}
		return nil, err
	}
	return s, nil
}

// ActivateMemory adds a memory to the working set and appends a
// session_activate access-log row; slot-budget violations surface as
// ErrSlotBudgetExceeded (CHECK agent_sessions_slot_budget_chk).
func (r *SessionRepo) ActivateMemory(ctx context.Context, sessionID, memoryID uuid.UUID) (*domain.AgentSession, error) {
	var out *domain.AgentSession
	err := inTx(ctx, r.pool, func(tctx context.Context) error {
		q := querier(tctx, r.pool)
		row := q.QueryRow(tctx, `UPDATE agent_sessions
			SET active_memories = array_append(active_memories, $2),
			    slots_used = CASE WHEN instruction_slot_budget = 0 THEN slots_used
			                      ELSE slots_used + 1 END
			WHERE session_id = $1 AND ended_at IS NULL
			  AND NOT ($2 = ANY (active_memories))
			RETURNING `+sessionCols, sessionID, memoryID)
		s, err := scanSession(row)
		if err != nil {
			return mapActivateErr(err, "session")
		}
		if _, err := q.Exec(tctx, `INSERT INTO memory_access_log
			(memory_id, accessed_by, access_type) VALUES ($1, $2, $3)`,
			memoryID, sessionID, domain.AccessTypeSessionActivate); err != nil {
			return mapErr(err, nil)
		}
		out = s
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// DeactivateMemory removes a memory from the working set (idempotent).
func (r *SessionRepo) DeactivateMemory(ctx context.Context, sessionID, memoryID uuid.UUID) (*domain.AgentSession, error) {
	row := querier(ctx, r.pool).QueryRow(ctx, `UPDATE agent_sessions
		SET active_memories = array_remove(active_memories, $2),
		    slots_used = CASE WHEN instruction_slot_budget = 0 THEN slots_used
		                      ELSE greatest(slots_used - 1, 0) END
		WHERE session_id = $1
		RETURNING `+sessionCols, sessionID, memoryID)
	s, err := scanSession(row)
	if err != nil {
		return nil, mapActivateErr(err, "session")
	}
	return s, nil
}

// mapActivateErr upgrades CHECK violations from activation to budget errors
// and row misses to SESSION_NOT_FOUND.
func mapActivateErr(err error, entity string) error {
	if err == nil {
		return nil
	}
	if de, ok := err.(*domain.Error); ok {
		if de.Code == domain.CodeValidationErr {
			if de.Details["constraint"] == "agent_sessions_slot_budget_chk" {
				return domain.ErrSlotBudgetExceeded
			}
		}
		if de.Message == entity+" not found" {
			return notFound(domain.CodeSessionNotFound, "session")
		}
	}
	return err
}

func scanSession(row pgx.Row) (*domain.AgentSession, error) {
	var (
		s     domain.AgentSession
		uuids []uuid.UUID
	)
	if err := row.Scan(&s.SessionID, &s.AgentType, &s.UserID, &s.SharedSpaceID,
		&s.ContextWindow.Model, &s.ContextWindow.MaxTokens, &s.ContextWindow.UsedTokens,
		&s.ContextWindow.InstructionSlotBudget, &s.ContextWindow.SlotsUsed,
		&uuids, &s.InjectionOrder, &s.CreatedAt, &s.EndedAt, &s.Summary); err != nil {
		return nil, mapErr(err, notFound(domain.CodeSessionNotFound, "session"))
	}
	if uuids != nil {
		s.ActiveMemories = uuids
	}
	return &s, nil
}

func uuidsToSlice(us []uuid.UUID) []uuid.UUID {
	if us == nil {
		return []uuid.UUID{}
	}
	return us
}
