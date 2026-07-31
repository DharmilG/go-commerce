# Architecture

Go-Commerce is a small service, but the interesting parts are not the CRUD
handlers. They are the boundaries where correctness can fail: JWT validation,
Redis TTLs, PostgreSQL row locks, idempotency, and shutdown while work is still
running.

## Package map

```text
cmd/api                 process entrypoint, signal handling, graceful shutdown
cmd/seed                demo catalog and admin bootstrap
internal/app            dependency wiring, routes, readiness, resource lifecycle
internal/auth           bcrypt, HS256 JWT, login/register, auth middleware
internal/product        catalog validation, queries, public and admin handlers
internal/cart           Redis hash store, Lua scripts, cart/checkout snapshots
internal/order          checkout transaction, idempotency, order reads
internal/worker         buffered channel pool and notification retry loop
internal/postgres       pgx pool and embedded migration runner
internal/cache          Redis client
internal/httpx          JSON envelope, request IDs, logging, panic recovery
migrations              versioned SQL embedded into the binary
```

Dependencies point downward: HTTP handlers call domain code; domain code calls
PostgreSQL/Redis; no domain package imports `internal/app`.

## Request path

```text
net/http ServeMux
  -> RequestID
  -> Logger
  -> Recoverer
  -> SecurityHeaders
  -> route handler
       -> auth.Middleware (Bearer token -> httpx.User in context)
       -> domain handler
            -> domain service/store
                 -> PostgreSQL pool or Redis client
```

`httpx.Chain` makes the first middleware outermost. The request ID is generated
or validated before logging so every log line and error envelope can carry it.
Authentication is applied per protected route rather than globally, which makes
the public catalog and health checks explicit.

## Data model

- `users`: unique case-insensitive email index, bcrypt password hash, role
  (`customer` or `admin`).
- `products`: SKU uniqueness, integer-cent price, non-negative stock, soft
  visibility through `is_active`.
- `orders`: owner, status, integer-cent totals, currency, JSONB shipping address.
- `order_items`: immutable product name/SKU/price snapshots. Historical orders
  do not change when the catalog changes.
- `idempotency_keys`: composite primary key `(user_id, key)`, request hash,
  stored response, expiry.

The migration runner uses `golang-migrate` with an `embed.FS`; the SQL is part of
the binary, so a container does not need a separate migration file volume.

## Cart storage

Each user has one Redis hash:

```text
<REDIS_PREFIX>cart:<user-uuid>  ->  { "<product-uuid>": "<quantity>", ... }
```

`HGETALL` reads the cart. Mutations use Lua scripts so that checking the
per-product limit, the distinct-item limit, writing the quantity, and refreshing
the TTL happen atomically. The TTL is refreshed by add, set, remove, and clear;
`GET /cart` deliberately does not refresh it.

Redis stores only quantities. Product names, active flags, prices, and stock are
fetched from PostgreSQL on every cart read and again inside the checkout
transaction. A client cannot submit a price.

## Checkout transaction

`POST /orders` is the critical section. The service:

1. Validates the idempotency key and shipping address.
2. Hashes the normalized request and checks for an unexpired idempotency record.
3. Begins a PostgreSQL transaction.
4. Takes a per-user advisory transaction lock
   (`pg_advisory_xact_lock(hashtextextended(...))`). This serializes retries and
   concurrent checkouts for the same user.
5. Re-checks the idempotency record after acquiring the lock.
6. Snapshots the Redis cart (product IDs and quantities only).
7. Loads the product rows with `SELECT ... FOR UPDATE`, ordered by product ID.
   A stable lock order prevents two multi-product checkouts from deadlocking.
8. Rejects missing, inactive, or under-stocked products.
9. Recalculates every line total from the locked database prices.
10. Inserts the order and immutable line snapshots.
11. Decrements stock with a guarded `UPDATE ... WHERE stock >= quantity`.
12. Inserts the idempotency record in the same transaction.
13. Commits.
14. Clears the cart and enqueues a notification as best-effort side effects.

If any step before commit fails, the transaction rolls back and the cart stays
intact. If the HTTP response is lost after commit, a retry with the same key
replays the stored response without consuming stock again.

## Idempotency details

The request hash covers the normalized shipping address. It intentionally does
not cover cart contents: the cart is server-owned and can be cleared or changed
between retries, while the same key must still identify the original checkout.
The key is scoped to the authenticated user, so two users can use the same string
without colliding.

Expired records are deleted by a periodic janitor and lazily before a new record
is inserted. A replay is returned with `Idempotency-Replayed: true`.

## Worker pool

After commit, `order.Service` publishes the order to a buffered channel. A fixed
number of goroutines consume it, call the `Notifier` interface, retry temporary
failures three times with backoff, and recover panics per job. The development
notifier only logs; production email/push providers can implement the same
interface.

The pool is deliberately non-blocking for the HTTP path. If the queue is full,
the event is logged and dropped; the order is already durable. A production
system would use an outbox table or a durable broker to remove that gap.

Shutdown order:

1. Stop accepting HTTP requests.
2. Wait for active handlers.
3. Stop accepting jobs, close the queue, and drain workers.
4. Close PostgreSQL and Redis.

## Failure and consistency notes

- Stock changes only happen inside the checkout transaction with row locks.
- The guarded update is a second line of defense against overselling; a stock
  check without the lock would be race-prone.
- Admin `stock` updates are absolute values. A concurrent order and admin update
  serialize on the product row; the last writer intentionally sets the value.
- Cart mutations are not serialized against checkout. A cart that changes while
  a checkout is in flight is handled by the checkout snapshot; after commit the
  cart is cleared.
- The worker queue is in memory. Restarting the process does not replay events.
- There are no refresh tokens, password-reset flow, or rate limits yet.
