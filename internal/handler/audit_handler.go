package handler

import (
	"net/http"

	"github.com/google/uuid"

	"payment-ledger/internal/service"
)

type AuditHandler struct {
	service service.AuditService
}

func NewAuditHandler(s service.AuditService) *AuditHandler {
	return &AuditHandler{service: s}
}

func (h *AuditHandler) AuditSystem(w http.ResponseWriter, r *http.Request) {
	report, err := h.service.AuditSystem(r.Context())
	if err != nil {
		HandleDomainError(w, err)
		return
	}

	JSONResponse(w, http.StatusOK, report)
}

func (h *AuditHandler) AuditAccount(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		ErrorResponse(w, http.StatusBadRequest, "Invalid account UUID format")
		return
	}

	report, err := h.service.AuditAccount(r.Context(), id)
	if err != nil {
		HandleDomainError(w, err)
		return
	}

	JSONResponse(w, http.StatusOK, report)
}
