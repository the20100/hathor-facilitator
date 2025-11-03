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
	log.Printf("=== Verify Request Received ===")
	log.Printf("Method: %s", r.Method)
	log.Printf("Path: %s", r.URL.Path)
	log.Printf("Content-Type: %s", r.Header.Get("Content-Type"))
	log.Printf("Content-Length: %s", r.Header.Get("Content-Length"))
	
	// Log all headers for debugging
	log.Printf("All request headers:")
	for name, values := range r.Header {
		for _, value := range values {
			// Truncate long headers for readability
			displayValue := value
			if len(displayValue) > 200 {
				displayValue = displayValue[:200] + "... (truncated)"
			}
			log.Printf("  %s: %s", name, displayValue)
		}
	}
	
	// Check X-PAYMENT header with case-insensitive lookup
	// Go's http.Header.Get is case-insensitive, but we'll try explicit variations just in case
	paymentHeader := r.Header.Get("X-PAYMENT")
	if paymentHeader == "" {
		// Try other case variations if needed
		paymentHeader = r.Header.Get("x-payment")
	}
	if paymentHeader == "" {
		paymentHeader = r.Header.Get("X-Payment")
	}
	
	log.Printf("X-PAYMENT header present: %v", paymentHeader != "")
	if paymentHeader != "" {
		log.Printf("X-PAYMENT header length: %d", len(paymentHeader))
	}
	
	if r.Method != http.MethodPost {
		log.Printf("Rejected: Method not allowed (got %s, expected POST)", r.Method)
		respondJSON(w, http.StatusPaymentRequired, VerifyResponse{
			Success: false,
			Error:   "Method not allowed",
		})
		return
	}

	var req VerifyRequest
	var err error

	// Check if payment payload is in X-PAYMENT header (base64-encoded)
	// (paymentHeader already retrieved above)
	if paymentHeader != "" {
		log.Printf("Processing request with X-PAYMENT header (length: %d)", len(paymentHeader))
		if len(paymentHeader) > 100 {
			log.Printf("X-PAYMENT header value (first 100 chars): %s...", paymentHeader[:100])
		} else {
			log.Printf("X-PAYMENT header value: %s", paymentHeader)
		}
		
		// Decode base64
		decoded, decodeErr := base64.StdEncoding.DecodeString(paymentHeader)
		if decodeErr != nil {
			log.Printf("Failed to decode X-PAYMENT header: %v", decodeErr)
			log.Printf("X-PAYMENT header value: %s", paymentHeader)
			respondJSON(w, http.StatusPaymentRequired, VerifyResponse{
				Success: false,
				Error:   "Invalid base64 encoding in X-PAYMENT header: " + decodeErr.Error(),
			})
			return
		}
		
		log.Printf("Successfully decoded X-PAYMENT header (decoded length: %d bytes)", len(decoded))
		if len(decoded) > 200 {
			log.Printf("Decoded content (first 200 chars): %s...", string(decoded[:200]))
		} else {
			log.Printf("Decoded content: %s", string(decoded))
		}

		// Parse JSON from decoded base64
		if err = json.Unmarshal(decoded, &req); err != nil {
			log.Printf("Failed to parse X-PAYMENT JSON: %v", err)
			log.Printf("Decoded content: %s", string(decoded))
			respondJSON(w, http.StatusPaymentRequired, VerifyResponse{
				Success: false,
				Error:   "Invalid JSON in X-PAYMENT header: " + err.Error(),
			})
			return
		}
		
		log.Printf("Successfully parsed X-PAYMENT JSON: scheme=%s, network=%s", req.Scheme, req.Network)

		// Read requirements from body if present
		bodyBytes, readErr := ioutil.ReadAll(r.Body)
		if readErr != nil {
			log.Printf("Error reading request body: %v", readErr)
		} else {
			log.Printf("Request body length: %d bytes", len(bodyBytes))
			if len(bodyBytes) > 0 {
				log.Printf("Request body content: %s", string(bodyBytes))
				var bodyReq struct {
					Requirements HathorPaymentRequirements `json:"requirements"`
				}
				if json.Unmarshal(bodyBytes, &bodyReq) == nil && bodyReq.Requirements.Address != "" {
					log.Printf("Merging requirements from body: address=%s, amount=%d, asset=%s", 
						bodyReq.Requirements.Address, bodyReq.Requirements.Amount, bodyReq.Requirements.Asset)
					req.Requirements = bodyReq.Requirements
				} else {
					log.Printf("Body did not contain valid requirements or address is empty")
				}
			} else {
				log.Printf("Request body is empty")
			}
		}
	} else {
		log.Printf("No X-PAYMENT header found, reading from request body")
		
		// Read body first for logging
		bodyBytes, readErr := ioutil.ReadAll(r.Body)
		if readErr != nil {
			log.Printf("Error reading request body: %v", readErr)
			respondJSON(w, http.StatusPaymentRequired, VerifyResponse{
				Success: false,
				Error:   "Error reading request body: " + readErr.Error(),
			})
			return
		}
		
		log.Printf("Request body length: %d bytes", len(bodyBytes))
		if len(bodyBytes) > 0 {
			log.Printf("Request body content: %s", string(bodyBytes))
		} else {
			log.Printf("Request body is empty (EOF)")
		}
		
		// Fall back to reading from request body
		if err = json.Unmarshal(bodyBytes, &req); err != nil {
			log.Printf("Failed to parse JSON from request body: %v", err)
			log.Printf("Body content was: %s", string(bodyBytes))
			respondJSON(w, http.StatusPaymentRequired, VerifyResponse{
				Success: false,
				Error:   "Bad request JSON: " + err.Error(),
			})
			return
		}
		
		log.Printf("Successfully parsed JSON from body: scheme=%s, network=%s", req.Scheme, req.Network)
	}

	// 1. Check scheme and network
	if req.Scheme != "exact" {
		respondJSON(w, http.StatusPaymentRequired, VerifyResponse{
			Success: false,
			Error:   "Unsupported scheme: only 'exact' is supported",
		})
		return
	}

	if req.Network != "hathor-mainnet" && req.Network != "hathor-testnet" {
		respondJSON(w, http.StatusPaymentRequired, VerifyResponse{
			Success: false,
			Error:   "Unsupported network: must be hathor-mainnet or hathor-testnet",
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
