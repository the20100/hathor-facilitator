# Hathor x402 Facilitator

A Go implementation of an x402 payment facilitator for the Hathor blockchain. This service enables HTTP 402 "Payment Required" responses with automatic on-chain payments on Hathor.

## Overview

The x402 protocol enables instant, automatic on-chain payments directly over HTTP by leveraging the HTTP 402 status code. This facilitator service handles the verification and settlement of Hathor transactions on behalf of resource servers.

## Features

- **Payment Verification**: Verifies signed Hathor transactions before settlement
- **Payment Settlement**: Broadcasts transactions to the Hathor network and waits for confirmations
- **UTXO Validation**: Checks that transaction inputs exist and are unspent
- **Signature Verification**: Validates ECDSA signatures on transaction inputs
- **Multi-network Support**: Supports both Hathor Mainnet and Testnet

## Architecture

The facilitator exposes three main HTTP endpoints:

- `GET /supported` - Returns the supported payment schemes and networks
- `POST /verify` - Verifies a payment payload without broadcasting
- `POST /settle` - Broadcasts the transaction and waits for confirmation

## Prerequisites

- Go 1.16 or later (Go 1.21 recommended)
- A running Hathor full node (default: `http://localhost:8080`)
- Hathor Headless Wallet (optional, default: `http://localhost:8000`)

**Note:** If you encounter build errors about `io.ReadAll`, ensure you're using Go 1.16 or later. You can check your Go version with `go version`.

## Installation

1. Clone the repository:
```bash
cd hathor-facilitator
```

2. Install dependencies:
```bash
go mod download
```

3. Build the project:
```bash
go build -o hathor-facilitator
```

## Configuration

The facilitator can be configured using environment variables:

- `PORT` - HTTP server port (default: 3000)
- `HATHOR_NODE_URL` - Hathor full node URL (default: `http://localhost:8080`)
- `HATHOR_WALLET_URL` - Hathor headless wallet URL (default: `http://localhost:8000`)
- `HATHOR_WALLET_ID` - Wallet ID for headless wallet (default: `main-wallet`)
- `MIN_CONFIRMATIONS` - Minimum confirmations required before returning success (default: 1)

Example:
```bash
export PORT=3000
export HATHOR_NODE_URL=http://localhost:8080
export HATHOR_WALLET_URL=http://localhost:8000
export HATHOR_WALLET_ID=facilitator-wallet
export MIN_CONFIRMATIONS=1
```

## Running

Start the facilitator:
```bash
./hathor-facilitator
```

Or run directly:
```bash
go run main.go
```

The facilitator will start on port 3000 (or the port specified in `PORT`).

## API Endpoints

### GET /supported

Returns the supported payment schemes, networks, and assets that this facilitator supports.

**Request:** `GET /supported`

**Response:**
```json
{
  "success": true,
  "schemes": [
    {
      "scheme": "exact",
      "networks": ["hathor-mainnet", "hathor-testnet"],
      "assets": ["HTR", "00"],
      "description": "Exact payment scheme - pay a fixed amount for a single request"
    }
  ]
}
```

### POST /verify

Verifies a payment payload without broadcasting the transaction.

**Request Options:**

The facilitator supports two ways to send the payment payload:

1. **X-PAYMENT Header (Recommended for x402 compliance):**
   The payment payload can be sent as a base64-encoded JSON string in the `X-PAYMENT` header:
   
   ```bash
   curl -X POST http://localhost:3000/verify \
     -H "Content-Type: application/json" \
     -H "X-PAYMENT: <base64-encoded-payment-payload>" \
     -d '{"requirements": {"amount": 100, "asset": "HTR", "address": "WPT6..."}}'
   ```

2. **Request Body:**
   Alternatively, send the full request as JSON in the body:
   
   ```json
   {
     "scheme": "exact",
     "network": "hathor-testnet",
     "payload": {
       "txHex": "00010002..."
     },
     "requirements": {
       "amount": 100,
       "asset": "HTR",
       "address": "WPT6...abcd"
     }
   }
   ```

**Response:**
```json
{
  "success": true,
  "payer": "WPT6...xyz",
  "txHash": "00000000..."
}
```

### POST /settle

Broadcasts the transaction and waits for confirmation.

**Request Options:** (Same as `/verify` - supports both X-PAYMENT header and request body)

**Response:**
```json
{
  "success": true,
  "txHash": "00000000...",
  "confirmations": 1
}
```

### GET /health

Health check endpoint.

**Response:**
```json
{
  "status": "healthy",
  "service": "hathor-facilitator"
}
```

## Payment Flow

1. Client requests a resource from a server
2. Server responds with HTTP 402 Payment Required, including payment requirements
3. Client constructs and signs a Hathor transaction
4. Client resubmits request with `X-PAYMENT` header containing the transaction hex
5. Server calls `/verify` to validate the payment
6. Server calls `/settle` to broadcast the transaction
7. Server returns the resource with `X-PAYMENT-RESPONSE` header

## Transaction Format

The payment payload contains a fully signed Hathor transaction in hexadecimal format. The transaction must:

- Include sufficient inputs to cover the payment amount
- Have an output to the merchant's address for the required amount
- Include valid ECDSA signatures on all inputs
- Have valid proof-of-work (nonce)

## Testing

For testing, you can use Hathor's testnet or localnet:

1. **Testnet**: Use public Hathor testnet nodes
2. **Localnet**: Set up a local Hathor network for development

Example test using `curl`:

```bash
curl -X POST http://localhost:3000/verify \
  -H "Content-Type: application/json" \
  -d '{
    "scheme": "exact",
    "network": "hathor-testnet",
    "payload": {
      "txHex": "00010002..."
    },
    "requirements": {
      "amount": 100,
      "asset": "HTR",
      "address": "WPT6..."
    }
  }'
```

## Security Considerations

- The facilitator does not hold user or merchant private keys
- Transaction signatures are verified before broadcasting
- Input UTXOs are validated to prevent double-spending
- The facilitator should run on a secure server environment
- Keep the Hathor node API private (only accessible to the facilitator)

## Limitations

- Currently supports only HTR (native token) payments
- Transaction parsing may need adjustments based on actual Hathor transaction format
- PoW validation is basic - full validation would require more complex logic
- Address derivation uses simplified hashing (should use RIPEMD160 in production)

## Future Enhancements

- Support for custom tokens
- Enhanced PoW validation
- Proper RIPEMD160 hashing for address derivation
- Support for additional x402 payment schemes
- Improved error handling and logging
- Metrics and monitoring

## License

[Specify your license]

## References

- [x402 Protocol Specification](https://x402.dev)
- [Hathor Network Documentation](https://docs.hathor.network)
- [Hathor Headless Wallet](https://docs.hathor.network/headless-wallet/)

## Contributing

Contributions are welcome! Please ensure your code follows Go best practices and includes appropriate tests.
