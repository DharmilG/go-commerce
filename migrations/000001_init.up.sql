CREATE TABLE users (
    id uuid PRIMARY KEY,
    email text NOT NULL,
    password_hash text NOT NULL,
    name text NOT NULL,
    role text NOT NULL DEFAULT 'customer' CHECK (role IN ('customer', 'admin')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX users_email_lower_idx ON users (lower(email));

CREATE TABLE products (
    id uuid PRIMARY KEY,
    sku text NOT NULL,
    name text NOT NULL,
    description text NOT NULL DEFAULT '',
    price_cents bigint NOT NULL CHECK (price_cents >= 0),
    stock integer NOT NULL CHECK (stock >= 0),
    category text NOT NULL DEFAULT '',
    image_url text NOT NULL DEFAULT '',
    is_active boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX products_sku_idx ON products (sku);
CREATE INDEX products_active_created_idx ON products (is_active, created_at DESC);
CREATE INDEX products_category_idx ON products (category);

CREATE TABLE orders (
    id uuid PRIMARY KEY,
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE RESTRICT,
    status text NOT NULL CHECK (status IN ('pending', 'confirmed', 'cancelled')),
    subtotal_cents bigint NOT NULL CHECK (subtotal_cents >= 0),
    shipping_cents bigint NOT NULL DEFAULT 0 CHECK (shipping_cents >= 0),
    total_cents bigint NOT NULL CHECK (total_cents >= 0),
    currency char(3) NOT NULL,
    shipping_address jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX orders_user_created_idx ON orders (user_id, created_at DESC);

CREATE TABLE order_items (
    id uuid PRIMARY KEY,
    order_id uuid NOT NULL REFERENCES orders (id) ON DELETE CASCADE,
    product_id uuid NOT NULL REFERENCES products (id) ON DELETE RESTRICT,
    sku text NOT NULL,
    product_name text NOT NULL,
    unit_price_cents bigint NOT NULL CHECK (unit_price_cents >= 0),
    quantity integer NOT NULL CHECK (quantity > 0),
    line_total_cents bigint NOT NULL CHECK (line_total_cents >= 0)
);

CREATE INDEX order_items_order_idx ON order_items (order_id);

CREATE TABLE idempotency_keys (
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    key text NOT NULL,
    request_hash text NOT NULL,
    order_id uuid REFERENCES orders (id) ON DELETE CASCADE,
    response_status integer NOT NULL,
    response_body jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    PRIMARY KEY (user_id, key)
);

CREATE INDEX idempotency_keys_expires_idx ON idempotency_keys (expires_at);
