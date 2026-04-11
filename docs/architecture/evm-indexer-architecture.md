# Explorer Architecture

**Version**: 3.0.0
**Status**: PRODUCTION
**Date**: 2026-04-10

---

## 1. Overview

Single-binary omni-chain block explorer. Clean-room Go implementation. Indexes all Lux native chains (EVM, DAG, linear, DEX, FHE, PQ, ZK) plus 100+ external chains for bridge/MPC/threshold operations. E2E post-quantum encrypted streaming backups to S3.

### What Changed (v2 → v3)

| v2 (explorer-v1) | v3 (this) |
|---|---|
| Elixir backend + 4 Rust microservices | Single Go binary |
| PostgreSQL (managed) | SQLite WAL (embedded, per-chain) |
| Redis (caching) | BadgerDB KV (embedded) |
| 6 Docker containers | 1 container, 1 binary |
| EVM only | 9 native chain types + 100+ external |
| GPG/plaintext backups | E2E PQ encrypted streaming (ML-KEM-768) |
| Graph Node + subgraphs | G-Chain native GraphQL |
| Explorer API v2 at `/api/v2` | Same API at `/v1/explorer` |

## 2. Architecture

```
┌───────────────────────────────────────────────────────────┐
│                       explorer binary                      │
│                                                             │
│  ┌─────────┐ ┌─────────┐ ┌─────────┐      ┌────────────┐ │
│  │ C-Chain  │ │ P-Chain │ │ X-Chain │ ...  │ Ethereum   │ │
│  │ indexer  │ │ indexer │ │ indexer │ N    │ indexer    │ │
│  │ (EVM)   │ │ (linear)│ │ (DAG)   │ chains│ (bridge)   │ │
│  └────┬────┘ └────┬────┘ └────┬────┘      └─────┬──────┘ │
│       │ write      │ write     │ write           │ write   │
│       ▼            ▼           ▼                 ▼         │
│  ┌─────────┐ ┌─────────┐ ┌─────────┐      ┌──────────┐  │
│  │ SQLite  │ │ SQLite  │ │ SQLite  │      │ SQLite   │  │
│  │ cchain/ │ │ pchain/ │ │ xchain/ │      │ ethereum/│  │
│  └────┬────┘ └────┬────┘ └────┬────┘      └─────┬────┘  │
│       │            │           │                  │        │
│       └────────────┴───────────┴──────────────────┘        │
│                            │                                │
│  ┌─────────────────────────▼──────────────────────────┐    │
│  │  API Server (Base)                                  │    │
│  │  /v1/explorer/{chain}/*   per-chain REST/GQL/WS     │    │
│  │  /v1/explorer/*           default chain             │    │
│  │  /*                       embedded frontend          │    │
│  └─────────────────────────────────────────────────────┘    │
│                            │                                │
│  ┌─────────────────────────▼──────────────────────────┐    │
│  │  Replicate → S3 (per-chain WAL streams)             │    │
│  │  E2E PQ encrypted: ML-KEM-768 + X25519 + ChaCha20  │    │
│  └─────────────────────────────────────────────────────┘    │
└─────────────────────────────────────────────────────────────┘
```

## 3. Per-Chain Storage Isolation

Each chain gets its own directory with independent SQLite + KV:

```
{data_dir}/
├── cchain/
│   ├── query/indexer.db    SQLite (blocks, txs, tokens, traces, contracts)
│   └── kv/                 BadgerDB (hash→data, height→block fast lookups)
├── pchain/
│   ├── query/indexer.db    Validators, delegators, staking, subnets
│   └── kv/
├── xchain/
│   ├── query/indexer.db    DAG vertices, edges, UTXOs, assets
│   └── kv/
├── zchain/
│   ├── query/indexer.db    ZK proofs, shielded transfers, nullifiers
│   └── kv/
├── tchain/
│   ├── query/indexer.db    FHE operations, threshold signatures, MPC keys
│   └── kv/
├── ethereum/
│   ├── query/indexer.db    External chain (bridge support)
│   └── kv/
└── ... (N chains)
```

