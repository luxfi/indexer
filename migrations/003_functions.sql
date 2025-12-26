-- Lux EVM Indexer Database Functions
-- Version: 1.0.0
-- Date: 2025-12-25
-- Utility functions for indexer operations

--------------------------------------------------------------------------------
-- Address Statistics Functions
--------------------------------------------------------------------------------

-- Update address transaction count after new transactions
CREATE OR REPLACE FUNCTION update_address_tx_count()
RETURNS TRIGGER AS $$
BEGIN
    -- Update from address count
    INSERT INTO addresses (chain_id, hash, transactions_count, inserted_at, updated_at)
    VALUES (NEW.chain_id, NEW.from_address_hash, 1, NOW(), NOW())
    ON CONFLICT (chain_id, hash) DO UPDATE
    SET transactions_count = addresses.transactions_count + 1,
        updated_at = NOW();

    -- Update to address count if exists
    IF NEW.to_address_hash IS NOT NULL THEN
        INSERT INTO addresses (chain_id, hash, transactions_count, inserted_at, updated_at)
        VALUES (NEW.chain_id, NEW.to_address_hash, 1, NOW(), NOW())
        ON CONFLICT (chain_id, hash) DO UPDATE
        SET transactions_count = addresses.transactions_count + 1,
            updated_at = NOW();
    END IF;

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Trigger for address tx count (disabled by default for bulk import)
-- CREATE TRIGGER trigger_update_address_tx_count
--     AFTER INSERT ON transactions
--     FOR EACH ROW
--     EXECUTE FUNCTION update_address_tx_count();

--------------------------------------------------------------------------------
-- Token Holder Count Functions
--------------------------------------------------------------------------------

-- Update token holder count after balance changes
CREATE OR REPLACE FUNCTION update_token_holder_count()
RETURNS TRIGGER AS $$
DECLARE
    v_old_value NUMERIC(100);
    v_new_value NUMERIC(100);
BEGIN
    v_old_value := COALESCE(OLD.value, 0);
    v_new_value := COALESCE(NEW.value, 0);

    -- If going from zero to non-zero, increment holder count
    IF v_old_value = 0 AND v_new_value > 0 THEN
        UPDATE tokens
        SET holder_count = holder_count + 1,
            updated_at = NOW()
        WHERE chain_id = NEW.chain_id
          AND contract_address_hash = NEW.token_contract_address_hash;
    -- If going from non-zero to zero, decrement holder count
    ELSIF v_old_value > 0 AND v_new_value = 0 THEN
        UPDATE tokens
        SET holder_count = GREATEST(0, holder_count - 1),
            updated_at = NOW()
        WHERE chain_id = NEW.chain_id
          AND contract_address_hash = NEW.token_contract_address_hash;
    END IF;

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

--------------------------------------------------------------------------------
-- Block Statistics Functions
--------------------------------------------------------------------------------

-- Calculate average block time for a chain
CREATE OR REPLACE FUNCTION get_average_block_time(
    p_chain_id BIGINT,
    p_block_count INTEGER DEFAULT 100
)
RETURNS NUMERIC AS $$
DECLARE
    v_avg_time NUMERIC;
BEGIN
    SELECT AVG(b1.timestamp - b2.timestamp)
    INTO v_avg_time
    FROM (
        SELECT number, timestamp,
               LAG(timestamp) OVER (ORDER BY number) as prev_timestamp
        FROM blocks
        WHERE chain_id = p_chain_id
        ORDER BY number DESC
        LIMIT p_block_count
    ) AS b1
    JOIN blocks b2 ON b1.number = b2.number AND b1.prev_timestamp IS NOT NULL
    WHERE b1.prev_timestamp IS NOT NULL;

    RETURN COALESCE(v_avg_time, 2000); -- Default 2 seconds
END;
$$ LANGUAGE plpgsql;

