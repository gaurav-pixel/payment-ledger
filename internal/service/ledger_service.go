package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"payment-ledger/internal/domain"
	"payment-ledger/internal/repository"
)

type CreateAccountRequest struct {
	AccountNumber  string              `json:"account_number"`
	HolderName     string              `json:"holder_name"`
	Type           domain.AccountType   `json:"type"`
	Currency       string              `json:"currency"`
	InitialBalance int64               `json:"initial_balance"`
	AllowOverdraft bool                `json:"allow_overdraft"`
	OverdraftLimit int64               `json:"overdraft_limit"`
}

type TransferResult struct {
	Transfer *domain.Transfer `json:"transfer"`
	Cached   bool             `json:"cached,omitempty"`
}

type LedgerService interface {
	CreateAccount(ctx context.Context, req CreateAccountRequest) (*domain.Account, error)
	GetAccount(ctx context.Context, id uuid.UUID) (*domain.Account, error)
	GetAccountByNumber(ctx context.Context, accNum string) (*domain.Account, error)
	ListAccounts(ctx context.Context, limit, offset int) ([]*domain.Account, error)

	ExecuteTransfer(ctx context.Context, req domain.CreateTransferRequest) (*TransferResult, error)
	GetTransfer(ctx context.Context, id uuid.UUID) (*domain.Transfer, error)
	ListTransfers(ctx context.Context, limit, offset int) ([]*domain.Transfer, error)

	GetAccountStatement(ctx context.Context, accountID uuid.UUID, limit, offset int) ([]domain.LedgerEntry, error)
}

type ledgerService struct {
	accounts      repository.AccountRepository
	transfers     repository.TransferRepository
	entries       repository.EntryRepository
	idempotency   repository.IdempotencyRepository
	txManager     repository.TxManager
}

func NewLedgerService(
	accounts repository.AccountRepository,
	transfers repository.TransferRepository,
	entries repository.EntryRepository,
	idempotency repository.IdempotencyRepository,
	txManager repository.TxManager,
) LedgerService {
	return &ledgerService{
		accounts:    accounts,
		transfers:   transfers,
		entries:     entries,
		idempotency: idempotency,
		txManager:   txManager,
	}
}

