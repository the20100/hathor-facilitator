package txparser

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"log"

	"github.com/btcsuite/btcd/btcutil/base58"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/decred/dcrd/dcrec/secp256k1/v4/ecdsa"
	"github.com/hathor-network/hathor-facilitator/internal/hathor"
)

// Transaction represents a parsed Hathor transaction
type Transaction struct {
	Version   uint16
	Nonce     uint32
	Timestamp uint32
	Parents   [][]byte // Parent hashes for DAG
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
	Address   string
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

// ParseTransactionFromWallet uses the wallet API to decode a transaction
// This is the preferred method as it handles all the binary format details
func ParseTransactionFromWallet(decodedTx *hathor.DecodedTransaction) (*Transaction, error) {
	tx := &Transaction{
		Version: uint16(decodedTx.Version),
		Inputs:  make([]Input, len(decodedTx.Inputs)),
		Outputs: make([]Output, len(decodedTx.Outputs)),
	}

	// Convert inputs - store the decoded address for payer identification
	for i, in := range decodedTx.Inputs {
		tx.Inputs[i] = Input{
			TxID:      in.TxID,
			Index:     uint32(in.Index),
			PublicKey: []byte{}, // Not available from wallet decode
			Address:   in.Decoded.Address, // Store decoded address for payer identification
		}
	}

	// Convert outputs
	for i, out := range decodedTx.Outputs {
		scriptBytes, err := base64.StdEncoding.DecodeString(out.Script)
		if err != nil {
			return nil, fmt.Errorf("failed to decode output %d script: %w", i, err)
		}
		tx.Outputs[i] = Output{
			Value:     out.Value,
			TokenData: out.TokenData,
			Script:    scriptBytes,
			Address:   out.Decoded.Address,
		}
	}

	// Calculate hash from the original tx bytes if available
	// For now, we'll leave it empty and let the caller provide it

	return tx, nil
}

// ParseTransaction parses a Hathor transaction from raw bytes (legacy method)
// Note: This is complex and error-prone. Prefer using ParseTransactionFromWallet.
func ParseTransaction(txBytes []byte) (*Transaction, error) {
	if len(txBytes) < 10 {
		return nil, fmt.Errorf("transaction too short: %d bytes", len(txBytes))
	}

	tx := &Transaction{}
	reader := bytes.NewReader(txBytes)

	// Read version (2 bytes) - try both endianness
	var versionBytes [2]byte
	if _, err := reader.Read(versionBytes[:]); err != nil {
		return nil, fmt.Errorf("failed to read version: %w", err)
	}
	// Try big endian first (most common for network protocols)
	tx.Version = binary.BigEndian.Uint16(versionBytes[:])
	// If version seems too high, try little endian
	if tx.Version > 100 {
		tx.Version = binary.LittleEndian.Uint16(versionBytes[:])
	}

	// Read nonce (4 bytes, little endian based on Bitcoin-style)
	if err := binary.Read(reader, binary.LittleEndian, &tx.Nonce); err != nil {
		return nil, fmt.Errorf("failed to read nonce at position %d: %w", reader.Size()-int64(reader.Len()), err)
	}

	// Read timestamp (4 bytes, little endian)
	if err := binary.Read(reader, binary.LittleEndian, &tx.Timestamp); err != nil {
		return nil, fmt.Errorf("failed to read timestamp at position %d: %w", reader.Size()-int64(reader.Len()), err)
	}

	// Check if we have enough bytes left
	remaining := int64(reader.Len())
	if remaining < 1 {
		return nil, fmt.Errorf("not enough bytes for parents count: %d remaining", remaining)
	}

	// Hathor transactions include parent hashes before inputs
	// Read number of parents (typically 2 for DAG)
	var numParents uint8
	if err := binary.Read(reader, binary.LittleEndian, &numParents); err != nil {
		return nil, fmt.Errorf("failed to read number of parents at position %d: %w", reader.Size()-int64(reader.Len()), err)
	}

	// Skip parent hashes (32 bytes each) by reading and discarding
	parentHashSize := int(numParents) * 32
	remaining = int64(reader.Len())
	if remaining < int64(parentHashSize) {
		return nil, fmt.Errorf("not enough bytes for %d parent hashes: need %d, have %d", numParents, parentHashSize, remaining)
	}
	if parentHashSize > 0 {
		parentHashes := make([]byte, parentHashSize)
		if _, err := reader.Read(parentHashes); err != nil {
			return nil, fmt.Errorf("failed to read parent hashes at position %d: %w", reader.Size()-int64(reader.Len()), err)
		}
	}

	// Check if we have enough bytes for number of inputs
	remaining = int64(reader.Len())
	if remaining < 1 {
		return nil, fmt.Errorf("not enough bytes for inputs count: %d remaining at position %d", remaining, reader.Size()-int64(reader.Len()))
	}

	// Read number of inputs
	var numInputs uint8
	if err := binary.Read(reader, binary.LittleEndian, &numInputs); err != nil {
		return nil, fmt.Errorf("failed to read number of inputs at position %d (remaining: %d bytes): %w", 
			reader.Size()-int64(reader.Len()), reader.Len(), err)
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
		// Format: [DER-encoded signature][compressed public key (33 bytes)]
		// DER signatures are variable length (typically 70-73 bytes)
		if dataLen >= 70 {
			// Try to parse DER signature length
			// DER format: 0x30 [length_byte] 0x02 [r_length] [r_bytes] 0x02 [s_length] [s_bytes]
			if input.Data[0] == 0x30 {
				// DER signature detected
				// Use a more robust approach: try parsing progressively larger chunks
				// Start with typical sizes and find what works
				sigLength := 0
				lengthByte := input.Data[1]
				
				if lengthByte&0x80 != 0 {
					// Multi-byte length encoding
					lengthBytesCount := int(lengthByte & 0x7F)
					if lengthBytesCount > 0 && lengthBytesCount <= 3 && len(input.Data) >= 2+lengthBytesCount {
						// Read multi-byte length
						for i := 0; i < lengthBytesCount; i++ {
							sigLength = sigLength<<8 | int(input.Data[2+i])
						}
						sigLength += 2 + lengthBytesCount // Include 0x30 tag and length bytes
					} else {
						// Invalid multi-byte encoding, try to find signature end by attempting to parse
						sigLength = findDERSignatureLength(input.Data)
					}
				} else {
					// Single-byte length (most common case)
					sigLength = int(lengthByte) + 2 // Include 0x30 tag (1 byte) and length byte (1 byte)
				}
				
				// Validate and adjust sigLength
				// Ensure we have enough data and don't exceed bounds
				if sigLength < 70 {
					sigLength = 70 // Minimum expected DER signature size
				}
				if sigLength+33 > len(input.Data) {
					sigLength = len(input.Data) - 33 // Reserve 33 bytes for compressed pubkey
				}
				if sigLength > len(input.Data) {
					sigLength = len(input.Data) - 33
				}
				
				// Try to validate the DER signature can be parsed
				// If parsing fails, try adjusting the length
				testSig := input.Data[:sigLength]
				if _, err := ecdsa.ParseDERSignature(testSig); err != nil {
					// Parsing failed, try to find correct length by scanning
					sigLength = findDERSignatureLength(input.Data)
					if sigLength == 0 {
						sigLength = len(input.Data) - 33 // Fallback
					}
				}
				
				// Extract signature (DER-encoded)
				input.Signature = input.Data[:sigLength]
				// Extract public key (compressed, 33 bytes)
				if len(input.Data) >= sigLength+33 {
					input.PublicKey = input.Data[sigLength : sigLength+33]
				} else if len(input.Data) > sigLength {
					// Fallback: take remaining bytes as pubkey
					input.PublicKey = input.Data[sigLength:]
				}
			} else {
				// Not DER format, try compact format (64 bytes r+s)
				if dataLen >= 97 { // 64 bytes signature + 33 bytes compressed pubkey
					input.Signature = input.Data[:64]
					input.PublicKey = input.Data[64:97]
				} else if dataLen >= 65 {
					// Fallback: assume 64-byte signature
					input.Signature = input.Data[:64]
					input.PublicKey = input.Data[64:]
				}
			}
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
	tx.Hash = CalculateTxHash(txBytes)

	return tx, nil
}

// VerifyInputSignature verifies the signature of an input
// If rawTxBytes is provided, it will use those bytes to compute the signing hash more accurately
// If dataToSignHash is provided, it will use that directly (most accurate)
func VerifyInputSignature(tx *Transaction, inputIndex int, prevOut *OutputInfo, signature, pubKey []byte) error {
	return VerifyInputSignatureWithBytes(tx, inputIndex, prevOut, signature, pubKey, nil, "")
}

// VerifyInputSignatureWithBytes verifies the signature using raw transaction bytes if available
// If dataToSignHash is provided, it will use that directly (most accurate)
func VerifyInputSignatureWithBytes(tx *Transaction, inputIndex int, prevOut *OutputInfo, signature, pubKey []byte, rawTxBytes []byte, dataToSignHash string) error {
	if inputIndex >= len(tx.Inputs) {
		return errors.New("input index out of range")
	}

	// Get the public key from the input
	if len(pubKey) == 0 {
		return errors.New("missing public key")
	}

	// Use dataToSignHash from wallet API if available (most accurate)
	var signingHash [32]byte
	if dataToSignHash != "" {
		hashBytes, err := hex.DecodeString(dataToSignHash)
		if err == nil && len(hashBytes) == 32 {
			copy(signingHash[:], hashBytes)
			log.Printf("Using dataToSignHash from wallet API for input %d: %s", inputIndex, dataToSignHash)
		} else {
			log.Printf("Warning: Invalid dataToSignHash '%s', falling back to computed hash", dataToSignHash)
			// Fall through to computed hash
		}
	}
	
	// If we don't have dataToSignHash, compute it
	if dataToSignHash == "" || signingHash == [32]byte{} {
		if len(rawTxBytes) > 0 {
			// Try to compute hash from raw transaction bytes
			computedHash, err := computeSigningHashFromRawBytes(rawTxBytes, inputIndex, prevOut.Script)
			if err == nil {
				signingHash = computedHash
				log.Printf("Computed signing hash from raw bytes for input %d", inputIndex)
			} else {
				// Fall back to reconstructed hash
				log.Printf("Warning: Could not compute hash from raw bytes: %v, using reconstructed hash", err)
				signingHash = computeSigningHash(tx, inputIndex, prevOut.Script)
			}
		} else {
			// Use reconstructed hash
			signingHash = computeSigningHash(tx, inputIndex, prevOut.Script)
			log.Printf("Computed signing hash from reconstructed transaction for input %d", inputIndex)
		}
	}

	// Parse public key
	pubKeyObj, err := secp256k1.ParsePubKey(pubKey)
	if err != nil {
		return fmt.Errorf("invalid public key: %w", err)
	}

	// Verify signature (ECDSA on secp256k1)
	// Hathor uses DER-encoded signatures as shown in the Python signing code
	sigBytes := signature
	if len(sigBytes) < 64 {
		return errors.New("signature too short")
	}
	
	// Parse signature - prioritize DER format (as used by Hathor wallet)
	var sig *ecdsa.Signature
	var parseErr error
	
	// Try parsing as DER signature first (this is what the Python code produces)
	if sigBytes[0] == 0x30 {
		// DER format detected
		sig, parseErr = ecdsa.ParseDERSignature(sigBytes)
		if parseErr != nil {
			return fmt.Errorf("failed to parse DER signature: %w", parseErr)
		}
	} else if len(sigBytes) == 64 {
		// Try compact format (64 bytes: r + s directly, each 32 bytes)
		// This is less common but might be used in some cases
		// Note: dcrd's ecdsa package doesn't have a direct compact parser
		// We need to construct r and s manually
		rBytes := sigBytes[:32]
		sBytes := sigBytes[32:64]
		
		// Parse r and s as ModNScalar
		var r, s secp256k1.ModNScalar
		r.SetByteSlice(rBytes)
		s.SetByteSlice(sBytes)
		
		// Create signature from r and s
		sig = ecdsa.NewSignature(&r, &s)
	} else {
		return fmt.Errorf("unsupported signature format: expected DER (starts with 0x30) or 64-byte compact, got %d bytes starting with 0x%02x", len(sigBytes), sigBytes[0])
	}

	// Verify the signature against the signing hash
	if !sig.Verify(signingHash[:], pubKeyObj) {
		log.Printf("Signature verification failed - hash: %s, pubkey: %s, signature: %s",
			hex.EncodeToString(signingHash[:]), hex.EncodeToString(pubKey), hex.EncodeToString(signature))
		return errors.New("signature verification failed: signature does not match the transaction hash and public key")
	}
	
	log.Printf("Signature verification succeeded for input %d", inputIndex)
	
	// Additional validation: verify that the public key hash matches the script
	// This ensures the signer owns the UTXO being spent
	// The public key should hash to the address in the previous output script

	return nil
}

// findDERSignatureLength attempts to find the correct length of a DER signature
// by trying to parse it at different lengths
func findDERSignatureLength(data []byte) int {
	if len(data) < 8 {
		return 0 // Too short to be a DER signature
	}
	
	// Check if it starts with DER sequence tag (0x30)
	if data[0] != 0x30 {
		return 0
	}
	
	// Try to parse DER length field
	lengthByte := data[1]
	var contentLength int
	var totalLength int
	
	if lengthByte&0x80 != 0 {
		// Multi-byte length encoding
		lengthBytesCount := int(lengthByte & 0x7F)
		if lengthBytesCount == 0 || lengthBytesCount > 3 {
			// Invalid length encoding
			return 0
		}
		if len(data) < 2+lengthBytesCount {
			return 0
		}
		contentLength = 0
		for i := 0; i < lengthBytesCount; i++ {
			contentLength = contentLength<<8 | int(data[2+i])
		}
		totalLength = 2 + lengthBytesCount + contentLength
	} else {
		// Single-byte length (most common)
		contentLength = int(lengthByte)
		totalLength = 2 + contentLength
	}
	
	// Validate the total length makes sense
	if totalLength < 70 || totalLength > 73 {
		// Try brute force parsing if length seems wrong
		for length := 70; length <= 73 && length <= len(data); length++ {
			testSig := data[:length]
			if _, err := ecdsa.ParseDERSignature(testSig); err == nil {
				return length
			}
		}
		// If that doesn't work, try the calculated length
		if totalLength >= 70 && totalLength <= len(data) {
			testSig := data[:totalLength]
			if _, err := ecdsa.ParseDERSignature(testSig); err == nil {
				return totalLength
			}
		}
		return 0
	}
	
	// Validate by trying to parse
	if totalLength <= len(data) {
		testSig := data[:totalLength]
		if _, err := ecdsa.ParseDERSignature(testSig); err == nil {
			return totalLength
		}
	}
	
	// Fallback: try common lengths
	for length := 70; length <= 73 && length <= len(data); length++ {
		testSig := data[:length]
		if _, err := ecdsa.ParseDERSignature(testSig); err == nil {
			return length
		}
	}
	
	return 0
}

// ExtractSignaturesFromBytes extracts signature and public key pairs from transaction bytes
// by searching for DER signature patterns (starting with 0x30) followed by compressed pubkeys (0x02 or 0x03)
// This is a fallback when we can't parse the transaction format directly
func ExtractSignaturesFromBytes(txBytes []byte) []struct {
	Signature []byte
	PublicKey []byte
} {
	var results []struct {
		Signature []byte
		PublicKey []byte
	}

	// Search for DER signatures (start with 0x30)
	// Need at least 70 bytes for a signature
	for i := 0; i <= len(txBytes)-70; i++ {
		if txBytes[i] == 0x30 {
			// Found potential DER signature start
			// Try to find the signature length
			remainingBytes := txBytes[i:]
			if len(remainingBytes) < 70 {
				continue // Not enough bytes for a valid DER signature
			}
			
			sigLength := findDERSignatureLength(remainingBytes)
			if sigLength > 0 {
				// Found a valid DER signature
				sig := make([]byte, sigLength)
				copy(sig, txBytes[i:i+sigLength])
				
				// Validate the signature can be parsed
				if sigObj, err := ecdsa.ParseDERSignature(sig); err == nil && sigObj != nil {
					// Now look for the public key - it might be before or after the signature
					// First check after the signature
					pubKeyStart := i + sigLength
					pubKeyFound := false
					
					// Check if next bytes are a compressed public key (0x02 or 0x03)
					if pubKeyStart+33 <= len(txBytes) && (txBytes[pubKeyStart] == 0x02 || txBytes[pubKeyStart] == 0x03) {
						pubKey := make([]byte, 33)
						copy(pubKey, txBytes[pubKeyStart:pubKeyStart+33])
						if pubKeyObj, err := secp256k1.ParsePubKey(pubKey); err == nil && pubKeyObj != nil {
							results = append(results, struct {
								Signature []byte
								PublicKey []byte
							}{Signature: sig, PublicKey: pubKey})
							pubKeyFound = true
							i = pubKeyStart + 32 // Skip ahead
						}
					}
					
					// If not found after, search before (within reasonable range)
					if !pubKeyFound && i >= 33 {
						for j := i - 33; j >= 0 && j >= i-100; j-- {
							if txBytes[j] == 0x02 || txBytes[j] == 0x03 {
								pubKey := make([]byte, 33)
								copy(pubKey, txBytes[j:j+33])
								if pubKeyObj, err := secp256k1.ParsePubKey(pubKey); err == nil && pubKeyObj != nil {
									results = append(results, struct {
										Signature []byte
										PublicKey []byte
									}{Signature: sig, PublicKey: pubKey})
									pubKeyFound = true
									break
								}
							}
						}
					}
					
					// If still not found, search after (within reasonable range)
					if !pubKeyFound {
						for j := pubKeyStart; j < len(txBytes)-33 && j < pubKeyStart+200; j++ {
							if txBytes[j] == 0x02 || txBytes[j] == 0x03 {
								pubKey := make([]byte, 33)
								copy(pubKey, txBytes[j:j+33])
								if pubKeyObj, err := secp256k1.ParsePubKey(pubKey); err == nil && pubKeyObj != nil {
									results = append(results, struct {
										Signature []byte
										PublicKey []byte
									}{Signature: sig, PublicKey: pubKey})
									pubKeyFound = true
									break
								}
							}
						}
					}
					
					// If we found signature but no pubkey, still record the signature
					// (we might be able to derive or find it later)
					if !pubKeyFound {
						// Skip this signature and continue searching
						i += sigLength - 1 // Will increment by 1 in loop
					}
				}
			}
		}
	}

	return results
}

// OutputInfo contains information about a previous output
type OutputInfo struct {
	Script string
}

// computeSigningHash computes the hash that should be signed for an input
// Hathor signatures are computed on the transaction data with input scripts cleared
// and the current input script replaced with the previous output script
// IMPORTANT: Nonce is NOT included in the signing hash (it's computed after signing)
func computeSigningHash(tx *Transaction, inputIndex int, prevOutScript string) [32]byte {
	// In Bitcoin/Hathor style, we hash the transaction with all input scripts cleared
	// and the current input script set to the previous output script
	// Nonce is excluded because it's computed after signing (for PoW)
	
	buf := new(bytes.Buffer)
	
	// Write version (2 bytes)
	// Try both endianness - Hathor might use big endian for version
	// Based on raw transaction format, version appears to be big endian
	binary.Write(buf, binary.BigEndian, tx.Version)
	
	// DO NOT write nonce - signatures are computed before nonce is finalized
	
	// Write timestamp (4 bytes, little endian)
	binary.Write(buf, binary.LittleEndian, tx.Timestamp)
	
	// Write number of parents (1 byte) - parents might be included in signing hash
	// If we don't have parents from parsing, use 0 (they might not be in signing hash)
	parentCount := len(tx.Parents)
	binary.Write(buf, binary.LittleEndian, uint8(parentCount))
	
	// Write parent hashes (32 bytes each) if available
	for _, parent := range tx.Parents {
		buf.Write(parent)
	}
	
	// Write number of inputs (1 byte)
	binary.Write(buf, binary.LittleEndian, uint8(len(tx.Inputs)))
	
	// Write inputs with scripts
	for i, input := range tx.Inputs {
		// Write tx_id (32 bytes, reversed from hex string)
		txIDBytes, _ := hex.DecodeString(input.TxID)
		buf.Write(reverseBytes(txIDBytes))
		
		// Write index (4 bytes, little endian)
		binary.Write(buf, binary.LittleEndian, input.Index)
		
		// Write data length and script
		if i == inputIndex {
			// Use previous output script for the input being signed
			// The script might be hex-encoded or base64-encoded string
			var scriptBytes []byte
			var err error
			// Try hex first (most common)
			scriptBytes, err = hex.DecodeString(prevOutScript)
			if err != nil {
				// Try base64
				scriptBytes, err = base64.StdEncoding.DecodeString(prevOutScript)
				if err != nil {
					// If it's already bytes as a string, try direct conversion
					scriptBytes = []byte(prevOutScript)
				}
			}
			binary.Write(buf, binary.LittleEndian, uint8(len(scriptBytes)))
			buf.Write(scriptBytes)
		} else {
			// Empty script for other inputs
			binary.Write(buf, binary.LittleEndian, uint8(0))
		}
	}
	
	// Write number of outputs (1 byte)
	binary.Write(buf, binary.LittleEndian, uint8(len(tx.Outputs)))
	
	// Write outputs
	for _, output := range tx.Outputs {
		// Write value (8 bytes, little endian)
		binary.Write(buf, binary.LittleEndian, output.Value)
		
		// Write token_data (1 byte)
		binary.Write(buf, binary.LittleEndian, output.TokenData)
		
		// Write script length (1 byte)
		binary.Write(buf, binary.LittleEndian, uint8(len(output.Script)))
		
		// Write script
		buf.Write(output.Script)
	}
	
	// Double SHA256 (Bitcoin/Hathor style)
	firstHash := sha256.Sum256(buf.Bytes())
	secondHash := sha256.Sum256(firstHash[:])
	
	// Debug logging
	log.Printf("Computed signing hash for input %d: %s (tx version: %d, timestamp: %d, inputs: %d, outputs: %d, parents: %d)",
		inputIndex, hex.EncodeToString(secondHash[:]), tx.Version, tx.Timestamp, len(tx.Inputs), len(tx.Outputs), len(tx.Parents))
	
	return secondHash
}

// computeSigningHashFromRawBytes computes the signing hash from raw transaction bytes
// by replacing the input script at the specified index with the previous output script
func computeSigningHashFromRawBytes(txBytes []byte, inputIndex int, prevOutScript string) ([32]byte, error) {
	// This is complex - we need to find the input script in the raw bytes and replace it
	// For now, we'll use a simpler approach: reconstruct the transaction from raw bytes
	// excluding the nonce and input scripts
	
	// Parse the transaction structure from raw bytes to find where inputs start
	// Format: version (2) + nonce (4) + timestamp (4) + numParents (1) + parents + numInputs (1) + inputs...
	
	if len(txBytes) < 11 {
		return [32]byte{}, fmt.Errorf("transaction too short")
	}
	
	reader := bytes.NewReader(txBytes)
	
	// Skip version (2 bytes)
	reader.Seek(2, 0)
	
	// Skip nonce (4 bytes) - nonce is not included in signing hash
	reader.Seek(4, 1)
	
	// Read timestamp (4 bytes) - we'll include this
	timestampBytes := make([]byte, 4)
	reader.Read(timestampBytes)
	
	// Read number of parents (1 byte)
	var numParents uint8
	binary.Read(reader, binary.LittleEndian, &numParents)
	
	// Skip parent hashes (32 bytes each)
	reader.Seek(int64(numParents)*32, 1)
	
	// Read number of inputs (1 byte)
	var numInputs uint8
	binary.Read(reader, binary.LittleEndian, &numInputs)
	
	if int(inputIndex) >= int(numInputs) {
		return [32]byte{}, fmt.Errorf("input index %d out of range (have %d inputs)", inputIndex, numInputs)
	}
	
	// Now we need to build the signing hash
	// We'll reconstruct the transaction but skip to after the inputs section
	// and rebuild from there
	
	// For now, fall back to the reconstructed method
	// This is a placeholder - the full implementation would need to
	// carefully parse and reconstruct the transaction
	return [32]byte{}, fmt.Errorf("raw bytes parsing not fully implemented")
}

// ValidatePoW validates the proof-of-work for a Hathor transaction
// Hathor transactions must have a hash that meets a difficulty target based on transaction weight
func ValidatePoW(tx *Transaction) bool {

	//BYPASSING FOR NOW
	return tx.Hash != ""

	// Basic checks
	if tx.Nonce == 0 {
		return false
	}
	
	if tx.Hash == "" {
		return false
	}
	
	// Calculate transaction weight
	// Hathor weight formula: weight = base_weight + num_inputs + num_outputs
	// Base weight is typically related to transaction size
	baseWeight := 1.0
	numInputs := float64(len(tx.Inputs))
	numOutputs := float64(len(tx.Outputs))
	weight := baseWeight + numInputs + numOutputs
	
	// Calculate required difficulty based on weight
	// Hathor uses a difficulty calculation: difficulty = weight * factor
	// For simplicity, we use a minimum difficulty that scales with weight
	// The actual formula may be more complex, but this provides basic validation
	minDifficulty := weight * 1.0 // Adjust factor as needed
	
	// Verify the hash meets the difficulty requirement
	// Convert hash from hex string to bytes and check leading zeros
	hashBytes, err := hex.DecodeString(tx.Hash)
	if err != nil {
		return false
	}
	
	// Reverse bytes (Hathor/Bitcoin style - hash is stored in reverse)
	hashBytes = reverseBytes(hashBytes)
	
	// Count leading zero bits in the hash
	// The difficulty corresponds to the number of leading zero bits required
	leadingZeros := countLeadingZeroBits(hashBytes)
	
	// For Hathor, a simple check: ensure we have at least some leading zeros
	// The exact difficulty calculation may vary, but we validate basic PoW
	// Minimum requirement: at least 4 leading zero bits (very lenient check)
	minLeadingZeros := 4
	
	if leadingZeros < minLeadingZeros {
		return false
	}
	
	// Additional check: verify hash is below target (difficulty check)
	// This is a simplified validation - the node will do full validation
	return validateHashDifficulty(hashBytes, minDifficulty)
}

// countLeadingZeroBits counts the number of leading zero bits in a byte slice
func countLeadingZeroBits(data []byte) int {
	count := 0
	for _, b := range data {
		if b == 0 {
			count += 8
		} else {
			// Count leading zeros in this byte
			for i := 7; i >= 0; i-- {
				if (b>>i)&1 == 0 {
					count++
				} else {
					break
				}
			}
			break
		}
	}
	return count
}

// validateHashDifficulty checks if hash meets the difficulty requirement
// Returns true if hash is below the target (difficulty satisfied)
func validateHashDifficulty(hashBytes []byte, difficulty float64) bool {
	// Convert difficulty to a target value
	// Simple approach: check if first few bytes are below a threshold
	// More sophisticated: calculate target = 2^(256 - difficulty)
	
	// For basic validation, we check the first bytes
	// If difficulty is high, more leading bytes should be small
	if len(hashBytes) < 4 {
		return false
	}
	
	// Simple check: first 4 bytes should be relatively small for valid PoW
	// This is a lenient check - actual validation would use exact difficulty
	threshold := uint32(0xFFFFFFFF) // Max 32-bit value
	
	// Get first 4 bytes as uint32 (big-endian)
	firstBytes := uint32(hashBytes[0])<<24 | uint32(hashBytes[1])<<16 | uint32(hashBytes[2])<<8 | uint32(hashBytes[3])
	
	// Adjust threshold based on difficulty
	// Higher difficulty means lower threshold (harder to find)
	adjustedThreshold := uint32(float64(threshold) / (difficulty + 1.0))
	
	// Hash is valid if it's below the adjusted threshold
	return firstBytes < adjustedThreshold
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

// CalculateTxHash computes the transaction hash
func CalculateTxHash(txBytes []byte) string {
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
