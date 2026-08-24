//go:build e2e

package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/phanijapps/mneme/internal/adapter/recall"
	"github.com/phanijapps/mneme/internal/adapter/repository"
	httptransport "github.com/phanijapps/mneme/internal/adapter/transport/http"
	"github.com/phanijapps/mneme/internal/domain"
	"github.com/phanijapps/mneme/internal/service"
)

// newTestServer wires repositories → services → chi router over the shared
// pool and serves it on a random local port; returns a client hitting the
// full middleware chain (auth, request-id, error envelopes).
func newTestServer(t *testing.T) *apiClient {
	t.Helper()
	pool := e2eEnv.pool

	memRepo := repository.NewMemoryRepo(pool)
	sessionRepo := repository.NewSessionRepo(pool)
	spaceRepo := repository.NewSpaceRepo(pool)
	proposalRepo := repository.NewProposalRepo(pool)
	recallRepo := repository.NewRecallRepo(pool)
	lifecycleRepo := repository.NewLifecycleRepo(pool)
	tx := repository.NewTxManager(pool)
	engine := recall.NewEngine(pool, recall.Options{Embedder: probeEmbedder{}})

	memSvc := service.NewMemoryService(memRepo, service.WithTx(tx))
	recallSvc := service.NewRecallService(engine, recallRepo, recallRepo,
		service.WithRecallSessions(sessionRepo), service.WithRecallTx(tx))
	sessionSvc := service.NewSessionService(sessionRepo,
		service.WithSessionMemories(memRepo), service.WithSessionJobs(lifecycleRepo))
	spaceSvc := service.NewSpaceService(spaceRepo,
		service.WithSpaceProposals(proposalRepo), service.WithSpaceMemories(memRepo),
		service.WithSpaceJobs(lifecycleRepo), service.WithSpaceTx(tx))
	lifecycleSvc := service.NewLifecycleService(lifecycleRepo)

	return serve(t, httptransport.NewRouter(httptransport.NewHandlers(
		memSvc, recallSvc, sessionSvc, spaceSvc, lifecycleSvc)))
}

// saveMemoryWithVector stores a memory with a raw 1536-dim embedding
// directly via the repository (bypassing any encoder) so dense-path rankings
// are fully deterministic.
func saveMemoryWithVector(t *testing.T, memType, content string, tags []string, axis int, origin string) uuid.UUID {
	t.Helper()
	memRepo := repository.NewMemoryRepo(e2eEnv.pool)
	now := time.Now().UTC().Truncate(time.Microsecond)
	mem := &domain.Memory{
		Type:               domain.MemoryType(memType),
		Content:            content,
		ContentFormat:      domain.ContentFormatPlain,
		Tags:               tags,
		Origin:             domain.Origin(origin),
		OwnerPrincipalType: domain.PrincipalAgent,
		OwnerPrincipalID:   "e2e-agent",
		AgentID:            strPtr("e2e-agent"),
		Actor:              strPtr("e2e-agent"),
		AccessScope:        domain.AccessScopeIndividual,
		Version:            1,
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	vec := seedVector(axis)
	mem.Embedding = &vec
	saved, err := memRepo.Save(context.Background(), mem)
	if err != nil {
		t.Fatalf("save seeded memory: %v", err)
	}
	return saved.ID
}

func strPtr(s string) *string { return &s }