**Why per-chain SQLite**:
- Zero write contention between chains (each is sole writer)
- Restore one chain without touching others
- Drop a chain = delete its directory
- WAL mode: 8 concurrent readers per chain (API layer)
- Per-chain S3 backup paths: `s3://bucket/{host}/{slug}/`

## 4. Chain Support Matrix

### Native Chains (9)

| Chain | Type | Indexer | Specialization |
|---|---|---|---|
| C-Chain | EVM | `evm/adapter.go` | Full EVM parity — blocks, txs, tokens, traces, contracts, DeFi |
| P-Chain | Linear | `pchain/adapter.go` | Validators, delegators, staking, subnet creation |
| X-Chain | DAG | `xchain/adapter.go` | UTXOs, atomic swaps, multi-asset transfers |
| A-Chain | DAG | `achain/adapter.go` | AI attestations, model registration, inference |
| B-Chain | DAG | `bchain/adapter.go` | Cross-chain bridge transfers, proof validation |
| Q-Chain | DAG | `qchain/adapter.go` | Post-quantum finality proofs, lattice signatures |
| T-Chain | DAG | `tchain/adapter.go` | Threshold FHE, MPC key shares, decryption proofs |
| Z-Chain | DAG | `zchain/adapter.go` | Zero-knowledge proofs, shielded transfers, nullifiers |
| K-Chain | DAG | `kchain/adapter.go` | PQ key material, certificate lifecycle, HSM bridge |

### Subnet EVM Chains

| Chain | Chain ID | Purpose |
|---|---|---|
| Zoo | 200200 | NFT/Gaming ecosystem |
| Hanzo AI | 36963 | AI compute chain |
| SPC | 36911 | Specialized compute |
| Pars | 494949 | Regional chain |
| DEX | — | CLOB orderbook, perpetuals, AMM |

### External Chains (bridge/MPC/threshold support)

| Type | Chains | Indexer |
|---|---|---|
| EVM | Ethereum, Polygon, Arbitrum, Optimism, Base, BSC, Avalanche, + 30 more | `multichain/evm_indexer.go` |
| Solana | Mainnet, Devnet | `multichain/solana_indexer.go` |
| Bitcoin | Mainnet (Ordinals, Runes, BRC-20) | `multichain/bitcoin_indexer.go` |
| Cosmos | Hub, Osmosis, Injective, dYdX, Celestia | `multichain/cosmos_indexer.go` |
| Move | Aptos, Sui | `multichain/other_indexers.go` |
| Other | NEAR, Tron, TON, Substrate | `multichain/other_indexers.go` |

## 5. E2E Post-Quantum Encrypted Streaming Backups

Every chain's SQLite WAL is continuously streamed to S3, encrypted end-to-end with post-quantum cryptography before leaving the process.

### Cryptographic Stack

| Layer | Algorithm | Standard |
|---|---|---|
| Key encapsulation | ML-KEM-768 (lattice) | NIST FIPS 203 |
| Key agreement | X25519 (classical) | RFC 7748 |
| Hybrid derivation | ML-KEM shared ∥ X25519 shared | defense-in-depth |
| Stream cipher | ChaCha20-Poly1305 AEAD | RFC 8439 |
| Key derivation | HKDF-SHA256 | RFC 5869 |
| Header auth | HMAC-SHA256 | RFC 2104 |
| Chunk size | 64 KB | age spec |

### Properties

- **Quantum-resistant**: ML-KEM-768 protects against harvest-now-decrypt-later
- **Hybrid**: secure if either ML-KEM or X25519 remains unbroken
- **Forward secrecy**: fresh ephemeral keypairs per WAL segment, zeroed after use
- **Streaming**: 64KB chunks, never buffers the full database in memory
- **Point-in-time restore**: recover to any TXID, not just latest snapshot
- **Per-chain isolation**: each chain streams to its own S3 prefix
- **Zero-trust S3**: all data is ciphertext before network transmission

### Data Flow

