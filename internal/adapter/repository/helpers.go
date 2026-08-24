// Package repository implements the port persistence interfaces on
// PostgreSQL via pgx/v5 (architecture.md §2.2 adapter layer).
package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/phanijapps/mneme/internal/domain"
)

// DBTX is the subset of pgxpool.Pool / pgx.Tx the repositories use; every
// method resolves the transaction bound to ctx when one exists.
type DBTX interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// querier returns the executor for ctx: the ambient transaction when WithTx
// bound one, otherwise the pool.
func querier(ctx context.Context, pool *pgxpool.Pool) DBTX {
	if tx, ok := ctx.Value(txCtxKey{}).(pgx.Tx); ok {
		return tx
	}
	return pool
}

// inTx runs fn inside a transaction: it joins the transaction already bound
// to ctx (savepoint-free nesting) or begins and commits a fresh one.
func inTx(ctx context.Context, pool *pgxpool.Pool, fn func(ctx context.Context) error) error {
	if _, ok := ctx.Value(txCtxKey{}).(pgx.Tx); ok {
		return fn(ctx)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return mapErr(err, nil)
	}
	if err := fn(context.WithValue(ctx, txCtxKey{}, tx)); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return mapErr(tx.Commit(ctx), nil)
}

// mapErr translates driver errors into domain errors. notFound (may be nil)
// replaces the generic sentinel for ErrNoRows, so callers get the entity-
// specific 404 code.
func mapErr(err error, notFound *domain.Error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		if notFound == nil {
			return domain.ErrNotFound
		}
		return notFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505": // unique_violation
			return &domain.Error{Code: domain.CodeProposalAlreadyResolved,
				Message: "unique constraint violated", Details: map[string]any{"constraint": pgErr.ConstraintName}}
		case "23503": // foreign_key_violation
			return &domain.Error{Code: domain.CodeValidationErr,
				Message: "referenced row does not exist", Details: map[string]any{"constraint": pgErr.ConstraintName}}
		case "23514": // check_violation
			return &domain.Error{Code: domain.CodeValidationErr,
				Message: "check constraint violated", Details: map[string]any{"constraint": pgErr.ConstraintName}}
		case "23502": // not_null_violation
			return domain.ErrValidation
		default:
			return &domain.Error{Code: domain.CodeInternal, Message: pgErr.Error()}
		}
	}
	return &domain.Error{Code: domain.CodeInternal, Message: err.Error()}
}

// notFound builds an entity-specific 404 domain error.
func notFound(code domain.ErrorCode, entity string) *domain.Error {
	return &domain.Error{Code: code, Message: entity + " not found"}
}

// jsonb marshals v for a JSONB column; nil-ish values (including typed nil
// pointers, which marshal to JSON null) produce SQL NULL so CHECK constraints
// like spaces_retention_chk see a missing column, not jsonb 'null'.
func jsonb(v any) ([]byte, error) {
	if v == nil {
		return nil, nil
	}
	switch t := v.(type) {
	case *string:
		if t == nil {
			return nil, nil
		}
	case []byte:
		return t, nil
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Pointer && rv.IsNil() {
		return nil, nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("marshal jsonb: %w", err)
	}
	return b, nil
}

// unmarshalJSONB decodes a JSONB column value into dst; NULL/empty is a no-op.
func unmarshalJSONB(data []byte, dst any) error {
	if len(data) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, dst); err != nil {
		return fmt.Errorf("unmarshal jsonb: %w", err)
	}
	return nil
}
