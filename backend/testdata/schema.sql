CREATE TABLE IF NOT EXISTS items (
    id text PRIMARY KEY,
    name text NOT NULL,
    price_usdt numeric(36,18) NOT NULL,
    stock int NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS orders (
    id text PRIMARY KEY,
    buyer_wallet text NOT NULL,
    tx_hash text,
    amount_usdt numeric(36,18) NOT NULL,
    created_at timestamptz DEFAULT now()
);
