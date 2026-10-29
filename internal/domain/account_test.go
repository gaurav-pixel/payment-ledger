package domain

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func TestAccountOperations(t *testing.T) {
	acc := &Account{
		ID:             uuid.New(),
		AccountNumber:  "TEST-01",
		HolderName:     "Test User",
		Type:           AccountTypeCustomer,
		Currency:       "USD",
		Balance:        1000,
		AllowOverdraft: false,
		OverdraftLimit: 500,
		Status:         AccountStatusActive,
	}

	// Status validation
	assert.NoError(t, acc.ValidateStatus())

	// CanCredit
	assert.NoError(t, acc.CanCredit(500))
	assert.ErrorIs(t, acc.CanCredit(-10), ErrInvalidAmount)

	// ApplyCredit
	newBal, err := acc.ApplyCredit(500)
	assert.NoError(t, err)
	assert.Equal(t, int64(1500), newBal)

	// CanDebit within balance
	assert.NoError(t, acc.CanDebit(1000))

	// ApplyDebit
	newBal, err = acc.ApplyDebit(1000)
	assert.NoError(t, err)
	assert.Equal(t, int64(500), newBal)

	// CanDebit with partial overdraft up to limit (500 limit, balance 500 -> can spend up to 1000)
	assert.NoError(t, acc.CanDebit(1000))
	newBal, err = acc.ApplyDebit(1000)
	assert.NoError(t, err)
	assert.Equal(t, int64(-500), newBal)

	// Exceeding overdraft limit
	assert.ErrorIs(t, acc.CanDebit(1), ErrInsufficientFunds)
	_, err = acc.ApplyDebit(1)
	assert.ErrorIs(t, err, ErrInsufficientFunds)

	// Frozen account
	acc.Status = AccountStatusFrozen
	assert.ErrorIs(t, acc.CanDebit(100), ErrAccountFrozen)
	assert.ErrorIs(t, acc.CanCredit(100), ErrAccountFrozen)

	// Closed account
	acc.Status = AccountStatusClosed
	assert.ErrorIs(t, acc.CanDebit(100), ErrAccountClosed)
	assert.ErrorIs(t, acc.CanCredit(100), ErrAccountClosed)
}
