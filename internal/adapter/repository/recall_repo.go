package repository

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/phanijapps/mneme/internal/domain"
	"github.com/phanijapps/mneme/internal/port"
)

// RecallRepo implements both port.RecallRequestRepository and
// port.RecallResultRepository on PostgreSQL.
type RecallRepo struct {
	pool *pgxpool.Pool
}

// NewRecallRepo returns a repository bound to pool implementing both recall
// repository interfaces.
func NewRecallRepo(pool *pgxpool.Pool) *RecallRepo { return &RecallRepo{pool: pool} }

var (
	_ port.RecallRequestRepository = (*RecallRepo)(nil)
	_ port.RecallResultRepository  = (*RecallRepo)(nil)
)

const recallRequestCols = `request_id, query, context, trigger, agent_id, session_id,
	strategies, top_k, rerank, min_score, slot_budget, token_budget,
	mode, status, error, requested_at`

// Create inserts a recall request (operational log record).
func (r *RecallRepo) Create(ctx context.Context, req *domain.RecallRequest) (*domain.RecallRequest, error) {
	if req.RequestID == uuid.Nil {
		req.RequestID = uuid.Must(uuid.NewV7())
	}
	ctxJSON, err := jsonb(req.Context)
	if err != nil {
		return nil, err
	}
	errJSON, err := jsonb(req.Error)
	if err != nil {
		return nil, err
	}
	if len(req.RetrievalParams.Strategies) == 0 {
		req.RetrievalParams.Strategies = []domain.StrategyType{domain.StrategyVector}
	}
	if req.RetrievalParams.TopK == 0 {
		req.RetrievalParams.TopK = 50
	}
	if req.RetrievalParams.Rerank == "" {
		req.RetrievalParams.Rerank = domain.RerankCrossEncoder
	}
	if req.RetrievalParams.MinScore == 0 {
		req.RetrievalParams.MinScore = 0.35
	}
	if req.Mode == "" {
		req.Mode = domain.RecallModeSync
	}
	if req.Status == "" {
		req.Status = domain.RecallStatusCompleted
	}
	row := querier(ctx, r.pool).QueryRow(ctx, `INSERT INTO recall_requests
		(request_id, query, context, trigger, agent_id, session_id,
		 strategies, top_k, rerank, min_score, slot_budget, token_budget,
		 mode, status, error, requested_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)
		RETURNING `+recallRequestCols,
		req.RequestID, req.Query, ctxJSON, req.Trigger, req.AgentID, req.SessionID,
		typesToStrings(req.RetrievalParams.Strategies), req.RetrievalParams.TopK,
		req.RetrievalParams.Rerank, req.RetrievalParams.MinScore,
		req.RetrievalParams.SlotBudget, req.RetrievalParams.TokenBudget,
		req.Mode, req.Status, errJSON, req.RequestedAt)
	return scanRecallRequest(row)
}

// GetByID fetches one recall request.
func (r *RecallRepo) GetByID(ctx context.Context, requestID uuid.UUID) (*domain.RecallRequest, error) {
	return scanRecallRequest(querier(ctx, r.pool).QueryRow(ctx,
		`SELECT `+recallRequestCols+` FROM recall_requests WHERE request_id = $1`, requestID))
}

// UpdateStatus advances queued→running→completed|failed; failure must carry
// the domain error (CHECK recall_requests_status_pairing_chk).
func (r *RecallRepo) UpdateStatus(ctx context.Context, requestID uuid.UUID, status domain.RecallStatus, failure *domain.Error) error {
	errJSON, err := jsonb(failure)
	if err != nil {
		return err
	}
	res, err := querier(ctx, r.pool).Exec(ctx, `UPDATE recall_requests
		SET status = $2, error = $3 WHERE request_id = $1`,
		requestID, status, errJSON)
	if err != nil {
		return mapErr(err, nil)
	}
	if res.RowsAffected() == 0 {
		return notFound(domain.CodeRecallNotFound, "recall request")
	}
	return nil
}

