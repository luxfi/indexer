# Lux EVM Indexer Architecture

**Version**: 2.0.0
**Status**: IMPLEMENTATION GUIDE
**Date**: 2025-12-25
**Author**: Architecture Team

---

## 1. Executive Summary

This document defines the architecture for the **Lux EVM Indexer**, a high-performance Go-based blockchain indexer that provides **100% Blockscout API v2 compatibility** while adding native support for Lux-specific features (Warp messaging, 19-field headers, Quasar consensus).

### Supported Chains

| Chain | Chain ID | Purpose | RPC Port |
|-------|----------|---------|----------|
| C-Chain Mainnet | 96369 | Smart contracts, DeFi | 9630 |
| C-Chain Testnet | 96368 | Development | 9640 |
| Zoo Mainnet | 200200 | NFT/Gaming | 9630 |
| Zoo Testnet | 200201 | Development | 9640 |
| Hanzo AI | 36963 | AI compute | 9630 |

### Design Goals

1. **Blockscout API v2 Compatibility** - Drop-in replacement for Blockscout frontend
2. **High Performance** - 10,000+ blocks/second indexing, 50K+ RPS API
3. **Horizontal Scalability** - Stateless API workers, sharded by chain
4. **Real-time Updates** - Sub-second block notifications via WebSocket
5. **Native Lux Support** - Warp messaging, ExtDataHash, precompile awareness

---

## 2. Module Structure

### Implementation Status (as of 2025-12-25)

```
github.com/luxfi/indexer/
├── cmd/indexer/main.go          # CLI entry point
├── chain/                        # Linear chain indexer (P-Chain)
│   ├── chain.go                 # Linear consensus indexer
│   └── chain_test.go
├── dag/                          # DAG indexer (X,A,B,Q,T,Z chains)
│   ├── dag.go                   # DAG consensus indexer
│   └── dag_test.go
├── storage/                      # Storage backends
│   ├── storage.go               # Storage interfaces
│   ├── postgres.go              # PostgreSQL implementation
│   ├── badger.go                # BadgerDB implementation
│   └── dgraph.go                # Dgraph graph database
│
├── evm/                          # EVM indexer (IMPLEMENTED)
│   ├── adapter.go               # Core EVM adapter (2,000+ lines)
│   ├── adapter_test.go          # Comprehensive tests
│   ├── types.go                 # Type definitions
│   ├── erc4337.go               # Account abstraction support
│   ├── mev.go                   # MEV detection/tracking
│   │
│   ├── api/                     # API layer (IMPLEMENTED)
│   │   ├── server.go            # HTTP/WebSocket server (1,042 lines)
│   │   │                        # - 40+ Blockscout v2 endpoints
│   │   │                        # - WebSocket subscriptions
│   │   │                        # - Etherscan RPC compatibility
│   │   │                        # - GraphQL playground
│   │   ├── types.go             # API response types (406 lines)
│   │   │                        # - Block, Transaction, Address
│   │   │                        # - Token, TokenTransfer, Log
│   │   │                        # - SmartContract, NFT, SearchResult
│   │   └── repository.go        # Database queries (961 lines)
│   │                            # - Blocks, Transactions
│   │                            # - Addresses, Tokens
│   │                            # - Search, Stats
│   │
│   ├── contracts/               # Contract verification (IMPLEMENTED)
│   │   ├── types.go             # Contract types
│   │   ├── verifier.go          # Solc-based verification (757 lines)
│   │   │                        # - Standard JSON compilation
│   │   │                        # - Bytecode comparison
│   │   │                        # - Constructor args extraction
│   │   ├── abi.go               # ABI parsing and decoding
│   │   ├── proxy.go             # Proxy contract detection
│   │   └── store.go             # Contract storage
│   │
│   ├── search/                  # Search functionality (IMPLEMENTED)
│   │   ├── search.go            # Universal search
│   │   └── search_test.go
│   │
│   ├── stats/                   # Statistics service (IMPLEMENTED)
│   │   ├── stats.go             # Gas prices, counters (715 lines)
│   │   │                        # - Gas price oracle
│   │   │                        # - Block time stats
│   │   │                        # - Daily/hourly charts
│   │   ├── stats_test.go
│   │   └── prometheus.go        # Prometheus metrics
│   │
│   └── account/                 # Account abstraction
│       └── account.go           # ERC-4337 support
│
├── *chain/adapter.go            # Chain-specific adapters
│   ├── achain/                  # A-Chain (Attestation)
│   ├── bchain/                  # B-Chain (Bridge)
│   ├── dex/                     # DEX indexer
│   ├── kchain/                  # K-Chain
│   ├── pchain/                  # P-Chain (Platform)
│   ├── qchain/                  # Q-Chain (Quantum)
│   ├── tchain/                  # T-Chain
│   ├── xchain/                  # X-Chain (Exchange)
│   └── zchain/                  # Z-Chain (Zoo)
│
├── migrations/                   # PostgreSQL migrations (IMPLEMENTED)
│   ├── 001_initial_schema.sql   # Core schema (477 lines)
│   ├── 002_indexes.sql          # Index definitions (343 lines)
│   └── 003_functions.sql        # Database functions (454 lines)
│
└── docs/
    └── architecture/
        └── evm-indexer-architecture.md  # This document
```

