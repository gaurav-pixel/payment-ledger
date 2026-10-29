package tests

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"payment-ledger/internal/domain"
	"payment-ledger/internal/service"
)

func TestConfigurableOverdraftRestrictions(t *testing.T) {
	svc, _, _ := setupTestService()
	ctx := context.Background()

	// 1. Standard account with zero overdraft allowed
	strictAcc, err := svc.CreateAccount(ctx, service.CreateAccountRequest{
		AccountNumber:  "STRICT-ACC",
		HolderName:     "Strict Customer",
		Currency:       "USD",
		InitialBalance: 1000, // $10.00
		AllowOverdraft: false,
		OverdraftLimit: 0,
	})
	require.NoError(t, err)

	// 2. VIP account with $50.00 overdraft cushion (overdraft_limit = 5000)
	vipAcc, err := svc.CreateAccount(ctx, service.CreateAccountRequest{
		AccountNumber:  "VIP-ACC",
		HolderName:     "VIP Customer",
		Currency:       "USD",
		InitialBalance: 1000, // $10.00
		AllowOverdraft: false,
		OverdraftLimit: 5000, // Can spend up to $60.00 total (down to -$50.00)
	})
	require.NoError(t, err)

	// 3. System Clearing / Settlement account with unbounded overdraft allowed
	clearingAcc, err := svc.CreateAccount(ctx, service.CreateAccountRequest{
		AccountNumber:  "CLEARING-SYS",
		HolderName:     "Central Clearinghouse",
		Type:           domain.AccountTypeLiability,
		Currency:       "USD",
		InitialBalance: 0,
		AllowOverdraft: true,
	})
	require.NoError(t, err)

	// Destination receiver
	receiver, err := svc.CreateAccount(ctx, service.CreateAccountRequest{
		AccountNumber:  "REC-001",
		HolderName:     "Store",
		Currency:       "USD",
		InitialBalance: 0,
	})
	require.NoError(t, err)

	// Case A: Strict account attempts to spend $15.00 (balance is $10.00) -> Must fail
	_, err = svc.ExecuteTransfer(ctx, domain.CreateTransferRequest{
		FromAccountID: &strictAcc.ID,
		ToAccountID:   &receiver.ID,
		Amount:        1500,
		Currency:      "USD",
		Description:   "Strict overdraft attempt",
	})
	assert.ErrorIs(t, err, domain.ErrInsufficientFunds)

	// Case B: VIP account spends $40.00 (balance becomes -$30.00, which is within the $50 limit) -> Must succeed!
	vipRes, err := svc.ExecuteTransfer(ctx, domain.CreateTransferRequest{
		FromAccountID: &vipAcc.ID,
		ToAccountID:   &receiver.ID,
		Amount:        4000,
		Currency:      "USD",
		Description:   "VIP authorized overdraft spending",
	})
	require.NoError(t, err)
	assert.NotNil(t, vipRes)

	vipCheck, _ := svc.GetAccount(ctx, vipAcc.ID)
	assert.Equal(t, int64(-3000), vipCheck.Balance, "VIP account enters authorized negative overdraft")

	// Case C: VIP account attempts to spend another $30.00 (would bring balance to -$60.00, exceeding -$50.00) -> Must fail!
	_, err = svc.ExecuteTransfer(ctx, domain.CreateTransferRequest{
		FromAccountID: &vipAcc.ID,
		ToAccountID:   &receiver.ID,
		Amount:        3000,
		Currency:      "USD",
		Description:   "VIP exceeding overdraft limit attempt",
	})
	assert.ErrorIs(t, err, domain.ErrInsufficientFunds)

	// Case D: Clearing account transfers $1,000,000 without bounds -> Must succeed!
	clearingRes, err := svc.ExecuteTransfer(ctx, domain.CreateTransferRequest{
		FromAccountID: &clearingAcc.ID,
		ToAccountID:   &receiver.ID,
		Amount:        100000000,
		Currency:      "USD",
		Description:   "Central clearing liquidity injection",
	})
	require.NoError(t, err)
	assert.NotNil(t, clearingRes)

	clearingCheck, _ := svc.GetAccount(ctx, clearingAcc.ID)
	assert.Equal(t, int64(-100000000), clearingCheck.Balance, "Clearing account can go arbitrarily negative")
}
