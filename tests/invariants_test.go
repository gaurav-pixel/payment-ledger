package tests

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"payment-ledger/internal/domain"
	"payment-ledger/internal/service"
)

func TestUnbalancedEntriesRejected(t *testing.T) {
	svc, _, _ := setupTestService()
	ctx := context.Background()

	accA, err := svc.CreateAccount(ctx, service.CreateAccountRequest{
		AccountNumber:  "ACC-UNBAL-A",
		HolderName:     "Sender",
		Currency:       "USD",
		InitialBalance: 10000,
	})
	require.NoError(t, err)

	accB, err := svc.CreateAccount(ctx, service.CreateAccountRequest{
		AccountNumber:  "ACC-UNBAL-B",
		HolderName:     "Receiver",
		Currency:       "USD",
		InitialBalance: 0,
	})
	require.NoError(t, err)

	// Unbalanced request: Debit 1000, Credit 900
	req := domain.CreateTransferRequest{
		IdempotencyKey: "tx-unbalanced-01",
		Currency:       "USD",
		Description:    "Unbalanced entry test",
		Entries: []domain.TransferEntryRequest{
			{AccountID: accA.ID, Direction: domain.DirectionDebit, Amount: 1000},
			{AccountID: accB.ID, Direction: domain.DirectionCredit, Amount: 900},
		},
	}

	result, err := svc.ExecuteTransfer(ctx, req)
	assert.Error(t, err)
	assert.Equal(t, domain.ErrUnbalancedEntries, err)
	assert.Nil(t, result)
}

func TestMultiLegDoubleEntryTransfer(t *testing.T) {
	svc, auditSvc, _ := setupTestService()
	ctx := context.Background()

	// 1. Customer account with $200.00
	customer, err := svc.CreateAccount(ctx, service.CreateAccountRequest{
		AccountNumber:  "CUST-001",
		HolderName:     "Customer John",
		Currency:       "USD",
		InitialBalance: 20000,
	})
	require.NoError(t, err)

	// 2. Merchant account with $0.00
	merchant, err := svc.CreateAccount(ctx, service.CreateAccountRequest{
		AccountNumber:  "MERCH-001",
		HolderName:     "Acme Store",
		Currency:       "USD",
		InitialBalance: 0,
	})
	require.NoError(t, err)

	// 3. Platform Fee Revenue account with $0.00
	feeAccount, err := svc.CreateAccount(ctx, service.CreateAccountRequest{
		AccountNumber:  "REV-FEES",
		HolderName:     "Platform Fee Revenue",
		Type:           domain.AccountTypeRevenue,
		Currency:       "USD",
		InitialBalance: 0,
	})
	require.NoError(t, err)

	// Split payment: $100 total
	// Customer debited: $100.00 (10,000 cents)
	// Merchant credited: $97.00 (9,700 cents)
	// Platform credited: $3.00 (300 cents)
	req := domain.CreateTransferRequest{
		IdempotencyKey: "tx-multileg-split-01",
		Currency:       "USD",
		Description:    "Marketplace order with 3% fee deduction",
		Entries: []domain.TransferEntryRequest{
			{AccountID: customer.ID, Direction: domain.DirectionDebit, Amount: 10000},
			{AccountID: merchant.ID, Direction: domain.DirectionCredit, Amount: 9700},
			{AccountID: feeAccount.ID, Direction: domain.DirectionCredit, Amount: 300},
		},
	}

	result, err := svc.ExecuteTransfer(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, int64(10000), result.Transfer.Amount)
	assert.Len(t, result.Transfer.Entries, 3)

	// Verify post-transfer account balances
	updatedCust, err := svc.GetAccount(ctx, customer.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(10000), updatedCust.Balance) // 20,000 - 10,000

	updatedMerch, err := svc.GetAccount(ctx, merchant.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(9700), updatedMerch.Balance) // 0 + 9,700

	updatedFee, err := svc.GetAccount(ctx, feeAccount.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(300), updatedFee.Balance) // 0 + 300

	// Verify System Ledger Invariant: Debits == Credits
	sysReport, err := auditSvc.AuditSystem(ctx)
	require.NoError(t, err)
	assert.True(t, sysReport.Balanced)
	assert.Equal(t, int64(10000), sysReport.TotalDebits)
	assert.Equal(t, int64(10000), sysReport.TotalCredits)

	// Verify Account Integrity for each account
	for _, accID := range []domain.Account{*customer, *merchant, *feeAccount} {
		accReport, err := auditSvc.AuditAccount(ctx, accID.ID)
		require.NoError(t, err)
		assert.True(t, accReport.Balanced, "Account %s cached balance matches computed balance", accID.AccountNumber)
	}
}
