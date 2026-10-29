package tests

import (
	"context"
	"fmt"
	"testing"

	"payment-ledger/internal/domain"
	"payment-ledger/internal/service"
)

func BenchmarkExecuteTransfer(b *testing.B) {
	svc, _, _ := setupTestService()
	ctx := context.Background()

	accA, _ := svc.CreateAccount(ctx, service.CreateAccountRequest{
		AccountNumber:  "BENCH-A",
		HolderName:     "Bench Alice",
		Currency:       "USD",
		InitialBalance: 100000000,
		AllowOverdraft: true,
	})

	accB, _ := svc.CreateAccount(ctx, service.CreateAccountRequest{
		AccountNumber:  "BENCH-B",
		HolderName:     "Bench Bob",
		Currency:       "USD",
		InitialBalance: 100000000,
		AllowOverdraft: true,
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := domain.CreateTransferRequest{
			IdempotencyKey: fmt.Sprintf("bench-key-%d", i),
			FromAccountID:  &accA.ID,
			ToAccountID:    &accB.ID,
			Amount:         100,
			Currency:       "USD",
			Description:    "Benchmark transfer",
		}
		_, _ = svc.ExecuteTransfer(ctx, req)
	}
}
