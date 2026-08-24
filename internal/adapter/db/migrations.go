// Package db provides the pgx connection pool and the goose migration runner
// for the mneme PostgreSQL schema.
package db

import (
	"context"
	"database/sql"
	"fmt"

	_ "github.com/jackc/pgx/v5/stdlib" // database/sql driver for goose
	"github.com/pressly/goose/v3"

	"github.com/phanijapps/mneme/migrations"
)

// migrationsDir is the root of the embedded FS (files live at the top level).
const migrationsDir = "."

// RunMigrations applies every embedded goose migration in order (goose up).
func RunMigrations(ctx context.Context, dbURL string) error {
	db, err := open(ctx, dbURL)
	if err != nil {
		return err
	}
	defer db.Close()

	goose.SetBaseFS(migrations.FS)
	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("set goose dialect: %w", err)
	}
	if err := goose.UpContext(ctx, db, migrationsDir); err != nil {
		return fmt.Errorf("goose up: %w", err)
	}
	return nil
}

// RollbackMigrations reverts every applied migration (goose reset), returning
// the database to an empty schema. Intended for tests and teardown.
func RollbackMigrations(ctx context.Context, dbURL string) error {
	db, err := open(ctx, dbURL)
	if err != nil {
		return err
	}
	defer db.Close()

	goose.SetBaseFS(migrations.FS)
	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("set goose dialect: %w", err)
	}
	if err := goose.ResetContext(ctx, db, migrationsDir); err != nil {
		return fmt.Errorf("goose reset: %w", err)
	}
	return nil
}

func open(ctx context.Context, dbURL string) (*sql.DB, error) {
	db, err := sql.Open("pgx", dbURL)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return db, nil
}