### Implementation Statistics

| Component | Lines of Code | Status |
|-----------|---------------|--------|
| evm/adapter.go | 2,000+ | Production |
| evm/api/server.go | 1,042 | Production |
| evm/api/repository.go | 961 | Production |
| evm/contracts/verifier.go | 757 | Production |
| evm/stats/stats.go | 715 | Production |
| evm/api/types.go | 406 | Production |
| migrations/*.sql | 1,274 | Production |
| **Total EVM Package** | **~7,000** | Production |

---

## 3. PostgreSQL Schema (Blockscout-Compatible)

### Core Tables

The schema is designed to match Blockscout's table structure for frontend compatibility while adding Lux-specific fields.

```sql
-- migrations/001_initial_schema.sql

-- Enable extensions
CREATE EXTENSION IF NOT EXISTS "pg_trgm";       -- Fuzzy search
CREATE EXTENSION IF NOT EXISTS "btree_gist";    -- GiST indexes
CREATE EXTENSION IF NOT EXISTS "pg_stat_statements";

-- Chain configuration (multi-chain support)
CREATE TABLE chains (
    chain_id        BIGINT PRIMARY KEY,
    name            VARCHAR(100) NOT NULL,
    symbol          VARCHAR(20) NOT NULL,
    decimals        INTEGER DEFAULT 18,
    rpc_url         TEXT NOT NULL,
    ws_url          TEXT,
    explorer_url    TEXT,
    is_active       BOOLEAN DEFAULT true,
    created_at      TIMESTAMPTZ DEFAULT NOW(),
    updated_at      TIMESTAMPTZ DEFAULT NOW()
);

INSERT INTO chains (chain_id, name, symbol, rpc_url) VALUES
    (96369, 'Lux C-Chain', 'LUX', 'http://localhost:9630/ext/bc/C/rpc'),
    (96368, 'Lux Testnet', 'LUX', 'http://localhost:9640/ext/bc/C/rpc'),
    (200200, 'Zoo Mainnet', 'ZOO', 'http://localhost:9630/ext/bc/Zoo/rpc'),
    (200201, 'Zoo Testnet', 'ZOO', 'http://localhost:9640/ext/bc/Zoo/rpc'),
    (36963, 'Hanzo AI', 'HANZO', 'http://localhost:9630/ext/bc/Hanzo/rpc');

-- Blocks table (partitioned by chain_id)
CREATE TABLE blocks (
    id              BIGSERIAL,
    chain_id        BIGINT NOT NULL REFERENCES chains(chain_id),
    number          BIGINT NOT NULL,
    hash            BYTEA NOT NULL,
    parent_hash     BYTEA NOT NULL,
    nonce           BYTEA,
    miner           BYTEA NOT NULL,
    difficulty      NUMERIC(78),
    total_difficulty NUMERIC(78),
    size            INTEGER NOT NULL,
    gas_limit       BIGINT NOT NULL,
    gas_used        BIGINT NOT NULL,
    base_fee        NUMERIC(78),
    timestamp       BIGINT NOT NULL,
    transaction_count INTEGER NOT NULL DEFAULT 0,

    -- Lux-specific fields (19-field header)
    ext_data_hash   BYTEA,
    ext_data_gas_used BIGINT,
    block_gas_cost  NUMERIC(78),

    -- Standard Ethereum fields
    extra_data      BYTEA,
    logs_bloom      BYTEA,
    state_root      BYTEA,
    transactions_root BYTEA,
    receipts_root   BYTEA,

    -- Indexing metadata
    indexed_at      TIMESTAMPTZ DEFAULT NOW(),
    consensus_state VARCHAR(20) DEFAULT 'finalized',

    PRIMARY KEY (chain_id, number)
) PARTITION BY LIST (chain_id);

-- Create partitions for known chains
CREATE TABLE blocks_96369 PARTITION OF blocks FOR VALUES IN (96369);
CREATE TABLE blocks_96368 PARTITION OF blocks FOR VALUES IN (96368);
CREATE TABLE blocks_200200 PARTITION OF blocks FOR VALUES IN (200200);
CREATE TABLE blocks_200201 PARTITION OF blocks FOR VALUES IN (200201);
CREATE TABLE blocks_36963 PARTITION OF blocks FOR VALUES IN (36963);

-- Transactions table
CREATE TABLE transactions (
    id              BIGSERIAL,
    chain_id        BIGINT NOT NULL,
    hash            BYTEA NOT NULL,
    block_number    BIGINT NOT NULL,
    block_hash      BYTEA NOT NULL,
    transaction_index INTEGER NOT NULL,

    -- Core fields
    from_address    BYTEA NOT NULL,
    to_address      BYTEA,
    value           NUMERIC(78) NOT NULL,
    gas             BIGINT NOT NULL,
    gas_price       NUMERIC(78),
    gas_used        BIGINT,

    -- EIP-1559 fields
    max_fee_per_gas NUMERIC(78),
    max_priority_fee NUMERIC(78),
    effective_gas_price NUMERIC(78),

    -- EIP-4844 blob fields
    max_fee_per_blob_gas NUMERIC(78),
    blob_gas_used   BIGINT,
    blob_gas_price  NUMERIC(78),

    -- TX data
    input           BYTEA,
    nonce           BIGINT NOT NULL,
    type            SMALLINT NOT NULL DEFAULT 0,

    -- Status
    status          SMALLINT,
    error           TEXT,
    revert_reason   TEXT,

    -- Contract creation
    created_contract BYTEA,

    -- Timestamps
    timestamp       BIGINT NOT NULL,
    indexed_at      TIMESTAMPTZ DEFAULT NOW(),

    PRIMARY KEY (chain_id, hash),
    FOREIGN KEY (chain_id, block_number) REFERENCES blocks(chain_id, number) ON DELETE CASCADE
) PARTITION BY LIST (chain_id);

CREATE TABLE transactions_96369 PARTITION OF transactions FOR VALUES IN (96369);
CREATE TABLE transactions_96368 PARTITION OF transactions FOR VALUES IN (96368);
CREATE TABLE transactions_200200 PARTITION OF transactions FOR VALUES IN (200200);
CREATE TABLE transactions_200201 PARTITION OF transactions FOR VALUES IN (200201);
CREATE TABLE transactions_36963 PARTITION OF transactions FOR VALUES IN (36963);

-- Internal transactions (traces)
CREATE TABLE internal_transactions (
    id              BIGSERIAL,
    chain_id        BIGINT NOT NULL,
    transaction_hash BYTEA NOT NULL,
    block_number    BIGINT NOT NULL,
    trace_address   INTEGER[] NOT NULL,

    type            VARCHAR(20) NOT NULL,
    call_type       VARCHAR(20),
    from_address    BYTEA NOT NULL,
    to_address      BYTEA,
    value           NUMERIC(78) NOT NULL,
    gas             BIGINT,
    gas_used        BIGINT,
    input           BYTEA,
    output          BYTEA,
    error           TEXT,
    created_contract BYTEA,

    indexed_at      TIMESTAMPTZ DEFAULT NOW(),

    PRIMARY KEY (chain_id, transaction_hash, trace_address)
) PARTITION BY LIST (chain_id);

CREATE TABLE internal_transactions_96369 PARTITION OF internal_transactions FOR VALUES IN (96369);
CREATE TABLE internal_transactions_96368 PARTITION OF internal_transactions FOR VALUES IN (96368);
CREATE TABLE internal_transactions_200200 PARTITION OF internal_transactions FOR VALUES IN (200200);
CREATE TABLE internal_transactions_200201 PARTITION OF internal_transactions FOR VALUES IN (200201);
CREATE TABLE internal_transactions_36963 PARTITION OF internal_transactions FOR VALUES IN (36963);

-- Event logs
CREATE TABLE logs (
    id              BIGSERIAL,
    chain_id        BIGINT NOT NULL,
    block_number    BIGINT NOT NULL,
    block_hash      BYTEA NOT NULL,
    transaction_hash BYTEA NOT NULL,
    transaction_index INTEGER NOT NULL,
    log_index       INTEGER NOT NULL,

    address         BYTEA NOT NULL,
    data            BYTEA,
    topic0          BYTEA,
    topic1          BYTEA,
    topic2          BYTEA,
    topic3          BYTEA,
    topics          BYTEA[],

    -- Decoded event (Blockscout compatibility)
    decoded_name    VARCHAR(100),
    decoded_params  JSONB,

    removed         BOOLEAN DEFAULT FALSE,
    timestamp       BIGINT NOT NULL,
    indexed_at      TIMESTAMPTZ DEFAULT NOW(),

    PRIMARY KEY (chain_id, block_number, log_index)
) PARTITION BY LIST (chain_id);

CREATE TABLE logs_96369 PARTITION OF logs FOR VALUES IN (96369);
CREATE TABLE logs_96368 PARTITION OF logs FOR VALUES IN (96368);
CREATE TABLE logs_200200 PARTITION OF logs FOR VALUES IN (200200);
CREATE TABLE logs_200201 PARTITION OF logs FOR VALUES IN (200201);
CREATE TABLE logs_36963 PARTITION OF logs FOR VALUES IN (36963);

-- Addresses table (Blockscout: addresses)
CREATE TABLE addresses (
    id              BIGSERIAL,
    chain_id        BIGINT NOT NULL,
    hash            BYTEA NOT NULL,  -- Blockscout uses 'hash' not 'address'

    -- Fetched balance
    fetched_coin_balance NUMERIC(100),
    fetched_coin_balance_block_number BIGINT,

    -- Contract info
    contract_code   BYTEA,

    -- Verification
    verified        BOOLEAN DEFAULT FALSE,

    -- Stats
    transactions_count INTEGER DEFAULT 0,
    token_transfers_count INTEGER DEFAULT 0,
    gas_used        NUMERIC(100),

    -- Timestamps
    inserted_at     TIMESTAMPTZ DEFAULT NOW(),
    updated_at      TIMESTAMPTZ DEFAULT NOW(),

    PRIMARY KEY (chain_id, hash)
) PARTITION BY LIST (chain_id);

CREATE TABLE addresses_96369 PARTITION OF addresses FOR VALUES IN (96369);
CREATE TABLE addresses_96368 PARTITION OF addresses FOR VALUES IN (96368);
CREATE TABLE addresses_200200 PARTITION OF addresses FOR VALUES IN (200200);
CREATE TABLE addresses_200201 PARTITION OF addresses FOR VALUES IN (200201);
CREATE TABLE addresses_36963 PARTITION OF addresses FOR VALUES IN (36963);

-- Smart contract source verification (Blockscout: smart_contracts)
CREATE TABLE smart_contracts (
    id              BIGSERIAL PRIMARY KEY,
    chain_id        BIGINT NOT NULL,
    address_hash    BYTEA NOT NULL,

    name            VARCHAR(255) NOT NULL,
    compiler_version VARCHAR(255) NOT NULL,
    optimization    BOOLEAN DEFAULT FALSE,
    optimization_runs INTEGER,
    contract_source_code TEXT,
    abi             JSONB,
    constructor_arguments TEXT,
    evm_version     VARCHAR(50),

    -- Multi-file support
    external_libraries JSONB,
    secondary_sources JSONB,

    -- Verification metadata
    verified_via    VARCHAR(50),  -- sourcify, manual, etc.
    is_vyper_contract BOOLEAN DEFAULT FALSE,

    inserted_at     TIMESTAMPTZ DEFAULT NOW(),
    updated_at      TIMESTAMPTZ DEFAULT NOW(),

    UNIQUE (chain_id, address_hash)
);

-- Tokens (Blockscout: tokens)
CREATE TABLE tokens (
    id              BIGSERIAL PRIMARY KEY,
    chain_id        BIGINT NOT NULL,
    contract_address_hash BYTEA NOT NULL,

    name            VARCHAR(255),
    symbol          VARCHAR(255),
    total_supply    NUMERIC(100),
    decimals        SMALLINT,
    type            VARCHAR(50) NOT NULL,  -- ERC-20, ERC-721, ERC-1155

    holder_count    INTEGER DEFAULT 0,

    -- Metadata
    cataloged       BOOLEAN DEFAULT FALSE,
    skip_metadata   BOOLEAN DEFAULT FALSE,

    inserted_at     TIMESTAMPTZ DEFAULT NOW(),
    updated_at      TIMESTAMPTZ DEFAULT NOW(),

    UNIQUE (chain_id, contract_address_hash)
);

-- Token transfers (Blockscout: token_transfers)
CREATE TABLE token_transfers (
    id              BIGSERIAL,
    chain_id        BIGINT NOT NULL,
    transaction_hash BYTEA NOT NULL,
    log_index       INTEGER NOT NULL,
    block_number    BIGINT NOT NULL,

    from_address_hash BYTEA NOT NULL,
    to_address_hash BYTEA NOT NULL,
    token_contract_address_hash BYTEA NOT NULL,

    amount          NUMERIC(100),
    token_id        NUMERIC(78),
    token_ids       NUMERIC(78)[],
    amounts         NUMERIC(100)[],

    block_hash      BYTEA NOT NULL,

    inserted_at     TIMESTAMPTZ DEFAULT NOW(),

    PRIMARY KEY (chain_id, transaction_hash, log_index)
) PARTITION BY LIST (chain_id);

CREATE TABLE token_transfers_96369 PARTITION OF token_transfers FOR VALUES IN (96369);
CREATE TABLE token_transfers_96368 PARTITION OF token_transfers FOR VALUES IN (96368);
CREATE TABLE token_transfers_200200 PARTITION OF token_transfers FOR VALUES IN (200200);
CREATE TABLE token_transfers_200201 PARTITION OF token_transfers FOR VALUES IN (200201);
CREATE TABLE token_transfers_36963 PARTITION OF token_transfers FOR VALUES IN (36963);

-- Address token balances (Blockscout: address_token_balances)
CREATE TABLE address_token_balances (
    id              BIGSERIAL,
    chain_id        BIGINT NOT NULL,
    address_hash    BYTEA NOT NULL,
    token_contract_address_hash BYTEA NOT NULL,
    block_number    BIGINT NOT NULL,

    value           NUMERIC(100),
    value_fetched_at TIMESTAMPTZ,
    token_id        NUMERIC(78),
    token_type      VARCHAR(50),

    inserted_at     TIMESTAMPTZ DEFAULT NOW(),
    updated_at      TIMESTAMPTZ DEFAULT NOW(),

    PRIMARY KEY (chain_id, address_hash, token_contract_address_hash, block_number, COALESCE(token_id, 0))
) PARTITION BY LIST (chain_id);

CREATE TABLE address_token_balances_96369 PARTITION OF address_token_balances FOR VALUES IN (96369);
CREATE TABLE address_token_balances_96368 PARTITION OF address_token_balances FOR VALUES IN (96368);
CREATE TABLE address_token_balances_200200 PARTITION OF address_token_balances FOR VALUES IN (200200);
CREATE TABLE address_token_balances_200201 PARTITION OF address_token_balances FOR VALUES IN (200201);
CREATE TABLE address_token_balances_36963 PARTITION OF address_token_balances FOR VALUES IN (36963);

-- Indexer checkpoints
CREATE TABLE indexer_status (
    id              SERIAL PRIMARY KEY,
    chain_id        BIGINT NOT NULL,
    indexer_type    VARCHAR(50) NOT NULL,

    min_block       BIGINT,
    max_block       BIGINT,

    inserted_at     TIMESTAMPTZ DEFAULT NOW(),
    updated_at      TIMESTAMPTZ DEFAULT NOW(),

    UNIQUE (chain_id, indexer_type)
);

-- Method signatures (4-byte database)
CREATE TABLE transaction_actions (
    hash            BYTEA PRIMARY KEY,  -- 4-byte selector
    text            TEXT NOT NULL,      -- function signature
    inserted_at     TIMESTAMPTZ DEFAULT NOW()
);

-- Event signatures
CREATE TABLE logs_actions (
    hash            BYTEA PRIMARY KEY,  -- 32-byte topic0
    text            TEXT NOT NULL,      -- event signature
    inserted_at     TIMESTAMPTZ DEFAULT NOW()
);
```

### Index Definitions

```sql
-- migrations/002_indexes.sql

-- Block indexes
CREATE INDEX idx_blocks_hash ON blocks USING HASH (hash);
CREATE INDEX idx_blocks_timestamp ON blocks (chain_id, timestamp DESC);
CREATE INDEX idx_blocks_miner ON blocks (chain_id, miner);
CREATE INDEX idx_blocks_consensus_state ON blocks (chain_id, consensus_state);

-- Transaction indexes (critical for address queries)
CREATE INDEX idx_tx_block ON transactions (chain_id, block_number DESC);
CREATE INDEX idx_tx_from ON transactions (chain_id, from_address, block_number DESC);
CREATE INDEX idx_tx_to ON transactions (chain_id, to_address, block_number DESC);
CREATE INDEX idx_tx_created_contract ON transactions (chain_id, created_contract)
    WHERE created_contract IS NOT NULL;
CREATE INDEX idx_tx_timestamp ON transactions (chain_id, timestamp DESC);
CREATE INDEX idx_tx_status ON transactions (chain_id, status) WHERE status = 0;

-- Internal transaction indexes
CREATE INDEX idx_internal_tx_block ON internal_transactions (chain_id, block_number DESC);
CREATE INDEX idx_internal_tx_from ON internal_transactions (chain_id, from_address);
CREATE INDEX idx_internal_tx_to ON internal_transactions (chain_id, to_address);

-- Log indexes (critical for event filtering)
CREATE INDEX idx_logs_address ON logs (chain_id, address, block_number DESC);
CREATE INDEX idx_logs_topic0 ON logs (chain_id, topic0, block_number DESC);
CREATE INDEX idx_logs_topic0_address ON logs (chain_id, topic0, address, block_number DESC);
CREATE INDEX idx_logs_tx ON logs (chain_id, transaction_hash);
CREATE INDEX idx_logs_block_range ON logs (chain_id, block_number)
    INCLUDE (address, topic0);

-- Address indexes
CREATE INDEX idx_addresses_balance ON addresses (chain_id, fetched_coin_balance DESC NULLS LAST);
CREATE INDEX idx_addresses_tx_count ON addresses (chain_id, transactions_count DESC);
CREATE INDEX idx_addresses_contract ON addresses (chain_id) WHERE contract_code IS NOT NULL;

-- Token indexes
CREATE INDEX idx_tokens_type ON tokens (chain_id, type);
CREATE INDEX idx_tokens_symbol ON tokens USING GIN (symbol gin_trgm_ops);
CREATE INDEX idx_tokens_name ON tokens USING GIN (name gin_trgm_ops);
CREATE INDEX idx_tokens_holders ON tokens (chain_id, holder_count DESC);

-- Token transfer indexes
CREATE INDEX idx_token_transfers_token ON token_transfers (chain_id, token_contract_address_hash, block_number DESC);
CREATE INDEX idx_token_transfers_from ON token_transfers (chain_id, from_address_hash, block_number DESC);
CREATE INDEX idx_token_transfers_to ON token_transfers (chain_id, to_address_hash, block_number DESC);

-- Token balance indexes
CREATE INDEX idx_token_balances_holder ON address_token_balances (chain_id, address_hash);
CREATE INDEX idx_token_balances_token ON address_token_balances (chain_id, token_contract_address_hash, value DESC NULLS LAST);

-- Smart contract indexes
CREATE INDEX idx_smart_contracts_address ON smart_contracts (chain_id, address_hash);
CREATE INDEX idx_smart_contracts_name ON smart_contracts USING GIN (name gin_trgm_ops);
```

### Table Partitioning Strategy

For scale (millions of blocks), add range partitioning within each chain:

```sql
-- migrations/003_range_partitioning.sql

-- For high-volume chains, sub-partition by block range (10M blocks per partition)
-- Example for C-Chain mainnet:

CREATE TABLE blocks_96369_0_10m PARTITION OF blocks_96369
    FOR VALUES FROM (0) TO (10000000);

CREATE TABLE blocks_96369_10m_20m PARTITION OF blocks_96369
    FOR VALUES FROM (10000000) TO (20000000);

-- Automate with pg_partman for production
```

---

## 4. Blockscout API v2 Contract

### Required Endpoints

The following endpoints MUST be implemented with **exact response format** matching Blockscout:

#### Blocks

```
GET /api/v2/blocks
GET /api/v2/blocks/{block_number_or_hash}
GET /api/v2/blocks/{block_number_or_hash}/transactions
GET /api/v2/blocks/{block_number_or_hash}/withdrawals
```

**Response Schema** (blocks list):
```json
{
  "items": [
    {
      "base_fee_per_gas": "25000000000",
      "burnt_fees": "0",
      "burnt_fees_percentage": 0,
      "difficulty": "0",
      "extra_data": "0x",
      "gas_limit": "30000000",
      "gas_target_percentage": 0,
      "gas_used": "21000",
      "gas_used_percentage": 0.07,
      "hash": "0x...",
      "height": 1234567,
      "miner": {
        "hash": "0x...",
        "is_contract": false,
        "is_verified": false
      },
      "nonce": "0x0000000000000000",
      "parent_hash": "0x...",
      "priority_fee": "0",
      "rewards": [],
      "size": 1234,
      "state_root": "0x...",
      "timestamp": "2025-12-25T12:00:00.000000Z",
      "total_difficulty": "0",
      "tx_count": 1,
      "tx_fees": "525000000000000",
      "type": "block",
      "uncles_hashes": [],
      "withdrawals_count": 0
    }
  ],
  "next_page_params": {
    "block_number": 1234566,
    "items_count": 50
  }
}
```

#### Transactions

```
GET /api/v2/transactions
GET /api/v2/transactions/{transaction_hash}
GET /api/v2/transactions/{transaction_hash}/token-transfers
GET /api/v2/transactions/{transaction_hash}/internal-transactions
GET /api/v2/transactions/{transaction_hash}/logs
GET /api/v2/transactions/{transaction_hash}/raw-trace
GET /api/v2/transactions/{transaction_hash}/state-changes
```

**Response Schema** (single transaction):
```json
{
  "timestamp": "2025-12-25T12:00:00.000000Z",
  "fee": {
    "type": "actual",
    "value": "525000000000000"
  },
  "gas_limit": "21000",
  "block": 1234567,
  "status": "ok",
  "method": "transfer",
  "confirmations": 100,
  "type": 2,
  "exchange_rate": "1.23",
  "to": {
    "hash": "0x...",
    "is_contract": false,
    "is_verified": false
  },
  "tx_burnt_fee": "0",
  "max_fee_per_gas": "30000000000",
  "result": "success",
  "hash": "0x...",
  "gas_price": "25000000000",
  "priority_fee": "0",
  "base_fee_per_gas": "25000000000",
  "from": {
    "hash": "0x...",
    "is_contract": false,
    "is_verified": false
  },
  "token_transfers": [],
  "tx_types": ["coin_transfer"],
  "gas_used": "21000",
  "created_contract": null,
  "position": 0,
  "nonce": 42,
  "has_error_in_internal_txs": false,
  "actions": [],
  "decoded_input": null,
  "token_transfers_overflow": false,
  "raw_input": "0x",
  "value": "1000000000000000000",
  "max_priority_fee_per_gas": "0",
  "revert_reason": null,
  "confirmation_duration": [0, 1000],
  "tx_tag": null
}
```

#### Addresses

```
GET /api/v2/addresses/{address_hash}
GET /api/v2/addresses/{address_hash}/transactions
GET /api/v2/addresses/{address_hash}/token-transfers
GET /api/v2/addresses/{address_hash}/internal-transactions
GET /api/v2/addresses/{address_hash}/logs
GET /api/v2/addresses/{address_hash}/tokens
GET /api/v2/addresses/{address_hash}/token-balances
GET /api/v2/addresses/{address_hash}/coin-balance-history
GET /api/v2/addresses/{address_hash}/coin-balance-history-by-day
GET /api/v2/addresses/{address_hash}/withdrawals
GET /api/v2/addresses/{address_hash}/counters
```

**Response Schema** (address):
```json
{
  "hash": "0x...",
  "is_contract": false,
  "is_verified": false,
  "name": null,
  "private_tags": [],
  "public_tags": [],
  "watchlist_names": [],
  "creator_address_hash": null,
  "creation_tx_hash": null,
  "token": null,
  "coin_balance": "1000000000000000000",
  "exchange_rate": "1.23",
  "implementation_name": null,
  "implementation_address": null,
  "block_number_balance_updated_at": 1234567,
  "has_decompiled_code": false,
  "has_validated_blocks": false,
  "has_logs": true,
  "has_tokens": true,
  "has_token_transfers": true,
  "watchlist_address_id": null,
  "has_beacon_chain_withdrawals": false
}
```

#### Tokens

```
GET /api/v2/tokens
GET /api/v2/tokens/{address_hash}
GET /api/v2/tokens/{address_hash}/transfers
GET /api/v2/tokens/{address_hash}/holders
GET /api/v2/tokens/{address_hash}/counters
GET /api/v2/tokens/{address_hash}/instances  (NFT only)
GET /api/v2/tokens/{address_hash}/instances/{id}
```

**Response Schema** (token):
```json
{
  "address": "0x...",
  "circulating_market_cap": null,
  "decimals": "18",
  "exchange_rate": null,
  "holders": "1234",
  "icon_url": null,
  "name": "Test Token",
  "symbol": "TEST",
  "total_supply": "1000000000000000000000000",
  "type": "ERC-20",
  "volume_24h": null
}
```

#### Smart Contracts

```
GET /api/v2/smart-contracts/{address_hash}
GET /api/v2/smart-contracts/{address_hash}/methods-read
GET /api/v2/smart-contracts/{address_hash}/methods-read-proxy
GET /api/v2/smart-contracts/{address_hash}/methods-write
GET /api/v2/smart-contracts/{address_hash}/methods-write-proxy
POST /api/v2/smart-contracts/{address_hash}/query-read-method
```

#### Stats

```
GET /api/v2/stats
GET /api/v2/stats/charts/transactions
GET /api/v2/stats/charts/market
```

**Response Schema** (stats):
```json
{
  "total_blocks": "1234567",
  "total_addresses": "100000",
  "total_transactions": "5000000",
  "average_block_time": 2000,
  "coin_price": "1.23",
  "total_gas_used": "500000000000",
  "transactions_today": "10000",
  "gas_used_today": "1000000000",
  "gas_prices": {
    "slow": 25,
    "average": 30,
    "fast": 40
  },
  "static_gas_price": null,
  "market_cap": "1000000000",
  "network_utilization_percentage": 15.5
}
```

#### Search

```
GET /api/v2/search?q={query}
GET /api/v2/search/check-redirect?q={query}
```

#### Logs

```
GET /api/v2/addresses/{address_hash}/logs?topic={topic0}
```

---

## 5. Integration Points

### EVM Node Connection (JSON-RPC)

```go
// evm/indexer/client.go

type RPCClient struct {
    endpoints []string    // Load-balanced endpoints
    current   int         // Round-robin counter
    client    *http.Client
}

// Required RPC methods:
// - eth_blockNumber
// - eth_getBlockByNumber (with full transactions)
// - eth_getBlockReceipts (batch-optimized)
// - eth_getTransactionReceipt
// - eth_getCode
// - eth_call (for token metadata)
// - debug_traceBlockByNumber (optional, for internal txs)
// - eth_getLogs

// Batch RPC for efficiency
func (c *RPCClient) BatchCall(requests []RPCRequest) ([]RPCResponse, error) {
    // Send as JSON-RPC batch
    body, _ := json.Marshal(requests)
    resp, err := c.client.Post(c.endpoint(), "application/json", bytes.NewReader(body))
    // ...
}
```

### WebSocket Real-time Updates

```
WebSocket Endpoint: /api/v2/socket/websocket

Subscribe messages (Blockscout format):
{
  "topic": "blocks:new_block",
  "event": "phx_join",
  "payload": {},
  "ref": "1"
}

{
  "topic": "addresses:0x...",
  "event": "phx_join",
  "payload": {},
  "ref": "2"
}

// Server pushes:
{
  "topic": "blocks:new_block",
  "event": "new_block",
  "payload": { /* block data */ },
  "ref": null
}
```

### Redis Caching Strategy

```
Cache Keys:
- block:{chain_id}:{number}              TTL: 1 hour
- block:{chain_id}:latest                TTL: 2 seconds
- tx:{chain_id}:{hash}                   TTL: 1 hour
- address:{chain_id}:{hash}              TTL: 5 minutes
- address:{chain_id}:{hash}:balance      TTL: 30 seconds
- token:{chain_id}:{hash}                TTL: 5 minutes
- stats:{chain_id}                       TTL: 10 seconds

