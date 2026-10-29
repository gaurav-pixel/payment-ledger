package mock

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"payment-ledger/internal/domain"
	"payment-ledger/internal/repository"
)

type InMemoryLedgerDB struct {
	mu          sync.RWMutex
	accounts    map[uuid.UUID]*domain.Account
	accountMu   map[uuid.UUID]*sync.Mutex
	accNumIndex map[string]uuid.UUID

	transfers   map[uuid.UUID]*domain.Transfer
	entries     []domain.LedgerEntry
	idempotency map[string]*domain.IdempotencyRecord
}

func NewInMemoryLedgerDB() *InMemoryLedgerDB {
	return &InMemoryLedgerDB{
		accounts:    make(map[uuid.UUID]*domain.Account),
		accountMu:   make(map[uuid.UUID]*sync.Mutex),
		accNumIndex: make(map[string]uuid.UUID),
		transfers:   make(map[uuid.UUID]*domain.Transfer),
		entries:     make([]domain.LedgerEntry, 0),
		idempotency: make(map[string]*domain.IdempotencyRecord),
	}
}

func (db *InMemoryLedgerDB) getAccountMutex(id uuid.UUID) *sync.Mutex {
	db.mu.Lock()
	defer db.mu.Unlock()
	if mu, exists := db.accountMu[id]; exists {
		return mu
	}
	mu := &sync.Mutex{}
	db.accountMu[id] = mu
	return mu
}

// AccountRepository Implementation
type mockAccountRepo struct {
	db *InMemoryLedgerDB
}

func NewMockAccountRepo(db *InMemoryLedgerDB) repository.AccountRepository {
	return &mockAccountRepo{db: db}
}

func (r *mockAccountRepo) Create(ctx context.Context, a *domain.Account) error {
	r.db.mu.Lock()
	defer r.db.mu.Unlock()

	if _, exists := r.db.accNumIndex[a.AccountNumber]; exists {
		return fmt.Errorf("account number %s already exists", a.AccountNumber)
	}

	clone := *a
	r.db.accounts[a.ID] = &clone
	r.db.accNumIndex[a.AccountNumber] = a.ID
	return nil
}

func (r *mockAccountRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.Account, error) {
	r.db.mu.RLock()
	defer r.db.mu.RUnlock()

	a, exists := r.db.accounts[id]
	if !exists {
		return nil, domain.ErrAccountNotFound
	}
	clone := *a
	return &clone, nil
}

func (r *mockAccountRepo) GetByAccountNumber(ctx context.Context, accNum string) (*domain.Account, error) {
	r.db.mu.RLock()
	defer r.db.mu.RUnlock()

	id, exists := r.db.accNumIndex[accNum]
	if !exists {
		return nil, domain.ErrAccountNotFound
	}
	a := r.db.accounts[id]
	clone := *a
	return &clone, nil
}

func (r *mockAccountRepo) GetForUpdate(ctx context.Context, tx pgx.Tx, ids []uuid.UUID) (map[uuid.UUID]*domain.Account, error) {
	// Deduplicate IDs
	unique := make(map[uuid.UUID]bool)
	var sortedIDs []uuid.UUID
	for _, id := range ids {
		if !unique[id] {
			unique[id] = true
			sortedIDs = append(sortedIDs, id)
		}
	}

	// CRITICAL: Consistent Lock Ordering by sorted UUID string
	sort.Slice(sortedIDs, func(i, j int) bool {
		return sortedIDs[i].String() < sortedIDs[j].String()
	})

	mockTx, ok := tx.(*mockTx)
	if !ok {
		return nil, fmt.Errorf("invalid transaction type for mock")
	}

	result := make(map[uuid.UUID]*domain.Account)

	for _, id := range sortedIDs {
		// Acquire row mutex deterministically
		mu := r.db.getAccountMutex(id)
		mu.Lock()
		mockTx.lockedAccountMus = append(mockTx.lockedAccountMus, mu)

		r.db.mu.RLock()
		acc, exists := r.db.accounts[id]
		if !exists {
			r.db.mu.RUnlock()
			return nil, domain.ErrAccountNotFound
		}
		// Clone for staging
		clone := *acc
		r.db.mu.RUnlock()

		result[id] = &clone
	}

	return result, nil
}

