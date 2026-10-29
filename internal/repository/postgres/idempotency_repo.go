package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"payment-ledger/internal/domain"
	"payment-ledger/internal/repository"
)

type idempotencyRepository struct {
	pool *pgxpool.Pool
}

func NewIdempotencyRepository(pool *pgxpool.Pool) repository.IdempotencyRepository {
	return &idempotencyRepository{pool: pool}
}

func (r *idempotencyRepository) Get(ctx context.Context, key string) (*domain.IdempotencyRecord, error) {
	query := `
		SELECT key, request_hash, status, transfer_id, status_code, response_body, created_at, expires_at
		FROM idempotency_keys
		WHERE key = $1 AND expires_at > NOW();
	`
	var rec domain.IdempotencyRecord
	var statusStr string

	err := r.pool.QueryRow(ctx, query, key).Scan(
		&rec.Key, &rec.RequestHash, &statusStr, &rec.TransferID,
		&rec.StatusCode, &rec.ResponseBody, &rec.CreatedAt, &rec.ExpiresAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil // No valid existing idempotency record found
		}
		return nil, fmt.Errorf("failed to fetch idempotency key: %w", err)
	}

	rec.Status = domain.IdempotencyStatus(statusStr)
	return &rec, nil
}

func (r *idempotencyRepository) CreateProcessing(ctx context.Context, record *domain.IdempotencyRecord) error {
	query := `
		INSERT INTO idempotency_keys (key, request_hash, status, transfer_id, status_code, response_body, created_at, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (key) DO NOTHING;
	`
	if record.CreatedAt.IsZero() {
		record.CreatedAt = time.Now()
	}
	if record.ExpiresAt.IsZero() {
		record.ExpiresAt = record.CreatedAt.Add(24 * time.Hour)
	}

	cmdTag, err := r.pool.Exec(ctx, query,
		record.Key, record.RequestHash, string(record.Status),
		record.TransferID, record.StatusCode, record.ResponseBody,
		record.CreatedAt, record.ExpiresAt,
	)
	if err != nil {
		return fmt.Errorf("failed to register idempotency key: %w", err)
	}

	if cmdTag.RowsAffected() == 0 {
		return domain.ErrDuplicateIdempotencyKey
	}
	return nil
}

func (r *idempotencyRepository) UpdateCompleted(ctx context.Context, key string, transferID *uuid.UUID, statusCode int, responseBody string) error {
	query := `
		UPDATE idempotency_keys
		SET status = $1, transfer_id = $2, status_code = $3, response_body = $4
		WHERE key = $5;
	`
	_, err := r.pool.Exec(ctx, query,
		string(domain.IdempotencyStatusCompleted), transferID, statusCode, responseBody, key,
	)
	if err != nil {
		return fmt.Errorf("failed to update completed idempotency key: %w", err)
	}
	return nil
}

func (r *idempotencyRepository) UpdateFailed(ctx context.Context, key string, statusCode int, responseBody string) error {
	query := `
		UPDATE idempotency_keys
		SET status = $1, status_code = $2, response_body = $3
		WHERE key = $4;
	`
	_, err := r.pool.Exec(ctx, query,
		string(domain.IdempotencyStatusFailed), statusCode, responseBody, key,
	)
	if err != nil {
		return fmt.Errorf("failed to update failed idempotency key: %w", err)
	}
	return nil
}
