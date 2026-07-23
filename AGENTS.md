# Go-Commerce project instructions

## What this project is

A quick-commerce backend in Go with:

- JWT registration and login
- PostgreSQL products and stock
- Redis carts with an abandoned-cart TTL
- Transactional order placement with row locking
- Idempotent order creation
- A goroutine/channel worker pool for order notifications
- Structured `log/slog` logging and graceful shutdown

## Toolchain

- Go 1.27.3 on Windows
- PowerShell is the default shell
- Docker Compose v5
- Local API: `http://localhost:8080`
- Host PostgreSQL: `localhost:5434`
- Host Redis: `localhost:6379`

The host ports are deliberate: `5432` is used by a native PostgreSQL service and
`5433` is used by an existing `ecom-pg` container. Do not stop or modify those
containers.

## Common commands

```powershell
# Infrastructure
docker compose up -d postgres redis
docker compose ps

# Run the API locally
go run ./cmd/api

# Unit tests
go test -race -count=1 ./...

# Integration tests (requires the Compose services)
$env:TEST_DATABASE_URL = "postgres://postgres:postgres@localhost:5434/go_commerce_test?sslmode=disable"
$env:TEST_REDIS_ADDR = "localhost:6379"
$env:TEST_REDIS_DB = "15"
go test -race -count=1 ./tests/integration/...

# Full containerized stack
docker compose up --build -d
docker compose down
```

Never use `docker compose down -v` unless the user explicitly asks to erase the
local database.

## Code rules

- Pass `context.Context` through every database, Redis, and HTTP-facing call.
- Use `log/slog`; do not add `fmt.Println` logging.
- Store money as integer cents. Never use floating point for prices.
- Never trust prices or totals sent by a client. Recalculate on the server.
- Stock changes happen only inside a PostgreSQL transaction with row locks.
- Order creation requires an `Idempotency-Key` and must stay replay-safe.
- Redis cart keys must refresh their TTL on every mutation.
- HTTP handlers return the shared JSON error envelope.
- Integration tests use real PostgreSQL and Redis. Do not mock infrastructure
  that this project owns.
- Use `httptest` for HTTP tests; use fakes only for notification/email stubs.
- Run tests with `-race -count=1`.

## Testing setup

- Project skill: `.opencode/skills/go-commerce-test-hardening/SKILL.md`
- Test subagent: `.opencode/agents/go-test-hardener.md`
- Integration tests live under `tests/integration`.
- The test database is created by the test harness; do not point tests at the
  development database.
- Redis tests use DB 15 by default, never the development DB 0.

## Commit rules

Read `docs/commit-conventions.md` before committing.

- Use a type and scope: `feat(auth): ...`, `fix(cart): ...`, `test(orders): ...`.
- The subject must state exactly what changed. Generic messages are forbidden.
- One logical change per commit.
- Every commit must build and pass the test suite.
- Do not commit secrets, `.env`, coverage files, or binary artifacts.
