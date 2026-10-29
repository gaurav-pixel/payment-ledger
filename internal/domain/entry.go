package domain

import (
	"time"

	"github.com/google/uuid"
)

type Direction string

const (
	DirectionDebit  Direction = "DEBIT"
	DirectionCredit Direction = "CREDIT"
)

type LedgerEntry struct {
	ID           uuid.UUID `json:"id"`
	TransferID   uuid.UUID `json:"transfer_id"`
	AccountID    uuid.UUID `json:"account_id"`
	Direction    Direction `json:"direction"`
	Amount       int64     `json:"amount"` // Always positive minor units
	BalanceAfter int64     `json:"balance_after"`
	SequenceNum  int64     `json:"sequence_num"`
	CreatedAt    time.Time `json:"created_at"`
}

func (e *LedgerEntry) Validate() error {
	if e.AccountID == uuid.Nil {
		return ErrAccountNotFound
	}
	if e.Amount <= 0 {
		return ErrInvalidAmount
	}
	if e.Direction != DirectionDebit && e.Direction != DirectionCredit {
		return ErrInvalidTransfer
	}
	return nil
}
