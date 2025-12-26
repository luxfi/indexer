-- Lux EVM Indexer Initial Schema
-- Version: 1.0.0
-- Date: 2025-12-25
-- Compatible with: Blockscout v6.x

-- Enable extensions
CREATE EXTENSION IF NOT EXISTS "pg_trgm";           -- Trigram for fuzzy search
CREATE EXTENSION IF NOT EXISTS "btree_gist";        -- GiST indexes
CREATE EXTENSION IF NOT EXISTS "pg_stat_statements"; -- Query analysis

--------------------------------------------------------------------------------
-- Chain Configuration
--------------------------------------------------------------------------------

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

--------------------------------------------------------------------------------
-- Blocks (Partitioned by chain_id)
--------------------------------------------------------------------------------

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
    is_empty        BOOLEAN DEFAULT FALSE,

    PRIMARY KEY (chain_id, number)
) PARTITION BY LIST (chain_id);

-- Create partitions for known chains
CREATE TABLE blocks_96369 PARTITION OF blocks FOR VALUES IN (96369);
CREATE TABLE blocks_96368 PARTITION OF blocks FOR VALUES IN (96368);
CREATE TABLE blocks_200200 PARTITION OF blocks FOR VALUES IN (200200);
CREATE TABLE blocks_200201 PARTITION OF blocks FOR VALUES IN (200201);
CREATE TABLE blocks_36963 PARTITION OF blocks FOR VALUES IN (36963);

--------------------------------------------------------------------------------
-- Transactions (Partitioned by chain_id)
--------------------------------------------------------------------------------

CREATE TABLE transactions (
    id              BIGSERIAL,
    chain_id        BIGINT NOT NULL,
    hash            BYTEA NOT NULL,
    block_number    BIGINT NOT NULL,
    block_hash      BYTEA NOT NULL,
    transaction_index INTEGER NOT NULL,

    -- Core fields (Blockscout compatible)
    from_address_hash BYTEA NOT NULL,
    to_address_hash BYTEA,
    value           NUMERIC(100) NOT NULL,
    gas             BIGINT NOT NULL,
    gas_price       NUMERIC(100),
    gas_used        BIGINT,

    -- EIP-1559 fields
    max_fee_per_gas NUMERIC(100),
    max_priority_fee_per_gas NUMERIC(100),

    -- EIP-4844 blob fields
    max_fee_per_blob_gas NUMERIC(100),
    blob_gas_used   BIGINT,
    blob_gas_price  NUMERIC(100),

    -- TX data
    input           BYTEA,
    nonce           BIGINT NOT NULL,
    type            SMALLINT NOT NULL DEFAULT 0,

    -- Status (Blockscout uses integer: 1=success, 0=fail, NULL=pending)
    status          SMALLINT,
    error           TEXT,
    revert_reason   TEXT,

    -- Contract creation
    created_contract_address_hash BYTEA,
    created_contract_code_indexed_at TIMESTAMPTZ,

    -- Cumulative gas
    cumulative_gas_used BIGINT,

    -- Timestamps
    block_timestamp BIGINT NOT NULL,
    inserted_at     TIMESTAMPTZ DEFAULT NOW(),
    updated_at      TIMESTAMPTZ DEFAULT NOW(),

    PRIMARY KEY (chain_id, hash),
    FOREIGN KEY (chain_id, block_number) REFERENCES blocks(chain_id, number) ON DELETE CASCADE
) PARTITION BY LIST (chain_id);

CREATE TABLE transactions_96369 PARTITION OF transactions FOR VALUES IN (96369);
CREATE TABLE transactions_96368 PARTITION OF transactions FOR VALUES IN (96368);
CREATE TABLE transactions_200200 PARTITION OF transactions FOR VALUES IN (200200);
CREATE TABLE transactions_200201 PARTITION OF transactions FOR VALUES IN (200201);
CREATE TABLE transactions_36963 PARTITION OF transactions FOR VALUES IN (36963);

--------------------------------------------------------------------------------
-- Internal Transactions (Traces)
--------------------------------------------------------------------------------