```
Indexer goroutine
    │ write
    ▼
SQLite WAL (disk)
    │ fsnotify
    ▼
Replicate (monitors WAL)
    │ read new frames
    ▼
LTX segment (immutable transaction log)
    │ encrypt
    ▼
age (ML-KEM-768 + X25519)
    │ 64KB ChaCha20-Poly1305 chunks
    ▼
S3 PUT (ciphertext only)
    │
    ▼
s3://bucket/{host}/{chain_slug}/{txid}.ltx.age
```

### Restore

```
S3 GET → age decrypt → LTX replay → SQLite rebuilt
```

Restore targets:
- Latest state (default)
- Specific TXID (point-in-time)
- Specific timestamp (nearest TXID)

## 6. API Layer

### Route Structure

```
/v1/explorer/                    Default chain (configurable)
/v1/explorer/{chain_slug}/       Per-chain endpoints
/v1/explorer/{chain_slug}/blocks
/v1/explorer/{chain_slug}/transactions/{hash}
/v1/explorer/{chain_slug}/addresses/{hash}
/v1/explorer/{chain_slug}/tokens
/v1/explorer/{chain_slug}/smart-contracts/{addr}
/v1/explorer/{chain_slug}/search
/v1/explorer/{chain_slug}/stats
/v1/explorer/{chain_slug}/graphql
/v1/explorer/{chain_slug}/ws
```

### Response Format

Explorer API v2 — cursor-based pagination:

```json
{
  "items": [...],
  "next_page_params": {
    "block_number": 12345,
    "index": 0,
    "items_count": 50
  }
}
```

### Frontend

The `explore` Next.js frontend is built as a static export and embedded in the binary via `go:embed`. Served at `/*` with SPA fallback routing.

`NEXT_PUBLIC_API_BASE_PATH=/v1/explorer` points the frontend at the embedded API.

## 7. Rust Static Libraries (Optional)

Pure Go is the default for all services. Rust verification implementations available as optional static libraries linked via CGO:

```
explorer-rs/ffi/
├── Cargo.toml              crate-type = ["staticlib"]
├── src/lib.rs              C ABI: explorer_verify_solidity(), etc.
├── explorer_ffi.h          C header
└── target/release/
    └── libexplorer_ffi.a   ~15MB static library

rustffi/
├── explorer_ffi.h          Header copy
└── verifier.go             Go CGO bindings (build tag: rustffi)
```

Build tag `rustffi` activates Rust. Without it, pure Go implementations in `evm/contracts/` are used.

## 8. Scalability

### Per-Chain Resource Usage

| State | RAM | Disk I/O | CPU |
|---|---|---|---|
| Idle (no new blocks) | ~2 MB | 0 | 0 |
| Active EVM indexing | ~50-100 MB | moderate | 1 core |
| Active DAG indexing | ~20-50 MB | moderate | 0.5 core |
| Active external chain | ~30-80 MB | moderate | 0.5 core |

### Tested Configurations

| Chains | RAM | CPU | Notes |
|---|---|---|---|
| 1 (single chain) | 256 MB | 1 core | Minimal dev setup |
| 15 (all Lux native) | 1 GB | 4 cores | Full Lux ecosystem |
| 30 (Lux + major external) | 3 GB | 8 cores | Bridge support |
| 100+ (full multi-chain) | 10 GB | 16 cores | Exchange/aggregator |
| 200+ (Liquidity infra) | 20 GB | 32 cores | White-label platform |

### Horizontal Scaling

For extreme scale, run multiple instances partitioned by chain set:
- Instance A: Lux native chains (9)
- Instance B: EVM external chains (50+)
- Instance C: Non-EVM chains (Solana, Bitcoin, Cosmos, etc.)

Each instance shares the same S3 backup bucket with isolated prefixes.

## 9. White-Label

Zero chain-specific branding in code. Runtime configuration only:

```yaml
chains:
  - slug: mychain
    name: "My Awesome Chain"
    chain_id: 12345
    type: evm
    rpc: http://node:8545
    coin: AWE
    enabled: true
    default: true
```

Frontend branding (logo, colors, footer) via Next.js environment variables at build time.
