package domain

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func TestTransferValidation(t *testing.T) {
	id1 := uuid.New()
	id2 := uuid.New()

	// 1. Self transfer rejected
	reqSelf := CreateTransferRequest{
		FromAccountID: &id1,
		ToAccountID:   &id1,
		Amount:        100,
	}
	assert.ErrorIs(t, reqSelf.NormalizeEntries(), ErrSelfTransfer)

	// 2. Negative amount rejected
	reqNeg := CreateTransferRequest{
		FromAccountID: &id1,
		ToAccountID:   &id2,
		Amount:        -50,
	}
	assert.ErrorIs(t, reqNeg.NormalizeEntries(), ErrInvalidAmount)

	// 3. Normal 2-leg transfer normalization
	reqValid := CreateTransferRequest{
		FromAccountID: &id1,
		ToAccountID:   &id2,
		Amount:        1500,
		Currency:      "USD",
	}
	assert.NoError(t, reqValid.NormalizeEntries())
	assert.Len(t, reqValid.Entries, 2)
	assert.NoError(t, reqValid.ValidateDoubleEntry())
	assert.Equal(t, int64(1500), reqValid.TotalAmount())

	// 4. Unbalanced entries
	reqUnbalanced := CreateTransferRequest{
		Entries: []TransferEntryRequest{
			{AccountID: id1, Direction: DirectionDebit, Amount: 1000},
			{AccountID: id2, Direction: DirectionCredit, Amount: 999},
		},
	}
	assert.ErrorIs(t, reqUnbalanced.ValidateDoubleEntry(), ErrUnbalancedEntries)
}