func (r *mockAccountRepo) UpdateBalance(ctx context.Context, tx pgx.Tx, accountID uuid.UUID, newBalance int64) error {
	mockTx, ok := tx.(*mockTx)
	if !ok {
		return fmt.Errorf("invalid transaction type for mock")
	}
	mockTx.stagedAccountBalances[accountID] = newBalance
	return nil
}

func (r *mockAccountRepo) List(ctx context.Context, limit, offset int) ([]*domain.Account, error) {
	r.db.mu.RLock()
	defer r.db.mu.RUnlock()

	var result []*domain.Account
	for _, acc := range r.db.accounts {
		clone := *acc
		result = append(result, &clone)
	}
	return result, nil
}

// TransferRepository Implementation
type mockTransferRepo struct {
	db *InMemoryLedgerDB
}

func NewMockTransferRepo(db *InMemoryLedgerDB) repository.TransferRepository {
	return &mockTransferRepo{db: db}
}

func (r *mockTransferRepo) Create(ctx context.Context, tx pgx.Tx, t *domain.Transfer) error {
	mockTx, ok := tx.(*mockTx)
	if !ok {
		return fmt.Errorf("invalid transaction type for mock")
	}
	clone := *t
	mockTx.stagedTransfer = &clone
	return nil
}

func (r *mockTransferRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.Transfer, error) {
	r.db.mu.RLock()
	defer r.db.mu.RUnlock()

	t, exists := r.db.transfers[id]
	if !exists {
		return nil, domain.ErrInvalidTransfer
	}
	clone := *t
	return &clone, nil
}

func (r *mockTransferRepo) List(ctx context.Context, limit, offset int) ([]*domain.Transfer, error) {
	r.db.mu.RLock()
	defer r.db.mu.RUnlock()

	var result []*domain.Transfer
	for _, t := range r.db.transfers {
		clone := *t
		result = append(result, &clone)
	}
	return result, nil
}

// EntryRepository Implementation
type mockEntryRepo struct {
	db *InMemoryLedgerDB
}

func NewMockEntryRepo(db *InMemoryLedgerDB) repository.EntryRepository {
	return &mockEntryRepo{db: db}
}

func (r *mockEntryRepo) CreateBatch(ctx context.Context, tx pgx.Tx, entries []domain.LedgerEntry) error {
	mockTx, ok := tx.(*mockTx)
	if !ok {
		return fmt.Errorf("invalid transaction type for mock")
	}
	mockTx.stagedEntries = append(mockTx.stagedEntries, entries...)
	return nil
}

func (r *mockEntryRepo) ListByAccount(ctx context.Context, accountID uuid.UUID, limit, offset int) ([]domain.LedgerEntry, error) {
	r.db.mu.RLock()
	defer r.db.mu.RUnlock()

	var result []domain.LedgerEntry
	for _, e := range r.db.entries {
		if e.AccountID == accountID {
			result = append(result, e)
		}
	}
	return result, nil
}

func (r *mockEntryRepo) ListByTransfer(ctx context.Context, transferID uuid.UUID) ([]domain.LedgerEntry, error) {
	r.db.mu.RLock()
	defer r.db.mu.RUnlock()

	var result []domain.LedgerEntry
	for _, e := range r.db.entries {
		if e.TransferID == transferID {
			result = append(result, e)
		}
	}
	return result, nil
}

// IdempotencyRepository Implementation
type mockIdempotencyRepo struct {
	db *InMemoryLedgerDB
}

func NewMockIdempotencyRepo(db *InMemoryLedgerDB) repository.IdempotencyRepository {
	return &mockIdempotencyRepo{db: db}
}

func (r *mockIdempotencyRepo) Get(ctx context.Context, key string) (*domain.IdempotencyRecord, error) {
	r.db.mu.RLock()
	defer r.db.mu.RUnlock()

	rec, exists := r.db.idempotency[key]
	if !exists {
		return nil, nil
	}
	clone := *rec
	return &clone, nil
}

func (r *mockIdempotencyRepo) CreateProcessing(ctx context.Context, record *domain.IdempotencyRecord) error {
	r.db.mu.Lock()
	defer r.db.mu.Unlock()

	if _, exists := r.db.idempotency[record.Key]; exists {
		return domain.ErrDuplicateIdempotencyKey
	}
	clone := *record
	r.db.idempotency[record.Key] = &clone
	return nil
}

