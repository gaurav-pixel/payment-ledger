package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/google/uuid"

	"payment-ledger/internal/config"
	"payment-ledger/internal/repository/postgres"
	"payment-ledger/internal/service"
	"payment-ledger/pkg/db"
)

func main() {
	log.Println("--- Payment Ledger Invariant Audit Tool ---")

	cfg := config.LoadConfig()
	dbConfig := db.Config{
		Host:     cfg.DBHost,
		Port:     cfg.DBPort,
		User:     cfg.DBUser,
		Password: cfg.DBPassword,
		Database: cfg.DBName,
		SSLMode:  cfg.DBSSLMode,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	pool, err := db.NewPool(ctx, dbConfig)
	if err != nil {
		log.Fatalf("Database connection failure: %v", err)
	}
	defer pool.Close()

	auditRepo := postgres.NewAuditRepository(pool)
	auditSvc := service.NewAuditService(auditRepo)

	// Check if a specific account UUID is supplied as CLI argument
	if len(os.Args) > 1 {
		accountIDStr := os.Args[1]
		accountID, err := uuid.Parse(accountIDStr)
		if err != nil {
			log.Fatalf("Invalid UUID argument: %s", accountIDStr)
		}

		accReport, err := auditSvc.AuditAccount(ctx, accountID)
		if err != nil {
			log.Fatalf("Account audit error: %v", err)
		}

		fmt.Printf("Account Audit for %s:\n", accReport.AccountID)
		fmt.Printf(" - Cached Balance:   %d\n", accReport.CachedBalance)
		fmt.Printf(" - Computed Balance: %d\n", accReport.ComputedBalance)
		fmt.Printf(" - Invariant Holds:  %t\n", accReport.Balanced)
		if !accReport.Balanced {
			os.Exit(1)
		}
		return
	}

	// System-wide ledger audit
	sysReport, err := auditSvc.AuditSystem(ctx)
	if err != nil {
		log.Fatalf("System audit error: %v", err)
	}

	fmt.Println("System Ledger Audit:")
	fmt.Printf(" - Total Debits:  %d\n", sysReport.TotalDebits)
	fmt.Printf(" - Total Credits: %d\n", sysReport.TotalCredits)
	fmt.Printf(" - Invariant Holds (Sum Debits == Sum Credits): %t\n", sysReport.Balanced)

	if !sysReport.Balanced {
		fmt.Println("CRITICAL ERROR: Ledger invariant violation detected!")
		os.Exit(1)
	}

	fmt.Println("Audit passed successfully: Ledger invariants intact.")
}
