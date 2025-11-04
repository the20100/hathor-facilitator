package txscan

import (
	"encoding/hex"
	"errors"

	"github.com/btcsuite/btcd/btcec/v2/ecdsa"
)

type InputData struct {
	SigDER []byte
	Pub33  []byte
	Raw    []byte // [lenSig][DER][0x21][pub33]
	Offset int    // where it was found in tx
}

// ExtractInputDatas finds sequences like [DER sig][0x21][pub33] and returns canonical inputData blobs.
func ExtractInputDatas(txHex string) ([]InputData, error) {
	b, err := hex.DecodeString(txHex)
	if err != nil {
		return nil, err
	}
	var out []InputData
	// naive sliding window search for DER sig + 33-byte pubkey
	for i := 0; i+70 < len(b); i++ { // min DER len ~70
		if b[i] != 0x30 { // DER SEQUENCE
			continue
		}
		// try a few plausible DER lengths
		for derLen := 70; derLen <= 73 && i+derLen <= len(b); derLen++ {
			der := b[i : i+derLen]
			// quick parse test
			if _, err := ecdsa.ParseDERSignature(der); err != nil {
				continue
			}
			// must be followed by 0x21 + 33-byte compressed pubkey
			j := i + derLen
			if j+1+33 > len(b) {
				continue
			}
			if b[j] != 0x21 {
				continue
			}
			pub := b[j+1 : j+1+33]
			if len(pub) != 33 || (pub[0] != 0x02 && pub[0] != 0x03) {
				continue
			}
			// assemble canonical inputData: [lenSig][DER][0x21][pub]
			raw := make([]byte, 0, 1+len(der)+1+len(pub))
			raw = append(raw, byte(len(der)))
			raw = append(raw, der...)
			raw = append(raw, 0x21)
			raw = append(raw, pub...)

			out = append(out, InputData{
				SigDER: der, Pub33: pub, Raw: raw, Offset: i,
			})
			// advance beyond this match to avoid duplicates
			i = j + 1 + 33 - 1
			break
		}
	}
	if len(out) == 0 {
		return nil, errors.New("no P2PKH inputData found in tx")
	}
	return out, nil
}

