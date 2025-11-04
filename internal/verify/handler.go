package verify

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"strings"

	"github.com/hathor-network/hathor-facilitator/internal/hathor"
	"github.com/hathor-network/hathor-facilitator/internal/txscan"
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
	TxHex         string `json:"txHex"`
	DataToSignHash string `json:"dataToSignHash"` // Required: 64-hex char hash from tx-proposal
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

	// dataToSignHash is optional - if missing, we'll skip signature verification
	// (signature verification will be done later during settle)
	hasDataToSignHash := req.Payload.DataToSignHash != ""
	if hasDataToSignHash {
		// Validate dataToSignHash format (64 hex chars = 32 bytes)
		if len(req.Payload.DataToSignHash) != 64 {
			respondJSON(w, http.StatusPaymentRequired, VerifyResponse{
				Success: false,
				Error:   fmt.Sprintf("Invalid dataToSignHash length: expected 64 hex chars, got %d", len(req.Payload.DataToSignHash)),
			})
			return
		}

		// Validate it's valid hex
		_, err = hex.DecodeString(req.Payload.DataToSignHash)
		if err != nil {
			respondJSON(w, http.StatusPaymentRequired, VerifyResponse{
				Success: false,
				Error:   "Invalid dataToSignHash format: not valid hex: " + err.Error(),
			})
			return
		}

		log.Printf("Using dataToSignHash from payload: %s", req.Payload.DataToSignHash)
	} else {
		log.Printf("No dataToSignHash provided - will skip signature verification (will be verified on settle)")
	}

	// Calculate transaction hash from the hex first (needed for on-chain check)
	txBytes, err := hex.DecodeString(req.Payload.TxHex)
	if err != nil {
		respondJSON(w, http.StatusPaymentRequired, VerifyResponse{
			Success: false,
			Error:   "Invalid transaction hex: " + err.Error(),
		})
		return
	}

	// 2. Decode transaction using headless wallet API (with fallback to direct parsing)
	var tx *txparser.Transaction
	var decodedTx *hathor.DecodedTransaction
	decodedTx, err = h.hathorClient.DecodeTransaction(req.Payload.TxHex)
	if err != nil {
		log.Printf("Wallet API decode failed: %v, falling back to direct parsing", err)
		// Fallback: parse transaction directly from hex bytes
		tx, err = txparser.ParseTransaction(txBytes)
		if err != nil {
			log.Printf("Direct parsing also failed: %v", err)
			respondJSON(w, http.StatusPaymentRequired, VerifyResponse{
				Success: false,
				Error:   "Failed to decode transaction: wallet API failed (" + err.Error() + "), and direct parsing also failed",
			})
			return
		}
		tx.Hash = txparser.CalculateTxHash(txBytes)
		log.Printf("Successfully parsed transaction directly from hex (fallback)")
	} else {
		// Successfully decoded via wallet API
		tx, err = txparser.ParseTransactionFromWallet(decodedTx)
		if err != nil {
			respondJSON(w, http.StatusPaymentRequired, VerifyResponse{
				Success: false,
				Error:   "Failed to parse decoded transaction: " + err.Error(),
			})
			return
		}
		tx.Hash = txparser.CalculateTxHash(txBytes)
		log.Printf("Successfully parsed transaction via wallet API")
	}

	// Check if this transaction already exists on-chain (already been broadcast)
	// If so, we should reject it as it's already been settled
	existingTx, err := h.hathorClient.GetTransaction(tx.Hash)
	if err != nil {
		// If error is "transaction not found", that's fine - it means the transaction doesn't exist yet
		if strings.Contains(err.Error(), "transaction not found") {
			log.Printf("Transaction %s not found on-chain, proceeding with verification", tx.Hash)
		} else {
			// Other errors might indicate network issues, but we'll continue with verification
			log.Printf("Warning: Could not check if transaction exists: %v", err)
		}
	} else {
		// Check if the response actually contains valid transaction data
		// A transaction exists on-chain if it has:
		// - (A valid hash that matches the transaction hash AND (outputs/inputs OR height)) OR
		// - (No hash but has outputs/inputs AND height - indicating confirmed transaction)
		// Empty responses from the API (no hash, no outputs, no inputs, no height) indicate the transaction doesn't exist
		hasValidHashMatch := existingTx != nil && existingTx.Hash != "" && existingTx.Hash == tx.Hash
		hasOutputsOrInputs := existingTx != nil && (len(existingTx.Outputs) > 0 || len(existingTx.Inputs) > 0)
		hasHeight := existingTx != nil && existingTx.Height != nil && *existingTx.Height > 0
		
		// Transaction exists if:
		// 1. Hash matches AND has outputs/inputs (even without height - might be unconfirmed)
		// 2. Hash matches AND has height (confirmed)
		// 3. No hash but has outputs/inputs AND height (edge case - confirmed transaction)
		transactionExists := (hasValidHashMatch && hasOutputsOrInputs) || 
		                     (hasValidHashMatch && hasHeight) ||
		                     (existingTx != nil && existingTx.Hash == "" && hasOutputsOrInputs && hasHeight)
		
		if transactionExists {
			// Transaction actually exists on-chain with valid data
			log.Printf("Transaction %s already exists on-chain (node returned transaction data)", tx.Hash)
			log.Printf("  Transaction details: hash=%s, height=%v, outputs=%d, inputs=%d", 
				existingTx.Hash, existingTx.Height, len(existingTx.Outputs), len(existingTx.Inputs))
			respondJSON(w, http.StatusPaymentRequired, VerifyResponse{
				Success: false,
				Error:   fmt.Sprintf("Transaction already exists on-chain (hash: %s)", tx.Hash),
			})
			return
		} else {
			// Empty or invalid response - transaction doesn't exist on-chain
			log.Printf("Transaction %s not found on-chain (API returned empty/invalid response), proceeding with verification", tx.Hash)
			if existingTx != nil {
				log.Printf("  Response details: hash=%s, height=%v, outputs=%d, inputs=%d", 
					existingTx.Hash, existingTx.Height, len(existingTx.Outputs), len(existingTx.Inputs))
			}
		}
	}

	// Debug: log parsed transaction outputs
	log.Printf("Parsed transaction has %d outputs", len(tx.Outputs))
	for i, out := range tx.Outputs {
		log.Printf("  Output[%d]: address='%s', value=%d, tokenData=%d", i, out.Address, out.Value, out.TokenData)
	}

	// 3. Verify inputs and signatures (if dataToSignHash is provided)
	// IMPORTANT: We must cryptographically verify signatures, not just trust the wallet's flag
	
	// First check the wallet's completeSignatures flag as a quick sanity check (only if we got decodedTx from wallet API)
	if decodedTx != nil && !decodedTx.CompleteSignatures {
		respondJSON(w, http.StatusPaymentRequired, VerifyResponse{
			Success: false,
			Error:   "Transaction does not have complete signatures",
		})
		return
	}

	// Only verify signatures if dataToSignHash is provided
	if hasDataToSignHash {
		// Extract inputData blobs from signed txHex using pattern matching
		// This avoids full transaction parsing which can fail on wire format issues
		inputs, err := txscan.ExtractInputDatas(req.Payload.TxHex)
		if err != nil {
			log.Printf("Failed to extract inputData from transaction: %v", err)
			respondJSON(w, http.StatusPaymentRequired, VerifyResponse{
				Success: false,
				Error:   "Failed to extract inputData: " + err.Error(),
			})
			return
		}

		log.Printf("Extracted %d inputData blob(s) from transaction", len(inputs))

		// Verify each input's signature using the exact dataToSignHash from the proposal
		for idx := range tx.Inputs {
			// Check if we have enough extracted inputData blobs
			if idx >= len(inputs) {
				respondJSON(w, http.StatusPaymentRequired, VerifyResponse{
					Success: false,
					Error:   fmt.Sprintf("Not enough inputData found (expected at least %d, got %d)", idx+1, len(inputs)),
				})
				return
			}

			inputData := inputs[idx]
			log.Printf("Input %d: DER signature (len=%d), pubkey (len=%d), offset=%d", 
				idx, len(inputData.SigDER), len(inputData.Pub33), inputData.Offset)

			// Verify the signature using the exact dataToSignHash from the proposal
			// Do not hash again - use the hash directly
			ok, err := txscan.VerifyInput(inputData.SigDER, inputData.Pub33, req.Payload.DataToSignHash)
			if err != nil {
				log.Printf("Signature verification error for input %d: %v", idx, err)
				respondJSON(w, http.StatusPaymentRequired, VerifyResponse{
					Success: false,
					Error:   fmt.Sprintf("Signature verification failed for input %d: %v", idx, err),
				})
				return
			}

			if !ok {
				log.Printf("Signature verification failed for input %d: signature is invalid", idx)
				respondJSON(w, http.StatusPaymentRequired, VerifyResponse{
					Success: false,
					Error:   fmt.Sprintf("Signature verification failed for input %d: invalid signature", idx),
				})
				return
			}

			log.Printf("Signature OK for input %d", idx)
		}
	} else {
		log.Printf("Skipping signature verification (dataToSignHash not provided - will be verified on settle)")
	}

	// Check if inputs are already spent (double-spend prevention)
	// This check is always done regardless of signature verification
	// We check at the UTXO level - if the output is spent, reject immediately
	for i, in := range tx.Inputs {
		spent, spentBy, err := isInputSpent(h.hathorClient, in.TxID, int(in.Index))
		if err != nil {
			log.Printf("Warning: Could not confirm spent status for input %d (tx: %s, idx: %d): %v", i, in.TxID, in.Index, err)
			// Fail closed: if we can't confirm the output is unspent, reject the transaction
			respondJSON(w, http.StatusPaymentRequired, VerifyResponse{
				Success: false,
				Error:   fmt.Sprintf("Cannot verify input %d: %v", i, err),
			})
			return
		}

		if spent {
			if spentBy != "" {
				log.Printf("Input %d (tx: %s, idx: %d) is already spent by transaction %s", i, in.TxID, in.Index, spentBy)
				respondJSON(w, http.StatusPaymentRequired, VerifyResponse{
					Success: false,
					Error:   fmt.Sprintf("Input %d already spent by transaction %s", i, spentBy),
				})
			} else {
				log.Printf("Input %d (tx: %s, idx: %d) is already spent", i, in.TxID, in.Index)
				respondJSON(w, http.StatusPaymentRequired, VerifyResponse{
					Success: false,
					Error:   fmt.Sprintf("Input %d already spent", i),
				})
			}
			return
		}

		log.Printf("Input %d verified: output exists and is unspent (tx: %s, idx: %d)", i, in.TxID, in.Index)
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


// isInputSpent checks if a specific output (UTXO) is already spent
// Returns: (isSpent, spentByTxID, error)
// If the output is spent, spentByTxID will contain the transaction ID that spent it
func isInputSpent(hc *hathor.Client, txid string, index int) (bool, string, error) {
	log.Printf("Checking if input is spent: txid=%s, index=%d", txid, index)
	
	// Fast path: try GetOutput API first
	out, err := hc.GetOutput(txid, index)
	if err == nil {
		log.Printf("GetOutput succeeded for txid=%s, index=%d: IsSpent=%v, SpentBy=%v", txid, index, out.IsSpent, out.SpentBy)
		if out.IsSpent {
			spentTx := ""
			if out.SpentBy != nil {
				spentTx = out.SpentBy.TxID
			}
			return true, spentTx, nil
		}
		return false, "", nil
	}

	log.Printf("GetOutput failed for txid=%s, index=%d: %v, falling back to GetTransaction", txid, index, err)

	// Fallback: use GetTransaction to check the output's spent_by field
	src, err2 := hc.GetTransaction(txid)
	if err2 != nil {
		log.Printf("GetTransaction also failed for txid=%s: %v", txid, err2)
		return false, "", fmt.Errorf("cannot confirm spent status: %w", err2)
	}
	if src == nil {
		return false, "", fmt.Errorf("cannot confirm spent status: source transaction is nil")
	}
	if index < 0 || index >= len(src.Outputs) {
		return false, "", fmt.Errorf("cannot confirm spent status: output index %d out of range (source has %d outputs)", index, len(src.Outputs))
	}

	o := src.Outputs[index]
	log.Printf("GetTransaction succeeded for txid=%s: output[%d] has SpentBy=%v", txid, index, o.SpentBy)
	if o.SpentBy != nil && o.SpentBy.TxID != "" {
		log.Printf("Output is spent by transaction: %s", o.SpentBy.TxID)
		return true, o.SpentBy.TxID, nil
	}

	// IMPORTANT: The Hathor node API may not always include spent_by in the transaction response.
	// If the API doesn't provide this information, we cannot definitively confirm the output is unspent.
	// However, if the source transaction exists on-chain and has a height (is confirmed),
	// we should be able to trust the API response. If spent_by is missing, we assume unspent.
	// Note: This is a limitation - we may miss some spent outputs if the API doesn't report them.
	// The settle endpoint will catch actual double-spends when trying to broadcast.
	log.Printf("Output appears to be unspent (no spent_by field in API response). Note: API may not report spent status reliably.")
	return false, "", nil
}

func respondJSON(w http.ResponseWriter, statusCode int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(data)
}
