#!/bin/bash

# Example usage of the Hathor x402 Facilitator

# Example 1: Verify a payment
echo "Example 1: Verifying a payment..."
curl -X POST http://localhost:3000/verify \
  -H "Content-Type: application/json" \
  -d '{
    "scheme": "exact",
    "network": "HathorTestnet",
    "payload": {
      "txHex": "<your-transaction-hex-here>"
    },
    "requirements": {
      "amount": 100,
      "asset": "HTR",
      "address": "WPT6..."
    }
  }'

# Example 2: Check health
echo -e "\nExample 2: Health check..."
curl http://localhost:3000/health

# Example 3: Settle a payment (same payload as verify)
echo -e "\nExample 3: Settling a payment..."
curl -X POST http://localhost:3000/settle \
  -H "Content-Type: application/json" \
  -d '{
    "scheme": "exact",
    "network": "HathorTestnet",
    "payload": {
      "txHex": "<your-transaction-hex-here>"
    },
    "requirements": {
      "amount": 100,
      "asset": "HTR",
      "address": "WPT6..."
    }
  }'