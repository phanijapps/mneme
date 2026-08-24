//go:build integration

package db

import (
	"context"
	"fmt"
	"testing"

	"github.com/moby/moby/api/types/network"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	_ "github.com/jackc/pgx/v5/stdlib" // driver for database/sql used by goose
)

// expectedTables is every table created by migrations 001–018, in catalog order.
var expectedTables = []string{
	"agent_sessions",
	"embedding_models",
	"entities",
	"entity_facts",
	"lifecycle_jobs",
	"memory_access_log",
	"memory_embeddings",
	"memory_entities",
	"memory_links",
	"memories",
	"promotion_proposals",
	"recall_requests",
	"recall_results",
	"shared_memory_spaces",
	"space_memberships",
}

func startPostgres(t *testing.T) string {
	t.Helper()
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
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = container.Terminate(context.Background())
	})

	host, err := container.Host(ctx)
	require.NoError(t, err)
	port, err := container.MappedPort(ctx, "5432/tcp")
	require.NoError(t, err)
	return fmt.Sprintf("postgres://mneme:mneme@%s:%s/mneme_test?sslmode=disable", host, port.Port())
}

func TestMigrationsApplyAndRollback(t *testing.T) {
	ctx := context.Background()
	dbURL := startPostgres(t)

	require.NoError(t, RunMigrations(ctx, dbURL), "migrations must apply cleanly")

	db, err := open(ctx, dbURL)
	require.NoError(t, err)
	defer db.Close()

	t.Run("all tables exist", func(t *testing.T) {
		rows, err := db.Query(`SELECT tablename FROM pg_tables WHERE schemaname = 'public' ORDER BY tablename`)
		require.NoError(t, err)
		defer rows.Close()

		var got []string
		for rows.Next() {
			var name string
			require.NoError(t, rows.Scan(&name))
			if name == "goose_db_version" {
				continue // goose bookkeeping, not a schema table
			}
			got = append(got, name)
		}
		require.NoError(t, rows.Err())
		require.ElementsMatch(t, expectedTables, got)
	})

	t.Run("all 18 versions applied", func(t *testing.T) {
		var n int
		require.NoError(t, db.QueryRow(`SELECT count(*) FROM goose_db_version`).Scan(&n))
		require.Equal(t, 19, n) // 18 migrations + goose's initial version-0 row
	})

	t.Run("seeded embedding models satisfy dims check", func(t *testing.T) {
		var n int
		require.NoError(t, db.QueryRow(`SELECT count(*) FROM embedding_models`).Scan(&n))
		require.Equal(t, 2, n) // 1536 + 768 models only; 3072/384 blocked by dims CHECK
	})

	t.Run("counter trigger installed", func(t *testing.T) {
		var exists bool
		require.NoError(t, db.QueryRow(`SELECT EXISTS (SELECT 1 FROM pg_trigger WHERE tgname = 'trg_proposals_pending_count')`).Scan(&exists))
		require.True(t, exists)
	})

	t.Run("indexes created", func(t *testing.T) {
		want := []string{
			"memories_embedding_hnsw_idx",
			"memories_search_tsv_idx",
			"memories_content_trgm_idx",
			"memories_tags_idx",
			"memories_validity_idx",
			"proposals_one_open_idx",
			"memory_embeddings_hnsw1536_idx",
			"memory_embeddings_hnsw768_idx",
		}
		var got []string
		rows, err := db.Query(`SELECT indexname FROM pg_indexes WHERE schemaname = 'public'`)
		require.NoError(t, err)
		defer rows.Close()
		for rows.Next() {
			var name string
			require.NoError(t, rows.Scan(&name))
			got = append(got, name)
		}
		require.NoError(t, rows.Err())
		for _, w := range want {
			require.Contains(t, got, w, "missing index %s", w)
		}
	})

	t.Run("counter trigger fires", func(t *testing.T) {
		// Space + session + memory + proposal; trigger must bump pending_proposals to 1.
		_, err := db.Exec(`INSERT INTO shared_memory_spaces (id, name, owner_type, owner_id, scope, default_access, write_policy, promote_policy, backend_kind)
			VALUES (gen_random_uuid(), 't', 'user', 'u1', 's', 'read', 'owner_approved', 'human_review', 'relational')`)
		require.NoError(t, err)
		_, err = db.Exec(`INSERT INTO agent_sessions (session_id, agent_type, user_id, model, max_tokens, instruction_slot_budget)
			VALUES (gen_random_uuid(), 'custom', 'u1', 'gpt', 1000, 0)`)
		require.NoError(t, err)
		_, err = db.Exec(`INSERT INTO memories (id, type, content, origin, owner_principal_id, access_scope, session_id)
			VALUES (gen_random_uuid(), 'semantic', 'body', 'user_instruction', 'u1', 'individual', (SELECT session_id FROM agent_sessions LIMIT 1))`)
		require.NoError(t, err)
		_, err = db.Exec(`INSERT INTO promotion_proposals (proposal_id, shared_space_id, candidate_memory_id, target_path, target_kind, target_role, diff)
			SELECT gen_random_uuid(), s.id, m.id, '/docs/x.md', 'memory_doc', 'semantic', 'diff'
			FROM shared_memory_spaces s, memories m LIMIT 1`)
		require.NoError(t, err)

		var pending int
		var sync string
		require.NoError(t, db.QueryRow(`SELECT pending_proposals, sync_status FROM shared_memory_spaces`).Scan(&pending, &sync))
		require.Equal(t, 1, pending)
		require.Equal(t, "pending_review", sync)
	})

	t.Run("rollback leaves clean state", func(t *testing.T) {
		require.NoError(t, RollbackMigrations(ctx, dbURL))

		rows, err := db.Query(`SELECT tablename FROM pg_tables WHERE schemaname = 'public' AND tablename <> 'goose_db_version'`)
		require.NoError(t, err)
		defer rows.Close()
		var got []string
		for rows.Next() {
			var name string
			require.NoError(t, rows.Scan(&name))
			got = append(got, name)
		}
		require.NoError(t, rows.Err())
		require.Empty(t, got, "expected no user tables after rollback, got %v", got)

		// Re-applying after a full reset proves the chain is reversible.
		require.NoError(t, RunMigrations(ctx, dbURL))
		var n int
		require.NoError(t, db.QueryRow(`SELECT count(*) FROM embedding_models`).Scan(&n))
		require.Equal(t, 2, n)
	})
}

