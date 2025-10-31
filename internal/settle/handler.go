package settle

import (
	"encoding/json"
	"fmt"
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
	Success       bool   `json:"success"`
	Error         string `json:"error,omitempty"`
	TxHash        string `json:"txHash,omitempty"`
	Confirmations int    `json:"confirmations,omitempty"`
}

func (h *Handler) Handle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondJSON(w, http.StatusMethodNotAllowed, SettleResponse{
			Success: false,
			Error:   "Method not allowed",
		})
		return
	}

	var req SettleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, http.StatusBadRequest, SettleResponse{
			Success: false,
			Error:   "Bad request JSON: " + err.Error(),
		})
		return
	}

	// Quick sanity check
	if req.Scheme != "exact" {
		respondJSON(w, http.StatusBadRequest, SettleResponse{
			Success: false,
			Error:   "Unsupported scheme: only 'exact' is supported",
		})
		return
	}

	if req.Network != "HathorMainnet" && req.Network != "HathorTestnet" {
		respondJSON(w, http.StatusBadRequest, SettleResponse{
			Success: false,
			Error:   "Unsupported network: must be HathorMainnet or HathorTestnet",
		})
		return
	}

	// Push the transaction to the Hathor network
	txID, err := h.hathorClient.PushTransaction(req.Payload.TxHex)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, SettleResponse{
			Success: false,
			Error:   "Broadcast failed: " + err.Error(),
		})
		return
	}

	// Wait for confirmation
	confCount := 0
	timeout := time.After(60 * time.Second)
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			// Timeout - return with current confirmations (might be 0)
			respondJSON(w, http.StatusOK, SettleResponse{
				Success:       true,
				TxHash:        txID,
				Confirmations: confCount,
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
					Success:       true,
					TxHash:        txID,
					Confirmations: confCount,
				})
				return
			}
			// Continue polling
		}
	}
}

func respondJSON(w http.ResponseWriter, statusCode int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(data)
}
