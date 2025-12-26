-- Lux EVM Indexer Index Definitions
-- Version: 1.0.0
-- Date: 2025-12-25
-- Optimized for Blockscout API query patterns

--------------------------------------------------------------------------------
-- Block Indexes
--------------------------------------------------------------------------------

-- Hash lookup (used by /api/v2/blocks/{hash})
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_blocks_hash
    ON blocks USING HASH (hash);

-- Timestamp ordering (used by block lists)
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_blocks_timestamp
    ON blocks (chain_id, timestamp DESC);

-- Miner lookup (used by /api/v2/addresses/{address}/blocks-validated)
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_blocks_miner
    ON blocks (chain_id, miner);

-- Consensus state (for pending blocks)
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_blocks_consensus_state
    ON blocks (chain_id, consensus_state)
    WHERE consensus_state != 'finalized';

-- Empty blocks filter
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_blocks_empty
    ON blocks (chain_id, number DESC)
    WHERE is_empty = TRUE;

--------------------------------------------------------------------------------
-- Transaction Indexes
--------------------------------------------------------------------------------

-- Block lookup (used by /api/v2/blocks/{block}/transactions)
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_tx_block
    ON transactions (chain_id, block_number DESC, transaction_index);

-- From address lookup (critical for address transaction lists)
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_tx_from
    ON transactions (chain_id, from_address_hash, block_number DESC);

-- To address lookup
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_tx_to
    ON transactions (chain_id, to_address_hash, block_number DESC)
    WHERE to_address_hash IS NOT NULL;

-- Contract creation
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_tx_created_contract
    ON transactions (chain_id, created_contract_address_hash)
    WHERE created_contract_address_hash IS NOT NULL;

-- Timestamp ordering
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_tx_timestamp
    ON transactions (chain_id, block_timestamp DESC);

-- Failed transactions
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_tx_status_failed
    ON transactions (chain_id, block_number DESC)
    WHERE status = 0;

-- Pending transactions (status NULL)
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_tx_pending
    ON transactions (chain_id, inserted_at DESC)
    WHERE status IS NULL;

-- Nonce lookup for address (for pending tx ordering)
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_tx_from_nonce
    ON transactions (chain_id, from_address_hash, nonce DESC);

--------------------------------------------------------------------------------
-- Internal Transaction Indexes
--------------------------------------------------------------------------------

-- Block lookup
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_internal_tx_block
    ON internal_transactions (chain_id, block_number DESC);

-- Transaction lookup (used by /api/v2/transactions/{hash}/internal-transactions)
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_internal_tx_hash
    ON internal_transactions (chain_id, transaction_hash);

-- From address
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_internal_tx_from
    ON internal_transactions (chain_id, from_address_hash);

-- To address
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_internal_tx_to
    ON internal_transactions (chain_id, to_address_hash)
    WHERE to_address_hash IS NOT NULL;

-- Contract creation
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_internal_tx_created
    ON internal_transactions (chain_id, created_contract_address_hash)
    WHERE created_contract_address_hash IS NOT NULL;

-- Error tracking
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_internal_tx_error
    ON internal_transactions (chain_id, block_number DESC)
    WHERE error IS NOT NULL;

--------------------------------------------------------------------------------
-- Log Indexes (Critical for event filtering)
--------------------------------------------------------------------------------

-- Address lookup (used by /api/v2/addresses/{address}/logs)
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_logs_address
    ON logs (chain_id, address_hash, block_number DESC);

-- Topic0 lookup (event signature)
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_logs_topic0
    ON logs (chain_id, first_topic, block_number DESC);

-- Combined topic0 + address (most common filter)
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_logs_topic0_address
    ON logs (chain_id, first_topic, address_hash, block_number DESC);

-- Transaction lookup
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_logs_tx
    ON logs (chain_id, transaction_hash);

-- Block range scan with covering columns
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_logs_block_range
    ON logs (chain_id, block_number DESC)
    INCLUDE (address_hash, first_topic);

-- Multi-topic filter (for complex event queries)
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_logs_topics
    ON logs (chain_id, first_topic, second_topic, block_number DESC)
    WHERE second_topic IS NOT NULL;

-- Decoded event type
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_logs_type
    ON logs (chain_id, type, block_number DESC)
    WHERE type IS NOT NULL;

--------------------------------------------------------------------------------
-- Address Indexes
--------------------------------------------------------------------------------

-- Balance ranking (used by /api/v2/addresses top holders)
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_addresses_balance
    ON addresses (chain_id, fetched_coin_balance DESC NULLS LAST);

-- Transaction count ranking
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_addresses_tx_count
    ON addresses (chain_id, transactions_count DESC);

-- Contract filter
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_addresses_contract
    ON addresses (chain_id)
    WHERE contract_code IS NOT NULL;

-- Verified contracts
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_addresses_verified
    ON addresses (chain_id)
    WHERE verified = TRUE;

-- Balance updated block
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_addresses_balance_block
    ON addresses (chain_id, fetched_coin_balance_block_number DESC);

--------------------------------------------------------------------------------
-- Smart Contract Indexes
--------------------------------------------------------------------------------

-- Address lookup
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_smart_contracts_address
    ON smart_contracts (chain_id, address_hash);