func (s *ledgerService) CreateAccount(ctx context.Context, req CreateAccountRequest) (*domain.Account, error) {
	if req.AccountNumber == "" || req.HolderName == "" || req.Currency == "" {
		return nil, domain.ErrInvalidTransfer
	}
	if req.Type == "" {
		req.Type = domain.AccountTypeCustomer
	}

	acc := &domain.Account{
		ID:             uuid.New(),
		AccountNumber:  req.AccountNumber,
		HolderName:     req.HolderName,
		Type:           req.Type,
		Currency:       req.Currency,
		Balance:        req.InitialBalance,
		AllowOverdraft: req.AllowOverdraft,
		OverdraftLimit: req.OverdraftLimit,
		Status:         domain.AccountStatusActive,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	if err := s.accounts.Create(ctx, acc); err != nil {
		return nil, fmt.Errorf("could not create account: %w", err)
	}

	return acc, nil
}

func (s *ledgerService) GetAccount(ctx context.Context, id uuid.UUID) (*domain.Account, error) {
	return s.accounts.GetByID(ctx, id)
}

func (s *ledgerService) GetAccountByNumber(ctx context.Context, accNum string) (*domain.Account, error) {
	return s.accounts.GetByAccountNumber(ctx, accNum)
}

func (s *ledgerService) ListAccounts(ctx context.Context, limit, offset int) ([]*domain.Account, error) {
	return s.accounts.List(ctx, limit, offset)
}

func (s *ledgerService) GetTransfer(ctx context.Context, id uuid.UUID) (*domain.Transfer, error) {
	transfer, err := s.transfers.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	entries, err := s.entries.ListByTransfer(ctx, id)
	if err == nil {
		transfer.Entries = entries
	}
	return transfer, nil
}

func (s *ledgerService) ListTransfers(ctx context.Context, limit, offset int) ([]*domain.Transfer, error) {
	return s.transfers.List(ctx, limit, offset)
}

func (s *ledgerService) GetAccountStatement(ctx context.Context, accountID uuid.UUID, limit, offset int) ([]domain.LedgerEntry, error) {
	return s.entries.ListByAccount(ctx, accountID, limit, offset)
}

// ExecuteTransfer processes transfers atomically with consistent lock ordering,
// row-level locking, double-entry verification, overdraft policy, and idempotency key caching.
func (s *ledgerService) ExecuteTransfer(ctx context.Context, req domain.CreateTransferRequest) (*TransferResult, error) {
	// 1. Normalize entries for 2-legged convenience format
	if err := req.NormalizeEntries(); err != nil {
		return nil, err
	}

	// 2. Enforce Double-Entry Invariants: Sum(Debits) == Sum(Credits)
	if err := req.ValidateDoubleEntry(); err != nil {
		return nil, err
	}

	var reqHash string
	// 3. Handle Idempotency Key if provided
	if req.IdempotencyKey != "" {
		reqBytes, _ := json.Marshal(req)
		reqHash = domain.ComputePayloadHash(reqBytes)

		existing, err := s.idempotency.Get(ctx, req.IdempotencyKey)
		if err != nil {
			return nil, fmt.Errorf("error verifying idempotency key: %w", err)
		}
		if existing != nil {
			if existing.RequestHash != reqHash {
				return nil, domain.ErrIdempotencyConflict
			}
			if existing.Status == domain.IdempotencyStatusProcessing {
				return nil, domain.ErrIdempotencyInProgress
			}
			if existing.Status == domain.IdempotencyStatusCompleted {
				var cachedTransfer domain.Transfer
				if err := json.Unmarshal([]byte(existing.ResponseBody), &cachedTransfer); err == nil {
					return &TransferResult{
						Transfer: &cachedTransfer,
						Cached:   true,
					}, nil
				}
			}
		}

		// Register key as PROCESSING
		processingRec := &domain.IdempotencyRecord{
			Key:          req.IdempotencyKey,
			RequestHash:  reqHash,
			Status:       domain.IdempotencyStatusProcessing,
			StatusCode:   200,
			ResponseBody: "",
			CreatedAt:    time.Now(),
			ExpiresAt:    time.Now().Add(24 * time.Hour),
		}
		if err := s.idempotency.CreateProcessing(ctx, processingRec); err != nil {
			if err == domain.ErrDuplicateIdempotencyKey {
				return nil, domain.ErrIdempotencyInProgress
			}
			return nil, fmt.Errorf("failed to register idempotency key: %w", err)
		}
	}

	// 4. Execute atomic transfer within a PostgreSQL transaction
	transfer := &domain.Transfer{
		ID:          uuid.New(),
		Description: req.Description,
		Status:      domain.TransferStatusCommitted,
		Currency:    req.Currency,
		Amount:      req.TotalAmount(),
		Metadata:    req.Metadata,
		CreatedAt:   time.Now(),
	}
	if req.IdempotencyKey != "" {
		transfer.IdempotencyKey = &req.IdempotencyKey
	}

	var createdEntries []domain.LedgerEntry

	txErr := s.txManager.ExecuteInTx(ctx, func(tx pgx.Tx) error {
		// a. Collect all involved account IDs
		var accountIDs []uuid.UUID
		for _, e := range req.Entries {
			accountIDs = append(accountIDs, e.AccountID)
		}

		// b. Acquire row-level locks with CONSISTENT LOCK ORDERING
		lockedAccounts, err := s.accounts.GetForUpdate(ctx, tx, accountIDs)
		if err != nil {
			return fmt.Errorf("row-level locking failed: %w", err)
		}

		// Verify all requested accounts exist
		for _, accID := range accountIDs {
			if _, exists := lockedAccounts[accID]; !exists {
				return domain.ErrAccountNotFound
			}
		}

		// c. Validate currencies & status
		for _, acc := range lockedAccounts {
			if err := acc.ValidateStatus(); err != nil {
				return err
			}
			if acc.Currency != req.Currency {
				return domain.ErrCurrencyMismatch
			}
		}

		// d. Validate and apply debit/credit adjustments with overdraft policy
		now := time.Now()
		for _, leg := range req.Entries {
			acc := lockedAccounts[leg.AccountID]
			var balanceAfter int64

			switch leg.Direction {
			case domain.DirectionDebit:
				newBal, err := acc.ApplyDebit(leg.Amount)
				if err != nil {
					return err // Insufficient funds or invalid status triggers ROLLBACK
				}
				balanceAfter = newBal

			case domain.DirectionCredit:
				newBal, err := acc.ApplyCredit(leg.Amount)
				if err != nil {
					return err
				}
				balanceAfter = newBal
			}

			// Prepare immutable ledger entry
			entry := domain.LedgerEntry{
				ID:           uuid.New(),
				TransferID:   transfer.ID,
				AccountID:    acc.ID,
				Direction:    leg.Direction,
				Amount:       leg.Amount,
				BalanceAfter: balanceAfter,
				CreatedAt:    now,
			}
			createdEntries = append(createdEntries, entry)
		}

		// e. Persist updated balances for locked accounts
		for _, acc := range lockedAccounts {
			if err := s.accounts.UpdateBalance(ctx, tx, acc.ID, acc.Balance); err != nil {
				return fmt.Errorf("failed to update balance for %s: %w", acc.ID, err)
			}
		}

		// f. Insert transfer record
		if err := s.transfers.Create(ctx, tx, transfer); err != nil {
			return fmt.Errorf("failed to insert transfer record: %w", err)
		}

		// g. Insert immutable ledger entries
		if err := s.entries.CreateBatch(ctx, tx, createdEntries); err != nil {
			return fmt.Errorf("failed to insert ledger entries: %w", err)
		}

		return nil
	})

	if txErr != nil {
		log.Printf("[Transfer Failed] %v - Request: %+v", txErr, req)
		if req.IdempotencyKey != "" {
			_ = s.idempotency.UpdateFailed(ctx, req.IdempotencyKey, 400, txErr.Error())
		}
		return nil, txErr
	}

	transfer.Entries = createdEntries

	// 5. If idempotency key was supplied, save completed result for instant replay
	if req.IdempotencyKey != "" {
		respBytes, _ := json.Marshal(transfer)
		_ = s.idempotency.UpdateCompleted(ctx, req.IdempotencyKey, &transfer.ID, 201, string(respBytes))
	}

	return &TransferResult{
		Transfer: transfer,
		Cached:   false,
	}, nil
}
