# Explorer

## Overview

Single-binary block explorer. Indexes EVM and multi-chain networks, serves explorer API v2, embeds the frontend. SQLite by default, PQ encrypted streaming backups to S3. No PostgreSQL, no Elixir, no external services.

## Package Information

- **Module**: `github.com/luxfi/explorer`
- **Binary**: `explorer` (NOT `lux-explorer`)
- **Go**: 1.26.1
- **Image**: `ghcr.io/luxfi/explorer`

## Architecture

```
explorer (single binary)
│
├── Indexer goroutines      Write → SQLite (WAL, sole writer)
├── Base HTTP server        Read → SQLite (RO, 8 conns)
│   ├── /v1/explorer/*      Explorer API v2 REST
│   ├── /v1/explorer/graphql GraphQL
│   ├── /v1/explorer/ws     WebSocket/SSE realtime
│   └── /*                  Embedded Next.js frontend (go:embed)
├── Replicate sidecar       WAL → S3 (age PQ encrypted)
└── ZapDB KV             Fast hash/height lookups
```

### Storage

- **SQLite WAL**: default, zero-config, concurrent readers
- **ZapDB**: KV layer for O(1) hash lookups
- **PostgreSQL**: optional via `-tags postgres`
- **No Redis**: ZapDB replaces it

### PQ Encrypted Streaming Backups

Every SQLite WAL write is streamed to S3, encrypted with post-quantum cryptography:

- **Algorithm**: ML-KEM-768 + X25519 hybrid (NIST FIPS 203)
- **Library**: `luxfi/age` v1.4.0
- **Transport**: `hanzoai/replicate` WAL streaming
- **Chunk size**: 64KB ChaCha20-Poly1305 AEAD per chunk
- **Forward secrecy**: fresh ephemeral keys per WAL segment
- **Restore**: point-in-time to any TXID from S3

```
SQLite WAL → Replicate → age (ML-KEM-768 ∥ X25519) → S3
```

Enable: `REPLICATE_S3_ENDPOINT` + `REPLICATE_AGE_RECIPIENT` env vars.

### Rust Services (Optional)

Pure Go implementations exist for everything. Rust originals available as static library via CGO:

```
explorer-rs/ffi/ → libexplorer_ffi.a (staticlib)
  ├── smart-contract-verifier  C ABI: explorer_verify_solidity()
  ├── sig-provider             C ABI: explorer_lookup_signature()
  └── memory mgmt              C ABI: explorer_free()

rustffi/verifier.go            Go CGO bindings (build tag: rustffi)
```

Build without Rust: `go build ./cmd/explorer/` (default, pure Go)
Build with Rust: `CGO_ENABLED=1 go build -tags rustffi ./cmd/explorer/`

## Directory Structure

```
cmd/explorer/           Unified binary entry point
cmd/indexer/            Per-chain indexer (legacy)
cmd/evmchains/          Multi-EVM (shared DB)
cmd/multichain/         100+ chain parallel indexer

explorer/               API layer (imports hanzoai/base as library)
  explorer.go           Plugin registration, read-only SQLite conn
  handlers.go           /v1/explorer/* route handlers (30 endpoints)
  format.go             Explorer v2 response formatters
  collections.go        User data: watchlists, tags, custom ABIs
  testutil/factory.go   Test data factory

evm/                    EVM indexer
  adapter.go            Main indexer (2330 lines)
  api/server.go         REST + GraphQL + WebSocket + JSON-RPC
  api/repository.go     Database queries
  api/graphql.go        GraphQL schema
  contracts/            Verification, proxy detection, ABI decode
  defi/                 DeFi protocol indexing
  account/              User accounts, API keys
  erc4337.go            Account Abstraction
  mev.go                MEV detection
  blob.go               EIP-4844

storage/                Dual-layer storage
  unified.go            KV + Query combined
  kv/                   ZapDB
  query/sqlite.go       SQLite (default)
  query/postgres.go     PostgreSQL (build tag)

rustffi/                Optional CGO bindings to Rust static lib
{x,a,b,q,t,z,k}chain/  DAG chain adapters
pchain/                 Platform chain adapter
chain/, dag/            Shared libraries
migrations/             SQL schema (4 files)
e2e/                    End-to-end tests
```

## API Routes

All under `/v1/explorer/`:

| Endpoint | Description |
|----------|-------------|
| `GET /blocks` | List blocks (cursor pagination) |
| `GET /blocks/{id}` | Block by hash or number |
| `GET /blocks/{id}/transactions` | Block's transactions |
| `GET /transactions` | List transactions |
| `GET /transactions/{hash}` | Transaction details |
| `GET /transactions/{hash}/token-transfers` | Token transfers in tx |
| `GET /transactions/{hash}/internal-transactions` | Internal calls |
| `GET /transactions/{hash}/logs` | Event logs |
| `GET /addresses/{hash}` | Address details + balance |
| `GET /addresses/{hash}/transactions` | Address transactions |
| `GET /addresses/{hash}/tokens` | Token balances |
| `GET /tokens` | List tokens |
| `GET /tokens/{addr}/holders` | Token holders |
| `GET /smart-contracts/{addr}` | Verified contract + ABI |
| `POST /smart-contracts/{addr}/verify` | Submit verification |
| `GET /search` | Full-text search |
| `GET /stats` | Chain statistics |
| `POST /graphql` | GraphQL queries |
| `WS /ws` | Realtime subscriptions |

## White-Label

Zero branding in code. Configure at runtime:

```bash
explorer --chain-name="My Chain" --coin=MYC --chain-id=12345
```

## EVM Feature Parity

All EVM explorer features implemented in Go:

- Block indexing (consensus, uncle, EIP-4844 blobs)
- Transaction types 0-3 (legacy, access list, EIP-1559, EIP-4844)
- Internal transaction tracing (3 tracers: callTracer, jsTracer, parityTracer)
- Token tracking: ERC-20, ERC-721, ERC-1155 (batch), historical balances
- Contract verification: Solidity (all versions), Vyper, Standard JSON
- Proxy detection: EIP-1967, EIP-1822, EIP-1167, Gnosis Safe, OpenZeppelin
- Account Abstraction: ERC-4337 UserOps, bundlers, paymasters
- MEV detection: sandwich, frontrun, backrun, liquidation
- DeFi: AMM V2/V3, perps, lending, staking, bridges, CLOB orderbook
- NFT marketplaces: Seaport, LooksRare, Blur, X2Y2, Rarible, Zora
- Charts, statistics, gas oracle
- User accounts, API keys, watchlists, address tags

## Testing

```bash
go test ./...                    # All tests (431+ across 22 packages)
go test ./explorer/...           # API layer (35 tests)
go test ./evm/...                # EVM indexer
go test ./storage/...            # Storage backends
```

## Dependencies

| Package | Purpose |
|---------|---------|
| `hanzoai/base` | HTTP server, collections, SSE, IAM |
| `hanzoai/replicate` | SQLite WAL streaming to S3 |
| `luxfi/age` | PQ encryption (ML-KEM-768 + X25519) |
| `luxfi/database` | ZapDB KV wrapper |
| `luxfi/zap` | Zero-copy wire protocol |

## What This Replaced

| Before (6 containers) | After (1 binary) |
|------------------------|-------------------|
| Elixir backend | `evm/` Go package |
| Rust verifier | `evm/contracts/verifier.go` |
| Rust sig-provider | `evm/contracts/abi.go` |
| PostgreSQL | SQLite WAL |
| Redis | ZapDB KV |
| Graph Node + subgraphs | G-Chain native GraphQL |
