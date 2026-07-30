# Test notes: checkout, cart, and auth hardening

Date: 2026-07-30
Agent: implementation and adversarial test pass
Commit: see `git log -- tests/integration`

## Contract

- Inputs and valid ranges:
  - passwords 8-72 bytes;
  - cart quantity 1-`MAX_ITEM_QUANTITY`, at most `MAX_CART_ITEMS` distinct products;
  - `Idempotency-Key` 8-255 visible ASCII bytes;
  - order address required fields: full name, line 1, city, postal code, country.
- Outputs and status codes:
  - auth `201/200/401/409`;
  - cart `200/204/400/404/409`;
  - checkout `201/400/409`;
  - orders `200/404`.
- Error identities:
  - `EMAIL_TAKEN`, `INVALID_CREDENTIALS`;
  - `PRODUCT_NOT_FOUND`, `INSUFFICIENT_STOCK`, `CART_FULL`;
  - `CART_EMPTY`, `PRODUCT_UNAVAILABLE`, `IDEMPOTENCY_CONFLICT`.
- Side effects:
  - checkout commits order rows, line snapshots, stock decrements, and the
    idempotency record atomically;
  - Redis cart quantities and TTL are updated atomically by Lua;
  - cart is cleared after a committed order;
  - notification events are queued after commit.
- Invariants:
  - stock never goes below zero;
  - a replayed idempotency key never creates a second order or consumes stock;
  - a failed multi-item checkout leaves every stock value unchanged;
  - customers cannot read another customer's order;
  - the server never uses a client-supplied price.

## Attacks performed

| Attack | Test | Result |
|---|---|---|
| Duplicate email with different case | `TestAuthLifecycle` | Passed |
| Wrong password and unknown email both return 401 | `TestAuthLifecycle` | Passed |
| Missing and malformed Bearer token | `TestAuthLifecycle` | Passed |
| Public list leaks inactive product | `TestCatalogVisibilityAndAdminAuthorization` | Passed |
| Customer calls admin product endpoint | `TestCatalogVisibilityAndAdminAuthorization` | Passed |
| Duplicate SKU and negative price | `TestCatalogVisibilityAndAdminAuthorization` | Passed |
| Cart TTL not refreshed by mutation | `TestCartTTLRefreshesOnEveryMutation` | Passed |
| Removing the last cart item leaves a Redis key | `TestCartTTLRefreshesOnEveryMutation` | Passed |
| Eight buyers race for one unit | `TestConcurrentCheckoutDoesNotOversell` | Passed |
| Ten retries with one idempotency key | `TestConcurrentSameIdempotencyKeyCreatesOneOrder` | Passed |
| Same key with a different address | `TestIdempotencyReplayAndConflict` | Passed |
| Replay after the cart was cleared | `TestReplayDoesNotDependOnCartStillExisting` | Passed |
| Partial checkout with one under-stocked item | `TestPartialCheckoutFailureRollsBackStock` | Passed |
| Product deactivated between cart and checkout | `TestUnavailableProductRejectsCheckout` | Passed |
| Price changed between cart and checkout | `TestServerRecalculatesPriceAtCheckout` | Passed |
| Customer reads another customer's order | `TestOrderOwnershipAndAdminRead` | Passed |
| Empty cart and invalid address | `TestAddressValidationAndEmptyCart` | Passed |

## Mutation results

| File and mutation | Test command | Result |
|---|---|---|
| Removed the `user_id` filter from order reads | `go test ./tests/integration -run TestOrderOwnershipAndAdminRead` | KILLED |
| Replaced `price * quantity` with `price` | `go test ./tests/integration -run TestServerRecalculatesPriceAtCheckout` | KILLED |

## Known gaps

- The worker queue is in memory. A full queue or process restart can drop an
  order notification even though the order is committed. A durable outbox or
  broker is the next step.
- Cart mutations are not serialized with checkout. Checkout uses a snapshot and
  clears the cart after commit; a concurrent add can be lost.
- Admin stock updates are absolute writes; there is no stock-adjustment ledger.
- There are no refresh tokens, password reset, email verification, or rate
  limits.
- The migration runner runs at API startup when `AUTO_MIGRATE=true`; a fleet
  should run migrations once as a separate deployment step.
- The suite does not include sustained load, fuzzing of JSON bodies, or a real
  email provider.

## Reproduction

```powershell
go vet ./...
$env:TEST_DATABASE_URL = "postgres://postgres:postgres@localhost:5434/go_commerce_test?sslmode=disable"
$env:TEST_REDIS_ADDR = "localhost:6379"
$env:TEST_REDIS_DB = "15"
go test -race -count=1 ./...
```

On Windows without a C compiler, run the race suite in Docker as documented in
`docs/development.md`.
