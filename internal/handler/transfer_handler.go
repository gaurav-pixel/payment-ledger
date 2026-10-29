package handler

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/google/uuid"

	"payment-ledger/internal/domain"
	"payment-ledger/internal/service"
)

type TransferHandler struct {
	service service.LedgerService
}

func NewTransferHandler(s service.LedgerService) *TransferHandler {
	return &TransferHandler{service: s}
}

func (h *TransferHandler) CreateTransfer(w http.ResponseWriter, r *http.Request) {
	var req domain.CreateTransferRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		ErrorResponse(w, http.StatusBadRequest, "Invalid JSON payload")
		return
	}

	// Support Idempotency-Key from HTTP header or request body
	if headerKey := r.Header.Get("Idempotency-Key"); headerKey != "" && req.IdempotencyKey == "" {
		req.IdempotencyKey = headerKey
	}

	result, err := h.service.ExecuteTransfer(r.Context(), req)
	if err != nil {
		HandleDomainError(w, err)
		return
	}

	statusCode := http.StatusCreated
	if result.Cached {
		statusCode = http.StatusOK
	}

	JSONResponse(w, statusCode, result.Transfer)
}

func (h *TransferHandler) GetTransfer(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		ErrorResponse(w, http.StatusBadRequest, "Invalid transfer UUID format")
		return
	}

	transfer, err := h.service.GetTransfer(r.Context(), id)
	if err != nil {
		HandleDomainError(w, err)
		return
	}

	JSONResponse(w, http.StatusOK, transfer)
}

func (h *TransferHandler) ListTransfers(w http.ResponseWriter, r *http.Request) {
	limitStr := r.URL.Query().Get("limit")
	offsetStr := r.URL.Query().Get("offset")

	limit, _ := strconv.Atoi(limitStr)
	offset, _ := strconv.Atoi(offsetStr)

	transfers, err := h.service.ListTransfers(r.Context(), limit, offset)
	if err != nil {
		HandleDomainError(w, err)
		return
	}

	JSONResponse(w, http.StatusOK, transfers)
}