-- Name search (fuzzy)
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_smart_contracts_name
    ON smart_contracts USING GIN (name gin_trgm_ops);

-- Compiler version
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_smart_contracts_compiler
    ON smart_contracts (chain_id, compiler_version);

-- Verified via
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_smart_contracts_verified_via
    ON smart_contracts (chain_id, verified_via);

--------------------------------------------------------------------------------
-- Token Indexes
--------------------------------------------------------------------------------

-- Type filter
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_tokens_type
    ON tokens (chain_id, type);

-- Symbol search (fuzzy, used by search)
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_tokens_symbol
    ON tokens USING GIN (symbol gin_trgm_ops);

-- Name search (fuzzy)
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_tokens_name
    ON tokens USING GIN (name gin_trgm_ops);

-- Holder count ranking
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_tokens_holders
    ON tokens (chain_id, holder_count DESC);

-- Market cap ranking
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_tokens_market_cap
    ON tokens (chain_id, circulating_market_cap DESC NULLS LAST);

-- Address lookup
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_tokens_address
    ON tokens (chain_id, contract_address_hash);

--------------------------------------------------------------------------------
-- Token Transfer Indexes
--------------------------------------------------------------------------------

-- Token contract lookup (used by /api/v2/tokens/{address}/transfers)
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_token_transfers_token
    ON token_transfers (chain_id, token_contract_address_hash, block_number DESC);

-- From address (used by /api/v2/addresses/{address}/token-transfers)
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_token_transfers_from
    ON token_transfers (chain_id, from_address_hash, block_number DESC);

-- To address
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_token_transfers_to
    ON token_transfers (chain_id, to_address_hash, block_number DESC);

-- Block lookup
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_token_transfers_block
    ON token_transfers (chain_id, block_number DESC);

-- Transaction lookup
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_token_transfers_tx
    ON token_transfers (chain_id, transaction_hash);

-- NFT token ID
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_token_transfers_token_id
    ON token_transfers (chain_id, token_contract_address_hash, token_id)
    WHERE token_id IS NOT NULL;

--------------------------------------------------------------------------------
-- Token Balance Indexes
--------------------------------------------------------------------------------

-- Holder lookup
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_token_balances_holder
    ON address_token_balances (chain_id, address_hash);

-- Token contract with balance ranking
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_token_balances_token
    ON address_token_balances (chain_id, token_contract_address_hash, value DESC NULLS LAST);

-- Current token balance indexes (fast queries)
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_current_token_balances_holder
    ON address_current_token_balances (chain_id, address_hash);

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_current_token_balances_token_value
    ON address_current_token_balances (chain_id, token_contract_address_hash, value DESC NULLS LAST)
    WHERE value > 0;

--------------------------------------------------------------------------------
-- Coin Balance History Indexes
--------------------------------------------------------------------------------

-- Address history lookup
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_coin_balances_address
    ON address_coin_balances (chain_id, address_hash, block_number DESC);

--------------------------------------------------------------------------------
-- Signature Indexes
--------------------------------------------------------------------------------

-- Hash lookup (for decoding)
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_tx_actions_hash
    ON transaction_actions USING HASH (hash);

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_logs_actions_hash
    ON logs_actions USING HASH (hash);

--------------------------------------------------------------------------------
-- Address Tags Indexes
--------------------------------------------------------------------------------

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_address_tags_address
    ON address_tags (chain_id, address_hash);

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_address_tags_name
    ON address_tags USING GIN (name gin_trgm_ops);

--------------------------------------------------------------------------------
-- Block Rewards Indexes
--------------------------------------------------------------------------------

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_block_rewards_address
    ON block_rewards (chain_id, address_hash, block_number DESC);

--------------------------------------------------------------------------------
-- Composite Indexes for Common Query Patterns
--------------------------------------------------------------------------------

-- Transaction list with all required columns (avoid table access)
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_tx_list_covering
    ON transactions (chain_id, block_number DESC, transaction_index)
    INCLUDE (hash, from_address_hash, to_address_hash, value, gas_used, status);

-- Address activity summary
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_addresses_activity
    ON addresses (chain_id, updated_at DESC)
    WHERE transactions_count > 0;

-- Recent tokens
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_tokens_recent
    ON tokens (chain_id, inserted_at DESC);

--------------------------------------------------------------------------------
-- Partial Indexes for Query Optimization
--------------------------------------------------------------------------------

-- Only non-zero balances matter for holder queries
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_current_token_balances_nonzero
    ON address_current_token_balances (chain_id, token_contract_address_hash, address_hash)
    WHERE value > 0;

-- Only successful transactions for most queries
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_tx_successful
    ON transactions (chain_id, block_number DESC)
    WHERE status = 1;

-- Only ERC-20 tokens for most token queries
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_tokens_erc20
    ON tokens (chain_id, holder_count DESC)
    WHERE type = 'ERC-20';

-- Only ERC-721 tokens
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_tokens_erc721
    ON tokens (chain_id, holder_count DESC)
    WHERE type = 'ERC-721';

-- Only ERC-1155 tokens
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_tokens_erc1155
    ON tokens (chain_id, holder_count DESC)
    WHERE type = 'ERC-1155';
