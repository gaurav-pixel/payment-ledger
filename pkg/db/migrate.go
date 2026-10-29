package db

import (
	"context"
	"fmt"
	"log"

	"github.com/jackc/pgx/v5/pgxpool"
)

// SQLMigrations embedded directly for self-contained, deterministic schema provisioning.
var migrationScripts = []string{
	// 01 Accounts
	`CREATE EXTENSION IF NOT EXISTS "pgcrypto";

CREATE TABLE IF NOT EXISTS accounts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    account_number VARCHAR(64) UNIQUE NOT NULL,
    holder_name VARCHAR(255) NOT NULL,
    type VARCHAR(32) NOT NULL CHECK (type IN ('ASSET', 'LIABILITY', 'EQUITY', 'REVENUE', 'EXPENSE', 'CUSTOMER')),
    currency VARCHAR(3) NOT NULL,
    balance BIGINT NOT NULL DEFAULT 0,
    allow_overdraft BOOLEAN NOT NULL DEFAULT FALSE,
    overdraft_limit BIGINT NOT NULL DEFAULT 0 CHECK (overdraft_limit >= 0),
    status VARCHAR(32) NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE', 'FROZEN', 'CLOSED')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_overdraft_balance CHECK (
        allow_overdraft = TRUE OR balance >= -overdraft_limit
    )
);

CREATE INDEX IF NOT EXISTS idx_accounts_currency ON accounts(currency);
CREATE INDEX IF NOT EXISTS idx_accounts_status ON accounts(status);

CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ language 'plpgsql';

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_trigger WHERE tgname = 'trg_accounts_updated_at'
    ) THEN
        CREATE TRIGGER trg_accounts_updated_at
        BEFORE UPDATE ON accounts
        FOR EACH ROW
        EXECUTE FUNCTION update_updated_at_column();
    END IF;
END $$;`,

	// 02 Transfers
	`CREATE TABLE IF NOT EXISTS transfers (
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
CREATE INDEX IF NOT EXISTS idx_transfers_created_at ON transfers(created_at DESC);`,

	// 03 Ledger Entries
	`CREATE TABLE IF NOT EXISTS ledger_entries (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    transfer_id UUID NOT NULL REFERENCES transfers(id) ON DELETE RESTRICT,
    account_id UUID NOT NULL REFERENCES accounts(id) ON DELETE RESTRICT,
    direction VARCHAR(16) NOT NULL CHECK (direction IN ('DEBIT', 'CREDIT')),
    amount BIGINT NOT NULL CHECK (amount > 0),
    balance_after BIGINT NOT NULL,
    sequence_num BIGSERIAL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_ledger_entries_account_created ON ledger_entries(account_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_ledger_entries_transfer_id ON ledger_entries(transfer_id);
CREATE INDEX IF NOT EXISTS idx_ledger_entries_seq ON ledger_entries(sequence_num);

CREATE OR REPLACE FUNCTION prevent_ledger_tampering()
RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION 'Ledger entries are strictly immutable. UPDATE or DELETE operations are forbidden.';
END;
$$ LANGUAGE plpgsql;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_trigger WHERE tgname = 'trg_immutable_ledger_entries'
    ) THEN
        CREATE TRIGGER trg_immutable_ledger_entries
        BEFORE UPDATE OR DELETE ON ledger_entries
        FOR EACH ROW
        EXECUTE FUNCTION prevent_ledger_tampering();
    END IF;
END $$;`,

	// 04 Idempotency Keys
	`CREATE TABLE IF NOT EXISTS idempotency_keys (
    key VARCHAR(255) PRIMARY KEY,
    request_hash VARCHAR(64) NOT NULL,
    status VARCHAR(32) NOT NULL CHECK (status IN ('PROCESSING', 'COMPLETED', 'FAILED')),
    transfer_id UUID REFERENCES transfers(id) ON DELETE SET NULL,
    status_code INT NOT NULL DEFAULT 200,
    response_body TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_idempotency_expires_at ON idempotency_keys(expires_at);`,
}

// RunMigrations applies all DDL migrations sequentially.
func RunMigrations(ctx context.Context, pool *pgxpool.Pool) error {
	log.Println("Applying database migrations...")
	for idx, script := range migrationScripts {
		if _, err := pool.Exec(ctx, script); err != nil {
			return fmt.Errorf("migration step %d failed: %w", idx+1, err)
		}
	}
	log.Println("Database migrations applied successfully.")
	return nil
}
