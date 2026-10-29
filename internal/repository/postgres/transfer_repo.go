package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"payment-ledger/internal/domain"
	"payment-ledger/internal/repository"
)

type transferRepository struct {
	pool *pgxpool.Pool
}

func NewTransferRepository(pool *pgxpool.Pool) repository.TransferRepository {
	return &transferRepository{pool: pool}
}

func (r *transferRepository) Create(ctx context.Context, tx pgx.Tx, t *domain.Transfer) error {
	query := `
		INSERT INTO transfers (id, idempotency_key, description, status, currency, amount, metadata, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, created_at;
	`
	if t.ID == uuid.Nil {
		t.ID = uuid.New()
	}

	metadataJSON, err := json.Marshal(t.Metadata)
	if err != nil {
		metadataJSON = []byte("{}")
	}

	return tx.QueryRow(ctx, query,
		t.ID, t.IdempotencyKey, t.Description, string(t.Status),
		t.Currency, t.Amount, metadataJSON, t.CreatedAt,
	).Scan(&t.ID, &t.CreatedAt)
}

func (r *transferRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.Transfer, error) {
	query := `
		SELECT id, idempotency_key, description, status, currency, amount, metadata, created_at
		FROM transfers
		WHERE id = $1;
	`
	var t domain.Transfer
	var statusStr string
	var metaBytes []byte

	err := r.pool.QueryRow(ctx, query, id).Scan(
		&t.ID, &t.IdempotencyKey, &t.Description, &statusStr,
		&t.Currency, &t.Amount, &metaBytes, &t.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrInvalidTransfer
		}
		return nil, fmt.Errorf("failed to fetch transfer: %w", err)
	}

	t.Status = domain.TransferStatus(statusStr)
	if len(metaBytes) > 0 {
		_ = json.Unmarshal(metaBytes, &t.Metadata)
	}

	return &t, nil
}

func (r *transferRepository) List(ctx context.Context, limit, offset int) ([]*domain.Transfer, error) {
	if limit <= 0 {
		limit = 50
	}
	query := `
		SELECT id, idempotency_key, description, status, currency, amount, metadata, created_at
		FROM transfers
		ORDER BY created_at DESC
		LIMIT $1 OFFSET $2;
	`
	rows, err := r.pool.Query(ctx, query, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to list transfers: %w", err)
	}
	defer rows.Close()

	var transfers []*domain.Transfer
	for rows.Next() {
		var t domain.Transfer
		var statusStr string
		var metaBytes []byte

		err := rows.Scan(
			&t.ID, &t.IdempotencyKey, &t.Description, &statusStr,
			&t.Currency, &t.Amount, &metaBytes, &t.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan transfer: %w", err)
		}

		t.Status = domain.TransferStatus(statusStr)
		if len(metaBytes) > 0 {
			_ = json.Unmarshal(metaBytes, &t.Metadata)
		}
		transfers = append(transfers, &t)
	}

	return transfers, nil
}
