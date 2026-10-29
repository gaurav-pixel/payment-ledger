package handler

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/google/uuid"

	"payment-ledger/internal/service"
)

type AccountHandler struct {
	service service.LedgerService
}

func NewAccountHandler(s service.LedgerService) *AccountHandler {
	return &AccountHandler{service: s}
}

func (h *AccountHandler) CreateAccount(w http.ResponseWriter, r *http.Request) {
	var req service.CreateAccountRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		ErrorResponse(w, http.StatusBadRequest, "Invalid JSON payload")
		return
	}

	acc, err := h.service.CreateAccount(r.Context(), req)
	if err != nil {
		HandleDomainError(w, err)
		return
	}

	JSONResponse(w, http.StatusCreated, acc)
}

func (h *AccountHandler) GetAccount(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		ErrorResponse(w, http.StatusBadRequest, "Invalid account UUID format")
		return
	}

	acc, err := h.service.GetAccount(r.Context(), id)
	if err != nil {
		HandleDomainError(w, err)
		return
	}

	JSONResponse(w, http.StatusOK, acc)
}

func (h *AccountHandler) ListAccounts(w http.ResponseWriter, r *http.Request) {
	limitStr := r.URL.Query().Get("limit")
	offsetStr := r.URL.Query().Get("offset")

	limit, _ := strconv.Atoi(limitStr)
	offset, _ := strconv.Atoi(offsetStr)

	accounts, err := h.service.ListAccounts(r.Context(), limit, offset)
	if err != nil {
		HandleDomainError(w, err)
		return
	}

	JSONResponse(w, http.StatusOK, accounts)
}

func (h *AccountHandler) GetAccountStatement(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		ErrorResponse(w, http.StatusBadRequest, "Invalid account UUID format")
		return
	}

	limitStr := r.URL.Query().Get("limit")
	offsetStr := r.URL.Query().Get("offset")

	limit, _ := strconv.Atoi(limitStr)
	offset, _ := strconv.Atoi(offsetStr)

	entries, err := h.service.GetAccountStatement(r.Context(), id, limit, offset)
	if err != nil {
		HandleDomainError(w, err)
		return
	}

	JSONResponse(w, http.StatusOK, entries)
}
