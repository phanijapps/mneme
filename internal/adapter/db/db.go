package db

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	pgxvec "github.com/pgvector/pgvector-go/pgx"
)

// PoolConfig configures NewPool. URL is a PostgreSQL connection string;
// pool sizing fields are optional and fall back to pgxpool defaults.
type PoolConfig struct {
	URL            string
	MaxConns       int32
	MinConns       int32
	MaxConnIdleSec int64
}

// NewPool creates a pgx connection pool with the pgvector types (vector,
// halfvec, sparsevec) registered on every connection via AfterConnect, so
// pgvector.Vector values bind and scan natively.
func NewPool(ctx context.Context, cfg PoolConfig) (*pgxpool.Pool, error) {
	if cfg.URL == "" {
		return nil, fmt.Errorf("db: connection URL is required")
	}

	poolCfg, err := pgxpool.ParseConfig(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("db: parse connection URL: %w", err)
	}
	if cfg.MaxConns > 0 {
		poolCfg.MaxConns = cfg.MaxConns
	}
	if cfg.MinConns > 0 {
		poolCfg.MinConns = cfg.MinConns
	}
	if cfg.MaxConnIdleSec > 0 {
		poolCfg.MaxConnIdleTime = time.Duration(cfg.MaxConnIdleSec) * time.Second
	}

	poolCfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		return pgxvec.RegisterTypes(ctx, conn)
	}

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("db: create pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("db: ping: %w", err)
	}
	return pool, nil
}
