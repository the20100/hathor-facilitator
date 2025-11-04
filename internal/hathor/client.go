package hathor

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	nodeURL     string
	walletURL   string
	walletID    string
	httpClient  *http.Client
}

func NewClient(nodeURL, walletURL, walletID string) *Client {
	return &Client{
		nodeURL:   nodeURL,
		walletURL: walletURL,
		walletID:  walletID,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// PushTransaction pushes a transaction to the Hathor network via the headless wallet
func (c *Client) PushTransaction(txHex string) (string, error) {
	url := fmt.Sprintf("%s/push-tx", c.walletURL)
	
	reqBody := map[string]string{
		"txHex": txHex,
	}
	bodyJSON, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}
	log.Printf("Push-tx request URL: %s", url)
	log.Printf("Push-tx request body: %s", string(bodyJSON))

	req, err := http.NewRequest("POST", url, bytes.NewReader(bodyJSON))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Wallet-Id", c.walletID)

	log.Printf("Push-tx headers: Content-Type=%s, X-Wallet-Id=%s", req.Header.Get("Content-Type"), req.Header.Get("X-Wallet-Id"))

	resp, err := c.httpClient.Do(req)
	if err != nil {
		log.Printf("Push-tx HTTP request failed: %v", err)
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Println("Error reading response: ", err)
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	log.Printf("Push-tx response status: %d", resp.StatusCode)
	log.Printf("Push-tx response body: %s", string(body))

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("push-tx failed with status %d: %s", resp.StatusCode, string(body))
	}

	var respJSON struct {
		Success bool   `json:"success"`
		Error   string `json:"error,omitempty"`
		Tx      struct {
			Hash string `json:"hash"`
		} `json:"tx"`
	}

	if err := json.Unmarshal(body, &respJSON); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	if !respJSON.Success {
		// Check if the error indicates a connection issue
		if strings.Contains(respJSON.Error, "ECONNREFUSED") || 
		   strings.Contains(respJSON.Error, "connection refused") ||
		   strings.Contains(respJSON.Error, "connect") {
			return "", fmt.Errorf("cannot connect to Hathor node (node may not be running): %s", respJSON.Error)
		}
		// Check for transaction validation errors
		if strings.Contains(respJSON.Error, "Invalid Opcode") ||
		   strings.Contains(respJSON.Error, "full validation failed") {
			return "", fmt.Errorf("transaction validation failed (transaction format may be incorrect): %s", respJSON.Error)
		}
		return "", fmt.Errorf("node error: %s", respJSON.Error)
	}

	return respJSON.Tx.Hash, nil
}

// GetTransaction retrieves transaction information from the node
func (c *Client) GetTransaction(txID string) (*TransactionInfo, error) {
	url := fmt.Sprintf("%s/v1a/transaction?id=%s", c.nodeURL, txID)

	resp, err := c.httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// Handle 404 specifically - transaction not found
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("transaction not found: %s", txID)
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := ioutil.ReadAll(resp.Body)
		return nil, fmt.Errorf("get transaction failed with status %d: %s", resp.StatusCode, string(body))
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// First unmarshal into a flexible map to handle varying response formats
	var rawData map[string]interface{}
	if err := json.Unmarshal(body, &rawData); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	// Check if response has "success" wrapper (some APIs wrap responses)
	var txData map[string]interface{}
	var metaData map[string]interface{}
	if success, ok := rawData["success"].(bool); ok && success {
		if tx, ok := rawData["tx"].(map[string]interface{}); ok {
			txData = tx
		} else {
			txData = rawData
		}
		// Extract meta if present
		if meta, ok := rawData["meta"].(map[string]interface{}); ok {
			metaData = meta
		}
	} else {
		txData = rawData
	}

	// Now extract what we need into our structured type
	var txInfo TransactionInfo
	
	if hash, ok := txData["hash"].(string); ok {
		txInfo.Hash = hash
	}
	
	if height, ok := txData["height"].(float64); ok {
		h := uint32(height)
		txInfo.Height = &h
	}
	
	// Parse outputs
	if outputsRaw, ok := txData["outputs"].([]interface{}); ok && len(outputsRaw) > 0 {
		txInfo.Outputs = make([]Output, len(outputsRaw))
		for i, outRaw := range outputsRaw {
			if outMap, ok := outRaw.(map[string]interface{}); ok {
				if val, ok := outMap["value"].(float64); ok {
					txInfo.Outputs[i].Value = uint64(val)
				}
				if td, ok := outMap["token_data"].(float64); ok {
					txInfo.Outputs[i].TokenData = uint8(td)
				}
				if script, ok := outMap["script"].(string); ok {
					txInfo.Outputs[i].Script = script
				}
				// Address might be directly in output or in decoded field
				if addr, ok := outMap["address"].(string); ok {
					txInfo.Outputs[i].Address = addr
				} else if decoded, ok := outMap["decoded"].(map[string]interface{}); ok {
					if addr, ok := decoded["address"].(string); ok {
						txInfo.Outputs[i].Address = addr
					}
				}
				// Check for spent_by in output (direct field)
				if spentBy, ok := outMap["spent_by"].(map[string]interface{}); ok {
					txInfo.Outputs[i].SpentBy = &SpentBy{}
					if txID, ok := spentBy["tx_id"].(string); ok {
						txInfo.Outputs[i].SpentBy.TxID = txID
					} else if txID, ok := spentBy["txId"].(string); ok {
						// Try camelCase variant
						txInfo.Outputs[i].SpentBy.TxID = txID
					}
					if idx, ok := spentBy["index"].(float64); ok {
						txInfo.Outputs[i].SpentBy.Index = int(idx)
					}
				}
			}
		}
	} else {
		// Log warning if no outputs found
		log.Printf("Warning: Transaction %s has no outputs in response", txID)
	}

	// Parse spent_outputs from meta (this is the primary way Hathor API indicates spent outputs)
	if metaData != nil {
		if spentOutputsRaw, ok := metaData["spent_outputs"]; ok {
			// spent_outputs can be either:
			// 1. An object: {"0": "txid", "1": "txid"}
			// 2. An array: [[0, ["txid"]], [1, []]]
			
			// Try object format first
			if spentOutputsObj, ok := spentOutputsRaw.(map[string]interface{}); ok {
				for outputIdxStr, txIDRaw := range spentOutputsObj {
					if txID, ok := txIDRaw.(string); ok && txID != "" {
						// Parse output index
						var outputIdx int
						if n, err := fmt.Sscanf(outputIdxStr, "%d", &outputIdx); err == nil && n == 1 {
							if outputIdx >= 0 && outputIdx < len(txInfo.Outputs) {
								if txInfo.Outputs[outputIdx].SpentBy == nil {
									txInfo.Outputs[outputIdx].SpentBy = &SpentBy{}
								}
								txInfo.Outputs[outputIdx].SpentBy.TxID = txID
								log.Printf("Output %d is spent by transaction %s (from meta.spent_outputs)", outputIdx, txID)
							}
						}
					}
				}
			} else if spentOutputsArr, ok := spentOutputsRaw.([]interface{}); ok {
				// Array format: [[0, ["txid"]], [1, []]]
				for _, entryRaw := range spentOutputsArr {
					if entry, ok := entryRaw.([]interface{}); ok && len(entry) >= 2 {
						if outputIdxFloat, ok := entry[0].(float64); ok {
							outputIdx := int(outputIdxFloat)
							if outputIdx >= 0 && outputIdx < len(txInfo.Outputs) {
								if txIDsArr, ok := entry[1].([]interface{}); ok && len(txIDsArr) > 0 {
									// Take the first txid from the array
									if txID, ok := txIDsArr[0].(string); ok && txID != "" {
										if txInfo.Outputs[outputIdx].SpentBy == nil {
											txInfo.Outputs[outputIdx].SpentBy = &SpentBy{}
										}
										txInfo.Outputs[outputIdx].SpentBy.TxID = txID
										log.Printf("Output %d is spent by transaction %s (from meta.spent_outputs array)", outputIdx, txID)
									}
								}
							}
						}
					}
				}
			}
		}
	}

	return &txInfo, nil
}

// GetOutput checks if an output exists and is unspent
// Fast path: tries the direct get_output node API endpoint first
// Fallback: uses GetTransaction to parse the transaction and find the output
func (c *Client) GetOutput(txID string, index int) (*OutputInfo, error) {
	// Fast path: try direct get_output endpoint first
	url := fmt.Sprintf("%s/get_output?tx_id=%s&index=%d", c.nodeURL, txID, index)
	resp, err := c.httpClient.Get(url)
	if err == nil {
		defer resp.Body.Close()
		
		if resp.StatusCode == http.StatusOK {
			var outputResp struct {
				Success   bool    `json:"success"`
				Spent     bool    `json:"spent"`
				Script    string  `json:"script"`
				TokenData uint8   `json:"token_data"`
				Value     uint64  `json:"value"`
				SpentBy   *SpentBy `json:"spent_by,omitempty"`
			}
			
			body, err := ioutil.ReadAll(resp.Body)
			if err == nil {
				if err := json.Unmarshal(body, &outputResp); err == nil && outputResp.Success {
					// Note: Address is not returned by get_output endpoint, but we don't need it for spent check
					return &OutputInfo{
						Value:     outputResp.Value,
						TokenData: outputResp.TokenData,
						Script:    outputResp.Script,
						Address:   "", // Not available from get_output endpoint
						IsSpent:   outputResp.Spent,
						SpentBy:   outputResp.SpentBy,
					}, nil
				}
			}
		}
		// If fast path fails, fall through to fallback
		log.Printf("get_output endpoint failed or returned error, falling back to GetTransaction")
	}

	// Fallback: use GetTransaction to parse the transaction and find the output
	txInfo, err := c.GetTransaction(txID)
	if err != nil {
		// If node API fails, try wallet decode API as fallback
		log.Printf("Node API failed for tx %s, trying wallet decode: %v", txID, err)
		return c.getOutputFromWallet(txID, index)
	}

	// Check if the output index is valid
	if index < 0 || index >= len(txInfo.Outputs) {
		// If node API doesn't have outputs, try wallet decode as fallback
		log.Printf("Node API has no output at index %d for tx %s, trying wallet decode", index, txID)
		return c.getOutputFromWallet(txID, index)
	}

	output := txInfo.Outputs[index]
	
	// Check if output is spent
	// Note: The node API may not always include spent_by field
	// If it's missing, we assume the output is unspent
	isSpent := false
	if output.SpentBy != nil && output.SpentBy.TxID != "" {
		isSpent = true
	}

	return &OutputInfo{
		Value:     output.Value,
		TokenData: output.TokenData,
		Script:    output.Script,
		Address:   output.Address,
		IsSpent:   isSpent,
		SpentBy:   output.SpentBy,
	}, nil
}

// getOutputFromWallet uses wallet decode API to get output information
func (c *Client) getOutputFromWallet(txID string, index int) (*OutputInfo, error) {
	// Note: We need the full transaction hex to decode it
	// For now, we'll return an error indicating we need the hex
	// In a production system, you might want to cache transaction data
	return nil, fmt.Errorf("wallet decode fallback requires transaction hex - node API response incomplete")
}

type TransactionInfo struct {
	Hash     string            `json:"hash"`
	Height   *uint32           `json:"height,omitempty"`
	Outputs  []Output          `json:"outputs"`
	Inputs   []Input           `json:"inputs"`
	Meta     *TransactionMeta  `json:"meta,omitempty"`
}

type Output struct {
	Value     uint64      `json:"value"`
	TokenData uint8       `json:"token_data"`
	Script    string      `json:"script"`
	Address   string      `json:"address"`
	SpentBy   *SpentBy    `json:"spent_by,omitempty"`
}

type SpentBy struct {
	TxID  string `json:"tx_id"`
	Index int    `json:"index"`
}

type Input struct {
	TxID  string `json:"tx_id"`
	Index int    `json:"index"`
}

type TransactionMeta struct {
	FirstBlock interface{} `json:"first_block,omitempty"` // Can be string or object, handle flexibly
}

type OutputInfo struct {
	Value     uint64
	TokenData uint8
	Script    string
	Address   string
	IsSpent   bool
	SpentBy   *SpentBy
}

// CheckConfirmations returns the number of confirmations for a transaction
func (c *Client) CheckConfirmations(txID string) (int, error) {
	txInfo, err := c.GetTransaction(txID)
	if err != nil {
		return 0, err
	}

	// If transaction has a height, it's confirmed
	if txInfo.Height != nil && *txInfo.Height > 0 {
		// Get current block height to calculate confirmations
		currentHeight, err := c.GetBestBlockHeight()
		if err != nil {
			// If we can't get current height, assume 1 confirmation if it has a height
			return 1, nil
		}
		if *txInfo.Height > 0 {
			confirmations := int(currentHeight - *txInfo.Height + 1)
			if confirmations < 0 {
				return 0, nil
			}
			return confirmations, nil
		}
	}

	return 0, nil
}

// GetBestBlockHeight gets the current best block height
func (c *Client) GetBestBlockHeight() (uint32, error) {
	url := fmt.Sprintf("%s/v1a/status", c.nodeURL)

	resp, err := c.httpClient.Get(url)
	if err != nil {
		return 0, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("get status failed with status %d", resp.StatusCode)
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("failed to read response: %w", err)
	}

	var status struct {
		BestBlockHeight uint32 `json:"best_block_height"`
	}

	if err := json.Unmarshal(body, &status); err != nil {
		return 0, fmt.Errorf("failed to parse response: %w", err)
	}

	return status.BestBlockHeight, nil
}

// DecodeTransactionHex decodes a hex transaction string to bytes
func DecodeTransactionHex(txHex string) ([]byte, error) {
	return hex.DecodeString(txHex)
}

// DecodeTransaction uses the headless wallet API to decode a transaction
func (c *Client) DecodeTransaction(txHex string) (*DecodedTransaction, error) {
	url := fmt.Sprintf("%s/wallet/decode", c.walletURL)

	reqBody := map[string]string{
		"txHex": txHex,
	}
	
	bodyJSON, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewReader(bodyJSON))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Wallet-Id", c.walletID)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("decode failed with status %d: %s", resp.StatusCode, string(body))
	}

	var respJSON struct {
		Success bool              `json:"success"`
		Error   string            `json:"error,omitempty"`
		Tx      DecodedTransaction `json:"tx"`
	}

	if err := json.Unmarshal(body, &respJSON); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	log.Println("Response JSON: ", respJSON)
	log.Println("Response Body: ", string(body))

	if !respJSON.Success {
		return nil, fmt.Errorf("wallet error: %s", respJSON.Error)
	}

	return &respJSON.Tx, nil
}

