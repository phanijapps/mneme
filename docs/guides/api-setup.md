# REST API Setup Guide

mneme's REST surface (`mneme-api`) serves 26 authenticated endpoints under
`/api/v1`, a health probe, and Swagger UI. This guide builds, runs, and
containerizes it.

---

## Environment variables

| Variable | Default | Purpose |
|----------|---------|---------|
| `DATABASE_URL` | `postgres://localhost:5432/mneme?sslmode=disable` | PostgreSQL connection string (see [db-setup.md](db-setup.md)) |
| `API_PORT` (or `PORT`) | `8080` | HTTP listen port |
| `X-API-Key` / `Authorization: Bearer` | — | Request credentials (below) |
| `LOG_LEVEL` | `info` | Log verbosity |

Notes:

- Auth is enforced by `AuthMiddleware` on the whole `/api/v1` surface via
  `Authorization: Bearer <token>` **or** `X-API-Key: <token>`. v1 ships a
  stub resolver: tokens of the form `user:<id>`, `agent:<id>`,
  `service:<id>`, `group:<id>` map to that principal type; any bare token
  is treated as an agent ID. Swap in your IdP by replacing the resolver.
- CORS is currently permissive (`*`, all methods) via `CORSMiddleware`; if
  you need origin restrictions (`MNEME_CORS_ORIGINS`-style allow-list),
  that is the single place to tighten it.
- `MNEME_LOG_LEVEL` / `LOG_LEVEL` sets the service log level.

## Building

```bash
git clone https://github.com/phanijapps/mneme.git
cd mneme
go build ./cmd/mneme-api    # → ./mneme-api
```

## Running locally

```bash
export DATABASE_URL="postgres://postgres:postgres@localhost:5432/mneme?sslmode=disable"
export API_PORT=8080

./mneme-api
# {"level":"INFO","msg":"mneme-api listening","addr":":8080"}
```

Migrations run automatically at startup (embedded goose, see
[db-setup.md](db-setup.md)); the server refuses to start if they fail.

## Docker containerization

`Dockerfile` (multi-stage):

```dockerfile
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /mneme-api ./cmd/mneme-api

FROM alpine:3.20
RUN adduser -D -u 10001 mneme
USER mneme
COPY --from=build /mneme-api /usr/local/bin/mneme-api
EXPOSE 8080
ENTRYPOINT ["mneme-api"]
```

Build and run:

```bash
docker build -t mneme-api .
docker run -d --name mneme-api -p 8080:8080 \
  -e DATABASE_URL="postgres://postgres:postgres@host.docker.internal:5432/mneme?sslmode=disable" \
  mneme-api
```

## Docker Compose

`docker-compose.yml` — API + PostgreSQL/pgvector together:

```yaml
services:
  db:
    image: pgvector/pgvector:pg16
    environment:
      POSTGRES_PASSWORD: postgres
      POSTGRES_DB: mneme
    ports:
      - "5432:5432"
    volumes:
      - mneme-pgdata:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U postgres -d mneme"]
      interval: 5s
      timeout: 3s
      retries: 10

  api:
    build: .
    depends_on:
      db:
        condition: service_healthy
    environment:
      DATABASE_URL: postgres://postgres:postgres@db:5432/mneme?sslmode=disable
      API_PORT: "8080"
    ports:
      - "8080:8080"

volumes:
  mneme-pgdata:
```

```bash
docker compose up -d        # db boots first (healthcheck), then the API
docker compose logs -f api
```

## Swagger UI

Interactive OpenAPI docs are served at:

```
http://localhost:8080/swagger/index.html
```

The UI is backed by swaggo annotations on every handler in
`internal/adapter/transport/http/handlers.go` (plus the general API info in
`internal/adapter/transport/http/docs.go`).

## API endpoints overview

26 endpoints across 5 groups (full contracts: [api-contracts.md](../api-contracts.md)):

| Group | Endpoints |
|-------|-----------|
| **Memories** (7) | `POST /memories` · `GET /memories` · `GET /memories/{id}` · `PUT /memories/{id}` · `DELETE /memories/{id}` · `POST /memories/{id}/links` · `GET /memories/{id}/links` |
| **Recall** (2) | `POST /recall` · `GET /recall/{request_id}` |
| **Sessions** (5) | `POST /sessions` · `GET /sessions/{session_id}` · `POST /sessions/{session_id}/memories` · `DELETE /sessions/{session_id}/memories/{memory_id}` · `POST /sessions/{session_id}/end` |
| **Spaces** (9) | `POST /spaces` · `GET /spaces` · `GET /spaces/{space_id}` · `PUT /spaces/{space_id}` · `POST /spaces/{space_id}/memories` · `GET /spaces/{space_id}/proposals` · `POST /spaces/{space_id}/proposals/{proposal_id}/approve` · `POST /spaces/{space_id}/proposals/{proposal_id}/reject` · `POST /spaces/{space_id}/sync` |
| **Lifecycle** (4) | `POST /lifecycle/consolidate` · `POST /lifecycle/decay` · `GET /lifecycle/jobs/{job_id}` · `GET /lifecycle/stats` |

All group paths are prefixed with `/api/v1`.

## Authentication

Every `/api/v1` request must carry credentials; `GET /health` and
`/swagger/*` are public:

```bash
# Bearer form
curl http://localhost:8080/api/v1/memories \
  -H "Authorization: Bearer agent:my-agent-1"

# API-key form (equivalent)
curl http://localhost:8080/api/v1/memories \
  -H "X-API-Key: agent:my-agent-1"
```

Missing/invalid credentials return a `401 UNAUTHENTICATED` error envelope
with a `request_id` for tracing. The resolved principal is also
server-stamped into memory provenance on writes, so clients cannot forge
ownership (api-contracts finding F21).

## Health check

```bash
curl http://localhost:8080/health
# {"status":"ok"}
```

Use it as the container healthcheck / load-balancer probe; it requires no
auth and confirms the router is serving.
