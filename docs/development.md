# Development guide

## Prerequisites

- Go 1.27.3
- Docker Desktop with Docker Compose v2/v5
- PowerShell (the commands below use PowerShell syntax)
- Ports `8080`, `5434`, and `6379` available

PostgreSQL is published on `5434` because this machine already uses `5432` for a
native PostgreSQL service and `5433` for another container. Redis uses `6379`.
Do not stop or modify those existing services.

## First run

```powershell
Copy-Item .env.example .env
docker compose up -d postgres redis
go run ./cmd/seed
go run ./cmd/api
```

The API runs migrations at startup when `AUTO_MIGRATE=true` (the default). The
seed command also runs migrations. It upserts eight demo products and, when
`ADMIN_EMAIL` and `ADMIN_PASSWORD` are set, creates an admin user.

```powershell
Invoke-RestMethod http://localhost:8080/healthz
Invoke-RestMethod http://localhost:8080/readyz
```

The default `.env.example` values are for local development only. Change
`JWT_SECRET` and the admin password before sharing an environment.

## Full containerized stack

```powershell
docker compose up --build -d
docker compose ps
```

The API container waits for healthy PostgreSQL and Redis, runs migrations, and
serves on `http://localhost:8080`. The final image also contains the seed binary:

```powershell
docker compose run --rm --entrypoint /app/seed api
```

Stop the stack without deleting local data:

```powershell
docker compose down
```

Never use `docker compose down -v` unless you intentionally want to erase the
PostgreSQL and Redis volumes.

## Configuration

Configuration is read from the process environment, then `.env` is loaded if it
exists. Important settings:

| Variable | Default | Notes |
|---|---|---|
| `DATABASE_URL` | required | Host-side URL uses `localhost:5434`. Compose overrides the host. |
| `JWT_SECRET` | required | At least 32 characters. |
| `REDIS_ADDR` | `localhost:6379` | Compose uses `redis:6379`. |
| `REDIS_DB` | `0` | Development database. |
| `CART_TTL` | `168h` | Abandoned-cart expiry. |
| `IDEMPOTENCY_TTL` | `168h` | Replay window for order keys. |
| `AUTO_MIGRATE` | `true` | Run embedded migrations at API startup. |
| `BCRYPT_COST` | `12` | Integration tests use a lower cost. |
| `MAX_CART_ITEMS` | `50` | Distinct products per cart. |
| `MAX_ITEM_QUANTITY` | `99` | Per-product quantity cap. |

`config.Load` validates required values and durations. Invalid configuration
fails before the server starts.

## Migrations

SQL lives in `migrations/` and is embedded into the binary through
`migrations/embed.go`. The runner is invoked by the API (when `AUTO_MIGRATE` is
true) and by the seed command.

There is no separate migration CLI in this project. To apply migrations without
starting the HTTP server:

```powershell
go run ./cmd/seed
```

If a migration fails, the process exits non-zero and logs the cause. Fix the SQL
in a new migration rather than editing an already-applied file.

## Tests

Run the fast package tests on the host:

```powershell
go vet ./...
go test -count=1 ./...
```

Run the same suite with the race detector. On Windows this requires cgo and a C
compiler:

```powershell
go test -race -count=1 ./...
```

If the host has no C compiler, run the race suite in the official Go container:

```powershell
docker run --rm -v "E:\Go-commerce:/src" -w /src `
  -e CGO_ENABLED=1 `
  -e TEST_DATABASE_URL="postgres://postgres:postgres@host.docker.internal:5434/go_commerce_test?sslmode=disable" `
  -e TEST_REDIS_ADDR="host.docker.internal:6379" `
  -e TEST_REDIS_DB=15 `
  golang:1.27 go test -race -count=1 ./...
```

Integration tests use real PostgreSQL and Redis. They do not mock the
infrastructure this project owns.

```powershell
$env:TEST_DATABASE_URL = "postgres://postgres:postgres@localhost:5434/go_commerce_test?sslmode=disable"
$env:TEST_REDIS_ADDR = "localhost:6379"
$env:TEST_REDIS_DB = "15"
go test -count=1 ./tests/integration/...
```

The harness:

- creates `go_commerce_test` through the maintenance database if it is missing;
- applies the embedded migrations;
- creates a fresh admin user after each reset;
- truncates the five application tables between tests;
- flushes Redis DB 15 only.

`TEST_REDIS_DB` must be non-zero. The harness refuses to flush the development
Redis database (`0`).

## Manual smoke flow

```powershell
$base = "http://localhost:8080/api/v1"
$session = Invoke-RestMethod -Method Post -Uri "$base/auth/register" `
  -ContentType "application/json" -Body (@{
    name = "Test Learner"; email = "learner@example.com"; password = "correct-horse-battery"
  } | ConvertTo-Json)

$headers = @{ Authorization = "Bearer $($session.access_token)" }
$products = Invoke-RestMethod -Uri "$base/products?limit=5"
$headers["Idempotency-Key"] = "manual-" + [guid]::NewGuid().ToString()

Invoke-RestMethod -Method Post -Uri "$base/cart/items" -Headers $headers `
  -ContentType "application/json" -Body (@{
    product_id = $products.data[0].id; quantity = 1
  } | ConvertTo-Json)

Invoke-RestMethod -Method Post -Uri "$base/orders" -Headers $headers `
  -ContentType "application/json" -Body (@{
    shipping_address = @{
      full_name = "Test Learner"; line1 = "1 Test Street"; city = "Ahmedabad"
      postal_code = "380001"; country = "IN"
    }
  } | ConvertTo-Json)
```

The full endpoint contract is in [api.md](api.md). The transaction and worker
design is in [architecture.md](architecture.md).

## Useful diagnostics

```powershell
docker compose ps
docker compose logs -f postgres
docker compose logs -f redis
docker compose logs -f api
Get-NetTCPConnection -LocalPort 8080
```

Common issues:

- `env file E:\Go-commerce\.env not found`: copy `.env.example` to `.env`.
- `go: -race requires cgo`: use the Docker race command or install a C compiler.
- `ping postgres`: verify the Compose services and that port `5434` is not taken.
- `ping redis`: verify port `6379` is not used by another client.
- `AUTO_MIGRATE=false` and tables missing: run the seed command or enable
  migrations for the first start.