CREATE TABLE internal_transactions (
    id              BIGSERIAL,
    chain_id        BIGINT NOT NULL,
    transaction_hash BYTEA NOT NULL,
    block_number    BIGINT NOT NULL,
    block_hash      BYTEA NOT NULL,
    index           INTEGER NOT NULL,
    trace_address   INTEGER[] NOT NULL DEFAULT '{}',

    -- Trace type (Blockscout compatible)
    type            VARCHAR(20) NOT NULL,  -- call, create, selfdestruct, reward
    call_type       VARCHAR(20),           -- call, delegatecall, staticcall, callcode

    -- Addresses
    from_address_hash BYTEA NOT NULL,
    to_address_hash BYTEA,
    created_contract_address_hash BYTEA,
    created_contract_code BYTEA,

    -- Values
    value           NUMERIC(100) NOT NULL DEFAULT 0,
    gas             BIGINT,
    gas_used        BIGINT,
    input           BYTEA,
    output          BYTEA,
    init            BYTEA,
    error           TEXT,

    -- Timestamps
    block_timestamp BIGINT NOT NULL,
    inserted_at     TIMESTAMPTZ DEFAULT NOW(),
    updated_at      TIMESTAMPTZ DEFAULT NOW(),

    PRIMARY KEY (chain_id, transaction_hash, trace_address)
) PARTITION BY LIST (chain_id);

CREATE TABLE internal_transactions_96369 PARTITION OF internal_transactions FOR VALUES IN (96369);
CREATE TABLE internal_transactions_96368 PARTITION OF internal_transactions FOR VALUES IN (96368);
CREATE TABLE internal_transactions_200200 PARTITION OF internal_transactions FOR VALUES IN (200200);
CREATE TABLE internal_transactions_200201 PARTITION OF internal_transactions FOR VALUES IN (200201);
CREATE TABLE internal_transactions_36963 PARTITION OF internal_transactions FOR VALUES IN (36963);

--------------------------------------------------------------------------------
-- Event Logs (Partitioned by chain_id)
--------------------------------------------------------------------------------

CREATE TABLE logs (
    id              BIGSERIAL,
    chain_id        BIGINT NOT NULL,
    block_number    BIGINT NOT NULL,
    block_hash      BYTEA NOT NULL,
    transaction_hash BYTEA NOT NULL,
    transaction_index INTEGER NOT NULL,
    index           INTEGER NOT NULL,  -- log_index within block

    -- Log data (Blockscout compatible field names)
    address_hash    BYTEA NOT NULL,
    data            BYTEA,
    first_topic     BYTEA,   -- topic0
    second_topic    BYTEA,   -- topic1
    third_topic     BYTEA,   -- topic2
    fourth_topic    BYTEA,   -- topic3

    -- Decoded event (if ABI available)
    type            VARCHAR(100),  -- Decoded event name

    -- Timestamps
    block_timestamp BIGINT NOT NULL,
    inserted_at     TIMESTAMPTZ DEFAULT NOW(),

    PRIMARY KEY (chain_id, block_number, index)
) PARTITION BY LIST (chain_id);

CREATE TABLE logs_96369 PARTITION OF logs FOR VALUES IN (96369);
CREATE TABLE logs_96368 PARTITION OF logs FOR VALUES IN (96368);
CREATE TABLE logs_200200 PARTITION OF logs FOR VALUES IN (200200);
CREATE TABLE logs_200201 PARTITION OF logs FOR VALUES IN (200201);
CREATE TABLE logs_36963 PARTITION OF logs FOR VALUES IN (36963);

--------------------------------------------------------------------------------
-- Addresses (Blockscout compatible)
--------------------------------------------------------------------------------

