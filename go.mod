module github.com/hathor-network/hathor-facilitator

go 1.21

require (
	github.com/btcsuite/btcd/btcutil v1.1.5
	github.com/decred/dcrd/dcrec/secp256k1/v4 v4.4.0
)

require github.com/decred/dcrd/crypto/blake256 v1.1.0 // indirect

replace github.com/btcsuite/btcd => github.com/btcsuite/btcd v0.24.0
