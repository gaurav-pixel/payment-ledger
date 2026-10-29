package service

import (
	"context"

	"github.com/google/uuid"

	"payment-ledger/internal/repository"
)

type SystemAuditReport struct {
	TotalDebits  int64 `json:"total_debits"`
	TotalCredits int64 `json:"total_credits"`
	Balanced     bool  `json:"is_balanced"`
}

type AccountAuditReport struct {
	AccountID       uuid.UUID `json:"account_id"`
	CachedBalance   int64     `json:"cached_balance"`
	ComputedBalance int64     `json:"computed_balance"`
	Balanced        bool      `json:"is_balanced"`
}

type AuditService interface {
	AuditSystem(ctx context.Context) (*SystemAuditReport, error)
	AuditAccount(ctx context.Context, accountID uuid.UUID) (*AccountAuditReport, error)
}

type auditService struct {
	auditRepo repository.AuditRepository
}

func NewAuditService(auditRepo repository.AuditRepository) AuditService {
	return &auditService{auditRepo: auditRepo}
}

func (s *auditService) AuditSystem(ctx context.Context) (*SystemAuditReport, error) {
	totalDebits, totalCredits, balanced, err := s.auditRepo.CheckSystemBalanceInvariant(ctx)
	if err != nil {
		return nil, err
	}
	return &SystemAuditReport{
		TotalDebits:  totalDebits,
		TotalCredits: totalCredits,
		Balanced:     balanced,
	}, nil
}

func (s *auditService) AuditAccount(ctx context.Context, accountID uuid.UUID) (*AccountAuditReport, error) {
	cached, computed, balanced, err := s.auditRepo.CheckAccountIntegrity(ctx, accountID)
	if err != nil {
		return nil, err
	}
	return &AccountAuditReport{
		AccountID:       accountID,
		CachedBalance:   cached,
		ComputedBalance: computed,
		Balanced:        balanced,
	}, nil
}