const recallResultCols = `result_id, request_id, candidates, injection_plan,
	slots_used, tokens_used, latency_ms, created_at`

// Save upserts the single result of a request (UNIQUE(request_id)).
func (r *RecallRepo) Save(ctx context.Context, res *domain.RecallResult) (*domain.RecallResult, error) {
	if res.ResultID == uuid.Nil {
		res.ResultID = uuid.Must(uuid.NewV7())
	}
	cands, err := jsonb(res.Candidates)
	if err != nil {
		return nil, err
	}
	plan, err := jsonb(res.InjectionPlan)
	if err != nil {
		return nil, err
	}
	row := querier(ctx, r.pool).QueryRow(ctx, `INSERT INTO recall_results
		(result_id, request_id, candidates, injection_plan, slots_used,
		 tokens_used, latency_ms, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		ON CONFLICT (request_id) DO UPDATE SET
		  candidates = EXCLUDED.candidates, injection_plan = EXCLUDED.injection_plan,
		  slots_used = EXCLUDED.slots_used, tokens_used = EXCLUDED.tokens_used,
		  latency_ms = EXCLUDED.latency_ms
		RETURNING `+recallResultCols,
		res.ResultID, res.RequestID, cands, plan, res.SlotsUsed, res.TokensUsed,
		res.LatencyMS, res.CreatedAt)
	return scanRecallResult(row)
}

// ListByRequest returns the (at most one) result of a request.
func (r *RecallRepo) ListByRequest(ctx context.Context, requestID uuid.UUID) ([]*domain.RecallResult, error) {
	rows, err := querier(ctx, r.pool).Query(ctx,
		`SELECT `+recallResultCols+` FROM recall_results WHERE request_id = $1`, requestID)
	if err != nil {
		return nil, mapErr(err, nil)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (*domain.RecallResult, error) {
		return scanRecallResult(row)
	})
}

func scanRecallRequest(row pgx.Row) (*domain.RecallRequest, error) {
	var (
		req      domain.RecallRequest
		ctxJSON  []byte
		errJSON  []byte
		strats   []string
		minScore float64
	)
	if err := row.Scan(&req.RequestID, &req.Query, &ctxJSON, &req.Trigger, &req.AgentID,
		&req.SessionID, &strats, &req.RetrievalParams.TopK, &req.RetrievalParams.Rerank,
		&minScore, &req.RetrievalParams.SlotBudget, &req.RetrievalParams.TokenBudget,
		&req.Mode, &req.Status, &errJSON, &req.RequestedAt); err != nil {
		return nil, mapErr(err, notFound(domain.CodeRecallNotFound, "recall request"))
	}
	req.RetrievalParams.MinScore = minScore
	if err := unmarshalJSONB(ctxJSON, &req.Context); err != nil {
		return nil, err
	}
	if len(errJSON) > 0 {
		req.Error = &domain.Error{}
		if err := unmarshalJSONB(errJSON, req.Error); err != nil {
			return nil, err
		}
	}
	req.RetrievalParams.Strategies = make([]domain.StrategyType, len(strats))
	for i, s := range strats {
		req.RetrievalParams.Strategies[i] = domain.StrategyType(s)
	}
	return &req, nil
}

func scanRecallResult(row pgx.Row) (*domain.RecallResult, error) {
	var (
		res  domain.RecallResult
		c    []byte
		p    []byte
		late *int
	)
	if err := row.Scan(&res.ResultID, &res.RequestID, &c, &p, &res.SlotsUsed,
		&res.TokensUsed, &late, &res.CreatedAt); err != nil {
		return nil, mapErr(err, notFound(domain.CodeRecallNotFound, "recall result"))
	}
	res.LatencyMS = late
	if err := unmarshalJSONB(c, &res.Candidates); err != nil {
		return nil, err
	}
	if err := unmarshalJSONB(p, &res.InjectionPlan); err != nil {
		return nil, err
	}
	return &res, nil
}
