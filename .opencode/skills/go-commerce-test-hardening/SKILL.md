---
name: Go Commerce Test Hardening
description: Attack-first testing for the Go commerce backend. Use whenever adding, reviewing, or hardening tests for JWT auth, PostgreSQL stock transactions, Redis cart TTL, idempotent checkout, HTTP handlers, worker shutdown, or any code in this repository. Requires real PostgreSQL and Redis, race detection, concurrency and failure-path tests, red-first mutation checks, and a written report of known gaps.
version: 1
created: "2026-07-25"
updated: "2026-07-25"
---

# Go commerce test hardening

A test is an attack on the code, not a demonstration that it runs. The goal is
not to make the suite green; the goal is to find the inputs, races, and failure
paths that make this commerce backend wrong.

## Rules that override convenience

- Never mock PostgreSQL or Redis. They are part of what this project owns, and
  the bugs we care about live in SQL, transactions, TTLs, and locking.
- Never write a test that only calls a function and checks that it did not
  panic. Every test must name the bug it would catch.
- Never derive expected values by copying the implementation's logic. Read the
  contract in `README.md`, `docs/api.md`, and the handler before writing tests.
- Never delete, weaken, or skip a test to make a run green. If the contract is
  wrong, report the contradiction.
- Never change production code from this skill's workflow. If a test exposes a
  bug, leave the failing test visible and report it to the parent agent.
- Never claim the code is bug-free. Report what was attacked and what was not.

## Workflow

### 1. Write the contract first

Before touching a test file, write down:

- valid inputs and boundaries;
- output and status codes;
- errors and their exact identity;
- side effects (stock, order count, cart TTL, worker jobs);
- invariants (stock never negative, one order per idempotency key, prices are
  server-side, carts expire).

Use `assets/TEST_NOTES.template.md` for the report. If the contract is unclear,
ask the parent agent instead of guessing from the implementation.

### 2. Build the attack list

Use `references/attack-matrix.md`. Add project-specific attacks before writing
tests. Each item becomes a test or an explicit known gap.

### 3. Use the real dependencies

The Compose stack publishes PostgreSQL on `localhost:5434` and Redis on
`localhost:6379`.

```powershell
docker compose up -d postgres redis
$env:TEST_DATABASE_URL = "postgres://postgres:postgres@localhost:5434/go_commerce_test?sslmode=disable"
$env:TEST_REDIS_ADDR = "localhost:6379"
$env:TEST_REDIS_DB = "15"
```

- Integration tests create and migrate their own test database.
- Redis tests use DB 15, never the development DB 0.
- Truncate tables and flush the test Redis DB between tests.
- Use `httptest` for HTTP. Use small fakes only for email/push stubs.

### 4. Write behavior-level tests

- Table-driven tests with named cases and `t.Run`.
- Assert specific errors with `errors.Is` or `errors.As`.
- Assert observable effects: order row count, stock, cart hash, TTL, HTTP body.
- For concurrency, use a start barrier and collect every result; do not assume
  goroutine scheduling order.
- Add fuzz tests for JWT parsing, idempotency-key validation, and JSON decoding
  where an invariant exists.

### 5. Prove red first

For each important test, prove it can fail. The safe way is the included script,
which mutates a temporary copy of the module; the real working tree is never
changed:

```powershell
pwsh -File .opencode/skills/go-commerce-test-hardening/scripts/mutate-and-test.ps1 `
  -FilePath internal/order/service.go `
  -Old "stock >= quantity" `
  -New "stock > quantity" `
  -TestPackage ./tests/integration/... `
  -Pattern "TestConcurrentCheckoutDoesNotOversell"
```

`KILLED` is the desired result: a test failed because the mutation was detected.
`LIVED` means the suite has a hole; add a test or record the gap.

### 6. Run the checks

```powershell
go vet ./...
go test -race -count=1 ./...
go test -race -count=5 ./tests/integration/...
go test -run=^$ -fuzz=Fuzz -fuzztime=30s ./internal/auth
```

A flaky test is a failing test. Find shared state, time dependence, or ordering
problems; never retry it until it happens to pass.

### 7. Manual mutation

Use the mutation script on the riskiest lines:

- stock comparisons and decrement arithmetic;
- idempotency request-hash comparisons;
- TTL refresh and cart item caps;
- auth role checks and token expiry;
- worker shutdown and queue-full branches.

At minimum, mutate every conditional in the checkout transaction and confirm the
test suite notices. Record surviving mutations in the report.

### 8. Report

Create `TEST_NOTES.md` at the repository root using the template. The report must
contain:

- what was attacked;
- bugs found, with failing test names and evidence;
- mutations killed and survived;
- known gaps and why they remain;
- exact commands to reproduce the run.

## Project attack matrix highlights

- **Auth:** duplicate email, wrong password, unknown email timing, missing
  bearer, expired token, tampered signature, `alg=none`, role escalation.
- **Products:** zero and negative stock, inactive products, pagination bounds,
  malformed UUIDs, duplicate SKU.
- **Cart:** TTL refresh, natural expiry, max quantity, max distinct items,
  inactive product, Redis outage, context cancellation, concurrent adds.
- **Orders:** empty cart, insufficient stock, multi-item rollback, price
  snapshot, duplicate product rows, cart-clear failure, order ownership.
- **Idempotency:** sequential replay, concurrent same key, same key with a
  different body, expired key, key scoped to another user, failed transaction
  not recording a key.
- **Workers:** queue full, slow handler, panic, retry, shutdown drain, context
  cancellation.
- **HTTP:** malformed JSON, unknown fields, wrong content type, oversized body,
  missing request ID, 404/405, panic recovery.

## Output the parent agent should see

In a few lines: what was attacked, what broke, which mutations survived, what is
not covered, and the commands to rerun everything.
