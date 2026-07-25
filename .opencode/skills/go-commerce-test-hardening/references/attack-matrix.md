# Attack matrix

Each row is a test or a recorded gap. Add the specific bug the test would catch.

## Authentication

- [ ] Register with a duplicate email returns a conflict and does not leak a stack trace.
- [ ] Email normalization: `A@Example.com` and `a@example.com` collide.
- [ ] Password shorter than 8 bytes is rejected.
- [ ] Password longer than bcrypt's 72-byte limit is rejected, not silently truncated.
- [ ] Login with an unknown email and a known email with the wrong password look identical.
- [ ] A missing `Authorization` header returns 401.
- [ ] A malformed bearer token returns 401.
- [ ] A token with a valid signature but expired `exp` returns 401.
- [ ] A token signed with another secret returns 401.
- [ ] An `alg=none` token is rejected.
- [ ] A customer cannot call admin product endpoints.
- [ ] The token's `sub` and role claims map to the correct database user.

## Products and stock

- [ ] List only returns active products to the public.
- [ ] Limit below 1, above 100, and offset beyond the result set are handled.
- [ ] A malformed product UUID returns 400/404, not a panic.
- [ ] Creating a duplicate SKU returns a conflict.
- [ ] Negative prices and negative stock are rejected.
- [ ] Admin stock updates are atomic with checkout.

## Cart

- [ ] Adding an item sets a TTL on the Redis hash.
- [ ] Every add, update, and removal refreshes the TTL.
- [ ] Removing the final item deletes the key, not an empty hash with no TTL.
- [ ] Adding beyond `MAX_ITEM_QUANTITY` is rejected.
- [ ] Adding beyond the product's current stock is rejected.
- [ ] Adding more than `MAX_CART_ITEMS` distinct products is rejected.
- [ ] Concurrent adds to the same cart do not lose updates or exceed the cap.
- [ ] An inactive product cannot be added.
- [ ] A cart containing an item that became inactive fails at checkout.
- [ ] Redis errors surface as 5xx, not an empty successful cart.
- [ ] A canceled request context aborts the Redis operation.

## Orders and stock safety

- [ ] Empty cart returns a specific client error.
- [ ] Missing `Idempotency-Key` returns 400.
- [ ] A key longer than 255 characters is rejected.
- [ ] A successful order decrements stock by exactly the purchased quantity.
- [ ] A multi-item order with one insufficient item rolls back every change.
- [ ] Concurrent buyers for stock 1: exactly one succeeds, final stock is 0.
- [ ] Deadlock-free multi-item checkout with products inserted in different orders.
- [ ] Prices and totals come from PostgreSQL, not the client.
- [ ] Product name/SKU/price snapshots survive later product edits.
- [ ] A user cannot read another user's order.
- [ ] An admin can read any order only if that behavior is documented.

## Idempotency

- [ ] The same key and same body return the original order.
- [ ] The same key and a different body return 409.
- [ ] Concurrent requests with the same key create one order and one stock decrement.
- [ ] A failed transaction does not leave a successful idempotency record.
- [ ] An expired idempotency key no longer replays.
- [ ] A key used by user A cannot replay or conflict with user B.
- [ ] The stored response body is valid JSON and matches the original order.

## Worker pool

- [ ] Every queued job is processed before a graceful shutdown returns.
- [ ] A full queue does not block the HTTP response forever.
- [ ] A panicking handler does not kill the worker goroutine.
- [ ] A slow handler respects its context timeout.
- [ ] Submitting after shutdown is rejected, not a panic.
- [ ] A canceled application context stops workers without leaking goroutines.

## HTTP and shutdown

- [ ] Unknown JSON fields are rejected.
- [ ] A body over 1 MiB returns 413.
- [ ] A non-JSON content type returns 415.
- [ ] Two JSON values in one body are rejected.
- [ ] Request IDs are echoed when safe and regenerated when hostile.
- [ ] Panic recovery returns JSON 500 and the process remains alive.
- [ ] `/readyz` returns 503 while shutting down and when a dependency is down.
- [ ] Active requests finish during graceful shutdown, up to the timeout.