// Note: For signature verification, we use dataToSignHash from the DecodeTransaction response
// if available, otherwise we compute it from the raw transaction bytes.
// The wallet API may provide this in the decode response for already-signed transactions.

type DecodedTransaction struct {
	Version            int                  `json:"version"`
	Type               string               `json:"type"`
	Tokens             []interface{}        `json:"tokens"`
	Inputs             []DecodedInput        `json:"inputs"`
	Outputs            []DecodedOutput      `json:"outputs"`
	CompleteSignatures bool                 `json:"completeSignatures"`
	DataToSignHash     string               `json:"dataToSignHash,omitempty"` // Hash used for signing (if available)
}

type DecodedInput struct {
	TxID       string       `json:"txId"`
	Index      int          `json:"index"`
	Decoded    InputDecoded `json:"decoded"`
	Token      string       `json:"token"`
	Value      uint64       `json:"value"`
	TokenData  uint8       `json:"token_data"`
	Script     string       `json:"script"` // base64 encoded
	Signed     bool         `json:"signed"`
	Mine       bool         `json:"mine"`
}

type InputDecoded struct {
	Type     string `json:"type"`
	Address  string `json:"address"`
	Timelock *int64 `json:"timelock"`
}

type DecodedOutput struct {
	Value     uint64        `json:"value"`
	TokenData uint8         `json:"token_data"`
	Script    string        `json:"script"` // base64 encoded
	Type      string        `json:"type"`
	Decoded   OutputDecoded `json:"decoded"`
	Mine      bool          `json:"mine"`
	Token     string        `json:"token"`
}

type OutputDecoded struct {
	Address  string `json:"address"`
	Timelock *int64 `json:"timelock"`
}
