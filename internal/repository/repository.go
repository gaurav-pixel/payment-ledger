package repository

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"payment-ledger/internal/domain"
)

type AccountRepository interface {
	Create(ctx context.Context, account *domain.Account) error
	GetByID(ctx context.Context, id uuid.UUID) (*domain.Account, error)
	GetByAccountNumber(ctx context.Context, accountNumber string) (*domain.Account, error)
	GetForUpdate(ctx context.Context, tx pgx.Tx, ids []uuid.UUID) (map[uuid.UUID]*domain.Account, error)
	UpdateBalance(ctx context.Context, tx pgx.Tx, accountID uuid.UUID, newBalance int64) error
	List(ctx context.Context, limit, offset int) ([]*domain.Account, error)
}

type TransferRepository interface {
	Create(ctx context.Context, tx pgx.Tx, transfer *domain.Transfer) error
	GetByID(ctx context.Context, id uuid.UUID) (*domain.Transfer, error)
	List(ctx context.Context, limit, offset int) ([]*domain.Transfer, error)
}

type EntryRepository interface {
	CreateBatch(ctx context.Context, tx pgx.Tx, entries []domain.LedgerEntry) error
	ListByAccount(ctx context.Context, accountID uuid.UUID, limit, offset int) ([]domain.LedgerEntry, error)
	ListByTransfer(ctx context.Context, transferID uuid.UUID) ([]domain.LedgerEntry, error)
}

type IdempotencyRepository interface {
	Get(ctx context.Context, key string) (*domain.IdempotencyRecord, error)
	CreateProcessing(ctx context.Context, record *domain.IdempotencyRecord) error
	UpdateCompleted(ctx context.Context, key string, transferID *uuid.UUID, statusCode int, responseBody string) error
	UpdateFailed(ctx context.Context, key string, statusCode int, responseBody string) error
}

type AuditRepository interface {
	CheckSystemBalanceInvariant(ctx context.Context) (totalDebits int64, totalCredits int64, balanced bool, err error)
	CheckAccountIntegrity(ctx context.Context, accountID uuid.UUID) (cachedBalance int64, computedBalance int64, balanced bool, err error)
}

type TxManager interface {
	ExecuteInTx(ctx context.Context, fn func(tx pgx.Tx) error) error
}
