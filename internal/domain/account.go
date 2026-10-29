package domain

import (
	"time"

	"github.com/google/uuid"
)

type AccountType string

const (
	AccountTypeAsset     AccountType = "ASSET"
	AccountTypeLiability AccountType = "LIABILITY"
	AccountTypeEquity    AccountType = "EQUITY"
	AccountTypeRevenue   AccountType = "REVENUE"
	AccountTypeExpense   AccountType = "EXPENSE"
	AccountTypeCustomer  AccountType = "CUSTOMER"
)

type AccountStatus string

const (
	AccountStatusActive AccountStatus = "ACTIVE"
	AccountStatusFrozen AccountStatus = "FROZEN"
	AccountStatusClosed AccountStatus = "CLOSED"
)

type Account struct {
	ID             uuid.UUID     `json:"id"`
	AccountNumber  string        `json:"account_number"`
	HolderName     string        `json:"holder_name"`
	Type           AccountType   `json:"type"`
	Currency       string        `json:"currency"`
	Balance        int64         `json:"balance"` // In minor units (e.g. cents)
	AllowOverdraft bool          `json:"allow_overdraft"`
	OverdraftLimit int64         `json:"overdraft_limit"` // e.g., 5000 = $50.00 overdraft allowed
	Status         AccountStatus `json:"status"`
	CreatedAt      time.Time     `json:"created_at"`
	UpdatedAt      time.Time     `json:"updated_at"`
}

// ValidateStatus checks if the account is in an operable state.
func (a *Account) ValidateStatus() error {
	switch a.Status {
	case AccountStatusActive:
		return nil
	case AccountStatusFrozen:
		return ErrAccountFrozen
	case AccountStatusClosed:
		return ErrAccountClosed
	default:
		return ErrAccountFrozen
	}
}

// CanDebit checks if the account can afford the deduction under its overdraft policy.
func (a *Account) CanDebit(amount int64) error {
	if amount <= 0 {
		return ErrInvalidAmount
	}
	if err := a.ValidateStatus(); err != nil {
		return err
	}

	// Calculate resulting balance
	newBalance := a.Balance - amount

	// If overdraft is not allowed at all and no limit is set
	if !a.AllowOverdraft {
		if newBalance < -a.OverdraftLimit {
			return ErrInsufficientFunds
		}
	}

	return nil
}

// CanCredit checks if account can receive funds.
func (a *Account) CanCredit(amount int64) error {
	if amount <= 0 {
		return ErrInvalidAmount
	}
	return a.ValidateStatus()
}

// ApplyDebit deducts funds and returns the new balance.
func (a *Account) ApplyDebit(amount int64) (int64, error) {
	if err := a.CanDebit(amount); err != nil {
		return a.Balance, err
	}
	a.Balance -= amount
	return a.Balance, nil
}

// ApplyCredit adds funds and returns the new balance.
func (a *Account) ApplyCredit(amount int64) (int64, error) {
	if err := a.CanCredit(amount); err != nil {
		return a.Balance, err
	}
	a.Balance += amount
	return a.Balance, nil
}
