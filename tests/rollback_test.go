package tests

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"payment-ledger/internal/domain"
	"payment-ledger/internal/service"
)

func TestRollbackOnInsufficientFunds(t *testing.T) {
	svc, auditSvc, _ := setupTestService()
	ctx := context.Background()

	// Account A with $50.00 balance, no overdraft
	accA, err := svc.CreateAccount(ctx, service.CreateAccountRequest{
		AccountNumber:  "ACC-ROLLBACK-A",
		HolderName:     "Sender",
		Currency:       "USD",
		InitialBalance: 5000,
		AllowOverdraft: false,
		OverdraftLimit: 0,
	})
	require.NoError(t, err)

	// Account B with $10.00 balance
	accB, err := svc.CreateAccount(ctx, service.CreateAccountRequest{
		AccountNumber:  "ACC-ROLLBACK-B",
		HolderName:     "Receiver",
		Currency:       "USD",
		InitialBalance: 1000,
		AllowOverdraft: false,
	})
	require.NoError(t, err)

	// Attempt to transfer $100.00 (exceeds balance of $50.00)
	req := domain.CreateTransferRequest{
		IdempotencyKey: "tx-insufficient-funds-01",
		FromAccountID:  &accA.ID,
		ToAccountID:    &accB.ID,
		Amount:         10000,
		Currency:       "USD",
		Description:    "Attempted overdraft transfer",
	}

	result, err := svc.ExecuteTransfer(ctx, req)
	assert.Error(t, err)
	assert.ErrorIs(t, err, domain.ErrInsufficientFunds)
	assert.Nil(t, result)

	// Verify Rollback: Account A's balance MUST remain 5000
	afterA, err := svc.GetAccount(ctx, accA.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(5000), afterA.Balance, "Account A balance untouched after rollback")

	// Verify Rollback: Account B's balance MUST remain 1000
	afterB, err := svc.GetAccount(ctx, accB.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(1000), afterB.Balance, "Account B balance untouched after rollback")

	// Verify no ledger entries created for either account
	stmtA, err := svc.GetAccountStatement(ctx, accA.ID, 10, 0)
	require.NoError(t, err)
	assert.Len(t, stmtA, 0, "No ledger entries recorded for rolled back transfer")

	// Audit system check
	report, err := auditSvc.AuditSystem(ctx)
	require.NoError(t, err)
	assert.True(t, report.Balanced)
	assert.Equal(t, int64(0), report.TotalDebits)
}

func TestRollbackOnMissingTargetAccount(t *testing.T) {
	svc, _, _ := setupTestService()
	ctx := context.Background()

	accA, err := svc.CreateAccount(ctx, service.CreateAccountRequest{
		AccountNumber:  "ACC-SOURCE",
		HolderName:     "Valid Source",
		Currency:       "USD",
		InitialBalance: 5000,
	})
	require.NoError(t, err)

	nonExistentID := uuid.New()

	req := domain.CreateTransferRequest{
		FromAccountID: &accA.ID,
		ToAccountID:   &nonExistentID,
		Amount:        1000,
		Currency:      "USD",
		Description:   "Transfer to non-existent account",
	}

	result, err := svc.ExecuteTransfer(ctx, req)
	assert.Error(t, err)
	assert.ErrorIs(t, err, domain.ErrAccountNotFound)
	assert.Nil(t, result)

	// Verify Account A balance was not deducted
	afterA, err := svc.GetAccount(ctx, accA.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(5000), afterA.Balance)
}

func TestRollbackOnCurrencyMismatch(t *testing.T) {
	svc, _, _ := setupTestService()
	ctx := context.Background()

	accUSD, err := svc.CreateAccount(ctx, service.CreateAccountRequest{
		AccountNumber:  "ACC-USD",
		HolderName:     "USD Holder",
		Currency:       "USD",
		InitialBalance: 10000,
	})
	require.NoError(t, err)

	accEUR, err := svc.CreateAccount(ctx, service.CreateAccountRequest{
		AccountNumber:  "ACC-EUR",
		HolderName:     "EUR Holder",
		Currency:       "EUR",
		InitialBalance: 10000,
	})
	require.NoError(t, err)

	req := domain.CreateTransferRequest{
		FromAccountID: &accUSD.ID,
		ToAccountID:   &accEUR.ID,
		Amount:        1000,
		Currency:      "USD", // accEUR is EUR -> mismatch
		Description:   "Currency mismatch transfer",
	}

	result, err := svc.ExecuteTransfer(ctx, req)
	assert.Error(t, err)
	assert.Equal(t, domain.ErrCurrencyMismatch, err)
	assert.Nil(t, result)

	// Both accounts remain unchanged
	checkUSD, _ := svc.GetAccount(ctx, accUSD.ID)
	checkEUR, _ := svc.GetAccount(ctx, accEUR.ID)
	assert.Equal(t, int64(10000), checkUSD.Balance)
	assert.Equal(t, int64(10000), checkEUR.Balance)
}
