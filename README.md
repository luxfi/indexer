# Explorer

Single-binary block explorer for EVM and multi-chain networks. Indexes, serves API, embeds frontend. Zero external dependencies.

```
explorer
├── /v1/explorer/*        Explorer API v2 REST endpoints
├── /v1/explorer/graphql   GraphQL endpoint
├── /v1/explorer/ws        WebSocket / SSE realtime subscriptions
├── /*                     Embedded frontend (Next.js static export)
├── /health                Healthcheck
│
├── SQLite (WAL)           Zero-config embedded database
├── ZapDB               Fast KV layer for hot lookups
└── Replicate → S3         E2E PQ encrypted streaming backups
```

## Quick Start

```bash
# Build
go build -o explorer ./cmd/explorer/

# Run — indexes chain, serves API + frontend on :8090
./explorer --rpc=http://localhost:9650/ext/bc/C/rpc

# That's it. Open http://localhost:8090
```

### Docker

```bash
docker build -f Dockerfile.explorer -t explorer .
docker run -p 8090:8090 -v explorer-data:/data \
  -e RPC_ENDPOINT=http://node:9650/ext/bc/C/rpc \
  explorer
```

### E2E Post-Quantum Encrypted Streaming Backups

Every SQLite write is continuously streamed to S3, encrypted end-to-end before leaving the process:

```
SQLite WAL → Replicate → age (ML-KEM-768 ∥ X25519) → S3
                          ↑
                          NIST FIPS 203 lattice KEM
                          + classical ECDH (defense-in-depth)
                          + ChaCha20-Poly1305 AEAD (64KB chunks)
                          + fresh ephemeral keys per segment
                          + forward secrecy (keys zeroed after use)
```

- **Quantum-resistant**: ML-KEM-768 protects against harvest-now-decrypt-later
- **Hybrid**: secure if either classical or post-quantum algorithm holds
- **Streaming**: never buffers the full database — 64KB encrypted chunks
- **Point-in-time**: restore to any transaction ID, not just the latest snapshot
- **No key rotation**: each WAL segment generates fresh ephemeral keypairs
- **Zero trust S3**: data is ciphertext before it leaves the process, S3 never sees plaintext

```bash
# Generate PQ keypair
age-keygen -pq -o explorer.key

# Enable streaming backups
export REPLICATE_S3_ENDPOINT=https://s3.amazonaws.com
export REPLICATE_S3_BUCKET=explorer-backups
export REPLICATE_AGE_RECIPIENT=age1pq1...  # from explorer.key

# Restore to point-in-time
explorer restore --from=s3://explorer-backups/mainnet --to=./restored.db \
  --age-identity=explorer.key --txid=12345
```

## Architecture

```
┌─────────────────────────────────────────────────────────┐
│                      explorer binary                     │
│                                                           │
│  ┌──────────────┐  ┌───────────────┐  ┌──────────────┐  │
│  │  Indexer      │  │  API Server   │  │  Frontend    │  │
│  │  goroutines   │  │  (Base)       │  │  (embedded)  │  │
│  │               │  │               │  │              │  │
│  │  EVM blocks   │  │ /v1/explorer  │  │  Next.js     │  │
│  │  txs, logs    │  │  REST, GQL    │  │  static      │  │
│  │  tokens       │  │  WebSocket    │  │  export      │  │
│  │  traces       │  │  SSE          │  │  go:embed    │  │
│  │  DeFi, NFTs   │  │  JSON-RPC     │  │              │  │
│  └───────┬───────┘  └───────┬───────┘  └──────────────┘  │
│          │ write             │ read-only                   │
│          ▼                   ▼                             │
│  ┌─────────────────────────────────────┐                  │
│  │  SQLite (WAL)  +  ZapDB (KV)     │                  │
│  └──────────────────┬──────────────────┘                  │
│                     │ continuous WAL stream                │
│  ┌──────────────────▼──────────────────┐                  │
│  │  S3 (E2E PQ encrypted)              │                  │
│  │  ML-KEM-768 + X25519 + ChaCha20    │                  │
│  │  point-in-time restore to any TXID  │                  │
│  └─────────────────────────────────────┘                  │
└───────────────────────────────────────────────────────────┘
```

### Rust Static Libraries (Optional)

Pure Go covers everything by default. The Rust verification services can optionally be compiled as a static library and linked via CGO:

