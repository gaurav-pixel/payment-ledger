package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"payment-ledger/internal/domain"
	"payment-ledger/internal/repository/mock"
	"payment-ledger/internal/service"
)

func setupTestRouter() http.Handler {
	db := mock.NewInMemoryLedgerDB()
	accRepo := mock.NewMockAccountRepo(db)
	txRepo := mock.NewMockTransferRepo(db)
	entryRepo := mock.NewMockEntryRepo(db)
	idempRepo := mock.NewMockIdempotencyRepo(db)
	auditRepo := mock.NewMockAuditRepo(db)
	txMgr := mock.NewMockTxManager(db)

	ledgerSvc := service.NewLedgerService(accRepo, txRepo, entryRepo, idempRepo, txMgr)
	auditSvc := service.NewAuditService(auditRepo)

	accountHandler := NewAccountHandler(ledgerSvc)
	transferHandler := NewTransferHandler(ledgerSvc)
	auditHandler := NewAuditHandler(auditSvc)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		JSONResponse(w, http.StatusOK, map[string]string{"status": "healthy"})
	})
	mux.HandleFunc("POST /api/v1/accounts", accountHandler.CreateAccount)
	mux.HandleFunc("GET /api/v1/accounts", accountHandler.ListAccounts)
	mux.HandleFunc("GET /api/v1/accounts/{id}", accountHandler.GetAccount)
	mux.HandleFunc("GET /api/v1/accounts/{id}/statement", accountHandler.GetAccountStatement)

	mux.HandleFunc("POST /api/v1/transfers", transferHandler.CreateTransfer)
	mux.HandleFunc("GET /api/v1/transfers", transferHandler.ListTransfers)
	mux.HandleFunc("GET /api/v1/transfers/{id}", transferHandler.GetTransfer)

	mux.HandleFunc("GET /api/v1/audit/system", auditHandler.AuditSystem)
	mux.HandleFunc("GET /api/v1/audit/accounts/{id}", auditHandler.AuditAccount)

	return mux
}

func TestHealthCheckEndpoint(t *testing.T) {
	router := setupTestRouter()
	req := httptest.NewRequest("GET", "/healthz", nil)
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	var resp APIResponse
	err := json.NewDecoder(rr.Body).Decode(&resp)
	require.NoError(t, err)
	assert.True(t, resp.Success)
}

func TestAccountAndTransferAPIEndpoints(t *testing.T) {
	router := setupTestRouter()

	// 1. Create Account A
	createReqA := service.CreateAccountRequest{
		AccountNumber:  "API-ACC-A",
		HolderName:     "Alice Smith",
		Currency:       "USD",
		InitialBalance: 10000,
	}
	bodyA, _ := json.Marshal(createReqA)
	reqA := httptest.NewRequest("POST", "/api/v1/accounts", bytes.NewBuffer(bodyA))
	rrA := httptest.NewRecorder()
	router.ServeHTTP(rrA, reqA)
	assert.Equal(t, http.StatusCreated, rrA.Code)

	var respA struct {
		Success bool           `json:"success"`
		Data    domain.Account `json:"data"`
	}
	_ = json.NewDecoder(rrA.Body).Decode(&respA)
	accAID := respA.Data.ID
	assert.NotEqual(t, uuid.Nil, accAID)

	// 2. Create Account B
	createReqB := service.CreateAccountRequest{
		AccountNumber:  "API-ACC-B",
		HolderName:     "Bob Jones",
		Currency:       "USD",
		InitialBalance: 2000,
	}
	bodyB, _ := json.Marshal(createReqB)
	reqB := httptest.NewRequest("POST", "/api/v1/accounts", bytes.NewBuffer(bodyB))
	rrB := httptest.NewRecorder()
	router.ServeHTTP(rrB, reqB)
	assert.Equal(t, http.StatusCreated, rrB.Code)

	var respB struct {
		Success bool           `json:"success"`
		Data    domain.Account `json:"data"`
	}
	_ = json.NewDecoder(rrB.Body).Decode(&respB)
	accBID := respB.Data.ID

	// 3. Execute Transfer from A to B ($30.00)
	transferReq := domain.CreateTransferRequest{
		FromAccountID: &accAID,
		ToAccountID:   &accBID,
		Amount:        3000,
		Currency:      "USD",
		Description:   "API test transfer",
	}
	bodyTx, _ := json.Marshal(transferReq)
	reqTx := httptest.NewRequest("POST", "/api/v1/transfers", bytes.NewBuffer(bodyTx))
	reqTx.Header.Set("Idempotency-Key", "http-api-tx-001")
	rrTx := httptest.NewRecorder()
	router.ServeHTTP(rrTx, reqTx)
	assert.Equal(t, http.StatusCreated, rrTx.Code)

	var txResp struct {
		Success bool            `json:"success"`
		Data    domain.Transfer `json:"data"`
	}
	_ = json.NewDecoder(rrTx.Body).Decode(&txResp)
	assert.Equal(t, int64(3000), txResp.Data.Amount)

	// 4. Retry same transfer with same Idempotency-Key -> Returns 200 OK
	reqTxReplay := httptest.NewRequest("POST", "/api/v1/transfers", bytes.NewBuffer(bodyTx))
	reqTxReplay.Header.Set("Idempotency-Key", "http-api-tx-001")
	rrTxReplay := httptest.NewRecorder()
	router.ServeHTTP(rrTxReplay, reqTxReplay)
	assert.Equal(t, http.StatusOK, rrTxReplay.Code, "Replay should return 200 OK")

	// 5. Query Account A balance -> 7000
	reqGetA := httptest.NewRequest("GET", "/api/v1/accounts/"+accAID.String(), nil)
	rrGetA := httptest.NewRecorder()
	router.ServeHTTP(rrGetA, reqGetA)
	assert.Equal(t, http.StatusOK, rrGetA.Code)
	var getAResp struct {
		Data domain.Account `json:"data"`
	}
	_ = json.NewDecoder(rrGetA.Body).Decode(&getAResp)
	assert.Equal(t, int64(7000), getAResp.Data.Balance)

	// 6. Query Statement for Account A
	reqStmtA := httptest.NewRequest("GET", "/api/v1/accounts/"+accAID.String()+"/statement", nil)
	rrStmtA := httptest.NewRecorder()
	router.ServeHTTP(rrStmtA, reqStmtA)
	assert.Equal(t, http.StatusOK, rrStmtA.Code)

	// 7. Audit System Ledger Invariant
	reqAudit := httptest.NewRequest("GET", "/api/v1/audit/system", nil)
	rrAudit := httptest.NewRecorder()
	router.ServeHTTP(rrAudit, reqAudit)
	assert.Equal(t, http.StatusOK, rrAudit.Code)
	var auditResp struct {
		Data service.SystemAuditReport `json:"data"`
	}
	_ = json.NewDecoder(rrAudit.Body).Decode(&auditResp)
	assert.True(t, auditResp.Data.Balanced)
	assert.Equal(t, int64(3000), auditResp.Data.TotalDebits)
	assert.Equal(t, int64(3000), auditResp.Data.TotalCredits)
}
