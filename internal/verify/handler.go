package verify

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/hathor-network/hathor-facilitator/internal/hathor"
	"github.com/hathor-network/hathor-facilitator/internal/txparser"
)

type Handler struct {
	hathorClient *hathor.Client
}

func NewHandler(hathorClient *hathor.Client) *Handler {
	return &Handler{
		hathorClient: hathorClient,
	}
}

type VerifyRequest struct {
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
	Asset   string `json:"asset"` // e.g., "HTR"
	Address string `json:"address"`
}

type VerifyResponse struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
	Payer   string `json:"payer,omitempty"`
	TxHash  string `json:"txHash,omitempty"`
}

func (h *Handler) Handle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondJSON(w, http.StatusMethodNotAllowed, VerifyResponse{
			Success: false,
			Error:   "Method not allowed",
		})
		return
	}

	var req VerifyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, http.StatusBadRequest, VerifyResponse{
			Success: false,
			Error:   "Bad request JSON: " + err.Error(),
		})
		return
	}

	// 1. Check scheme and network
	if req.Scheme != "exact" {
		respondJSON(w, http.StatusBadRequest, VerifyResponse{
			Success: false,
			Error:   "Unsupported scheme: only 'exact' is supported",
		})
		return
	}

	if req.Network != "HathorMainnet" && req.Network != "HathorTestnet" {
		respondJSON(w, http.StatusBadRequest, VerifyResponse{
			Success: false,
			Error:   "Unsupported network: must be HathorMainnet or HathorTestnet",
		})
		return
	}

	// 2. Decode and parse transaction
	txBytes, err := hathor.DecodeTransactionHex(req.Payload.TxHex)
	if err != nil {
		respondJSON(w, http.StatusBadRequest, VerifyResponse{
			Success: false,
			Error:   "Invalid hex string: " + err.Error(),
		})
		return
	}

	tx, err := txparser.ParseTransaction(txBytes)
	if err != nil {
		respondJSON(w, http.StatusBadRequest, VerifyResponse{
			Success: false,
			Error:   "Failed to parse transaction: " + err.Error(),
		})
		return
	}

	// 3. Verify signatures for each input
	for i, input := range tx.Inputs {
		prevOut, err := h.hathorClient.GetOutput(input.TxID, int(input.Index))
		if err != nil {
			respondJSON(w, http.StatusBadRequest, VerifyResponse{
				Success: false,
				Error:   fmt.Sprintf("Input %d not found: %v", i, err),
			})
			return
		}

		// Check if already spent
		if prevOut.IsSpent {
			respondJSON(w, http.StatusBadRequest, VerifyResponse{
				Success: false,
				Error:   fmt.Sprintf("Input %d already spent", i),
			})
			return
		}

		// Convert hathor.OutputInfo to txparser.OutputInfo
		outputInfo := &txparser.OutputInfo{
			Script: prevOut.Script,
		}

		// Verify signature
		if err := txparser.VerifyInputSignature(tx, i, outputInfo, input.Signature, input.PublicKey); err != nil {
			respondJSON(w, http.StatusBadRequest, VerifyResponse{
				Success: false,
				Error:   fmt.Sprintf("Invalid signature on input %d: %v", i, err),
			})
			return
		}
	}

	// 4. Validate payment output
	reqAmount := req.Requirements.Amount
	reqAddress := req.Requirements.Address
	reqAsset := req.Requirements.Asset

	var paidAmount uint64 = 0
	for _, output := range tx.Outputs {
		if output.Address == reqAddress {
			// Check asset type
			assetMatch := false
			if reqAsset == "HTR" && output.TokenData == 0 {
				assetMatch = true
			} else if reqAsset != "HTR" {
				// For custom tokens, would need to check token ID
				// For now, we assume HTR only
			}

			if assetMatch {
				paidAmount += output.Value
			}
		}
	}

	if paidAmount < reqAmount {
		respondJSON(w, http.StatusBadRequest, VerifyResponse{
			Success: false,
			Error:   fmt.Sprintf("Insufficient amount: paid %d, required %d", paidAmount, reqAmount),
		})
		return
	}

	// 5. Validate PoW (basic check - full validation would require more complex logic)
	if !txparser.ValidatePoW(tx) {
		respondJSON(w, http.StatusBadRequest, VerifyResponse{
			Success: false,
			Error:   "Transaction PoW insufficient",
		})
		return
	}

	// Success - derive payer address from first input
	userAddress := ""
	if len(tx.Inputs) > 0 {
		pubKey := tx.Inputs[0].PublicKey
		if addr, err := txparser.PublicKeyToAddress(pubKey, req.Network == "HathorMainnet"); err == nil {
			userAddress = addr
		}
	}

	respondJSON(w, http.StatusOK, VerifyResponse{
		Success: true,
		Payer:   userAddress,
		TxHash:  tx.Hash,
	})
}

func respondJSON(w http.ResponseWriter, statusCode int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(data)
}
