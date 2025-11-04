package txscan

import (
	"encoding/hex"
	"fmt"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/ecdsa"
)

func VerifyInput(sigDER, pub33 []byte, dataToSignHashHex string) (bool, error) {
	h, err := hex.DecodeString(dataToSignHashHex)
	if err != nil {
		return false, fmt.Errorf("bad hash hex: %w", err)
	}
	if len(h) != 32 {
		return false, fmt.Errorf("hash must be 32 bytes, got %d", len(h))
	}
	sig, err := ecdsa.ParseDERSignature(sigDER)
	if err != nil {
		return false, fmt.Errorf("bad DER signature: %w", err)
	}
	pk, err := btcec.ParsePubKey(pub33)
	if err != nil {
		return false, fmt.Errorf("parse pubkey failed: %w", err)
	}
	return sig.Verify(h, pk), nil
}

