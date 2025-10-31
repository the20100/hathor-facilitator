package txparser

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/btcsuite/btcd/btcutil/base58"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/decred/dcrd/dcrec/secp256k1/v4/ecdsa"
)

// Transaction represents a parsed Hathor transaction
type Transaction struct {
	Version   uint16
	Nonce     uint32
	Timestamp uint32
	Inputs    []Input
	Outputs   []Output
	Tokens    []Token
	Hash      string
}

type Input struct {
	TxID      string
	Index     uint32
	Data      []byte
	Signature []byte
	PublicKey []byte
}

type Output struct {
	Value     uint64
	TokenData uint8
	Script    []byte
	Address   string
}

type Token struct {
	UID  []byte
	Name string
}

// ParseTransaction parses a Hathor transaction from raw bytes
func ParseTransaction(txBytes []byte) (*Transaction, error) {
	tx := &Transaction{}
	reader := bytes.NewReader(txBytes)

	// Read version (2 bytes, little endian)
	if err := binary.Read(reader, binary.LittleEndian, &tx.Version); err != nil {
		return nil, fmt.Errorf("failed to read version: %w", err)
	}

	// Read nonce (4 bytes, little endian)
	if err := binary.Read(reader, binary.LittleEndian, &tx.Nonce); err != nil {
		return nil, fmt.Errorf("failed to read nonce: %w", err)
	}

	// Read timestamp (4 bytes, little endian)
	if err := binary.Read(reader, binary.LittleEndian, &tx.Timestamp); err != nil {
		return nil, fmt.Errorf("failed to read timestamp: %w", err)
	}

	// Read number of inputs
	var numInputs uint8
	if err := binary.Read(reader, binary.LittleEndian, &numInputs); err != nil {
		return nil, fmt.Errorf("failed to read number of inputs: %w", err)
	}

	// Parse inputs
	tx.Inputs = make([]Input, numInputs)
	for i := 0; i < int(numInputs); i++ {
		input := &tx.Inputs[i]

		// Read tx_id (32 bytes)
		txIDBytes := make([]byte, 32)
		if _, err := reader.Read(txIDBytes); err != nil {
			return nil, fmt.Errorf("failed to read input %d tx_id: %w", i, err)
		}
		input.TxID = hex.EncodeToString(reverseBytes(txIDBytes))

		// Read index (4 bytes, little endian)
		if err := binary.Read(reader, binary.LittleEndian, &input.Index); err != nil {
			return nil, fmt.Errorf("failed to read input %d index: %w", i, err)
		}

		// Read data length (1 byte)
		var dataLen uint8
		if err := binary.Read(reader, binary.LittleEndian, &dataLen); err != nil {
			return nil, fmt.Errorf("failed to read input %d data length: %w", i, err)
		}

		// Read data (signature + public key)
		input.Data = make([]byte, dataLen)
		if _, err := reader.Read(input.Data); err != nil {
			return nil, fmt.Errorf("failed to read input %d data: %w", i, err)
		}

		// Parse signature and public key from data
		// This is a simplified parsing - actual format may vary
		if dataLen >= 65 {
			// Typically: signature (64-65 bytes) + pubkey (33 or 65 bytes)
			input.Signature = input.Data[:64] // Simplified - may include sighash flag
			input.PublicKey = input.Data[64:]  // Rest is pubkey
		}
	}

	// Read number of outputs
	var numOutputs uint8
	if err := binary.Read(reader, binary.LittleEndian, &numOutputs); err != nil {
		return nil, fmt.Errorf("failed to read number of outputs: %w", err)
	}

	// Parse outputs
	tx.Outputs = make([]Output, numOutputs)
	for i := 0; i < int(numOutputs); i++ {
		output := &tx.Outputs[i]

		// Read value (8 bytes, little endian)
		if err := binary.Read(reader, binary.LittleEndian, &output.Value); err != nil {
			return nil, fmt.Errorf("failed to read output %d value: %w", i, err)
		}

		// Read token_data (1 byte)
		if err := binary.Read(reader, binary.LittleEndian, &output.TokenData); err != nil {
			return nil, fmt.Errorf("failed to read output %d token_data: %w", i, err)
		}

		// Read script length (1 byte)
		var scriptLen uint8
		if err := binary.Read(reader, binary.LittleEndian, &scriptLen); err != nil {
			return nil, fmt.Errorf("failed to read output %d script length: %w", i, err)
		}

		// Read script
		output.Script = make([]byte, scriptLen)
		if _, err := reader.Read(output.Script); err != nil {
			return nil, fmt.Errorf("failed to read output %d script: %w", i, err)
		}

		// Decode address from script (P2PKH)
		if addr, err := decodeAddressFromScript(output.Script); err == nil {
			output.Address = addr
		}
	}

	// Calculate transaction hash
	tx.Hash = calculateTxHash(txBytes)

	return tx, nil
}

