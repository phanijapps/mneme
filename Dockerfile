# syntax=docker/dockerfile:1

# ---- Stage 1: build both binaries ----
# CGO_ENABLED=0: the api and viz binaries are pure Go (chi/pgx/pgvector-go)
# and link statically, so the runtime image needs no libc scaffolding.
FROM golang:1.26 AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/mneme-api ./cmd/mneme-api/ \
 && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/mneme-viz ./cmd/mneme-viz/

# ---- Stage 2: minimal runtime, both binaries in one image ----
# One image, selected per service via compose `command:`. debian-slim rather
# than scratch so healthcheck tooling (wget/curl) and future CGO builds work.
FROM debian:bookworm-slim

RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates wget \
 && rm -rf /var/lib/apt/lists/*

COPY --from=builder /out/mneme-api /out/mneme-api
COPY --from=builder /out/mneme-viz /out/mneme-viz

EXPOSE 8080 8090

# Default to the API; compose overrides `command:` per service. CMD, not
# ENTRYPOINT — an exec-form ENTRYPOINT would turn compose's command into
# arguments instead of replacing the binary.
CMD ["/out/mneme-api"]