func (r *mockIdempotencyRepo) UpdateCompleted(ctx context.Context, key string, transferID *uuid.UUID, statusCode int, responseBody string) error {
	r.db.mu.Lock()
	defer r.db.mu.Unlock()

	rec, exists := r.db.idempotency[key]
	if !exists {
		return fmt.Errorf("idempotency record %s not found", key)
	}
	rec.Status = domain.IdempotencyStatusCompleted
	rec.TransferID = transferID
	rec.StatusCode = statusCode
	rec.ResponseBody = responseBody
	return nil
}

func (r *mockIdempotencyRepo) UpdateFailed(ctx context.Context, key string, statusCode int, responseBody string) error {
	r.db.mu.Lock()
	defer r.db.mu.Unlock()

	rec, exists := r.db.idempotency[key]
	if !exists {
		return fmt.Errorf("idempotency record %s not found", key)
	}
	rec.Status = domain.IdempotencyStatusFailed
	rec.StatusCode = statusCode
	rec.ResponseBody = responseBody
	return nil
}

// AuditRepository Implementation
type mockAuditRepo struct {
	db *InMemoryLedgerDB
}

func NewMockAuditRepo(db *InMemoryLedgerDB) repository.AuditRepository {
	return &mockAuditRepo{db: db}
}

func (r *mockAuditRepo) CheckSystemBalanceInvariant(ctx context.Context) (int64, int64, bool, error) {
	r.db.mu.RLock()
	defer r.db.mu.RUnlock()

	var totalDebits int64 = 0
	var totalCredits int64 = 0

	for _, e := range r.db.entries {
		if e.Direction == domain.DirectionDebit {
			totalDebits += e.Amount
		} else if e.Direction == domain.DirectionCredit {
			totalCredits += e.Amount
		}
	}

	return totalDebits, totalCredits, totalDebits == totalCredits, nil
}

func (r *mockAuditRepo) CheckAccountIntegrity(ctx context.Context, accountID uuid.UUID) (int64, int64, bool, error) {
	r.db.mu.RLock()
	defer r.db.mu.RUnlock()

	acc, exists := r.db.accounts[accountID]
	if !exists {
		return 0, 0, false, domain.ErrAccountNotFound
	}

	var latestBalanceAfter int64 = 0
	var foundEntry bool
	for _, e := range r.db.entries {
		if e.AccountID == accountID {
			latestBalanceAfter = e.BalanceAfter
			foundEntry = true
		}
	}

	if !foundEntry {
		latestBalanceAfter = acc.Balance
	}

	return acc.Balance, latestBalanceAfter, acc.Balance == latestBalanceAfter, nil
}

// Transaction Manager and Mock Tx
type mockTx struct {
	pgx.Tx
	lockedAccountMus      []*sync.Mutex
	stagedAccountBalances map[uuid.UUID]int64
	stagedTransfer        *domain.Transfer
	stagedEntries         []domain.LedgerEntry
}

type MockTxManager struct {
	db *InMemoryLedgerDB
}

func NewMockTxManager(db *InMemoryLedgerDB) repository.TxManager {
	return &MockTxManager{db: db}
}

func (m *MockTxManager) ExecuteInTx(ctx context.Context, fn func(tx pgx.Tx) error) error {
	tx := &mockTx{
		stagedAccountBalances: make(map[uuid.UUID]int64),
	}

	// Release locked accounts once tx completes or aborts
	defer func() {
		for _, mu := range tx.lockedAccountMus {
			mu.Unlock()
		}
	}()

	if err := fn(tx); err != nil {
		// ROLLBACK: Staged writes discarded, locks automatically released in defer
		return err
	}

	// COMMIT: Commit staged writes atomically under DB write lock
	m.db.mu.Lock()
	defer m.db.mu.Unlock()

	for accID, newBal := range tx.stagedAccountBalances {
		if acc, exists := m.db.accounts[accID]; exists {
			acc.Balance = newBal
			acc.UpdatedAt = time.Now()
		}
	}

	if tx.stagedTransfer != nil {
		m.db.transfers[tx.stagedTransfer.ID] = tx.stagedTransfer
	}

	for _, entry := range tx.stagedEntries {
		entry.SequenceNum = int64(len(m.db.entries) + 1)
		m.db.entries = append(m.db.entries, entry)
	}

	return nil
}
