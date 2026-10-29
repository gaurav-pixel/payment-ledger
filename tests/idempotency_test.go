package tests

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"payment-ledger/internal/domain"
	"payment-ledger/internal/service"
)

func TestIdempotencyExactReplay(t *testing.T) {
	svc, _, _ := setupTestService()
	ctx := context.Background()

	accA, err := svc.CreateAccount(ctx, service.CreateAccountRequest{
		AccountNumber:  "IDEMP-A",
		HolderName:     "Alice",
		Currency:       "USD",
		InitialBalance: 10000,
	})
	require.NoError(t, err)

	accB, err := svc.CreateAccount(ctx, service.CreateAccountRequest{
		AccountNumber:  "IDEMP-B",
		HolderName:     "Bob",
		Currency:       "USD",
		InitialBalance: 0,
	})
	require.NoError(t, err)

	idempotencyKey := "unique-idemp-key-100"

	req := domain.CreateTransferRequest{
		IdempotencyKey: idempotencyKey,
		FromAccountID:  &accA.ID,
		ToAccountID:    &accB.ID,
		Amount:         2500, // $25.00
		Currency:       "USD",
		Description:    "First payment attempt",
	}

	// 1st Attempt: Fresh transfer
	res1, err := svc.ExecuteTransfer(ctx, req)
	require.NoError(t, err)
	assert.False(t, res1.Cached)
	assert.NotNil(t, res1.Transfer)

	// Verify balance after first transfer
	accAAfter1, _ := svc.GetAccount(ctx, accA.ID)
	assert.Equal(t, int64(7500), accAAfter1.Balance)

	// 2nd Attempt: Exact replay with identical key and payload
	res2, err := svc.ExecuteTransfer(ctx, req)
	require.NoError(t, err)
	assert.True(t, res2.Cached, "Result should be flagged as cached replay")
	assert.Equal(t, res1.Transfer.ID, res2.Transfer.ID, "Transfer ID must match original")

	// Verify balance was NOT deducted a second time!
	accAAfter2, _ := svc.GetAccount(ctx, accA.ID)
	assert.Equal(t, int64(7500), accAAfter2.Balance, "Balance must remain unchanged after idempotent replay")

	accBAfter2, _ := svc.GetAccount(ctx, accB.ID)
	assert.Equal(t, int64(2500), accBAfter2.Balance)
}

func TestIdempotencyKeyPayloadConflict(t *testing.T) {
	svc, _, _ := setupTestService()
	ctx := context.Background()

	accA, err := svc.CreateAccount(ctx, service.CreateAccountRequest{
		AccountNumber:  "IDEMP-CONF-A",
		HolderName:     "Alice",
		Currency:       "USD",
		InitialBalance: 10000,
	})
	require.NoError(t, err)

	accB, err := svc.CreateAccount(ctx, service.CreateAccountRequest{
		AccountNumber:  "IDEMP-CONF-B",
		HolderName:     "Bob",
		Currency:       "USD",
		InitialBalance: 0,
	})
	require.NoError(t, err)

	idempotencyKey := "reused-conflict-key-999"

	// 1st request: Transfer $10
	req1 := domain.CreateTransferRequest{
		IdempotencyKey: idempotencyKey,
		FromAccountID:  &accA.ID,
		ToAccountID:    &accB.ID,
		Amount:         1000,
		Currency:       "USD",
		Description:    "Legitimate transfer",
	}
	_, err = svc.ExecuteTransfer(ctx, req1)
	require.NoError(t, err)

	// 2nd request: Reuse SAME idempotency key but with DIFFERENT amount ($50)
	req2 := domain.CreateTransferRequest{
		IdempotencyKey: idempotencyKey,
		FromAccountID:  &accA.ID,
		ToAccountID:    &accB.ID,
		Amount:         5000, // Conflict!
		Currency:       "USD",
		Description:    "Conflicting transfer with same key",
	}

	_, err = svc.ExecuteTransfer(ctx, req2)
	assert.Error(t, err)
	assert.Equal(t, domain.ErrIdempotencyConflict, err, "Must return conflict when key is reused with different payload")
}

func TestConcurrentIdenticalIdempotencyRequests(t *testing.T) {
	svc, _, _ := setupTestService()
	ctx := context.Background()

	accA, err := svc.CreateAccount(ctx, service.CreateAccountRequest{
		AccountNumber:  "IDEMP-CONCUR-A",
		HolderName:     "Alice",
		Currency:       "USD",
		InitialBalance: 50000,
	})
	require.NoError(t, err)

	accB, err := svc.CreateAccount(ctx, service.CreateAccountRequest{
		AccountNumber:  "IDEMP-CONCUR-B",
		HolderName:     "Bob",
		Currency:       "USD",
		InitialBalance: 0,
	})
	require.NoError(t, err)

	idempotencyKey := "concurrent-idemp-burst-42"
	req := domain.CreateTransferRequest{
		IdempotencyKey: idempotencyKey,
		FromAccountID:  &accA.ID,
		ToAccountID:    &accB.ID,
		Amount:         3000,
		Currency:       "USD",
		Description:    "Concurrent burst transfer",
	}

	concurrency := 20
	var wg sync.WaitGroup
	results := make([]*service.TransferResult, concurrency)
	errors := make([]error, concurrency)

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			res, err := svc.ExecuteTransfer(ctx, req)
			results[idx] = res
			errors[idx] = err
		}(i)
	}

	wg.Wait()

	// Exactly ONE executed, or executed + cached replays; balance MUST only be deducted ONCE!
	finalA, _ := svc.GetAccount(ctx, accA.ID)
	assert.Equal(t, int64(47000), finalA.Balance, "Account A balance deducted exactly once across concurrent duplicate requests")

	finalB, _ := svc.GetAccount(ctx, accB.ID)
	assert.Equal(t, int64(3000), finalB.Balance, "Account B credited exactly once")
}