func TestNewPoolRegistersVectorTypes(t *testing.T) {
	ctx := context.Background()
	dbURL := startPostgres(t)
	require.NoError(t, RunMigrations(ctx, dbURL))

	pool, err := NewPool(ctx, PoolConfig{URL: dbURL, MaxConns: 2})
	require.NoError(t, err)
	defer pool.Close()

	// Round-trip a 1536-dim vector through memories.embedding via pgx.
	vec := make([]float32, 1536)
	for i := range vec {
		vec[i] = float32(i) / 1536
	}
	lit := "["
	for i, v := range vec {
		if i > 0 {
			lit += ","
		}
		lit += fmt.Sprintf("%f", v)
	}
	lit += "]"

	var id string
	err = pool.QueryRow(ctx, `
		INSERT INTO memories (id, type, content, origin, owner_principal_id, access_scope, embedding, embedding_model)
		VALUES (gen_random_uuid(), 'semantic', 'vec round trip', 'user_instruction', 'u1', 'individual', $1::vector, 'text-embedding-3-small')
		RETURNING id`, lit).Scan(&id)
	require.NoError(t, err)

	var dims int
	require.NoError(t, pool.QueryRow(ctx, `SELECT vector_dims(embedding) FROM memories WHERE id = $1`, id).Scan(&dims))
	require.Equal(t, 1536, dims)
}
