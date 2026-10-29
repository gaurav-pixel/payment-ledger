package domain

import "errors"

var (
	ErrAccountNotFound         = errors.New("account not found")
	ErrAccountFrozen           = errors.New("account is frozen")
	ErrAccountClosed           = errors.New("account is closed")
	ErrInsufficientFunds       = errors.New("insufficient funds for transfer (overdraft limit exceeded)")
	ErrCurrencyMismatch        = errors.New("currency mismatch between accounts or entries")
	ErrUnbalancedEntries       = errors.New("unbalanced transaction: total debits must equal total credits")
	ErrInvalidAmount           = errors.New("amount must be strictly positive")
	ErrDuplicateIdempotencyKey = errors.New("idempotency key already exists")
	ErrIdempotencyConflict     = errors.New("idempotency key reused with different request payload")
	ErrIdempotencyInProgress   = errors.New("concurrent request in progress with same idempotency key")
	ErrInvalidTransfer         = errors.New("invalid transfer request")
	ErrSelfTransfer            = errors.New("sender and receiver accounts cannot be identical")
	ErrNoEntries               = errors.New("transfer must have at least two entries")
	ErrLedgerImmutable         = errors.New("ledger entries are immutable and cannot be updated or deleted")
)
