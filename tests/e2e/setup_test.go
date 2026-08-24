//go:build e2e

// Package e2e runs full-stack tests — HTTP router → services → repositories
// → pgvector — against one shared pgvector/pgvector:pg16 testcontainer.
package e2e

import (
	"context"
	"fmt"
	"math"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moby/moby/api/types/network"
	pgvector "github.com/pgvector/pgvector-go"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/phanijapps/mneme/internal/adapter/db"
)

// e2eEnv is the process-wide harness: one container, one pool, migrations
// applied once. Tests stay independent by using unique payloads per test
// (unique tags/content) rather than truncating shared state.
var e2eEnv struct {
	dbURL string
	pool  *pgxpool.Pool
}

func TestMain(m *testing.M) {
	ctx := context.Background()

	req := testcontainers.ContainerRequest{
		Image:        "pgvector/pgvector:pg16",
		ExposedPorts: []string{"5432/tcp"},
		Env: map[string]string{
			"POSTGRES_USER":     "mneme",
			"POSTGRES_PASSWORD": "mneme",
			"POSTGRES_DB":       "mneme_e2e",
		},
		WaitingFor: wait.ForSQL("5432/tcp", "pgx", func(host string, port network.Port) string {
			return fmt.Sprintf("postgres://mneme:mneme@%s:%s/mneme_e2e?sslmode=disable", host, port.Port())
		}),
	}
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		fmt.Println("e2e: start container:", err)
		os.Exit(1)
	}
	defer func() { _ = container.Terminate(ctx) }()

	host, err := container.Host(ctx)
	if err != nil {
		fmt.Println("e2e: container host:", err)
		os.Exit(1)
	}
	port, err := container.MappedPort(ctx, "5432/tcp")
	if err != nil {
		fmt.Println("e2e: mapped port:", err)
		os.Exit(1)
	}
	e2eEnv.dbURL = fmt.Sprintf("postgres://mneme:mneme@%s:%s/mneme_e2e?sslmode=disable", host, port.Port())

	if err := db.RunMigrations(ctx, e2eEnv.dbURL); err != nil {
		fmt.Println("e2e: migrations:", err)
		os.Exit(1)
	}

	pool, err := db.NewPool(ctx, db.PoolConfig{URL: e2eEnv.dbURL})
	if err != nil {
		fmt.Println("e2e: pool:", err)
		os.Exit(1)
	}
	defer pool.Close()
	e2eEnv.pool = pool

	os.Exit(m.Run())
}

// ---------------------------------------------------------------------------
// Deterministic embedding helpers.
// ---------------------------------------------------------------------------

// probeBasis maps probe keywords to basis axes shared by query and seeded
// memories, making dense-path cosine similarity exact and predictable.
var probeBasis = map[string]int{
	"deployment": 0, "kubernetes": 1, "postgres": 2, "vector": 3,
	"budget": 4, "telemetry": 5, "recipes": 6, "pasta": 7, "gardening": 8,
}

// queryVector builds the query embedding for a text query: the normalized
// sum of matched keyword axes; unknown queries fall to a fixed unit vector.
func queryVector(query string) pgvector.Vector {
	v := make([]float32, 1536)
	matched := 0
	for word, axis := range probeBasis {
		if containsFold(query, word) {
			v[axis] = 1
			matched++
		}
	}
	if matched == 0 {
		for i := range v {
			v[i] = float32(1.0 / math.Sqrt(1536))
		}
		return pgvector.NewVector(v)
	}
	var norm float64
	for _, x := range v {
		norm += float64(x) * float64(x)
	}
	s := float32(1.0 / math.Sqrt(norm))
	for i := range v {
		v[i] *= s
	}
	return pgvector.NewVector(v)
}

// seedVector builds a unit 1536-dim vector with 1.0 at axis.
func seedVector(axis int) pgvector.Vector {
	v := make([]float32, 1536)
	v[axis] = 1
	return pgvector.NewVector(v)
}

func containsFold(haystack, needle string) bool {
	h, n := []rune(lower(haystack)), []rune(lower(needle))
	for i := 0; i+len(n) <= len(h); i++ {
		match := true
		for j := range n {
			if h[i+j] != n[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

func lower(s string) string {
	b := []rune(s)
	for i := range b {
		if b[i] >= 'A' && b[i] <= 'Z' {
			b[i] += 'a' - 'A'
		}
	}
	return string(b)
}

// probeEmbedder implements recall.QueryEmbedder deterministically.
type probeEmbedder struct{}

func (probeEmbedder) EmbedQuery(_ context.Context, query string) ([]float32, error) {
	return queryVector(query).Slice(), nil
}

// ---------------------------------------------------------------------------
// Direct-SQL / domain helpers used by tests to arrange deterministic state.
// ---------------------------------------------------------------------------

// addSpaceMembership inserts a membership row directly (the REST surface
// exposes memberships only via space participants).
func addSpaceMembership(t *testing.T, spaceID uuid.UUID, principalType, principalID, access string) {
	t.Helper()
	_, err := e2eEnv.pool.Exec(context.Background(),
		`INSERT INTO space_memberships (space_id, principal_type, principal_id, access_level)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT DO NOTHING`,
		spaceID, principalType, principalID, access)
	if err != nil {
		t.Fatalf("insert membership: %v", err)
	}
}

// memoryRow loads one raw memories row for post-condition checks that must
// bypass service visibility rules (soft-delete retention, supersession
// closure).
func memoryRow(t *testing.T, id uuid.UUID) (version int, validUntil *time.Time, deletedAt *time.Time, supersededBy *uuid.UUID, decayScore *float64, rowExists bool) {
	t.Helper()
	err := e2eEnv.pool.QueryRow(context.Background(),
		`SELECT version, valid_until, deleted_at, superseded_by, decay_score
		   FROM memories WHERE id = $1`,
		id).Scan(&version, &validUntil, &deletedAt, &supersededBy, &decayScore)
	if err != nil {
		if err.Error() == "no rows in result set" {
			return 0, nil, nil, nil, nil, false
		}
		t.Fatalf("load memory row %s: %v", id, err)
	}
	return version, validUntil, deletedAt, supersededBy, decayScore, true
}
