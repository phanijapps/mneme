//go:build tools

// Package tools pins build-time and test-time module dependencies so that
// `go mod tidy` retains them in go.mod even before implementation code
// imports them directly. This follows the pattern used by golangci-lint,
// swaggo, and other Go projects for tool dependency tracking.
//
// This package is excluded from normal builds via the `tools` build tag.
package tools

import (
	_ "github.com/go-chi/chi/v5"
	_ "github.com/go-playground/validator/v10"
	_ "github.com/google/uuid"
	_ "github.com/jackc/pgx/v5"
	_ "github.com/mark3labs/mcp-go/mcp"
	_ "github.com/mark3labs/mcp-go/server"
	_ "github.com/pgvector/pgvector-go"
	_ "github.com/pressly/goose/v3"
	_ "github.com/stretchr/testify/assert"
	_ "github.com/stretchr/testify/require"
	_ "github.com/swaggo/http-swagger"
	_ "github.com/swaggo/swag"
	_ "github.com/testcontainers/testcontainers-go"
)
