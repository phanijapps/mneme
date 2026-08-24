package repository

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// txCtxKey is the private key under which WithTx binds the live pgx.Tx.
type txCtxKey struct{}

// TxManager implements port.TransactionManager over a pgxpool.Pool:
// repositories executed inside fn transparently join the transaction
// through the ctx binding.
type TxManager struct {
	pool *pgxpool.Pool
}

// NewTxManager returns a TransactionManager bound to pool.
func NewTxManager(pool *pgxpool.Pool) *TxManager { return &TxManager{pool: pool} }

// WithTx runs fn inside a single database transaction. fn's error rolls
// back; a nil return commits. Nested WithTx calls join the outer tx.
func (m *TxManager) WithTx(ctx context.Context, fn func(ctx context.Context) error) error {
	return inTx(ctx, m.pool, fn)
}
