CREATE TABLE IF NOT EXISTS transfers (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    idempotency_key VARCHAR(255) UNIQUE,
    description TEXT NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'COMMITTED' CHECK (status IN ('PENDING', 'COMMITTED', 'FAILED', 'REVERSED')),
    currency VARCHAR(3) NOT NULL,
    amount BIGINT NOT NULL CHECK (amount > 0),
    metadata JSONB DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_transfers_idempotency ON transfers(idempotency_key);
CREATE INDEX IF NOT EXISTS idx_transfers_created_at ON transfers(created_at DESC);
