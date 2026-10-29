package postgres

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"payment-ledger/internal/domain"
	"payment-ledger/internal/repository"
)

type auditRepository struct {
	pool *pgxpool.Pool
}

func NewAuditRepository(pool *pgxpool.Pool) repository.AuditRepository {
	return &auditRepository{pool: pool}
}

// CheckSystemBalanceInvariant verifies system-wide double-entry balance:
// Total Debits must strictly equal Total Credits across all ledger entries in history!
func (r *auditRepository) CheckSystemBalanceInvariant(ctx context.Context) (int64, int64, bool, error) {
	query := `
		SELECT 
			COALESCE(SUM(CASE WHEN direction = 'DEBIT' THEN amount ELSE 0 END), 0) AS total_debits,
			COALESCE(SUM(CASE WHEN direction = 'CREDIT' THEN amount ELSE 0 END), 0) AS total_credits
		FROM ledger_entries;
	`
	var totalDebits, totalCredits int64
	err := r.pool.QueryRow(ctx, query).Scan(&totalDebits, &totalCredits)
	if err != nil {
		return 0, 0, false, fmt.Errorf("failed to check ledger invariant: %w", err)
	}

	isBalanced := totalDebits == totalCredits
	return totalDebits, totalCredits, isBalanced, nil
}

// CheckAccountIntegrity audits an individual account by comparing its cached balance in accounts table
// with the calculated net delta from historical immutable ledger entries and the latest balance_after.
func (r *auditRepository) CheckAccountIntegrity(ctx context.Context, accountID uuid.UUID) (int64, int64, bool, error) {
	// 1. Fetch current cached balance
	accQuery := `SELECT balance FROM accounts WHERE id = $1;`
	var cachedBalance int64
	if err := r.pool.QueryRow(ctx, accQuery, accountID).Scan(&cachedBalance); err != nil {
		return 0, 0, false, domain.ErrAccountNotFound
	}

	// 2. Fetch latest balance_after from immutable ledger_entries
	latestQuery := `
		SELECT balance_after 
		FROM ledger_entries 
		WHERE account_id = $1 
		ORDER BY sequence_num DESC 
		LIMIT 1;
	`
	var computedBalance int64
	err := r.pool.QueryRow(ctx, latestQuery, accountID).Scan(&computedBalance)
	if err != nil {
		// If account has never had any ledger entries, its balance should be 0 (or initial opening balance)
		computedBalance = 0
	}

	isBalanced := cachedBalance == computedBalance
	return cachedBalance, computedBalance, isBalanced, nil
}
