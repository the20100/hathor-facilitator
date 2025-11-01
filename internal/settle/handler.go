package settle

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"time"

	"github.com/hathor-network/hathor-facilitator/internal/hathor"
)

type Handler struct {
	hathorClient    *hathor.Client
	minConfirmations int
}

func NewHandler(hathorClient *hathor.Client, minConfirmations int) *Handler {
	return &Handler{
		hathorClient:     hathorClient,
		minConfirmations: minConfirmations,
	}
}

type SettleRequest struct {
	Scheme       string                    `json:"scheme"`
	Network      string                    `json:"network"`
	Payload      HathorPaymentPayload      `json:"payload"`
	Requirements HathorPaymentRequirements `json:"requirements"`
}

type HathorPaymentPayload struct {
	TxHex string `json:"txHex"`
}

type HathorPaymentRequirements struct {
	Amount  uint64 `json:"amount"`
	Asset   string `json:"asset"`
	Address string `json:"address"`
}

type SettleResponse struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
	TxID    string `json:"txID,omitempty"`
}

func (h *Handler) Handle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondJSON(w, http.StatusPaymentRequired, SettleResponse{
			Success: false,
			Error:   "Method not allowed",
		})
		return
	}

	var req SettleRequest
	var err error

	// Check if payment payload is in X-PAYMENT header (base64-encoded)
	if paymentHeader := r.Header.Get("X-PAYMENT"); paymentHeader != "" {
		// Decode base64
		decoded, decodeErr := base64.StdEncoding.DecodeString(paymentHeader)
		if decodeErr != nil {
			log.Printf("Failed to decode X-PAYMENT header: %v", decodeErr)
			respondJSON(w, http.StatusPaymentRequired, SettleResponse{
				Success: false,
				Error:   "Invalid base64 encoding in X-PAYMENT header: " + decodeErr.Error(),
			})
			return
		}

		// Parse JSON from decoded base64
		if err = json.Unmarshal(decoded, &req); err != nil {
			log.Printf("Failed to parse X-PAYMENT JSON: %v", err)
			respondJSON(w, http.StatusPaymentRequired, SettleResponse{
				Success: false,
				Error:   "Invalid JSON in X-PAYMENT header: " + err.Error(),
			})
			return
		}

		// Read requirements from body if present
		bodyBytes, _ := ioutil.ReadAll(r.Body)
		if len(bodyBytes) > 0 {
			var bodyReq struct {
				Requirements HathorPaymentRequirements `json:"requirements"`
			}
			if json.Unmarshal(bodyBytes, &bodyReq) == nil && bodyReq.Requirements.Address != "" {
				req.Requirements = bodyReq.Requirements
			}
		}
	} else {
		// Fall back to reading from request body
		if err = json.NewDecoder(r.Body).Decode(&req); err != nil {
			respondJSON(w, http.StatusPaymentRequired, SettleResponse{
				Success: false,
				Error:   "Bad request JSON: " + err.Error(),
			})
			return
		}
	}

	// Quick sanity check
	if req.Scheme != "exact" {
		respondJSON(w, http.StatusPaymentRequired, SettleResponse{
			Success: false,
			Error:   "Unsupported scheme: only 'exact' is supported",
		})
		return
	}

	if req.Network != "HathorMainnet" && req.Network != "HathorTestnet" {
		respondJSON(w, http.StatusPaymentRequired, SettleResponse{
			Success: false,
			Error:   "Unsupported network: must be HathorMainnet or HathorTestnet",
		})
		return
	}

	// First verify the transaction can be decoded (similar to /verify)
	// This ensures we have a valid transaction before attempting to broadcast
	decodedTx, err := h.hathorClient.DecodeTransaction(req.Payload.TxHex)
	if err != nil {
		log.Printf("Settle: Failed to decode transaction before broadcast: %v", err)
		respondJSON(w, http.StatusPaymentRequired, SettleResponse{
			Success: false,
			Error:   "Invalid transaction format: " + err.Error(),
		})
		return
	}

	if !decodedTx.CompleteSignatures {
		respondJSON(w, http.StatusPaymentRequired, SettleResponse{
			Success: false,
			Error:   "Transaction does not have complete signatures",
		})
		return
	}

	// Push the transaction to the Hathor network
	txID, err := h.hathorClient.PushTransaction(req.Payload.TxHex)
	if err != nil {
		respondJSON(w, http.StatusPaymentRequired, SettleResponse{
			Success: false,
			Error:   "Broadcast failed: " + err.Error(),
		})
		return
	}

	// Wait for confirmation only if minConfirmations > 0
	if h.minConfirmations > 0 {
		confCount := 0
		timeout := time.After(60 * time.Second)
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-timeout:
				// Timeout - transaction was broadcast but confirmation timeout
				// Still return success since transaction was accepted
				respondJSON(w, http.StatusOK, SettleResponse{
					Success: true,
					TxID:    txID,
				})
				return

			case <-ticker.C:
				confirmations, err := h.hathorClient.CheckConfirmations(txID)
				if err != nil {
					// Log error but continue polling
					fmt.Printf("Error checking confirmations: %v\n", err)
					continue
				}

				confCount = confirmations
				if confCount >= h.minConfirmations {
					// Sufficient confirmations
					respondJSON(w, http.StatusOK, SettleResponse{
						Success: true,
						TxID:    txID,
					})
					return
				}
				// Continue polling
			}
		}
	} else {
		// If not waiting for confirmations, return immediately after broadcast
		respondJSON(w, http.StatusOK, SettleResponse{
			Success: true,
			TxID:    txID,
		})
		return
	}
}

func respondJSON(w http.ResponseWriter, statusCode int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(data)
}
