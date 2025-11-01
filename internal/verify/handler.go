package verify

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
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
}

func (h *Handler) Handle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondJSON(w, http.StatusPaymentRequired, VerifyResponse{
			Success: false,
			Error:   "Method not allowed",
		})
		return
	}

	var req VerifyRequest
	var err error

	// Check if payment payload is in X-PAYMENT header (base64-encoded)
	if paymentHeader := r.Header.Get("X-PAYMENT"); paymentHeader != "" {
		// Decode base64
		decoded, decodeErr := base64.StdEncoding.DecodeString(paymentHeader)
		if decodeErr != nil {
			log.Printf("Failed to decode X-PAYMENT header: %v", decodeErr)
			respondJSON(w, http.StatusPaymentRequired, VerifyResponse{
				Success: false,
				Error:   "Invalid base64 encoding in X-PAYMENT header: " + decodeErr.Error(),
			})
			return
		}

		// Parse JSON from decoded base64
		if err = json.Unmarshal(decoded, &req); err != nil {
			log.Printf("Failed to parse X-PAYMENT JSON: %v", err)
			respondJSON(w, http.StatusPaymentRequired, VerifyResponse{
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
			respondJSON(w, http.StatusPaymentRequired, VerifyResponse{
				Success: false,
				Error:   "Bad request JSON: " + err.Error(),
			})
			return
		}
	}

	// 1. Check scheme and network
	if req.Scheme != "exact" {
		respondJSON(w, http.StatusPaymentRequired, VerifyResponse{
			Success: false,
			Error:   "Unsupported scheme: only 'exact' is supported",
		})
		return
	}

	if req.Network != "HathorMainnet" && req.Network != "HathorTestnet" {
		respondJSON(w, http.StatusPaymentRequired, VerifyResponse{
			Success: false,
			Error:   "Unsupported network: must be HathorMainnet or HathorTestnet",
		})
		return
	}

	// 2. Decode transaction using headless wallet API
	decodedTx, err := h.hathorClient.DecodeTransaction(req.Payload.TxHex)
	if err != nil {
		log.Printf("Failed to decode transaction: %v", err)
		respondJSON(w, http.StatusPaymentRequired, VerifyResponse{
			Success: false,
			Error:   "Failed to decode transaction: " + err.Error(),
		})
		return
	}

	tx, err := txparser.ParseTransactionFromWallet(decodedTx)
	if err != nil {
		respondJSON(w, http.StatusPaymentRequired, VerifyResponse{
			Success: false,
			Error:   "Failed to parse decoded transaction: " + err.Error(),
		})
		return
	}

	// Calculate transaction hash from the hex
	txBytes, err := hex.DecodeString(req.Payload.TxHex)
	if err != nil {
		respondJSON(w, http.StatusPaymentRequired, VerifyResponse{
			Success: false,
			Error:   "Invalid transaction hex: " + err.Error(),
		})
		return
	}
	tx.Hash = txparser.CalculateTxHash(txBytes)

	// Parse the raw transaction to extract signatures and public keys
	// The wallet decode doesn't provide raw signature data, so we need to parse the raw bytes
	rawTx, err := txparser.ParseTransaction(txBytes)
	if err != nil {
		log.Printf("Warning: Failed to parse raw transaction for signature extraction: %v", err)
		// Continue with wallet decode data, but we won't be able to verify signatures
		rawTx = nil
	}

	// Debug: log parsed transaction outputs
	log.Printf("Parsed transaction has %d outputs", len(tx.Outputs))
	for i, out := range tx.Outputs {
		log.Printf("  Output[%d]: address='%s', value=%d, tokenData=%d", i, out.Address, out.Value, out.TokenData)
	}

	// 3. Verify inputs and signatures
	// IMPORTANT: We must cryptographically verify signatures, not just trust the wallet's flag
	
	// First check the wallet's completeSignatures flag as a quick sanity check
	if !decodedTx.CompleteSignatures {
		respondJSON(w, http.StatusPaymentRequired, VerifyResponse{
			Success: false,
			Error:   "Transaction does not have complete signatures",
		})
		return
	}

	// Now verify each input's signature cryptographically
	for i, input := range tx.Inputs {
		// Try to get the previous output to verify it exists and is unspent
		prevOut, err := h.hathorClient.GetOutput(input.TxID, int(input.Index))
		if err != nil {
			log.Printf("Warning: Could not fetch input %d (tx: %s, idx: %d): %v", i, input.TxID, input.Index, err)
			// We still need to verify the signature even if we can't fetch the output
			// Try to use the script from the wallet decode
			if rawTx != nil && i < len(rawTx.Inputs) && len(rawTx.Inputs[i].Signature) > 0 {
				// Use script from decoded input if available
				if len(decodedTx.Inputs) > i {
					scriptBytes, _ := base64.StdEncoding.DecodeString(decodedTx.Inputs[i].Script)
					outputInfo := &txparser.OutputInfo{
						Script: hex.EncodeToString(scriptBytes),
					}
					if err := txparser.VerifyInputSignature(rawTx, i, outputInfo, rawTx.Inputs[i].Signature, rawTx.Inputs[i].PublicKey); err != nil {
						respondJSON(w, http.StatusPaymentRequired, VerifyResponse{
							Success: false,
							Error:   fmt.Sprintf("Signature verification failed for input %d: %v", i, err),
						})
						return
					}
				}
			}
			continue
		}

		// Check if already spent
		if prevOut.IsSpent {
			respondJSON(w, http.StatusPaymentRequired, VerifyResponse{
				Success: false,
				Error:   fmt.Sprintf("Input %d already spent", i),
			})
			return
		}

		// Verify signature cryptographically using raw transaction data
		if rawTx != nil && i < len(rawTx.Inputs) && len(rawTx.Inputs[i].Signature) > 0 && len(rawTx.Inputs[i].PublicKey) > 0 {
			outputInfo := &txparser.OutputInfo{
				Script: prevOut.Script,
			}
			if err := txparser.VerifyInputSignature(rawTx, i, outputInfo, rawTx.Inputs[i].Signature, rawTx.Inputs[i].PublicKey); err != nil {
				respondJSON(w, http.StatusPaymentRequired, VerifyResponse{
					Success: false,
					Error:   fmt.Sprintf("Signature verification failed for input %d: %v", i, err),
				})
				return
			}
			log.Printf("Signature verification passed for input %d", i)
		} else {
			// If we couldn't extract signature from raw transaction, this is a problem
			log.Printf("Warning: Could not extract signature data for input %d from raw transaction", i)
			// For now, if we can't verify, we fail (strict mode)
			respondJSON(w, http.StatusPaymentRequired, VerifyResponse{
				Success: false,
				Error:   fmt.Sprintf("Could not verify signature for input %d: signature data not available", i),
			})
			return
		}
	}

	// 4. Validate payment output
	reqAmount := req.Requirements.Amount
	reqAddress := req.Requirements.Address
	reqAsset := req.Requirements.Asset

	// Normalize asset: "00" or "HTR" both mean native HTR token (token_data = 0)
	isHTR := reqAsset == "HTR" || reqAsset == "00" || reqAsset == ""

	var paidAmount uint64 = 0
	log.Printf("Checking payment: address=%s, amount=%d, asset=%s (isHTR=%v)", reqAddress, reqAmount, reqAsset, isHTR)
	
	for i, output := range tx.Outputs {
		log.Printf("Output %d: address=%s, value=%d, tokenData=%d", i, output.Address, output.Value, output.TokenData)
		
		if output.Address == reqAddress {
			// Check asset type
			assetMatch := false
			if isHTR && output.TokenData == 0 {
				assetMatch = true
				log.Printf("  -> Asset match (HTR)")
			} else if !isHTR {
				// For custom tokens, would need to check token ID
				// For now, we assume HTR only
				log.Printf("  -> Asset mismatch (not HTR)")
			}

			if assetMatch {
				paidAmount += output.Value
				log.Printf("  -> Added %d to paidAmount (total: %d)", output.Value, paidAmount)
			}
		} else {
			log.Printf("  -> Address mismatch (expected: %s, got: %s)", reqAddress, output.Address)
		}
	}

	log.Printf("Final check: paidAmount=%d, required=%d", paidAmount, reqAmount)

	if paidAmount < reqAmount {
		respondJSON(w, http.StatusPaymentRequired, VerifyResponse{
			Success: false,
			Error:   fmt.Sprintf("Insufficient amount: paid %d, required %d", paidAmount, reqAmount),
		})
		return
	}

	// 5. Validate PoW
	if !txparser.ValidatePoW(tx) {
		respondJSON(w, http.StatusPaymentRequired, VerifyResponse{
			Success: false,
			Error:   "Transaction PoW insufficient",
		})
		return
	}

	// Success
	respondJSON(w, http.StatusOK, VerifyResponse{
		Success: true,
	})
}

func respondJSON(w http.ResponseWriter, statusCode int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(data)
}