-- Get gas price statistics
CREATE OR REPLACE FUNCTION get_gas_price_stats(
    p_chain_id BIGINT,
    p_block_count INTEGER DEFAULT 200
)
RETURNS TABLE (
    slow NUMERIC,
    average NUMERIC,
    fast NUMERIC
) AS $$
BEGIN
    RETURN QUERY
    SELECT
        PERCENTILE_CONT(0.1) WITHIN GROUP (ORDER BY gas_price) as slow,
        PERCENTILE_CONT(0.5) WITHIN GROUP (ORDER BY gas_price) as average,
        PERCENTILE_CONT(0.9) WITHIN GROUP (ORDER BY gas_price) as fast
    FROM (
        SELECT gas_price
        FROM transactions t
        JOIN blocks b ON t.chain_id = b.chain_id AND t.block_number = b.number
        WHERE t.chain_id = p_chain_id
          AND t.gas_price IS NOT NULL
          AND t.gas_price > 0
        ORDER BY b.number DESC
        LIMIT p_block_count * 100  -- ~100 tx per block average
    ) recent_txs;
END;
$$ LANGUAGE plpgsql;

--------------------------------------------------------------------------------
-- Search Functions
--------------------------------------------------------------------------------

-- Universal search function (Blockscout compatible)
CREATE OR REPLACE FUNCTION search_all(
    p_chain_id BIGINT,
    p_query TEXT,
    p_limit INTEGER DEFAULT 10
)
RETURNS TABLE (
    type TEXT,
    hash BYTEA,
    name TEXT,
    symbol TEXT,
    block_number BIGINT,
    address_hash BYTEA,
    token_type TEXT
) AS $$
DECLARE
    v_query_normalized TEXT;
    v_is_hash BOOLEAN;
    v_is_number BOOLEAN;
BEGIN
    v_query_normalized := LOWER(TRIM(p_query));

    -- Check if query looks like a hash (0x + 64 hex chars or 40 hex chars for address)
    v_is_hash := v_query_normalized ~ '^0x[0-9a-f]{40}$' OR v_query_normalized ~ '^0x[0-9a-f]{64}$';

    -- Check if query is a number
    v_is_number := v_query_normalized ~ '^[0-9]+$';

    -- Search blocks by number
    IF v_is_number THEN
        RETURN QUERY
        SELECT
            'block'::TEXT as type,
            b.hash,
            NULL::TEXT as name,
            NULL::TEXT as symbol,
            b.number as block_number,
            NULL::BYTEA as address_hash,
            NULL::TEXT as token_type
        FROM blocks b
        WHERE b.chain_id = p_chain_id
          AND b.number = v_query_normalized::BIGINT
        LIMIT 1;
    END IF;

    -- Search by hash (transaction, block, or address)
    IF v_is_hash THEN
        -- Transaction hash
        RETURN QUERY
        SELECT
            'transaction'::TEXT as type,
            t.hash,
            NULL::TEXT as name,
            NULL::TEXT as symbol,
            t.block_number,
            NULL::BYTEA as address_hash,
            NULL::TEXT as token_type
        FROM transactions t
        WHERE t.chain_id = p_chain_id
          AND t.hash = decode(substring(v_query_normalized from 3), 'hex')
        LIMIT 1;

        -- Block hash
        RETURN QUERY
        SELECT
            'block'::TEXT as type,
            b.hash,
            NULL::TEXT as name,
            NULL::TEXT as symbol,
            b.number as block_number,
            NULL::BYTEA as address_hash,
            NULL::TEXT as token_type
        FROM blocks b
        WHERE b.chain_id = p_chain_id
          AND b.hash = decode(substring(v_query_normalized from 3), 'hex')
        LIMIT 1;

        -- Address
        IF LENGTH(v_query_normalized) = 42 THEN
            RETURN QUERY
            SELECT
                'address'::TEXT as type,
                NULL::BYTEA as hash,
                NULL::TEXT as name,
                NULL::TEXT as symbol,
                NULL::BIGINT as block_number,
                a.hash as address_hash,
                NULL::TEXT as token_type
            FROM addresses a
            WHERE a.chain_id = p_chain_id
              AND a.hash = decode(substring(v_query_normalized from 3), 'hex')
            LIMIT 1;
        END IF;
    END IF;

    -- Token search by name/symbol
    RETURN QUERY
    SELECT
        'token'::TEXT as type,
        NULL::BYTEA as hash,
        tk.name,
        tk.symbol,
        NULL::BIGINT as block_number,
        tk.contract_address_hash as address_hash,
        tk.type as token_type
    FROM tokens tk
    WHERE tk.chain_id = p_chain_id
      AND (
          tk.name ILIKE '%' || p_query || '%'
          OR tk.symbol ILIKE '%' || p_query || '%'
      )
    ORDER BY tk.holder_count DESC
    LIMIT p_limit;

    -- Contract search by name
    RETURN QUERY
    SELECT
        'contract'::TEXT as type,
        NULL::BYTEA as hash,
        sc.name,
        NULL::TEXT as symbol,
        NULL::BIGINT as block_number,
        sc.address_hash as address_hash,
        NULL::TEXT as token_type
    FROM smart_contracts sc
    WHERE sc.chain_id = p_chain_id
      AND sc.name ILIKE '%' || p_query || '%'
    LIMIT p_limit;
