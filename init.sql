-- Order status enum type
CREATE TYPE order_status AS ENUM (
    'PENDING',
    'PROCESSING',
    'COMPLETED',
    'CANCELLED',
    'FAILED'
);

-- Payment status enum type
CREATE TYPE payment_status AS ENUM (
    'PENDING',
    'PROCESSING',
    'COMPLETED',
    'FAILED',
    'REFUNDED'
);

-- Saga status enum type
CREATE TYPE saga_status AS ENUM (
    'STARTED',
    'COMPLETED',
    'IN_PROGRESS',
    'CANCELLED',
    'FAILED',
    'COMPENSATED',
    'COMPENSATING',
    'COMPENSATION_FAILED'
);

-- Reservation status enum type
CREATE TYPE reservation_status AS ENUM (
    'ACTIVE',
    'RELEASED',
    'CONFIRMED'
);

-- Inventory table (must be created before order_items which references it)
CREATE TABLE IF NOT EXISTS products (
    product_id UUID PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    description TEXT,
    price DECIMAL(10, 2) NOT NULL CHECK (price >= 0),
    stock INTEGER NOT NULL CHECK (stock >= 0)
);

-- Orders table
CREATE TABLE IF NOT EXISTS orders (
    id UUID PRIMARY KEY,
    customer_id VARCHAR(255) NOT NULL,
    public_id VARCHAR(255) NOT NULL,
    total_amount DECIMAL(10, 2) NOT NULL,
    currency VARCHAR(3) NOT NULL DEFAULT 'USD',
    status order_status NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Order items table
CREATE TABLE IF NOT EXISTS order_items (
    id UUID PRIMARY KEY,
    order_id UUID NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    item_id UUID NOT NULL REFERENCES products(product_id) ON DELETE RESTRICT,
    quantity INTEGER NOT NULL CHECK (quantity > 0),
    price DECIMAL(10, 2) NOT NULL CHECK (price >= 0)
);

-- Inventory reservations table (links reservations to specific orders)
CREATE TABLE IF NOT EXISTS inventory_reservations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    -- TODO: add REFERENCES orders(id) once the orders service writes to this shared DB
    order_id UUID NOT NULL,
    product_id UUID NOT NULL REFERENCES products(product_id),
    quantity INTEGER NOT NULL CHECK (quantity > 0),
    status reservation_status NOT NULL DEFAULT 'ACTIVE',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Payments table
CREATE TABLE IF NOT EXISTS payments (
    id UUID PRIMARY KEY,
    order_id UUID NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    amount DECIMAL(10, 2) NOT NULL CHECK (amount > 0),
    currency TEXT NOT NULL,
    status payment_status NOT NULL DEFAULT 'PENDING',
    payment_intent_id TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- SAGA state table (tracks distributed transaction state)
CREATE TABLE IF NOT EXISTS saga_states (
    saga_id UUID PRIMARY KEY,
    order_id UUID NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    current_step VARCHAR(50) NOT NULL,
    status saga_status NOT NULL DEFAULT 'STARTED',
    next_retry_at TIMESTAMPTZ,
    compensation_retries INTEGER NOT NULL DEFAULT 0,
    payload JSONB,
    started_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Idempotency keys
CREATE TABLE IF NOT EXISTS idempotency_keys (
    key VARCHAR(255) PRIMARY KEY,
    response JSONB NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Indexes for performance optimization
CREATE INDEX IF NOT EXISTS idx_orders_customer_id ON orders(customer_id);
CREATE INDEX IF NOT EXISTS idx_orders_public_id ON orders(public_id);
CREATE INDEX IF NOT EXISTS idx_orders_status ON orders(status);
CREATE INDEX IF NOT EXISTS idx_orders_created_at ON orders(created_at);

CREATE INDEX IF NOT EXISTS idx_order_items_order_id ON order_items(order_id);
CREATE INDEX IF NOT EXISTS idx_order_items_item_id ON order_items(item_id);

CREATE INDEX IF NOT EXISTS idx_inventory_reservations_order_id ON inventory_reservations(order_id);
CREATE INDEX IF NOT EXISTS idx_inventory_reservations_product_id ON inventory_reservations(product_id);
CREATE INDEX IF NOT EXISTS idx_inventory_reservations_status ON inventory_reservations(status);

CREATE INDEX IF NOT EXISTS idx_payments_order_id ON payments(order_id);
CREATE INDEX IF NOT EXISTS idx_payments_status ON payments(status);

CREATE INDEX IF NOT EXISTS idx_saga_state_order_id ON saga_states(order_id);
CREATE INDEX IF NOT EXISTS idx_saga_state_status ON saga_states(status);

CREATE INDEX IF NOT EXISTS idx_idempotency_keys_created_at ON idempotency_keys(created_at);

-- Seed some inventory
INSERT INTO products (product_id, name, description, price, stock) VALUES
    (gen_random_uuid(), 'Product A', 'Premium quality product A', 29.99, 100),
    (gen_random_uuid(), 'Product B', 'Standard product B', 19.99, 50),
    (gen_random_uuid(), 'Product C', 'Deluxe product C', 49.99, 75)
ON CONFLICT DO NOTHING;