Pub/Sub Channels:
- indexer:{chain_id}:new_block
- indexer:{chain_id}:new_transaction
- indexer:{chain_id}:token_transfer
```

---

## 6. Performance Requirements

### Indexing Performance

| Metric | Target | Notes |
|--------|--------|-------|
| Block throughput | 10,000 blocks/sec | Backfill mode |
| Real-time latency | < 500ms | Block notification |
| Trace indexing | 1,000 blocks/sec | With debug_traceBlock |
| Token tracking | 50,000 transfers/sec | ERC-20/721/1155 |

### API Performance

| Metric | Target | Notes |
|--------|--------|-------|
| Request throughput | 50,000 RPS | Per API worker |
| P50 latency | < 10ms | Cached responses |
| P99 latency | < 100ms | Database queries |
| WebSocket connections | 100,000 | Per worker |

### Database Performance

| Metric | Target | Notes |
|--------|--------|-------|
| Write throughput | 100,000 rows/sec | Batch inserts |
| Read latency | < 5ms | Indexed queries |
| Storage efficiency | < 1KB/block | Average |

### Caching Strategy

| Cache Layer | Hit Rate Target | TTL |
|-------------|-----------------|-----|
| In-memory (Go) | 95% hot data | 30 seconds |
| Redis | 80% warm data | 5 minutes |
| PostgreSQL | 100% cold data | Persistent |

---

## 7. Migration Strategy

### From Blockscout

1. **Schema Mapping**: Use compatible table names
2. **Data Export**: Export blocks, transactions, tokens from Blockscout DB
3. **Import Pipeline**: Stream import with validation
4. **API Validation**: Compare responses with Blockscout
5. **Cutover**: DNS switch with health checks

### Deployment Phases

1. **Phase 1**: Core indexing (blocks, transactions)
2. **Phase 2**: Token tracking (ERC-20/721/1155)
3. **Phase 3**: Contract verification
4. **Phase 4**: Search and analytics
5. **Phase 5**: Real-time subscriptions

---

## 8. Operational Considerations

### Health Checks

```
GET /health           - Basic liveness
GET /health/ready     - Readiness (DB, Redis connected)
GET /api/v2/health    - Blockscout-compatible health
```

### Metrics (Prometheus)

```
indexer_blocks_indexed_total{chain_id}
indexer_blocks_per_second{chain_id}
indexer_reorgs_total{chain_id}
indexer_lag_blocks{chain_id}

api_requests_total{endpoint,status}
api_request_duration_seconds{endpoint}
api_active_websocket_connections{chain_id}

db_query_duration_seconds{query}
db_connection_pool_size
cache_hit_rate{layer}
```

### Logging

Structured JSON logging with:
- Request IDs for tracing
- Chain ID context
- Block numbers for indexer logs
- Transaction hashes for API logs

---

## 9. Appendix: Lux-Specific Features

### 19-Field Block Header

Lux C-Chain uses extended headers with:
- Field 16: `ExtDataHash` (cross-chain data)
- Field 17: `ExtDataGasUsed`
- Field 18: `BlockGasCost`

These are indexed for Warp message tracking.

### Precompile Awareness

The indexer recognizes Lux precompiles:
- `0x0200...0001-000E` - Standard precompiles
- `0x0300` - AI Mining precompile
- `0x0400-04FF` - DEX precompiles

### Warp Message Indexing

Cross-chain messages are decoded and indexed:
- Source chain ID
- Destination chain ID
- Message payload
- BLS aggregated signatures

---

*Last Updated: 2025-12-25*
*Architecture Version: 2.0.0*
