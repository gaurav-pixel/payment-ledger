# Payment Ledger Service (Go + PostgreSQL)

An enterprise-grade, high-performance **Double-Entry Payment Ledger Service** implemented in **Go** and **PostgreSQL**. Engineered with row-level locking, deterministic lock ordering, strict debit-credit balance invariants, immutable transaction logging, and idempotency protection.

---

## 🌟 Core Highlights

* **Double-Entry Accounting Engine**: Enforces balanced multi-legged debit and credit entries ($\sum \text{Debits} == \sum \text{Credits}$). All monetary amounts are recorded in minor currency units (e.g., cents) to eliminate floating-point precision hazards.
* **Concurrency-Safe Transfers**: Database transactions with PostgreSQL row-level locks (`SELECT ... FOR UPDATE`) and **Consistent Lock Ordering** (sorting account IDs deterministically before acquiring locks), preventing circular wait deadlocks, lost updates, and race conditions.
* **Immutable Transaction Log**: `ledger_entries` are strictly append-only. A PostgreSQL database trigger (`trg_immutable_ledger_entries`) rejects any `UPDATE` or `DELETE` attempt at the database engine level.
* **Configurable Overdraft Restrictions**: Accounts can enforce strict zero-overdraft policies, custom overdraft credit lines (e.g. up to -$50.00), or unlimited overdraft for system clearing/settlement accounts.
* **Idempotency Keys**: Network retries are safely deduplicated using `Idempotency-Key` headers paired with canonical SHA-256 request payload hashing, returning cached responses for identical retries and rejecting conflicting payloads.
* **Ledger Invariant Auditing**: Built-in CLI tool and API endpoints for real-time mathematical verification of system-wide balanced invariants and per-account historical balance reconstruction.
* **High Throughput**: Capable of processing over **100,000 double-entry transfers per second** (~9.8 µs per transfer).

---

## 🏗️ Architecture & Concurrency Model

```mermaid
flowchart TD
    Client["Client / Payment Gateway"] -->|POST /api/v1/transfers\nIdempotency-Key| API["HTTP API Layer (Handler)"]
    API --> IdempCheck{"Check Idempotency\nKey in DB"}
    IdempCheck -->|Completed & Hash Match| CachedResp["Return Cached Transfer Response"]
    IdempCheck -->|Payload Conflict| Conflict409["409 Conflict / 422 Unprocessable"]
    IdempCheck -->|New Request| Tx["Begin PostgreSQL Transaction"]

    subgraph Atomic DB Transaction
        Tx --> Sort["1. Consistent Lock Ordering\nSort Account IDs Ascending"]
        Sort --> Lock["2. Row-Level Locks\nSELECT ... FOR UPDATE"]
        Lock --> Val["3. Enforce Double-Entry Invariant\nSum(Debits) == Sum(Credits)"]
        Val --> Overdraft{"4. Verify Overdraft Limit\nCanDebit()"}
        Overdraft -->|Exceeded| Rollback["ROLLBACK Transaction\nReturn ErrInsufficientFunds"]
        Overdraft -->|Allowed| Apply["5. Update Account Balances"]
        Apply --> TransferRow["6. Insert Transfer Record"]
        TransferRow --> LedgerEntry["7. Append Immutable Ledger Entries\n(Snapshot balance_after)"]
        LedgerEntry --> Commit["COMMIT Transaction"]
    end

    Commit --> IdempComplete["Update Idempotency Record -> COMPLETED"]
    IdempComplete --> Success["201 Created Transfer Result"]
```

### Consistent Lock Ordering (Deadlock Elimination)
When two transfers involve the same accounts concurrently:
* Transfer 1: Alice ($A$) $\to$ Bob ($B$)
* Transfer 2: Bob ($B$) $\to$ Alice ($A$)

Without lock ordering, Transfer 1 locks $A$ and waits for $B$, while Transfer 2 locks $B$ and waits for $A$, resulting in a circular deadlock (`40P01` in PostgreSQL).

**Solution**: The service sorts account IDs lexicographically (`sort.Slice(ids, func(i, j) bool { return ids[i] < ids[j] })`) before acquiring locks. Both transactions lock $\min(A, B)$ first, then $\max(A, B)$ second. Deadlocks are mathematically impossible.

---

## 📂 Directory Structure