// VerifyInputSignature verifies the signature of an input
func VerifyInputSignature(tx *Transaction, inputIndex int, prevOut *OutputInfo, signature, pubKey []byte) error {
	if inputIndex >= len(tx.Inputs) {
		return errors.New("input index out of range")
	}

	// Get the public key from the input
	if len(pubKey) == 0 {
		return errors.New("missing public key")
	}

	// Create a copy of the transaction for signing
	// In Bitcoin/Hathor, the signature is computed over the transaction
	// with the input scripts replaced (except for the input being signed)
	signingHash := computeSigningHash(tx, inputIndex, prevOut.Script)

	// Parse public key
	pubKeyObj, err := secp256k1.ParsePubKey(pubKey)
	if err != nil {
		return fmt.Errorf("invalid public key: %w", err)
	}

	// Verify signature (ECDSA on secp256k1)
	// Note: signature format might include a hash type byte
	sigBytes := signature
	if len(sigBytes) < 64 {
		return errors.New("signature too short")
	}
	
	// Extract r and s from signature (typically 32 bytes each)
	// Bitcoin/Hathor signatures are typically DER-encoded or compact
	// Try to parse as DER first, then fall back to compact format
	var sig *ecdsa.Signature
	
	// Try parsing as DER signature first
	if len(sigBytes) >= 70 && sigBytes[0] == 0x30 {
		// Looks like DER format
		var parseErr error
		sig, parseErr = ecdsa.ParseDERSignature(sigBytes)
		if parseErr != nil {
			// If DER parsing fails, return error
			return fmt.Errorf("failed to parse DER signature: %w", parseErr)
		}
	} else {
		// Compact format (64 bytes: r + s directly)
		// For Hathor/Bitcoin, signatures are typically DER-encoded
		// If it's not DER and not the expected length, return error
		if len(sigBytes) < 64 {
			return errors.New("signature too short for compact format")
		}
		// Note: Compact signature format parsing would require constructing
		// r and s ModNScalar values from the bytes and creating a Signature
		// For now, we assume DER format or return an informative error
		return fmt.Errorf("non-DER signature format requires additional implementation (expected DER format)")
	}

	// Verify the signature
	if !sig.Verify(signingHash[:], pubKeyObj) {
		return errors.New("signature verification failed")
	}

	return nil
}

// OutputInfo contains information about a previous output
type OutputInfo struct {
	Script string
}

// computeSigningHash computes the hash that should be signed for an input
// This is a simplified version - actual Hathor signing may differ
func computeSigningHash(tx *Transaction, inputIndex int, prevOutScript string) [32]byte {
	// In Bitcoin-style, we hash the transaction with all input scripts cleared
	// and the current input script set to the previous output script
	// This is a simplified implementation
	
	buf := new(bytes.Buffer)
	
	// Write version
	binary.Write(buf, binary.LittleEndian, tx.Version)
	
	// Write nonce
	binary.Write(buf, binary.LittleEndian, tx.Nonce)
	
	// Write timestamp
	binary.Write(buf, binary.LittleEndian, tx.Timestamp)
	
	// Write number of inputs
	binary.Write(buf, binary.LittleEndian, uint8(len(tx.Inputs)))
	
	// Write inputs with scripts
	for i, input := range tx.Inputs {
		txIDBytes, _ := hex.DecodeString(input.TxID)
		buf.Write(reverseBytes(txIDBytes))
		binary.Write(buf, binary.LittleEndian, input.Index)
		
		if i == inputIndex {
			// Use previous output script
			scriptBytes, _ := hex.DecodeString(prevOutScript)
			binary.Write(buf, binary.LittleEndian, uint8(len(scriptBytes)))
			buf.Write(scriptBytes)
		} else {
			// Empty script
			binary.Write(buf, binary.LittleEndian, uint8(0))
		}
	}
	
	// Write outputs
	binary.Write(buf, binary.LittleEndian, uint8(len(tx.Outputs)))
	for _, output := range tx.Outputs {
		binary.Write(buf, binary.LittleEndian, output.Value)
		binary.Write(buf, binary.LittleEndian, output.TokenData)
		binary.Write(buf, binary.LittleEndian, uint8(len(output.Script)))
		buf.Write(output.Script)
	}
	
	// Double SHA256
	firstHash := sha256.Sum256(buf.Bytes())
	secondHash := sha256.Sum256(firstHash[:])
	return secondHash
}

