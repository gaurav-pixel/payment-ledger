package postgres

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"payment-ledger/internal/domain"
	"payment-ledger/internal/repository"
)

type accountRepository struct {
	pool *pgxpool.Pool
}

func NewAccountRepository(pool *pgxpool.Pool) repository.AccountRepository {
	return &accountRepository{pool: pool}
}

func (r *accountRepository) Create(ctx context.Context, a *domain.Account) error {
	query := `
		INSERT INTO accounts (id, account_number, holder_name, type, currency, balance, allow_overdraft, overdraft_limit, status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING id, created_at, updated_at;
	`
	if a.ID == uuid.Nil {
		a.ID = uuid.New()
	}

	return r.pool.QueryRow(ctx, query,
		a.ID, a.AccountNumber, a.HolderName, string(a.Type), a.Currency,
		a.Balance, a.AllowOverdraft, a.OverdraftLimit, string(a.Status),
		a.CreatedAt, a.UpdatedAt,
	).Scan(&a.ID, &a.CreatedAt, &a.UpdatedAt)
}

func (r *accountRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.Account, error) {
	query := `
		SELECT id, account_number, holder_name, type, currency, balance, allow_overdraft, overdraft_limit, status, created_at, updated_at
		FROM accounts
		WHERE id = $1;
	`
	var a domain.Account
	var accType, accStatus string

	err := r.pool.QueryRow(ctx, query, id).Scan(
		&a.ID, &a.AccountNumber, &a.HolderName, &accType, &a.Currency,
		&a.Balance, &a.AllowOverdraft, &a.OverdraftLimit, &accStatus,
		&a.CreatedAt, &a.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrAccountNotFound
		}
		return nil, fmt.Errorf("failed to fetch account by id: %w", err)
	}

	a.Type = domain.AccountType(accType)
	a.Status = domain.AccountStatus(accStatus)
	return &a, nil
}

func (r *accountRepository) GetByAccountNumber(ctx context.Context, accNum string) (*domain.Account, error) {
	query := `
		SELECT id, account_number, holder_name, type, currency, balance, allow_overdraft, overdraft_limit, status, created_at, updated_at
		FROM accounts
		WHERE account_number = $1;
	`
	var a domain.Account
	var accType, accStatus string

	err := r.pool.QueryRow(ctx, query, accNum).Scan(
		&a.ID, &a.AccountNumber, &a.HolderName, &accType, &a.Currency,
		&a.Balance, &a.AllowOverdraft, &a.OverdraftLimit, &accStatus,
		&a.CreatedAt, &a.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrAccountNotFound
		}
		return nil, fmt.Errorf("failed to fetch account by account_number: %w", err)
	}

	a.Type = domain.AccountType(accType)
	a.Status = domain.AccountStatus(accStatus)
	return &a, nil
}

// GetForUpdate locks the accounts in strict deterministic order (sorted ascending by UUID).
// Consistent Lock Ordering prevents circular deadlock wait conditions between concurrent transactions.
func (r *accountRepository) GetForUpdate(ctx context.Context, tx pgx.Tx, ids []uuid.UUID) (map[uuid.UUID]*domain.Account, error) {
	if len(ids) == 0 {
		return make(map[uuid.UUID]*domain.Account), nil
	}

	// Deduplicate IDs
	uniqueMap := make(map[uuid.UUID]bool)
	var sortedIDs []uuid.UUID
	for _, id := range ids {
		if !uniqueMap[id] {
			uniqueMap[id] = true
			sortedIDs = append(sortedIDs, id)
		}
	}

	// CRITICAL CONCURRENCY INVARIANT: Sort IDs deterministically to enforce consistent lock ordering!
	sort.Slice(sortedIDs, func(i, j int) bool {
		return sortedIDs[i].String() < sortedIDs[j].String()
	})

	query := `
		SELECT id, account_number, holder_name, type, currency, balance, allow_overdraft, overdraft_limit, status, created_at, updated_at
		FROM accounts
		WHERE id = ANY($1)
		ORDER BY id ASC
		FOR UPDATE;
	`

	rows, err := tx.Query(ctx, query, sortedIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to acquire row locks on accounts: %w", err)
	}
	defer rows.Close()

	result := make(map[uuid.UUID]*domain.Account)
	for rows.Next() {
		var a domain.Account
		var accType, accStatus string

		err := rows.Scan(
			&a.ID, &a.AccountNumber, &a.HolderName, &accType, &a.Currency,
			&a.Balance, &a.AllowOverdraft, &a.OverdraftLimit, &accStatus,
			&a.CreatedAt, &a.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan locked account row: %w", err)
		}

		a.Type = domain.AccountType(accType)
		a.Status = domain.AccountStatus(accStatus)
		result[a.ID] = &a
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating locked accounts: %w", err)
	}

	return result, nil
}

func (r *accountRepository) UpdateBalance(ctx context.Context, tx pgx.Tx, accountID uuid.UUID, newBalance int64) error {
	query := `
		UPDATE accounts
		SET balance = $1, updated_at = NOW()
		WHERE id = $2;
	`
	cmdTag, err := tx.Exec(ctx, query, newBalance, accountID)
	if err != nil {
		return fmt.Errorf("failed to update account balance: %w", err)
	}
	if cmdTag.RowsAffected() == 0 {
		return domain.ErrAccountNotFound
	}
	return nil
}

func (r *accountRepository) List(ctx context.Context, limit, offset int) ([]*domain.Account, error) {
	if limit <= 0 {
		limit = 50
	}
	query := `
		SELECT id, account_number, holder_name, type, currency, balance, allow_overdraft, overdraft_limit, status, created_at, updated_at
		FROM accounts
		ORDER BY created_at DESC
		LIMIT $1 OFFSET $2;
	`
	rows, err := r.pool.Query(ctx, query, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to list accounts: %w", err)
	}
	defer rows.Close()

	var accounts []*domain.Account
	for rows.Next() {
		var a domain.Account
		var accType, accStatus string

		err := rows.Scan(
			&a.ID, &a.AccountNumber, &a.HolderName, &accType, &a.Currency,
			&a.Balance, &a.AllowOverdraft, &a.OverdraftLimit, &accStatus,
			&a.CreatedAt, &a.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan account: %w", err)
		}

		a.Type = domain.AccountType(accType)
		a.Status = domain.AccountStatus(accStatus)
		accounts = append(accounts, &a)
	}

	return accounts, nil
}