```
explorer-rs/ffi/ → libexplorer_ffi.a
  explorer_verify_solidity()     smart-contract-verifier
  explorer_verify_vyper()        vyper verification
  explorer_lookup_signature()    sig-provider (4-byte selectors)
  explorer_lookup_event()        event signature lookup
  explorer_free()                memory management
```

```bash
# Pure Go (default)
go build -o explorer ./cmd/explorer/

# With Rust linked
cd explorer-rs/ffi && cargo build --release
CGO_ENABLED=1 CGO_LDFLAGS="-L... -lexplorer_ffi" go build -tags rustffi -o explorer ./cmd/explorer/
```

## Chains

### EVM (C-Chain + Subnet Chains)

Full EVM explorer feature parity:

- Blocks (consensus, uncle, EIP-4844 blobs, Lux extended header)
- Transactions (type 0-3, internal traces via 3 tracer types, state changes)
- Tokens (ERC-20, ERC-721, ERC-1155, batch transfers, historical balances)
- Smart contracts (Solidity/Vyper verification, proxy detection, ABI decode)
- Account Abstraction (ERC-4337 UserOps, bundlers, paymasters)
- MEV (sandwich, frontrun, backrun, liquidation detection)
- DeFi (AMM V2/V3, perps, lending, staking, bridges, CLOB, NFT marketplaces)
- Search, statistics, gas oracle, charts, user accounts, API keys, watchlists

### DAG Chains (X, A, B, Q, T, Z, K)

| Chain | Purpose |
|-------|---------|
| X-Chain | Asset exchange, UTXOs, atomic swaps |
| A-Chain | AI compute, attestations, model registration |
| B-Chain | Cross-chain bridge, proof validation |
| Q-Chain | Quantum finality, lattice proofs |
| T-Chain | Threshold MPC, key shares |
| Z-Chain | Zero-knowledge, shielded transfers |
| K-Chain | PQ key management, certificate lifecycle |

### Multi-Chain (100+ External)

Parallel indexer: Ethereum, Polygon, Arbitrum, Optimism, Base, BSC, Solana, Bitcoin (Ordinals/Runes/BRC-20), Cosmos, Aptos, Sui, NEAR, Tron, TON, and more.

## API

All endpoints under `/v1/explorer/`.

```
GET  /v1/explorer/blocks
GET  /v1/explorer/blocks/{hash_or_number}
GET  /v1/explorer/transactions
GET  /v1/explorer/transactions/{hash}
GET  /v1/explorer/transactions/{hash}/token-transfers
GET  /v1/explorer/transactions/{hash}/internal-transactions
GET  /v1/explorer/transactions/{hash}/logs
GET  /v1/explorer/addresses/{hash}
GET  /v1/explorer/addresses/{hash}/transactions
GET  /v1/explorer/addresses/{hash}/tokens
GET  /v1/explorer/tokens
GET  /v1/explorer/tokens/{address}/holders
GET  /v1/explorer/smart-contracts/{address}
POST /v1/explorer/smart-contracts/{address}/verify
GET  /v1/explorer/search
GET  /v1/explorer/stats
POST /v1/explorer/graphql
WS   /v1/explorer/ws
POST /v1/explorer/rpc  (Etherscan-compatible)
```

## White-Label

Zero branding in code. Everything at runtime:

```bash
explorer --chain-name="My Chain" --coin=MYC --chain-id=12345
```

## Configuration

```bash
# Required
RPC_ENDPOINT=http://localhost:9650/ext/bc/C/rpc

# Optional
DATA_DIR=~/.explorer/data
HTTP_ADDR=:8090
CHAIN_ID=96369
CHAIN_NAME=My Chain
COIN_SYMBOL=ETH

# PQ Encrypted Backups
REPLICATE_S3_ENDPOINT=https://s3.amazonaws.com
REPLICATE_S3_BUCKET=explorer-backups
REPLICATE_AGE_RECIPIENT=age1pq1...
```

## Testing

```bash
go test ./...                  # 431+ tests, 22 packages
go test ./explorer/...         # API layer (35 tests)
go test ./evm/...              # EVM indexer
go test ./storage/...          # Storage backends
```

## Related

- [`luxfi/explore`](https://github.com/luxfi/explore) — Frontend (Next.js, targets this API)
- [`luxfi/explorer-v1`](https://github.com/luxfi/explorer-v1) — Legacy Elixir stack (archived, targets same frontend)
- [`luxfi/explorer-rs`](https://github.com/luxfi/explorer-rs) — Rust services (optional staticlib for CGO linking)

## License

MIT
