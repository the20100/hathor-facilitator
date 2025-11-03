#!/bin/bash

# Example usage of the Hathor x402 Facilitator

# Example 1: Verify a payment
echo "Example 1: Verifying a payment..."
#curl -X POST http://localhost:8443/verify \
#  -H "Content-Type: application/json" \
#  -d '{
#    "scheme": "exact",
#    "network": "hathor-testnet",
#    "payload": {
#      "txHex": "0001000102000007ce56b8597fa4c971b35dafcbc04cf50106a52152df7e9bd5711cae63ae000020e2b155047b37cc650c030ca6d3dad7cabdc03ed202b8a06eefed6db7d73e3e5a0000006400001976a9140f0ed2dff2ffe4aab409bed706b56d2ea7428ed588ac0000012c00001976a914ffd45ca4c06a620e80fffff926e0cfa027deca3188ac0000000000000000000000000000000000"
#    },
#    "requirements": {
#      "amount": 100,
#      "asset": "00",
#      "address": "WQ3emmxYypZduTsRgA7x6s1Bj5GqsPcnmo"
#    }
#  }' | python3 -m json.tool

{
    "scheme": "exact",
    "network": "hathor-testnet",
    "payload": {
      "txHex": "0001000102000007ce56b8597fa4c971b35dafcbc04cf50106a52152df7e9bd5711cae63ae000020e2b155047b37cc650c030ca6d3dad7cabdc03ed202b8a06eefed6db7d73e3e5a0000006400001976a9140f0ed2dff2ffe4aab409bed706b56d2ea7428ed588ac0000012c00001976a914ffd45ca4c06a620e80fffff926e0cfa027deca3188ac0000000000000000000000000000000000"
    },
    "requirements": {
      "amount": 100,
      "asset": "HTR",
      "address": "WQ3emmxYypZduTsRgA7x6s1Bj5GqsPcnmo"
    }
  }

curl -X POST http://localhost:8443/verify \
  -H "Content-Type: application/json" \
  -H "X-PAYMENT: ewogICAgInNjaGVtZSI6ICJleGFjdCIsCiAgICAibmV0d29yayI6ICJIYXRob3JUZXN0bmV0IiwKICAgICJwYXlsb2FkIjogewogICAgICAidHhIZXgiOiAiMDAwMTAwMDEwMjAwMDAwN2NlNTZiODU5N2ZhNGM5NzFiMzVkYWZjYmMwNGNmNTAxMDZhNTIxNTJkZjdlOWJkNTcxMWNhZTYzYWUwMDAwMjA0ZTFkZmQxMGEyMjlmYmVmOGE4ZDE2ZTM1MTM2ODc5ODI1NTA2MzhmZTE5MjI3NWE1YmRlN2ZiYmQ1ODFlZDBhMDAwMDAxMmMwMDAwMTk3NmE5MTRmZmQ0NWNhNGMwNmE2MjBlODBmZmZmZjkyNmUwY2ZhMDI3ZGVjYTMxODhhYzAwMDAwMDY0MDAwMDE5NzZhOTE0MGYwZWQyZGZmMmZmZTRhYWI0MDliZWQ3MDZiNTZkMmVhNzQyOGVkNTg4YWMwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwIgogICAgfSwKICAgICJyZXF1aXJlbWVudHMiOiB7CiAgICAgICJhbW91bnQiOiAxMDAsCiAgICAgICJhc3NldCI6ICIwMCIsCiAgICAgICJhZGRyZXNzIjogIldRM2VtbXhZeXBaZHVUc1JnQTd4NnMxQmo1R3FzUGNubW8iCiAgICB9CiAgfQ==" | python3 -m json.tool


# Example 2: Check health
echo -e "\nExample 2: Health check..."
curl http://localhost:8443/health

# Example 3: Settle a payment (same payload as verify)
echo -e "\nExample 3: Settling a payment..."
curl -X POST http://localhost:8443/settle \
  -H "Content-Type: application/json" \
  -d '{
    "scheme": "exact",
    "network": "hathor-testnet",
    "payload": {
      "txHex": "<your-transaction-hex-here>"
    },
    "requirements": {
      "amount": 100,
      "asset": "HTR",
      "address": "WPT6..."
    }
  }'