CREATE TABLE addresses (
    id              BIGSERIAL,
    chain_id        BIGINT NOT NULL,
    hash            BYTEA NOT NULL,

    -- Fetched balance (Blockscout uses this naming)
    fetched_coin_balance NUMERIC(100),
    fetched_coin_balance_block_number BIGINT,

    -- Contract info
    contract_code   BYTEA,

    -- Stats
    transactions_count INTEGER DEFAULT 0,
    token_transfers_count INTEGER DEFAULT 0,
    gas_used        NUMERIC(100) DEFAULT 0,

    -- Nonce (last known)
    nonce           BIGINT,

    -- Decompiled code
    decompiled      BOOLEAN DEFAULT FALSE,
    verified        BOOLEAN DEFAULT FALSE,

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

--------------------------------------------------------------------------------
-- Smart Contracts (Verified Sources)
--------------------------------------------------------------------------------

CREATE TABLE smart_contracts (
    id              BIGSERIAL PRIMARY KEY,
    chain_id        BIGINT NOT NULL,
    address_hash    BYTEA NOT NULL,

    -- Contract metadata
    name            VARCHAR(255) NOT NULL,
    compiler_version VARCHAR(255) NOT NULL,
    optimization    BOOLEAN DEFAULT FALSE,
    optimization_runs INTEGER,
    contract_source_code TEXT,
    abi             JSONB,
    constructor_arguments TEXT,
    evm_version     VARCHAR(50),

    -- Multi-file support
    file_path       VARCHAR(255),
    external_libraries JSONB DEFAULT '[]',
    secondary_sources JSONB DEFAULT '[]',

    -- Verification metadata
    verified_via    VARCHAR(50),  -- sourcify, manual, etherscan, etc.
    partially_verified BOOLEAN DEFAULT FALSE,
    is_vyper_contract BOOLEAN DEFAULT FALSE,
    is_changed_bytecode BOOLEAN DEFAULT FALSE,

    -- License
    license_type    VARCHAR(50),

    -- Timestamps
    inserted_at     TIMESTAMPTZ DEFAULT NOW(),
    updated_at      TIMESTAMPTZ DEFAULT NOW(),

    UNIQUE (chain_id, address_hash)
);

--------------------------------------------------------------------------------
-- Tokens (ERC-20, ERC-721, ERC-1155)
--------------------------------------------------------------------------------

CREATE TABLE tokens (
    id              BIGSERIAL PRIMARY KEY,
    chain_id        BIGINT NOT NULL,
    contract_address_hash BYTEA NOT NULL,

    -- Token metadata
    name            VARCHAR(255),
    symbol          VARCHAR(255),
    total_supply    NUMERIC(100),
    decimals        SMALLINT,
    type            VARCHAR(50) NOT NULL,  -- ERC-20, ERC-721, ERC-1155

    -- Stats
    holder_count    INTEGER DEFAULT 0,
    fiat_value      NUMERIC(100),
    circulating_market_cap NUMERIC(100),
    total_supply_updated_at_block BIGINT,

    -- Metadata flags
    cataloged       BOOLEAN DEFAULT FALSE,
    skip_metadata   BOOLEAN DEFAULT FALSE,

    -- Icon
    icon_url        TEXT,

    -- Timestamps
    inserted_at     TIMESTAMPTZ DEFAULT NOW(),
    updated_at      TIMESTAMPTZ DEFAULT NOW(),

    UNIQUE (chain_id, contract_address_hash)
);

--------------------------------------------------------------------------------
-- Token Transfers
--------------------------------------------------------------------------------

CREATE TABLE token_transfers (
    id              BIGSERIAL,
    chain_id        BIGINT NOT NULL,
    transaction_hash BYTEA NOT NULL,
    log_index       INTEGER NOT NULL,
    block_number    BIGINT NOT NULL,
    block_hash      BYTEA NOT NULL,

    -- Transfer details (Blockscout compatible field names)
    from_address_hash BYTEA NOT NULL,
    to_address_hash BYTEA NOT NULL,
    token_contract_address_hash BYTEA NOT NULL,

    -- ERC-20 amount
    amount          NUMERIC(100),

    -- ERC-721/1155 token ID
    token_id        NUMERIC(78),

    -- ERC-1155 batch support
    token_ids       NUMERIC(78)[],
    amounts         NUMERIC(100)[],

    -- Type (derived from token type)
    token_type      VARCHAR(50),

    -- Timestamps
    block_timestamp BIGINT NOT NULL,
    inserted_at     TIMESTAMPTZ DEFAULT NOW(),

    PRIMARY KEY (chain_id, transaction_hash, log_index)
) PARTITION BY LIST (chain_id);

CREATE TABLE token_transfers_96369 PARTITION OF token_transfers FOR VALUES IN (96369);
CREATE TABLE token_transfers_96368 PARTITION OF token_transfers FOR VALUES IN (96368);
CREATE TABLE token_transfers_200200 PARTITION OF token_transfers FOR VALUES IN (200200);
CREATE TABLE token_transfers_200201 PARTITION OF token_transfers FOR VALUES IN (200201);
CREATE TABLE token_transfers_36963 PARTITION OF token_transfers FOR VALUES IN (36963);

--------------------------------------------------------------------------------
-- Address Token Balances (Current State)
--------------------------------------------------------------------------------

CREATE TABLE address_token_balances (
    id              BIGSERIAL,
    chain_id        BIGINT NOT NULL,
    address_hash    BYTEA NOT NULL,
    token_contract_address_hash BYTEA NOT NULL,
    block_number    BIGINT NOT NULL,

    -- Balance
    value           NUMERIC(100),
    value_fetched_at TIMESTAMPTZ,

    -- ERC-721/1155 token ID
    token_id        NUMERIC(78),
    token_type      VARCHAR(50),

    -- Timestamps
    inserted_at     TIMESTAMPTZ DEFAULT NOW(),
    updated_at      TIMESTAMPTZ DEFAULT NOW(),

    PRIMARY KEY (chain_id, address_hash, token_contract_address_hash, block_number, COALESCE(token_id, 0))
) PARTITION BY LIST (chain_id);

CREATE TABLE address_token_balances_96369 PARTITION OF address_token_balances FOR VALUES IN (96369);
CREATE TABLE address_token_balances_96368 PARTITION OF address_token_balances FOR VALUES IN (96368);
CREATE TABLE address_token_balances_200200 PARTITION OF address_token_balances FOR VALUES IN (200200);
CREATE TABLE address_token_balances_200201 PARTITION OF address_token_balances FOR VALUES IN (200201);
CREATE TABLE address_token_balances_36963 PARTITION OF address_token_balances FOR VALUES IN (36963);

--------------------------------------------------------------------------------
-- Address Current Token Balances (Latest only, for fast queries)
--------------------------------------------------------------------------------

CREATE TABLE address_current_token_balances (
    id              BIGSERIAL,
    chain_id        BIGINT NOT NULL,
    address_hash    BYTEA NOT NULL,
    token_contract_address_hash BYTEA NOT NULL,

    -- Balance
    value           NUMERIC(100),
    value_fetched_at TIMESTAMPTZ,
    block_number    BIGINT NOT NULL,

    -- ERC-721/1155
    token_id        NUMERIC(78),
    token_type      VARCHAR(50),

    -- Old balance for delta tracking
    old_value       NUMERIC(100),

    -- Timestamps
    inserted_at     TIMESTAMPTZ DEFAULT NOW(),
    updated_at      TIMESTAMPTZ DEFAULT NOW(),

    PRIMARY KEY (chain_id, address_hash, token_contract_address_hash, COALESCE(token_id, 0))
) PARTITION BY LIST (chain_id);

CREATE TABLE address_current_token_balances_96369 PARTITION OF address_current_token_balances FOR VALUES IN (96369);
CREATE TABLE address_current_token_balances_96368 PARTITION OF address_current_token_balances FOR VALUES IN (96368);
CREATE TABLE address_current_token_balances_200200 PARTITION OF address_current_token_balances FOR VALUES IN (200200);
CREATE TABLE address_current_token_balances_200201 PARTITION OF address_current_token_balances FOR VALUES IN (200201);
CREATE TABLE address_current_token_balances_36963 PARTITION OF address_current_token_balances FOR VALUES IN (36963);

--------------------------------------------------------------------------------
-- Coin Balance History
--------------------------------------------------------------------------------

CREATE TABLE address_coin_balances (
    id              BIGSERIAL,
    chain_id        BIGINT NOT NULL,
    address_hash    BYTEA NOT NULL,
    block_number    BIGINT NOT NULL,
    value           NUMERIC(100),
    value_fetched_at TIMESTAMPTZ,
    delta           NUMERIC(100),

    inserted_at     TIMESTAMPTZ DEFAULT NOW(),

    PRIMARY KEY (chain_id, address_hash, block_number)
) PARTITION BY LIST (chain_id);

CREATE TABLE address_coin_balances_96369 PARTITION OF address_coin_balances FOR VALUES IN (96369);
CREATE TABLE address_coin_balances_96368 PARTITION OF address_coin_balances FOR VALUES IN (96368);
CREATE TABLE address_coin_balances_200200 PARTITION OF address_coin_balances FOR VALUES IN (200200);
CREATE TABLE address_coin_balances_200201 PARTITION OF address_coin_balances FOR VALUES IN (200201);
CREATE TABLE address_coin_balances_36963 PARTITION OF address_coin_balances FOR VALUES IN (36963);

--------------------------------------------------------------------------------
-- Indexer Status
--------------------------------------------------------------------------------

CREATE TABLE indexer_status (
    id              SERIAL PRIMARY KEY,
    chain_id        BIGINT NOT NULL,
    indexer_type    VARCHAR(50) NOT NULL,  -- blocks, transactions, traces, tokens, etc.

    -- Block range
    min_block       BIGINT,
    max_block       BIGINT,

    -- Stats
    indexed_count   BIGINT DEFAULT 0,
    error_count     BIGINT DEFAULT 0,
    last_error      TEXT,

    -- State
    is_running      BOOLEAN DEFAULT FALSE,

    -- Timestamps
    started_at      TIMESTAMPTZ,
    inserted_at     TIMESTAMPTZ DEFAULT NOW(),
    updated_at      TIMESTAMPTZ DEFAULT NOW(),

    UNIQUE (chain_id, indexer_type)
);

--------------------------------------------------------------------------------
-- Method/Event Signatures (4-byte database)
--------------------------------------------------------------------------------

CREATE TABLE transaction_actions (
    id              BIGSERIAL PRIMARY KEY,
    hash            BYTEA NOT NULL,      -- 4-byte selector
    text            TEXT NOT NULL,        -- function signature
    inserted_at     TIMESTAMPTZ DEFAULT NOW(),

    UNIQUE (hash)
);

CREATE TABLE logs_actions (
    id              BIGSERIAL PRIMARY KEY,
    hash            BYTEA NOT NULL,      -- 32-byte topic0
    text            TEXT NOT NULL,        -- event signature
    inserted_at     TIMESTAMPTZ DEFAULT NOW(),

    UNIQUE (hash)
);

-- Pre-populate common signatures
INSERT INTO transaction_actions (hash, text) VALUES
    (E'\\xa9059cbb', 'transfer(address,uint256)'),
    (E'\\x095ea7b3', 'approve(address,uint256)'),
    (E'\\x23b872dd', 'transferFrom(address,address,uint256)'),
    (E'\\x40c10f19', 'mint(address,uint256)'),
    (E'\\x42966c68', 'burn(uint256)'),
    (E'\\xa22cb465', 'setApprovalForAll(address,bool)')
ON CONFLICT (hash) DO NOTHING;

INSERT INTO logs_actions (hash, text) VALUES
    (E'\\xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef', 'Transfer(address,address,uint256)'),
    (E'\\x8c5be1e5ebec7d5bd14f71427d1e84f3dd0314c0f7b2291e5b200ac8c7c3b925', 'Approval(address,address,uint256)'),
    (E'\\xc3d58168c5ae7397731d063d5bbf3d657854427343f4c083240f7aacaa2d0f62', 'TransferSingle(address,address,address,uint256,uint256)'),
    (E'\\x4a39dc06d4c0dbc64b70af90fd698a233a518aa5d07e595d983b8c0526c8f7fb', 'TransferBatch(address,address,address,uint256[],uint256[])')
ON CONFLICT (hash) DO NOTHING;

--------------------------------------------------------------------------------
-- Address Tags (Labels)
--------------------------------------------------------------------------------

CREATE TABLE address_tags (
    id              BIGSERIAL PRIMARY KEY,
    chain_id        BIGINT NOT NULL,
    address_hash    BYTEA NOT NULL,

    -- Tag info
    name            VARCHAR(255) NOT NULL,
    label           VARCHAR(255),
    display_name    VARCHAR(255),

    -- Tag type
    is_public       BOOLEAN DEFAULT TRUE,

    -- Timestamps
    inserted_at     TIMESTAMPTZ DEFAULT NOW(),
    updated_at      TIMESTAMPTZ DEFAULT NOW(),

    UNIQUE (chain_id, address_hash, name)
);

--------------------------------------------------------------------------------
-- Block Rewards
--------------------------------------------------------------------------------

CREATE TABLE block_rewards (
    id              BIGSERIAL,
    chain_id        BIGINT NOT NULL,
    block_number    BIGINT NOT NULL,
    address_hash    BYTEA NOT NULL,

    -- Reward info
    address_type    VARCHAR(20) NOT NULL,  -- validator, uncle, emission
    reward          NUMERIC(100) NOT NULL,

    -- Timestamps
    inserted_at     TIMESTAMPTZ DEFAULT NOW(),

    PRIMARY KEY (chain_id, block_number, address_hash, address_type)
) PARTITION BY LIST (chain_id);

CREATE TABLE block_rewards_96369 PARTITION OF block_rewards FOR VALUES IN (96369);
CREATE TABLE block_rewards_96368 PARTITION OF block_rewards FOR VALUES IN (96368);
CREATE TABLE block_rewards_200200 PARTITION OF block_rewards FOR VALUES IN (200200);
CREATE TABLE block_rewards_200201 PARTITION OF block_rewards FOR VALUES IN (200201);
CREATE TABLE block_rewards_36963 PARTITION OF block_rewards FOR VALUES IN (36963);
