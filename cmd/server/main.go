package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"payment-ledger/internal/config"
	"payment-ledger/internal/handler"
	"payment-ledger/internal/middleware"
	"payment-ledger/internal/repository"
	"payment-ledger/internal/repository/mock"
	"payment-ledger/internal/repository/postgres"
	"payment-ledger/internal/service"
	"payment-ledger/pkg/db"
)

func main() {
	log.Println("Starting Payment Ledger Service...")

	cfg := config.LoadConfig()

	dbConfig := db.Config{
		Host:            cfg.DBHost,
		Port:            cfg.DBPort,
		User:            cfg.DBUser,
		Password:        cfg.DBPassword,
		Database:        cfg.DBName,
		SSLMode:         cfg.DBSSLMode,
		MaxConns:        cfg.DBMaxConns,
		MinConns:        cfg.DBMinConns,
		MaxConnLifetime: time.Hour,
		MaxConnIdleTime: 30 * time.Minute,
	}

	var (
		accountRepo     repository.AccountRepository
		transferRepo    repository.TransferRepository
		entryRepo       repository.EntryRepository
		idempotencyRepo repository.IdempotencyRepository
		auditRepo       repository.AuditRepository
		txManager       repository.TxManager
	)

	useInMemory := os.Getenv("DB_DRIVER") == "inmemory" || cfg.DBHost == "inmemory"

	if !useInMemory {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		pool, err := db.NewPool(ctx, dbConfig)
		if err != nil {
			log.Printf("[WARN] PostgreSQL connection not reachable on %s:%d (%v). Falling back to in-memory transactional ledger engine for local preview.", cfg.DBHost, cfg.DBPort, err)
			useInMemory = true
		} else {
			defer pool.Close()
			log.Println("PostgreSQL connection pool established successfully.")

			// Run database migrations automatically
			if err := db.RunMigrations(context.Background(), pool); err != nil {
				log.Fatalf("Fatal: Database migration failed: %v", err)
			}

			txManager = db.NewTxManager(pool)
			accountRepo = postgres.NewAccountRepository(pool)
			transferRepo = postgres.NewTransferRepository(pool)
			entryRepo = postgres.NewEntryRepository(pool)
			idempotencyRepo = postgres.NewIdempotencyRepository(pool)
			auditRepo = postgres.NewAuditRepository(pool)
		}
	}

	if useInMemory {
		log.Println("[INFO] Operating in Transactional In-Memory Mode with Concurrency Row-Locking & Consistent Lock Ordering.")
		memDB := mock.NewInMemoryLedgerDB()
		txManager = mock.NewMockTxManager(memDB)
		accountRepo = mock.NewMockAccountRepo(memDB)
		transferRepo = mock.NewMockTransferRepo(memDB)
		entryRepo = mock.NewMockEntryRepo(memDB)
		idempotencyRepo = mock.NewMockIdempotencyRepo(memDB)
		auditRepo = mock.NewMockAuditRepo(memDB)
	}

	// Services
	ledgerService := service.NewLedgerService(
		accountRepo,
		transferRepo,
		entryRepo,
		idempotencyRepo,
		txManager,
	)
	auditService := service.NewAuditService(auditRepo)

	// Handlers
	accountHandler := handler.NewAccountHandler(ledgerService)
	transferHandler := handler.NewTransferHandler(ledgerService)
	auditHandler := handler.NewAuditHandler(auditService)

	mux := http.NewServeMux()

	// Health check
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		handler.JSONResponse(w, http.StatusOK, map[string]string{
			"status":    "healthy",
			"timestamp": time.Now().Format(time.RFC3339),
			"service":   "payment-ledger-service",
		})
	})

	// Account routes
	mux.HandleFunc("POST /api/v1/accounts", accountHandler.CreateAccount)
	mux.HandleFunc("GET /api/v1/accounts", accountHandler.ListAccounts)
	mux.HandleFunc("GET /api/v1/accounts/{id}", accountHandler.GetAccount)
	mux.HandleFunc("GET /api/v1/accounts/{id}/statement", accountHandler.GetAccountStatement)

	// Transfer routes
	mux.HandleFunc("POST /api/v1/transfers", transferHandler.CreateTransfer)
	mux.HandleFunc("GET /api/v1/transfers", transferHandler.ListTransfers)
	mux.HandleFunc("GET /api/v1/transfers/{id}", transferHandler.GetTransfer)

	// Audit routes
	mux.HandleFunc("GET /api/v1/audit/system", auditHandler.AuditSystem)
	mux.HandleFunc("GET /api/v1/audit/accounts/{id}", auditHandler.AuditAccount)

	// Wrap middleware chain
	var rootHandler http.Handler = mux
	rootHandler = middleware.CORSMiddleware(rootHandler)
	rootHandler = middleware.LoggingMiddleware(rootHandler)
	rootHandler = middleware.RecoveryMiddleware(rootHandler)

	server := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      rootHandler,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Server shutdown channel
	stopChan := make(chan os.Signal, 1)
	signal.Notify(stopChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		log.Printf("Payment Ledger Service is listening on port %s", cfg.Port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP server error: %v", err)
		}
	}()

	<-stopChan
	log.Println("Shutting down Payment Ledger Service gracefully...")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	log.Println("Payment Ledger Service gracefully stopped.")
}