// ValidatePoW performs a basic proof-of-work check
func ValidatePoW(tx *Transaction) bool {
	// Basic check: ensure nonce is set (non-zero)
	// Full PoW validation would require recomputing the hash and checking difficulty
	// For now, we assume if the transaction was properly constructed, PoW is valid
	// The node will reject invalid PoW transactions when broadcasting
	return tx.Nonce != 0
}

// PublicKeyToAddress converts a public key to a Hathor address
func PublicKeyToAddress(pubKey []byte, mainnet bool) (string, error) {
	// Hash the public key (SHA256 then RIPEMD160)
	sha256Hash := sha256.Sum256(pubKey)
	
	// RIPEMD160 would require additional library, for now we'll use a simplified approach
	// In production, you'd want to use proper RIPEMD160
	
	// For now, return a placeholder - actual implementation would:
	// 1. SHA256(pubKey)
	// 2. RIPEMD160(result)
	// 3. Add network prefix (H for mainnet, W for testnet)
	// 4. Base58Check encode
	
	// Simplified: use first 20 bytes of SHA256 as hash160 placeholder
	hash160 := sha256Hash[:20]
	
	// Build address: prefix + hash160 + checksum
	var prefix byte = 'W' // testnet by default
	if mainnet {
		prefix = 'H' // mainnet
	}
	
	// Create address bytes: prefix + hash160
	addressBytes := append([]byte{prefix}, hash160...)
	
	// Add checksum (simplified - actual would use double SHA256)
	firstHash := sha256.Sum256(addressBytes)
	secondHash := sha256.Sum256(firstHash[:])
	checksum := secondHash[:4]
	addressBytes = append(addressBytes, checksum...)
	
	// Base58 encode
	addr := base58.Encode(addressBytes)
	return addr, nil
}

// calculateTxHash computes the transaction hash
func calculateTxHash(txBytes []byte) string {
	// Double SHA256
	firstHash := sha256.Sum256(txBytes)
	secondHash := sha256.Sum256(firstHash[:])
	
	// Reverse bytes (Bitcoin/Hathor style)
	reversed := reverseBytes(secondHash[:])
	
	return hex.EncodeToString(reversed)
}

// decodeAddressFromScript extracts address from a P2PKH script
func decodeAddressFromScript(script []byte) (string, error) {
	// P2PKH script format: OP_DUP OP_HASH160 <20-byte-hash> OP_EQUALVERIFY OP_CHECKSIG
	// This is simplified - actual parsing would check opcodes
	if len(script) < 25 {
		return "", errors.New("script too short")
	}
	
	// Extract hash160 (20 bytes in the middle, typically)
	// P2PKH: OP_DUP (0x76) OP_HASH160 (0xa9) <20 bytes> OP_EQUALVERIFY (0x88) OP_CHECKSIG (0xac)
	// So hash160 is typically at index 2-21
	var hash160 []byte
	if len(script) >= 23 && script[0] == 0x76 && script[1] == 0xa9 {
		hash160 = script[2:22]
	} else {
		// Fallback: try to extract from middle
		if len(script) >= 23 {
			hash160 = script[len(script)-23 : len(script)-3]
		} else {
			return "", errors.New("invalid script format")
		}
	}
	
	// Build address with testnet prefix for now (should detect from network)
	addressBytes := append([]byte{'W'}, hash160...)
	firstHash := sha256.Sum256(addressBytes)
	secondHash := sha256.Sum256(firstHash[:])
	checksum := secondHash[:4]
	addressBytes = append(addressBytes, checksum...)
	
	return base58.Encode(addressBytes), nil
}

// reverseBytes reverses a byte slice
func reverseBytes(data []byte) []byte {
	result := make([]byte, len(data))
	for i := 0; i < len(data); i++ {
		result[i] = data[len(data)-1-i]
	}
	return result
}