END;
$$ LANGUAGE plpgsql;

--------------------------------------------------------------------------------
-- Gap Detection Functions
--------------------------------------------------------------------------------

-- Find gaps in indexed blocks
CREATE OR REPLACE FUNCTION find_block_gaps(
    p_chain_id BIGINT,
    p_min_block BIGINT DEFAULT 0,
    p_max_block BIGINT DEFAULT NULL
)
RETURNS TABLE (
    gap_start BIGINT,
    gap_end BIGINT
) AS $$
BEGIN
    RETURN QUERY
    WITH block_numbers AS (
        SELECT number
        FROM blocks
        WHERE chain_id = p_chain_id
          AND number >= p_min_block
          AND (p_max_block IS NULL OR number <= p_max_block)
        ORDER BY number
    ),
    gaps AS (
        SELECT
            number + 1 as gap_start,
            LEAD(number) OVER (ORDER BY number) - 1 as gap_end
        FROM block_numbers
    )
    SELECT g.gap_start, g.gap_end
    FROM gaps g
    WHERE g.gap_end IS NOT NULL
      AND g.gap_end >= g.gap_start
    ORDER BY g.gap_start;
END;
$$ LANGUAGE plpgsql;

--------------------------------------------------------------------------------
-- Maintenance Functions
--------------------------------------------------------------------------------

-- Vacuum analyze all partitions for a chain
CREATE OR REPLACE FUNCTION maintain_chain_partitions(p_chain_id BIGINT)
RETURNS VOID AS $$
DECLARE
    v_table_name TEXT;
BEGIN
    FOR v_table_name IN
        SELECT tablename
        FROM pg_tables
        WHERE tablename LIKE '%_' || p_chain_id
          AND schemaname = 'public'
    LOOP
        EXECUTE format('ANALYZE %I', v_table_name);
    END LOOP;
END;
$$ LANGUAGE plpgsql;

-- Clean up old pending transactions
CREATE OR REPLACE FUNCTION cleanup_stale_pending_txs(
    p_chain_id BIGINT,
    p_max_age INTERVAL DEFAULT '1 hour'
)
RETURNS INTEGER AS $$
DECLARE
    v_deleted INTEGER;
BEGIN
    DELETE FROM transactions
    WHERE chain_id = p_chain_id
      AND status IS NULL
      AND inserted_at < NOW() - p_max_age;

    GET DIAGNOSTICS v_deleted = ROW_COUNT;
    RETURN v_deleted;
