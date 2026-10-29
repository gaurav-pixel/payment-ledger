package tests

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"payment-ledger/internal/domain"
	"payment-ledger/internal/repository/mock"
	"payment-ledger/internal/service"
)

func setupTestService() (service.LedgerService, service.AuditService, *mock.InMemoryLedgerDB) {
	db := mock.NewInMemoryLedgerDB()
	accRepo := mock.NewMockAccountRepo(db)
	txRepo := mock.NewMockTransferRepo(db)
	entryRepo := mock.NewMockEntryRepo(db)
	idempRepo := mock.NewMockIdempotencyRepo(db)
	auditRepo := mock.NewMockAuditRepo(db)
	txMgr := mock.NewMockTxManager(db)

	ledgerSvc := service.NewLedgerService(accRepo, txRepo, entryRepo, idempRepo, txMgr)
	auditSvc := service.NewAuditService(auditRepo)
	return ledgerSvc, auditSvc, db
}

// TestConcurrentBidirectionalTransfers stress tests consistent lock ordering.
// Account A transfers to Account B while Account B transfers to Account A concurrently.
// Without lock ordering, this triggers circular wait deadlocks.
// With deterministic lock ordering, all transfers complete without deadlocks!
func TestConcurrentBidirectionalTransfers(t *testing.T) {
	svc, auditSvc, _ := setupTestService()
	ctx := context.Background()

	// Initial balances: $10,000.00 each
	initialBalance := int64(1000000)

	accA, err := svc.CreateAccount(ctx, service.CreateAccountRequest{
		AccountNumber:  "ACC-A",
		HolderName:     "Alice",
		Currency:       "USD",
		InitialBalance: initialBalance,
		AllowOverdraft: false,
	})
	require.NoError(t, err)

	accB, err := svc.CreateAccount(ctx, service.CreateAccountRequest{
		AccountNumber:  "ACC-B",
		HolderName:     "Bob",
		Currency:       "USD",
		InitialBalance: initialBalance,
		AllowOverdraft: false,
	})
	require.NoError(t, err)

	numWorkers := 50
	transfersPerWorker := 20
	transferAmount := int64(100) // $1.00

	var wg sync.WaitGroup
	var completedAToB int64
	var completedBToA int64
	var failedCount int64

	// Concurrently send A -> B and B -> A
	for i := 0; i < numWorkers; i++ {
		wg.Add(2)

		// Goroutine 1: Alice -> Bob
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < transfersPerWorker; j++ {
				req := domain.CreateTransferRequest{
					IdempotencyKey: fmt.Sprintf("A-to-B-w%d-t%d", workerID, j),
					FromAccountID:  &accA.ID,
					ToAccountID:    &accB.ID,
					Amount:         transferAmount,
					Currency:       "USD",
					Description:    "Alice to Bob concurrent transfer",
				}
				_, err := svc.ExecuteTransfer(ctx, req)
				if err == nil {
					atomic.AddInt64(&completedAToB, 1)
				} else {
					atomic.AddInt64(&failedCount, 1)
				}
			}
		}(i)

		// Goroutine 2: Bob -> Alice (reverse direction, tests deadlock prevention!)
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < transfersPerWorker; j++ {
				req := domain.CreateTransferRequest{
					IdempotencyKey: fmt.Sprintf("B-to-A-w%d-t%d", workerID, j),
					FromAccountID:  &accB.ID,
					ToAccountID:    &accA.ID,
					Amount:         transferAmount,
					Currency:       "USD",
					Description:    "Bob to Alice concurrent transfer",
				}
				_, err := svc.ExecuteTransfer(ctx, req)
				if err == nil {
					atomic.AddInt64(&completedBToA, 1)
				} else {
					atomic.AddInt64(&failedCount, 1)
				}
			}
		}(i)
	}

	wg.Wait()

	t.Logf("Completed A->B: %d, Completed B->A: %d, Failures: %d", completedAToB, completedBToA, failedCount)
	assert.Equal(t, int64(0), failedCount, "Zero failures or deadlocks expected under consistent lock ordering")

	// Validate final balances
	finalA, err := svc.GetAccount(ctx, accA.ID)
	require.NoError(t, err)
	finalB, err := svc.GetAccount(ctx, accB.ID)
	require.NoError(t, err)

	expectedA := initialBalance - (completedAToB * transferAmount) + (completedBToA * transferAmount)
	expectedB := initialBalance - (completedBToA * transferAmount) + (completedAToB * transferAmount)

	assert.Equal(t, expectedA, finalA.Balance, "Account A balance matches exact algebraic delta")
	assert.Equal(t, expectedB, finalB.Balance, "Account B balance matches exact algebraic delta")

	// Verify total money conservation invariant
	totalInitial := initialBalance * 2
	totalFinal := finalA.Balance + finalB.Balance
	assert.Equal(t, totalInitial, totalFinal, "Total system money strictly conserved")

	// Verify ledger system invariant
	sysReport, err := auditSvc.AuditSystem(ctx)
	require.NoError(t, err)
	assert.True(t, sysReport.Balanced, "Total debits must equal total credits in ledger entries")
}