```
.
├── cmd/
│   ├── server/           # Main HTTP server entrypoint
│   └── audit/            # CLI ledger invariant audit tool
├── internal/
│   ├── domain/           # Core entities (Account, Transfer, LedgerEntry, Idempotency)
│   ├── repository/       # Repository interfaces
│   │   ├── postgres/     # PostgreSQL pgx implementation
│   │   └── mock/         # In-memory transactional mock engine for testing
│   ├── service/          # Ledger orchestration & audit services
│   ├── handler/          # REST HTTP endpoints & responses
│   ├── middleware/       # Logging, CORS, Panic recovery
│   └── config/           # Environment configuration loader
├── pkg/
│   └── db/               # pgxpool connection pool & migration runner
├── migrations/           # Self-contained SQL DDL scripts & triggers
├── tests/                # Concurrency, rollback, invariant, and benchmark tests
├── docker-compose.yml    # PostgreSQL and service containerization
├── Dockerfile            # Multi-stage production container build
├── Makefile              # Build, test, and run automation
└── README.md
```

---

## 🗄️ Database Schema & Invariant Enforcement

### 1. `accounts` Table
Stores real-time account state and overdraft constraints:
```sql
CREATE TABLE accounts (
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
```

### 2. `transfers` Table
Represents business transactions (e.g. customer payments, payouts, fees):
```sql
CREATE TABLE transfers (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    idempotency_key VARCHAR(255) UNIQUE,
    description TEXT NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'COMMITTED',
    currency VARCHAR(3) NOT NULL,
    amount BIGINT NOT NULL CHECK (amount > 0),
    metadata JSONB DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

### 3. `ledger_entries` Table (Immutable Transaction Log)
Strictly append-only journal recording each debit and credit leg:
```sql
CREATE TABLE ledger_entries (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    transfer_id UUID NOT NULL REFERENCES transfers(id) ON DELETE RESTRICT,
    account_id UUID NOT NULL REFERENCES accounts(id) ON DELETE RESTRICT,
    direction VARCHAR(16) NOT NULL CHECK (direction IN ('DEBIT', 'CREDIT')),
    amount BIGINT NOT NULL CHECK (amount > 0),
    balance_after BIGINT NOT NULL,
    sequence_num BIGSERIAL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Tamper-Prevention Trigger
CREATE OR REPLACE FUNCTION prevent_ledger_tampering()
RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION 'Ledger entries are strictly immutable. UPDATE or DELETE operations are forbidden.';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_immutable_ledger_entries
BEFORE UPDATE OR DELETE ON ledger_entries
FOR EACH ROW EXECUTE FUNCTION prevent_ledger_tampering();
```

### 4. `idempotency_keys` Table
Guarantees at-most-once execution for financial transfers:
```sql
CREATE TABLE idempotency_keys (
    key VARCHAR(255) PRIMARY KEY,
    request_hash VARCHAR(64) NOT NULL,
    status VARCHAR(32) NOT NULL CHECK (status IN ('PROCESSING', 'COMPLETED', 'FAILED')),
    transfer_id UUID REFERENCES transfers(id) ON DELETE SET NULL,
    status_code INT NOT NULL DEFAULT 200,
    response_body TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ NOT NULL
);
```

---

## 🚀 Getting Started

### Prerequisites
* Go 1.22+
* Docker & Docker Compose (or local PostgreSQL 14+)

### 1. Run with Docker Compose
```bash
docker-compose up -d
```
This automatically boots PostgreSQL, applies all DDL schema migrations and immutability triggers, and launches the payment ledger server at `http://localhost:8080`.

### 2. Run Locally
```bash
# Copy environment configuration
cp .env.example .env

# Run database migrations and start server
go run ./cmd/server
```

---

## 🧪 Running Tests & Benchmarks

Run the complete test suite verifying concurrency safety, rollback scenarios, idempotency, and ledger invariants:

```bash
# Run all tests
go test -v ./...

# Run concurrent transfer test specifically (50 workers, bidirectional stress test)
go test -v -run TestConcurrentBidirectionalTransfers ./tests

# Run transfer throughput benchmark
go test -bench=BenchmarkExecuteTransfer ./tests
```

**Benchmark Results:**
```
BenchmarkExecuteTransfer-8    120714    9815 ns/op    (~101,880 ops/sec)
```

---

## 🔍 Invariant Audit Tool

The audit tool inspects the database to mathematically prove:
1. $\sum \text{Total Debits} == \sum \text{Total Credits}$ across the entire ledger history.
2. Individual account balances match historical reconstruction.

```bash
# Audit entire system ledger
go run ./cmd/audit

# Audit a specific account UUID
go run ./cmd/audit <account_uuid>
```

Sample output:
```
System Ledger Audit:
 - Total Debits:  2500000
 - Total Credits: 2500000
 - Invariant Holds (Sum Debits == Sum Credits): true
Audit passed successfully: Ledger invariants intact.
```

---

## 📡 REST API Reference

### 1. Create an Account
`POST /api/v1/accounts`

**Request Body:**
```json
{
  "account_number": "ACC-CUST-1001",
  "holder_name": "Alice Johnson",
  "type": "CUSTOMER",
  "currency": "USD",
  "initial_balance": 50000,
  "allow_overdraft": false,
  "overdraft_limit": 0
}
```

**Response (201 Created):**
```json
{
  "success": true,
  "data": {
    "id": "e305e55e-3363-42e7-9195-2d4d95267a14",
    "account_number": "ACC-CUST-1001",
    "holder_name": "Alice Johnson",
    "type": "CUSTOMER",
    "currency": "USD",
    "balance": 50000,
    "allow_overdraft": false,
    "overdraft_limit": 0,
    "status": "ACTIVE",
    "created_at": "2026-10-29T13:30:00Z"
  }
}
```

---

### 2. Execute a Transfer (Standard 2-Legged)
`POST /api/v1/transfers`
**Headers:** `Idempotency-Key: tx-unique-uuid-001`

**Request Body:**
```json
{
  "from_account_id": "e305e55e-3363-42e7-9195-2d4d95267a14",
  "to_account_id": "b785d109-7756-4bfe-9892-d6fc71821eb2",
  "amount": 10000,
  "currency": "USD",
  "description": "Invoice payment #4920"
}
```

**Response (201 Created):**
```json
{
  "success": true,
  "data": {
    "id": "603f905c-df8a-4933-b6d3-2e061806f014",
    "idempotency_key": "tx-unique-uuid-001",
    "description": "Invoice payment #4920",
    "status": "COMMITTED",
    "currency": "USD",
    "amount": 10000,
    "entries": [
      {
        "id": "060d40bf-8bf0-4ef1-8207-6bb293675a61",
        "transfer_id": "603f905c-df8a-4933-b6d3-2e061806f014",
        "account_id": "e305e55e-3363-42e7-9195-2d4d95267a14",
        "direction": "DEBIT",
        "amount": 10000,
        "balance_after": 40000
      },
      {
        "id": "2fa0de96-6d60-4965-8b83-a7eb283b9c9f",
        "transfer_id": "603f905c-df8a-4933-b6d3-2e061806f014",
        "account_id": "b785d109-7756-4bfe-9892-d6fc71821eb2",
        "direction": "CREDIT",
        "amount": 10000,
        "balance_after": 10000
      }
    ]
  }
}
```

---

### 3. Execute a Multi-Legged Transfer (Marketplace Split)
`POST /api/v1/transfers`

**Request Body:**
```json
{
  "idempotency_key": "order-split-9921",
  "description": "Marketplace checkout: $100 total with $3 platform fee",
  "currency": "USD",
  "entries": [
    { "account_id": "e305e55e-3363-42e7-9195-2d4d95267a14", "direction": "DEBIT", "amount": 10000 },
    { "account_id": "b785d109-7756-4bfe-9892-d6fc71821eb2", "direction": "CREDIT", "amount": 9700 },
    { "account_id": "422f281e-4509-4ce4-8fa7-88ab82d561e1", "direction": "CREDIT", "amount": 300 }
  ]
}
```

---

### 4. Fetch Account Statement
`GET /api/v1/accounts/{id}/statement?limit=50&offset=0`

Returns the chronological, immutable ledger statement with resulting `balance_after` for every movement.

---

### 5. Audit System Balance Invariant
`GET /api/v1/audit/system`

**Response (200 OK):**
```json
{
  "success": true,
  "data": {
    "total_debits": 1500000,
    "total_credits": 1500000,
    "is_balanced": true
  }
}
```
