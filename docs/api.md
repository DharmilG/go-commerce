# API reference

Base URL: `http://localhost:8080/api/v1`

All request and response bodies are JSON. Money is represented as integer cents
(`price_cents`, `total_cents`) so that no floating point value is ever used for
prices.

## Common headers

| Header | Use |
|---|---|
| `Authorization: Bearer <token>` | Required for account, cart, and order routes. |
| `Content-Type: application/json` | Required for every request with a body. |
| `Idempotency-Key: <key>` | Required by `POST /orders`. 8–255 visible ASCII characters. |
| `X-Request-ID` | Optional correlation ID. Unsafe values are replaced with a UUID. |

Successful responses include the `X-Request-ID` response header. Errors use the
shared envelope:

```json
{
  "error": {
    "code": "VALIDATION_ERROR",
    "message": "name must be between 2 and 100 characters",
    "details": {
      "field": "name"
    },
    "request_id": "9b9a5d0e-6d0e-4af1-a2f0-1c9f9d1e12aa"
  }
}
```

## Health

### `GET /healthz`

Liveness probe. Returns `200`.

```json
{"status":"ok"}
```

### `GET /readyz`

Readiness probe. Pings PostgreSQL and Redis. Returns `200` when both are
reachable and `503` while shutting down or when a dependency is unavailable.

```json
{
  "status": "ready",
  "checks": {
    "postgres": "ok",
    "redis": "ok"
  }
}
```

## Authentication

### `POST /auth/register`

```json
{
  "name": "Test Learner",
  "email": "learner@example.com",
  "password": "correct-horse-battery"
}
```

The email is trimmed and lowercased. Passwords must be 8–72 bytes. The response
is `201`:

```json
{
  "user": {
    "id": "4a1d3a6c-5a7b-4d2e-9f2a-9e4b6c1d8f20",
    "email": "learner@example.com",
    "name": "Test Learner",
    "role": "customer",
    "created_at": "2026-07-23T10:15:00Z"
  },
  "access_token": "eyJhbGciOiJIUzI1NiIs...",
  "token_type": "Bearer",
  "expires_in": 86400
}
```

Errors: `400 VALIDATION_ERROR`, `409 EMAIL_TAKEN`.

### `POST /auth/login`

```json
{
  "email": "learner@example.com",
  "password": "correct-horse-battery"
}
```

Returns the same session shape as registration. Unknown emails and wrong
passwords both return `401 INVALID_CREDENTIALS`.

### `GET /auth/me`

Requires a Bearer token. Returns the current database user:

```json
{
  "user": {
    "id": "4a1d3a6c-5a7b-4d2e-9f2a-9e4b6c1d8f20",
    "email": "learner@example.com",
    "name": "Test Learner",
    "role": "customer",
    "created_at": "2026-07-23T10:15:00Z"
  }
}
```

## Products

### `GET /products`

Public catalog. Only active products are returned.

Query parameters:

| Name | Default | Rules |
|---|---:|---|
| `limit` | 20 | 1–100 |
| `offset` | 0 | 0 or greater |
| `search` | empty | Matches name or SKU with `ILIKE` |
| `category` | empty | Exact category match |

```json
{
  "data": [
    {
      "id": "b720aa2f-2f73-4b73-8985-42f1bb8a6f7c",
      "sku": "KB-MECH-01",
      "name": "Mechanical Keyboard",
      "description": "Hot-swappable 75% keyboard with tactile switches.",
      "price_cents": 12900,
      "stock": 12,
      "category": "Peripherals",
      "image_url": "https://images.example.com/keyboard.jpg",
      "is_active": true,
      "created_at": "2026-07-23T10:20:00Z",
      "updated_at": "2026-07-23T10:20:00Z"
    }
  ],
  "pagination": {
    "limit": 20,
    "offset": 0,
    "total": 1
  }
}
```

### `GET /products/{id}`

Returns `{"product": {...}}` or `404 PRODUCT_NOT_FOUND`.

### `POST /admin/products` (admin)

Requires an admin token.

```json
{
  "sku": "HUB-USBC-01",
  "name": "USB-C Hub",
  "description": "Seven-port hub with HDMI, Ethernet, and SD reader.",
  "price_cents": 7900,
  "stock": 25,
  "category": "Accessories",
  "image_url": "https://images.example.com/hub.jpg",
  "is_active": true
}
```

`is_active` defaults to `true`. Returns `201` with
`{"product": {...}}`. Errors: `400 VALIDATION_ERROR`,
`409 SKU_TAKEN`.

### `PATCH /admin/products/{id}` (admin)

Send only the fields that should change:

```json
{
  "price_cents": 8900,
  "stock": 40
}
```

`stock` is an absolute value, not a delta. Returns `200` with the updated
product.

## Cart

Cart quantities live in Redis. Each mutation refreshes the abandoned-cart TTL
(`CART_TTL`, default seven days). Product names, prices, stock, and availability
are read from PostgreSQL when the cart is returned; they are never trusted from
the client.

### `GET /cart`

Requires a Bearer token. Does not refresh the TTL.

```json
{
  "cart": {
    "items": [
      {
        "product_id": "b720aa2f-2f73-4b73-8985-42f1bb8a6f7c",
        "sku": "KB-MECH-01",
        "name": "Mechanical Keyboard",
        "unit_price_cents": 12900,
        "quantity": 2,
        "line_total_cents": 25800,
        "stock": 12,
        "is_active": true,
        "available": true
      }
    ],
    "item_count": 2,
    "subtotal_cents": 25800,
    "currency": "USD",
    "expires_in_seconds": 604800
  }
}
```

