package postgres

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"payment-ledger/internal/domain"
	"payment-ledger/internal/repository"
)

type entryRepository struct {
	pool *pgxpool.Pool
}

func NewEntryRepository(pool *pgxpool.Pool) repository.EntryRepository {
	return &entryRepository{pool: pool}
}

// CreateBatch persists ledger entries append-only in the transaction.
func (r *entryRepository) CreateBatch(ctx context.Context, tx pgx.Tx, entries []domain.LedgerEntry) error {
	query := `
		INSERT INTO ledger_entries (id, transfer_id, account_id, direction, amount, balance_after, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING sequence_num;
	`
	for i := range entries {
		if entries[i].ID == uuid.Nil {
			entries[i].ID = uuid.New()
		}
		err := tx.QueryRow(ctx, query,
			entries[i].ID, entries[i].TransferID, entries[i].AccountID,
			string(entries[i].Direction), entries[i].Amount, entries[i].BalanceAfter,
			entries[i].CreatedAt,
		).Scan(&entries[i].SequenceNum)
		if err != nil {
			return fmt.Errorf("failed to insert ledger entry: %w", err)
		}
	}
	return nil
}

func (r *entryRepository) ListByAccount(ctx context.Context, accountID uuid.UUID, limit, offset int) ([]domain.LedgerEntry, error) {
	if limit <= 0 {
		limit = 50
	}
	query := `
		SELECT id, transfer_id, account_id, direction, amount, balance_after, sequence_num, created_at
		FROM ledger_entries
		WHERE account_id = $1
		ORDER BY created_at DESC, sequence_num DESC
		LIMIT $2 OFFSET $3;
	`
	rows, err := r.pool.Query(ctx, query, accountID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to list entries for account: %w", err)
	}
	defer rows.Close()

	var entries []domain.LedgerEntry
	for rows.Next() {
		var e domain.LedgerEntry
		var dir string
		err := rows.Scan(
			&e.ID, &e.TransferID, &e.AccountID, &dir,
			&e.Amount, &e.BalanceAfter, &e.SequenceNum, &e.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan ledger entry: %w", err)
		}
		e.Direction = domain.Direction(dir)
		entries = append(entries, e)
	}
	return entries, nil
}

func (r *entryRepository) ListByTransfer(ctx context.Context, transferID uuid.UUID) ([]domain.LedgerEntry, error) {
	query := `
		SELECT id, transfer_id, account_id, direction, amount, balance_after, sequence_num, created_at
		FROM ledger_entries
		WHERE transfer_id = $1
		ORDER BY sequence_num ASC;
	`
	rows, err := r.pool.Query(ctx, query, transferID)
	if err != nil {
		return nil, fmt.Errorf("failed to list entries for transfer: %w", err)
	}
	defer rows.Close()

	var entries []domain.LedgerEntry
	for rows.Next() {
		var e domain.LedgerEntry
		var dir string
		err := rows.Scan(
			&e.ID, &e.TransferID, &e.AccountID, &dir,
			&e.Amount, &e.BalanceAfter, &e.SequenceNum, &e.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan ledger entry: %w", err)
		}
		e.Direction = domain.Direction(dir)
		entries = append(entries, e)
	}
	return entries, nil
}
