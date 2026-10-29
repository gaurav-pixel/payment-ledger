package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"time"

	"github.com/google/uuid"
)

type IdempotencyStatus string

const (
	IdempotencyStatusProcessing IdempotencyStatus = "PROCESSING"
	IdempotencyStatusCompleted  IdempotencyStatus = "COMPLETED"
	IdempotencyStatusFailed     IdempotencyStatus = "FAILED"
)

type IdempotencyRecord struct {
	Key          string            `json:"key"`
	RequestHash  string            `json:"request_hash"`
	Status       IdempotencyStatus `json:"status"`
	TransferID   *uuid.UUID        `json:"transfer_id,omitempty"`
	StatusCode   int               `json:"status_code"`
	ResponseBody string            `json:"response_body"`
	CreatedAt    time.Time         `json:"created_at"`
	ExpiresAt    time.Time         `json:"expires_at"`
}

// ComputePayloadHash returns the SHA-256 hash of the canonical request payload.
func ComputePayloadHash(payload []byte) string {
	hash := sha256.Sum256(payload)
	return hex.EncodeToString(hash[:])
}