An unavailable line remains visible so the customer can remove it, but it is
excluded from `subtotal_cents` and blocks checkout.

### `POST /cart/items`

```json
{
  "product_id": "b720aa2f-2f73-4b73-8985-42f1bb8a6f7c",
  "quantity": 2
}
```

Adds to the existing quantity. Returns the full cart. Errors include
`404 PRODUCT_NOT_FOUND`, `409 INSUFFICIENT_STOCK`, `409 CART_FULL`, and
`400 VALIDATION_ERROR`.

### `PUT /cart/items/{productID}`

Sets an absolute quantity. `{"quantity": 0}` removes the line. Returns the full
cart.

### `DELETE /cart/items/{productID}`

Removes one line and returns the full cart.

### `DELETE /cart`

Clears the cart. Returns `204`.

## Orders

### `POST /orders`

Requires a Bearer token and an `Idempotency-Key` header. The body only contains
shipping details; line items and prices always come from the authenticated
user's cart and PostgreSQL.

```json
{
  "shipping_address": {
    "full_name": "Test Learner",
    "line1": "1 Test Street",
    "line2": "Unit 2",
    "city": "Ahmedabad",
    "state": "Gujarat",
    "postal_code": "380001",
    "country": "IN"
  }
}
```

Returns `201`:

```json
{
  "order": {
    "id": "6fc4d5f4-2d5c-4c8c-bf8a-2d7d6f75cb8b",
    "user_id": "4a1d3a6c-5a7b-4d2e-9f2a-9e4b6c1d8f20",
    "status": "confirmed",
    "subtotal_cents": 25800,
    "shipping_cents": 0,
    "total_cents": 25800,
    "currency": "USD",
    "shipping_address": {
      "full_name": "Test Learner",
      "line1": "1 Test Street",
      "line2": "Unit 2",
      "city": "Ahmedabad",
      "state": "Gujarat",
      "postal_code": "380001",
      "country": "IN"
    },
    "items": [
      {
        "id": "f3a0b4e0-0d38-4f6b-9f2e-2f1c2b4d5e6f",
        "product_id": "b720aa2f-2f73-4b73-8985-42f1bb8a6f7c",
        "sku": "KB-MECH-01",
        "name": "Mechanical Keyboard",
        "unit_price_cents": 12900,
        "quantity": 2,
        "line_total_cents": 25800
      }
    ],
    "created_at": "2026-07-23T10:30:00Z",
    "updated_at": "2026-07-23T10:30:00Z"
  }
}
```

The server locks the product rows in PostgreSQL, re-reads every price, checks
stock, writes the order and line snapshots, decrements stock, and writes the
idempotency record in one transaction. If any item fails, the whole transaction
rolls back.

#### Idempotency behavior

- Repeating the same key with the same normalized address returns the original
  response body and status with `Idempotency-Replayed: true`.
- Reusing the key with a different address returns `409 IDEMPOTENCY_CONFLICT`.
- The idempotency record is committed with the order, so a retry after a lost
  response still returns the first order.
- Keys are scoped per user.
- Records expire after `IDEMPOTENCY_TTL` (default seven days).

Errors:

| Status | Code | Meaning |
|---:|---|---|
| 400 | `IDEMPOTENCY_KEY_REQUIRED` | Header is missing. |
| 400 | `INVALID_IDEMPOTENCY_KEY` | Key is not 8–255 visible ASCII characters. |
| 400 | `VALIDATION_ERROR` | Shipping address is invalid. |
| 409 | `IDEMPOTENCY_CONFLICT` | Key was used for a different request. |
| 409 | `CART_EMPTY` | Cart has no items. |
| 409 | `INSUFFICIENT_STOCK` | A locked product has less stock than requested. |
| 409 | `PRODUCT_UNAVAILABLE` | A product disappeared or became inactive. |

### `GET /orders`

Returns the authenticated user's orders, newest first.

| Query | Default | Rules |
|---|---:|---|
| `limit` | 20 | 1–100 |
| `offset` | 0 | 0 or greater |

```json
{
  "data": [],
  "pagination": {
    "limit": 20,
    "offset": 0,
    "total": 0
  }
}
```

### `GET /orders/{id}`

Returns `{"order": {...}}`. Customers can only read their own orders; an admin
token can read any order. Missing or foreign orders return
`404 ORDER_NOT_FOUND` so ownership is not disclosed.

## PowerShell smoke test

```powershell
$base = "http://localhost:8080/api/v1"

$session = Invoke-RestMethod -Method Post -Uri "$base/auth/register" `
  -ContentType "application/json" -Body (@{
    name = "Test Learner"
    email = "learner@example.com"
    password = "correct-horse-battery"
  } | ConvertTo-Json)

$headers = @{ Authorization = "Bearer $($session.access_token)" }
$products = Invoke-RestMethod -Uri "$base/products?limit=5"
$productId = $products.data[0].id

Invoke-RestMethod -Method Post -Uri "$base/cart/items" -Headers $headers `
  -ContentType "application/json" -Body (@{
    product_id = $productId
    quantity = 1
  } | ConvertTo-Json) | Out-Null

$headers["Idempotency-Key"] = "smoke-" + [guid]::NewGuid().ToString()
$order = Invoke-RestMethod -Method Post -Uri "$base/orders" -Headers $headers `
  -ContentType "application/json" -Body (@{
    shipping_address = @{
      full_name = "Test Learner"
      line1 = "1 Test Street"
      city = "Ahmedabad"
      postal_code = "380001"
      country = "IN"
    }
  } | ConvertTo-Json)

$order.order.id
```
