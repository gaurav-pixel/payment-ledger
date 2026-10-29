package domain

import (
	"time"

	"github.com/google/uuid"
)

type TransferStatus string

const (
	TransferStatusPending   TransferStatus = "PENDING"
	TransferStatusCommitted TransferStatus = "COMMITTED"
	TransferStatusFailed    TransferStatus = "FAILED"
	TransferStatusReversed  TransferStatus = "REVERSED"
)

type TransferEntryRequest struct {
	AccountID uuid.UUID `json:"account_id"`
	Direction Direction `json:"direction"`
	Amount    int64     `json:"amount"` // Positive minor unit
}

type CreateTransferRequest struct {
	IdempotencyKey string                 `json:"idempotency_key"`
	Description    string                 `json:"description"`
	Currency       string                 `json:"currency"`
	Entries        []TransferEntryRequest `json:"entries,omitempty"`

	// Convenience fields for 2-legged peer-to-peer transfers
	FromAccountID *uuid.UUID `json:"from_account_id,omitempty"`
	ToAccountID   *uuid.UUID `json:"to_account_id,omitempty"`
	Amount        int64      `json:"amount,omitempty"`

	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

type Transfer struct {
	ID             uuid.UUID              `json:"id"`
	IdempotencyKey *string                `json:"idempotency_key,omitempty"`
	Description    string                 `json:"description"`
	Status         TransferStatus         `json:"status"`
	Currency       string                 `json:"currency"`
	Amount         int64                  `json:"amount"`
	Metadata       map[string]interface{} `json:"metadata,omitempty"`
	CreatedAt      time.Time              `json:"created_at"`
	Entries        []LedgerEntry          `json:"entries,omitempty"`
}

// NormalizeEntries converts convenient From/To fields into explicit Double-Entry legs if necessary.
func (req *CreateTransferRequest) NormalizeEntries() error {
	if len(req.Entries) == 0 {
		if req.FromAccountID == nil || req.ToAccountID == nil {
			return ErrNoEntries
		}
		if *req.FromAccountID == *req.ToAccountID {
			return ErrSelfTransfer
		}
		if req.Amount <= 0 {
			return ErrInvalidAmount
		}

		req.Entries = []TransferEntryRequest{
			{
				AccountID: *req.FromAccountID,
				Direction: DirectionDebit,
				Amount:    req.Amount,
			},
			{
				AccountID: *req.ToAccountID,
				Direction: DirectionCredit,
				Amount:    req.Amount,
			},
		}
	}
	return nil
}

// ValidateDoubleEntry enforces the fundamental double-entry invariant: Sum(Debits) == Sum(Credits).
func (req *CreateTransferRequest) ValidateDoubleEntry() error {
	if len(req.Entries) < 2 {
		return ErrNoEntries
	}

	var totalDebits int64 = 0
	var totalCredits int64 = 0

	for _, entry := range req.Entries {
		if entry.AccountID == uuid.Nil {
			return ErrAccountNotFound
		}
		if entry.Amount <= 0 {
			return ErrInvalidAmount
		}

		switch entry.Direction {
		case DirectionDebit:
			totalDebits += entry.Amount
		case DirectionCredit:
			totalCredits += entry.Amount
		default:
			return ErrInvalidTransfer
		}
	}

	if totalDebits != totalCredits {
		return ErrUnbalancedEntries
	}

	return nil
}

// TotalAmount returns total transaction volume (Sum of debits).
func (req *CreateTransferRequest) TotalAmount() int64 {
	var total int64
	for _, e := range req.Entries {
		if e.Direction == DirectionDebit {
			total += e.Amount
		}
	}
	return total
}