END;
$$ LANGUAGE plpgsql;

--------------------------------------------------------------------------------
-- Helper Functions for API
--------------------------------------------------------------------------------

-- Format address as 0x-prefixed hex string
CREATE OR REPLACE FUNCTION format_address(p_bytes BYTEA)
RETURNS TEXT AS $$
BEGIN
    RETURN '0x' || encode(p_bytes, 'hex');
END;
$$ LANGUAGE plpgsql IMMUTABLE;

-- Format hash as 0x-prefixed hex string
CREATE OR REPLACE FUNCTION format_hash(p_bytes BYTEA)
RETURNS TEXT AS $$
BEGIN
    RETURN '0x' || encode(p_bytes, 'hex');
END;
$$ LANGUAGE plpgsql IMMUTABLE;

-- Parse hex string to bytea (with or without 0x prefix)
CREATE OR REPLACE FUNCTION parse_hex(p_hex TEXT)
RETURNS BYTEA AS $$
BEGIN
    IF LEFT(p_hex, 2) = '0x' OR LEFT(p_hex, 2) = '0X' THEN
        RETURN decode(substring(p_hex from 3), 'hex');
    ELSE
        RETURN decode(p_hex, 'hex');
    END IF;
EXCEPTION
    WHEN OTHERS THEN
        RETURN NULL;
END;
$$ LANGUAGE plpgsql IMMUTABLE;

-- Calculate effective gas price for EIP-1559 transactions
CREATE OR REPLACE FUNCTION calc_effective_gas_price(
    p_base_fee NUMERIC,
    p_max_fee NUMERIC,
    p_max_priority_fee NUMERIC,
    p_gas_price NUMERIC
)
RETURNS NUMERIC AS $$
BEGIN
    -- For legacy transactions
    IF p_gas_price IS NOT NULL AND p_max_fee IS NULL THEN
        RETURN p_gas_price;
    END IF;

    -- For EIP-1559 transactions
    IF p_base_fee IS NOT NULL AND p_max_fee IS NOT NULL THEN
        RETURN LEAST(
            p_max_fee,
            p_base_fee + COALESCE(p_max_priority_fee, 0)
        );
    END IF;

    RETURN p_gas_price;
END;
$$ LANGUAGE plpgsql IMMUTABLE;

--------------------------------------------------------------------------------
-- Statistics Aggregation Functions
--------------------------------------------------------------------------------

-- Get daily transaction count for charts
CREATE OR REPLACE FUNCTION get_daily_tx_count(
    p_chain_id BIGINT,
    p_days INTEGER DEFAULT 30
)
RETURNS TABLE (
    date DATE,
    tx_count BIGINT
) AS $$
BEGIN
    RETURN QUERY
    SELECT
        DATE(to_timestamp(block_timestamp)) as date,
        COUNT(*)::BIGINT as tx_count
    FROM transactions
    WHERE chain_id = p_chain_id
      AND block_timestamp >= EXTRACT(EPOCH FROM NOW() - (p_days || ' days')::INTERVAL)
    GROUP BY DATE(to_timestamp(block_timestamp))
    ORDER BY date DESC;
END;
$$ LANGUAGE plpgsql;

-- Get daily new addresses count
CREATE OR REPLACE FUNCTION get_daily_new_addresses(
    p_chain_id BIGINT,
    p_days INTEGER DEFAULT 30
)
RETURNS TABLE (
    date DATE,
    address_count BIGINT
) AS $$
BEGIN
    RETURN QUERY
    SELECT
        DATE(inserted_at) as date,
        COUNT(*)::BIGINT as address_count
    FROM addresses
    WHERE chain_id = p_chain_id
      AND inserted_at >= NOW() - (p_days || ' days')::INTERVAL
    GROUP BY DATE(inserted_at)
    ORDER BY date DESC;
END;
$$ LANGUAGE plpgsql;
