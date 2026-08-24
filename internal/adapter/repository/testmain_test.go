//go:build integration

package repository

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moby/moby/api/types/network"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/phanijapps/mneme/internal/adapter/db"
	"github.com/phanijapps/mneme/internal/domain"
)

// testEnv is one pgvector container shared by every repository integration
// test; migrations run once at startup (suites run sequentially, no -parallel).
type testEnv struct {
	pool *pgxpool.Pool
	mem  *MemoryRepo
	spc  *SpaceRepo
	prop *ProposalRepo
	sess *SessionRepo
}

var env *testEnv

func startEnv(m *testing.M) int {
	ctx := context.Background()

	req := testcontainers.ContainerRequest{
		Image:        "pgvector/pgvector:pg16",
		ExposedPorts: []string{"5432/tcp"},
		Env: map[string]string{
			"POSTGRES_USER":     "mneme",
			"POSTGRES_PASSWORD": "mneme",
			"POSTGRES_DB":       "mneme_test",
		},
		WaitingFor: wait.ForSQL("5432/tcp", "pgx", func(host string, port network.Port) string {
			return fmt.Sprintf("postgres://mneme:mneme@%s:%s/mneme_test?sslmode=disable", host, port.Port())
		}),
	}
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		fmt.Println("start container:", err)
		return 1
	}
	defer func() { _ = container.Terminate(ctx) }()

	host, err := container.Host(ctx)
	if err != nil {
		fmt.Println("container host:", err)
		return 1
	}
	port, err := container.MappedPort(ctx, "5432/tcp")
	if err != nil {
		fmt.Println("mapped port:", err)
		return 1
	}
	dbURL := fmt.Sprintf("postgres://mneme:mneme@%s:%s/mneme_test?sslmode=disable", host, port.Port())

	if err := db.RunMigrations(ctx, dbURL); err != nil {
		fmt.Println("migrations:", err)
		return 1
	}

	pool, err := db.NewPool(ctx, db.PoolConfig{URL: dbURL})
	if err != nil {
		fmt.Println("pool:", err)
		return 1
	}
	defer pool.Close()

	env = &testEnv{
		pool: pool,
		mem:  NewMemoryRepo(pool),
		spc:  NewSpaceRepo(pool),
		prop: NewProposalRepo(pool),
		sess: NewSessionRepo(pool),
	}
	return m.Run()
}

func TestMain(m *testing.M) { os.Exit(startEnv(m)) }

// now-ish helper: tests pass explicit UTC timestamps so stored values round-trip
// deterministically (pg timestamptz truncates to microseconds).
func utcNow() time.Time { return time.Now().UTC().Truncate(time.Microsecond) }

// newMemory builds a minimal valid individual-scope memory.
func newMemory(content string) *domain.Memory {
	now := utcNow()
	return &domain.Memory{
		Type:               domain.MemoryTypeEpisodic,
		Content:            content,
		ContentFormat:      domain.ContentFormatPlain,
		Tags:               []string{"integration"},
		Origin:             domain.OriginAgentObservation,
		OwnerPrincipalType: domain.PrincipalUser,
		OwnerPrincipalID:   "user-1",
		CreatedAt:          now,
		UpdatedAt:          now,
		Version:            1,
		AccessScope:        domain.AccessScopeIndividual,
	}
}
