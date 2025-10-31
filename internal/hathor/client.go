package hathor

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
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

	req, err := http.NewRequest("POST", url, bytes.NewReader(bodyJSON))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Wallet-Id", c.walletID)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

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

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get transaction failed with status %d", resp.StatusCode)
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var txInfo TransactionInfo
	if err := json.Unmarshal(body, &txInfo); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &txInfo, nil
}

// GetOutput checks if an output exists and is unspent
func (c *Client) GetOutput(txID string, index int) (*OutputInfo, error) {
	// First get the transaction
	txInfo, err := c.GetTransaction(txID)
	if err != nil {
		return nil, err
	}

	// Check if the output index is valid
	if index < 0 || index >= len(txInfo.Outputs) {
		return nil, fmt.Errorf("output index %d out of range", index)
	}

	output := txInfo.Outputs[index]
	
	// Check if output is spent
	isSpent := output.SpentBy != nil && output.SpentBy.TxID != ""

	return &OutputInfo{
		Value:     output.Value,
		TokenData: output.TokenData,
		Script:    output.Script,
		Address:   output.Address,
		IsSpent:   isSpent,
		SpentBy:   output.SpentBy,
	}, nil
}

type TransactionInfo struct {
	Hash     string       `json:"hash"`
	Height   *uint32      `json:"height,omitempty"`
	Outputs  []Output     `json:"outputs"`
	Inputs   []Input      `json:"inputs"`
	Meta     *TransactionMeta `json:"meta,omitempty"`
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
	FirstBlock *struct {
		Height uint32 `json:"height"`
	} `json:"first_block,omitempty"`
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
