package hathorcheck

import (
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/ecdsa"
)

// ParseInputDataP2PKH parses [lenSig][DER][lenPub=0x21][pub33] from inputData hex.
// Expected layout:  <lenSig:1><DER:lenSig><lenPub:1==0x21><pub:33>
func ParseInputDataP2PKH(inputDataHex string) (sigDER []byte, pubkey33 []byte, err error) {
	b, err := hex.DecodeString(inputDataHex)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid inputData hex: %w", err)
	}

	if len(b) < 1+1+33 {
		return nil, nil, errors.New("inputData too short")
	}

	i := 0
	lenSig := int(b[i])
	i++

	if len(b) < i+lenSig+1+33 {
		return nil, nil, fmt.Errorf("inputData length mismatch (need %d, have %d)", i+lenSig+1+33, len(b))
	}

	sigDER = b[i : i+lenSig]
	i += lenSig

	lenPub := int(b[i])
	i++

	if lenPub != 33 {
		return nil, nil, fmt.Errorf("unexpected pubkey length %d (want 33)", lenPub)
	}

	pubkey33 = b[i : i+33]
	return sigDER, pubkey33, nil
}

// VerifySignature checks DER(signature) over the exact 32-byte dataToSignHash using the 33-byte compressed pubkey.
// hashHex must be the 64-hex chars from tx-proposal dataToSignHash.
// Do not hash again.
func VerifySignature(hashHex string, sigDER []byte, pubkey33 []byte) (bool, error) {
	h, err := hex.DecodeString(hashHex)
	if err != nil {
		return false, fmt.Errorf("bad hash hex: %w", err)
	}

	if len(h) != 32 {
		return false, fmt.Errorf("hash must be 32 bytes, got %d", len(h))
	}

	if len(pubkey33) != 33 || (pubkey33[0] != 0x02 && pubkey33[0] != 0x03) {
		return false, errors.New("pubkey is not 33-byte compressed")
	}

	sig, err := ecdsa.ParseDERSignature(sigDER)
	if err != nil {
		return false, fmt.Errorf("bad DER signature: %w", err)
	}

	pk, err := btcec.ParsePubKey(pubkey33)
	if err != nil {
		return false, fmt.Errorf("parse pubkey failed: %w", err)
	}

	return sig.Verify(h, pk), nil
}

