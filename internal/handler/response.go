package handler

import (
	"encoding/json"
	"net/http"

	"payment-ledger/internal/domain"
)

type APIResponse struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data,omitempty"`
	Error   *string     `json:"error,omitempty"`
}

func JSONResponse(w http.ResponseWriter, statusCode int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(APIResponse{
		Success: statusCode >= 200 && statusCode < 300,
		Data:    data,
	})
}

func ErrorResponse(w http.ResponseWriter, statusCode int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(APIResponse{
		Success: false,
		Error:   &message,
	})
}

func HandleDomainError(w http.ResponseWriter, err error) {
	switch err {
	case domain.ErrAccountNotFound:
		ErrorResponse(w, http.StatusNotFound, err.Error())
	case domain.ErrAccountFrozen, domain.ErrAccountClosed:
		ErrorResponse(w, http.StatusForbidden, err.Error())
	case domain.ErrInsufficientFunds:
		ErrorResponse(w, http.StatusUnprocessableEntity, err.Error())
	case domain.ErrCurrencyMismatch, domain.ErrUnbalancedEntries, domain.ErrInvalidAmount, domain.ErrSelfTransfer, domain.ErrNoEntries, domain.ErrInvalidTransfer:
		ErrorResponse(w, http.StatusBadRequest, err.Error())
	case domain.ErrIdempotencyConflict:
		ErrorResponse(w, http.StatusConflict, err.Error())
	case domain.ErrIdempotencyInProgress:
		ErrorResponse(w, http.StatusConflict, err.Error())
	case domain.ErrDuplicateIdempotencyKey:
		ErrorResponse(w, http.StatusConflict, err.Error())
	default:
		ErrorResponse(w, http.StatusInternalServerError, err.Error())
	}
}
