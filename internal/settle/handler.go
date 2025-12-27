package settle

import (
	"encoding/base64"
	"encoding/json"
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
	log.Printf("=== Settle Request Received ===")
	log.Printf("Method: %s", r.Method)
	log.Printf("Path: %s", r.URL.Path)
	log.Printf("Content-Type: %s", r.Header.Get("Content-Type"))
	log.Printf("Content-Length: %s", r.Header.Get("Content-Length"))
	log.Printf("RemoteAddr: %s", r.RemoteAddr)
	
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
	
	if r.Method != http.MethodPost {
		log.Printf("Rejected: Method not allowed (got %s, expected POST)", r.Method)
		respondJSON(w, http.StatusPaymentRequired, SettleResponse{
			Success: false,
			Error:   "Method not allowed",
		})
		return
	}

	var req SettleRequest
	var err error

	// Check if payment payload is in X-PAYMENT header (base64-encoded)
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
			respondJSON(w, http.StatusPaymentRequired, SettleResponse{
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
			respondJSON(w, http.StatusPaymentRequired, SettleResponse{
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
			respondJSON(w, http.StatusPaymentRequired, SettleResponse{
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
			respondJSON(w, http.StatusPaymentRequired, SettleResponse{
				Success: false,
				Error:   "Bad request JSON: " + err.Error(),
			})
			return
		}
		
		log.Printf("Successfully parsed JSON from body: scheme=%s, network=%s", req.Scheme, req.Network)
	}

	// Quick sanity check
	log.Printf("Validating request: scheme=%s, network=%s", req.Scheme, req.Network)
	if req.Scheme != "exact" {
		log.Printf("Rejected: Unsupported scheme: %s (expected 'exact')", req.Scheme)
		respondJSON(w, http.StatusPaymentRequired, SettleResponse{
			Success: false,
			Error:   "Unsupported scheme: only 'exact' is supported",
		})
		return
	}

	if req.Network != "hathor-mainnet" && req.Network != "hathor-testnet" {
		log.Printf("Rejected: Unsupported network: %s (expected hathor-mainnet or hathor-testnet)", req.Network)
		respondJSON(w, http.StatusPaymentRequired, SettleResponse{
			Success: false,
			Error:   "Unsupported network: must be hathor-mainnet or hathor-testnet",
		})
		return
	}

	// First verify the transaction can be decoded (similar to /verify)
	// This ensures we have a valid transaction before attempting to broadcast
	log.Printf("Decoding transaction before broadcast...")
	log.Printf("TxHex length: %d characters", len(req.Payload.TxHex))
	if len(req.Payload.TxHex) > 200 {
		log.Printf("TxHex (first 200 chars): %s...", req.Payload.TxHex[:200])
	} else {
		log.Printf("TxHex: %s", req.Payload.TxHex)
	}
	
	decodedTx, err := h.hathorClient.DecodeTransaction(req.Payload.TxHex)
	if err != nil {
		log.Printf("Settle: Failed to decode transaction before broadcast: %v", err)
		respondJSON(w, http.StatusPaymentRequired, SettleResponse{
			Success: false,
			Error:   "Invalid transaction format: " + err.Error(),
		})
		return
	}
	
	log.Printf("Transaction decoded successfully: CompleteSignatures=%v", decodedTx.CompleteSignatures)

	if !decodedTx.CompleteSignatures {
		log.Printf("Rejected: Transaction does not have complete signatures")
		respondJSON(w, http.StatusPaymentRequired, SettleResponse{
			Success: false,
			Error:   "Transaction does not have complete signatures",
		})
		return
	}

	// Push the transaction to the Hathor network
	log.Printf("Broadcasting transaction to Hathor network...")
	txID, err := h.hathorClient.PushTransaction(req.Payload.TxHex)
	if err != nil {
		log.Printf("Broadcast failed: %v", err)
		respondJSON(w, http.StatusPaymentRequired, SettleResponse{
			Success: false,
			Error:   "Broadcast failed: " + err.Error(),
		})
		return
	}
	
	log.Printf("Transaction broadcast successfully: txID=%s", txID)

	// Wait for confirmation only if minConfirmations > 0
	if h.minConfirmations > 0 {
		log.Printf("Waiting for %d confirmations (timeout: 60s, polling interval: 5s)...", h.minConfirmations)
		confCount := 0
		timeout := time.After(60 * time.Second)
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-timeout:
				// Timeout - transaction was broadcast but confirmation timeout
				// Still return success since transaction was accepted
				log.Printf("Confirmation timeout reached (60s), but transaction was broadcast successfully. Current confirmations: %d/%d", confCount, h.minConfirmations)
				respondJSON(w, http.StatusOK, SettleResponse{
					Success: true,
					TxID:    txID,
				})
				return

			case <-ticker.C:
				log.Printf("Checking confirmations for txID=%s...", txID)
				confirmations, err := h.hathorClient.CheckConfirmations(txID)
				if err != nil {
					// Log error but continue polling
					log.Printf("Error checking confirmations: %v", err)
					continue
				}

				confCount = confirmations
				log.Printf("Current confirmations: %d/%d", confCount, h.minConfirmations)
				if confCount >= h.minConfirmations {
					// Sufficient confirmations
					log.Printf("Sufficient confirmations reached: %d/%d", confCount, h.minConfirmations)
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
		log.Printf("Skipping confirmation wait (minConfirmations=0), returning success immediately")
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
