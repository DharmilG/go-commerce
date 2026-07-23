# Go-Commerce

A quick-commerce backend in Go. It covers JWT authentication, a PostgreSQL
catalog, Redis carts with an abandoned-cart TTL, transactional checkout that
cannot oversell stock, and idempotent order creation.

## What it does

- Register and log in with bcrypt-hashed passwords and HS256 access tokens.
- Browse a PostgreSQL-backed product catalog; admins can create and update
  products.
- Keep carts in Redis hashes with per-mutation TTL refreshes.
- Place orders with server-side price recalculation, row-locked stock
  decrements, and all-or-nothing transactions.
- Retry checkout safely with an `Idempotency-Key`; replays return the original
  response without consuming stock twice.
- Process order notifications through a bounded goroutine worker pool with
  retries, panic recovery, and drain-on-shutdown.
- Serve structured `log/slog` logs, request IDs, readiness checks, and graceful
  shutdown.

## Stack

- Go 1.27
- PostgreSQL 17
- Redis 7
- Docker Compose
- `net/http` with Go 1.22+ route patterns, `pgx`, `go-redis`, `log/slog`
- `golang-migrate` with embedded SQL

## Local ports

| Service | Port |
|---|---:|
| API | 8080 |
| PostgreSQL | 5434 |
| Redis | 6379 |

PostgreSQL is published on `5434` because `5432` is used by a native PostgreSQL
service and `5433` is used by another local container.

## Quickstart

```powershell
Copy-Item .env.example .env
docker compose up -d postgres redis
go run ./cmd/seed
go run ./cmd/api
```

In a second terminal:

```powershell
Invoke-RestMethod http://localhost:8080/healthz
Invoke-RestMethod http://localhost:8080/readyz
```

The seed command applies migrations, upserts demo products, and creates the
admin user configured by `ADMIN_EMAIL` and `ADMIN_PASSWORD`.

## Containerized stack

```powershell
docker compose up --build -d
docker compose ps
docker compose run --rm --entrypoint /app/seed api
```

Stop it without erasing local data:

```powershell
docker compose down
```

## Main endpoints

| Method | Path | Auth |
|---|---|---|
| `POST` | `/api/v1/auth/register` | Public |
| `POST` | `/api/v1/auth/login` | Public |
| `GET` | `/api/v1/auth/me` | Bearer |
| `GET` | `/api/v1/products` | Public |
| `GET` | `/api/v1/products/{id}` | Public |
| `GET` | `/api/v1/cart` | Bearer |
| `POST` | `/api/v1/cart/items` | Bearer |
| `PUT` | `/api/v1/cart/items/{productID}` | Bearer |
| `DELETE` | `/api/v1/cart/items/{productID}` | Bearer |
| `DELETE` | `/api/v1/cart` | Bearer |
| `POST` | `/api/v1/orders` | Bearer + `Idempotency-Key` |
| `GET` | `/api/v1/orders` | Bearer |
| `GET` | `/api/v1/orders/{id}` | Bearer or admin |
| `POST` | `/api/v1/admin/products` | Admin |
| `PATCH` | `/api/v1/admin/products/{id}` | Admin |

Full request and response examples are in [docs/api.md](docs/api.md).

## Testing

```powershell
go vet ./...
go test -count=1 ./...
```

Integration tests require the Compose PostgreSQL and Redis:

```powershell
$env:TEST_DATABASE_URL = "postgres://postgres:postgres@localhost:5434/go_commerce_test?sslmode=disable"
$env:TEST_REDIS_ADDR = "localhost:6379"
$env:TEST_REDIS_DB = "15"
go test -count=1 ./tests/integration/...
```

The integration suite covers auth, cart TTL refreshes, concurrent oversell,
idempotent replay and conflict, transaction rollback, ownership checks, and
server-side pricing. On Windows, `-race` needs cgo and a C compiler; the
[Docker race command](docs/development.md#tests) runs the full suite in
`golang:1.27`. CI runs `go test -race` against real PostgreSQL and Redis.

## Project layout

```text
cmd/api              HTTP entrypoint and graceful shutdown
cmd/seed             demo catalog and admin bootstrap
internal/app         dependency wiring, routes, readiness
internal/auth        passwords, JWTs, auth middleware
internal/product     catalog store and handlers
internal/cart        Redis cart store and handlers
internal/order       checkout transaction and idempotency
internal/worker      notification worker pool
internal/postgres    pool and embedded migrations
internal/cache       Redis client
internal/httpx       JSON, request IDs, logging, recovery
migrations           versioned SQL
tests/integration    real PostgreSQL + Redis tests
docs                 API, architecture, development, commit rules
```

## Documentation

- [API reference](docs/api.md)
- [Architecture and transaction design](docs/architecture.md)
- [Development and testing guide](docs/development.md)
- [Commit conventions](docs/commit-conventions.md)
