-- NFT Marketplace Tables Migration
-- Version: 1.0.0
-- Date: 2025-01-24
-- Description: Adds NFT marketplace indexing for OpenSea Seaport, LooksRare, Blur, Rarible, X2Y2, Zora

--------------------------------------------------------------------------------
-- NFT Orders (Listings, Bids, Offers)
--------------------------------------------------------------------------------

CREATE TABLE nft_orders (
    id              BIGSERIAL,
    chain_id        BIGINT NOT NULL REFERENCES chains(chain_id),
    order_hash      BYTEA NOT NULL,
    protocol        VARCHAR(50) NOT NULL,  -- seaport, looksrare, blur, rarible, x2y2, zora
    order_type      VARCHAR(20) NOT NULL,  -- listing, bid, offer, auction
    status          VARCHAR(20) NOT NULL DEFAULT 'active',  -- active, filled, cancelled, expired
    maker           BYTEA NOT NULL,
    taker           BYTEA,
    collection      BYTEA NOT NULL,
    token_id        NUMERIC(78),  -- NULL for collection offers
    token_standard  VARCHAR(20) DEFAULT 'ERC721',  -- ERC721, ERC1155
    quantity        BIGINT DEFAULT 1,
    price           NUMERIC(100) NOT NULL,
    payment_token   BYTEA NOT NULL,  -- 0x0 for native currency
    start_time      TIMESTAMPTZ,
    end_time        TIMESTAMPTZ,
    salt            NUMERIC(100),
    zone            BYTEA,  -- Seaport zone
    conduit         BYTEA,  -- Seaport conduit
    signature       TEXT,
    raw_order       TEXT,  -- Original order JSON
    block_number    BIGINT NOT NULL,
    tx_hash         BYTEA NOT NULL,
    log_index       INTEGER NOT NULL,
    timestamp       BIGINT NOT NULL,
    created_at      TIMESTAMPTZ DEFAULT NOW(),
    updated_at      TIMESTAMPTZ DEFAULT NOW(),

    PRIMARY KEY (chain_id, order_hash)
) PARTITION BY LIST (chain_id);

-- Create partitions
CREATE TABLE nft_orders_96369 PARTITION OF nft_orders FOR VALUES IN (96369);
CREATE TABLE nft_orders_96368 PARTITION OF nft_orders FOR VALUES IN (96368);
CREATE TABLE nft_orders_200200 PARTITION OF nft_orders FOR VALUES IN (200200);
CREATE TABLE nft_orders_200201 PARTITION OF nft_orders FOR VALUES IN (200201);
CREATE TABLE nft_orders_36963 PARTITION OF nft_orders FOR VALUES IN (36963);

-- Indexes
CREATE INDEX idx_nft_orders_collection ON nft_orders(chain_id, collection);
CREATE INDEX idx_nft_orders_maker ON nft_orders(chain_id, maker);
CREATE INDEX idx_nft_orders_status ON nft_orders(chain_id, status) WHERE status = 'active';
CREATE INDEX idx_nft_orders_protocol ON nft_orders(chain_id, protocol);
CREATE INDEX idx_nft_orders_type ON nft_orders(chain_id, order_type);
CREATE INDEX idx_nft_orders_token ON nft_orders(chain_id, collection, token_id) WHERE token_id IS NOT NULL;
CREATE INDEX idx_nft_orders_timestamp ON nft_orders(chain_id, timestamp DESC);
CREATE INDEX idx_nft_orders_expiry ON nft_orders(chain_id, end_time) WHERE status = 'active';

--------------------------------------------------------------------------------
-- NFT Order Fees
--------------------------------------------------------------------------------

CREATE TABLE nft_order_fees (
    id              BIGSERIAL PRIMARY KEY,
    chain_id        BIGINT NOT NULL,
    order_hash      BYTEA NOT NULL,
    recipient       BYTEA NOT NULL,
    amount          NUMERIC(100) NOT NULL,
    basis_points    INTEGER,
    fee_type        VARCHAR(20) NOT NULL,  -- protocol, royalty, platform
    created_at      TIMESTAMPTZ DEFAULT NOW(),

    FOREIGN KEY (chain_id, order_hash) REFERENCES nft_orders(chain_id, order_hash) ON DELETE CASCADE
);

CREATE INDEX idx_nft_order_fees_order ON nft_order_fees(chain_id, order_hash);
CREATE INDEX idx_nft_order_fees_recipient ON nft_order_fees(chain_id, recipient);

--------------------------------------------------------------------------------
-- NFT Sales
--------------------------------------------------------------------------------

CREATE TABLE nft_sales (
    id              BIGSERIAL,
    chain_id        BIGINT NOT NULL REFERENCES chains(chain_id),
    order_hash      BYTEA,  -- May be null for some protocols
    protocol        VARCHAR(50) NOT NULL,
    collection      BYTEA NOT NULL,
    token_id        NUMERIC(78) NOT NULL,
    token_standard  VARCHAR(20) DEFAULT 'ERC721',
    quantity        BIGINT DEFAULT 1,
    seller          BYTEA NOT NULL,
    buyer           BYTEA NOT NULL,
    price           NUMERIC(100) NOT NULL,
    payment_token   BYTEA NOT NULL,
    price_usd       NUMERIC(24, 8),  -- USD value at time of sale
    fee_total       NUMERIC(100) DEFAULT 0,
    royalty_total   NUMERIC(100) DEFAULT 0,
    seller_received NUMERIC(100),
    block_number    BIGINT NOT NULL,
    tx_hash         BYTEA NOT NULL,
    log_index       INTEGER NOT NULL,
    timestamp       BIGINT NOT NULL,
    created_at      TIMESTAMPTZ DEFAULT NOW(),

    PRIMARY KEY (chain_id, tx_hash, log_index)
) PARTITION BY LIST (chain_id);

-- Create partitions
CREATE TABLE nft_sales_96369 PARTITION OF nft_sales FOR VALUES IN (96369);
CREATE TABLE nft_sales_96368 PARTITION OF nft_sales FOR VALUES IN (96368);
CREATE TABLE nft_sales_200200 PARTITION OF nft_sales FOR VALUES IN (200200);
CREATE TABLE nft_sales_200201 PARTITION OF nft_sales FOR VALUES IN (200201);
CREATE TABLE nft_sales_36963 PARTITION OF nft_sales FOR VALUES IN (36963);

-- Indexes
CREATE INDEX idx_nft_sales_collection ON nft_sales(chain_id, collection);
CREATE INDEX idx_nft_sales_token ON nft_sales(chain_id, collection, token_id);
CREATE INDEX idx_nft_sales_seller ON nft_sales(chain_id, seller);
CREATE INDEX idx_nft_sales_buyer ON nft_sales(chain_id, buyer);
CREATE INDEX idx_nft_sales_protocol ON nft_sales(chain_id, protocol);
CREATE INDEX idx_nft_sales_timestamp ON nft_sales(chain_id, timestamp DESC);
CREATE INDEX idx_nft_sales_block ON nft_sales(chain_id, block_number DESC);
CREATE INDEX idx_nft_sales_price ON nft_sales(chain_id, collection, price DESC);

--------------------------------------------------------------------------------
-- NFT Collections (Marketplace Stats)
--------------------------------------------------------------------------------

CREATE TABLE nft_collections (
    chain_id            BIGINT NOT NULL REFERENCES chains(chain_id),
    address             BYTEA NOT NULL,
    name                VARCHAR(255),
    symbol              VARCHAR(50),
    token_standard      VARCHAR(20) DEFAULT 'ERC721',
    total_supply        BIGINT,
    floor_price         NUMERIC(100) DEFAULT 0,
    floor_token         BYTEA,  -- Payment token for floor price
    royalty_bps         INTEGER DEFAULT 0,  -- Royalty in basis points
    royalty_recipient   BYTEA,
    volume_24h          NUMERIC(100) DEFAULT 0,
    volume_7d           NUMERIC(100) DEFAULT 0,
    volume_30d          NUMERIC(100) DEFAULT 0,
    volume_total        NUMERIC(100) DEFAULT 0,
    sales_24h           BIGINT DEFAULT 0,
    sales_7d            BIGINT DEFAULT 0,
    sales_30d           BIGINT DEFAULT 0,
    sales_total         BIGINT DEFAULT 0,
    listing_count       BIGINT DEFAULT 0,
    owner_count         BIGINT DEFAULT 0,
    avg_price_24h       NUMERIC(100),
    price_change_24h    NUMERIC(10, 4),  -- Percentage
    created_at          TIMESTAMPTZ DEFAULT NOW(),
    updated_at          TIMESTAMPTZ DEFAULT NOW(),

    PRIMARY KEY (chain_id, address)
);

-- Indexes
CREATE INDEX idx_nft_collections_volume ON nft_collections(chain_id, volume_total DESC);
CREATE INDEX idx_nft_collections_floor ON nft_collections(chain_id, floor_price DESC);
CREATE INDEX idx_nft_collections_sales ON nft_collections(chain_id, sales_total DESC);
CREATE INDEX idx_nft_collections_name ON nft_collections USING gin (name gin_trgm_ops);

--------------------------------------------------------------------------------
-- NFT Collection Activity (Aggregated)
--------------------------------------------------------------------------------

CREATE TABLE nft_collection_activity (
    id              BIGSERIAL PRIMARY KEY,
    chain_id        BIGINT NOT NULL,
    collection      BYTEA NOT NULL,
    activity_type   VARCHAR(20) NOT NULL,  -- sale, listing, bid, transfer, mint
    from_address    BYTEA,
    to_address      BYTEA,
    token_id        NUMERIC(78),
    quantity        BIGINT DEFAULT 1,
    price           NUMERIC(100),
    payment_token   BYTEA,
    protocol        VARCHAR(50),
    block_number    BIGINT NOT NULL,
    tx_hash         BYTEA NOT NULL,
    log_index       INTEGER NOT NULL,
    timestamp       BIGINT NOT NULL,
    created_at      TIMESTAMPTZ DEFAULT NOW(),

    FOREIGN KEY (chain_id, collection) REFERENCES nft_collections(chain_id, address)
);

CREATE INDEX idx_nft_activity_collection ON nft_collection_activity(chain_id, collection);
CREATE INDEX idx_nft_activity_type ON nft_collection_activity(chain_id, activity_type);
CREATE INDEX idx_nft_activity_timestamp ON nft_collection_activity(chain_id, timestamp DESC);
CREATE INDEX idx_nft_activity_token ON nft_collection_activity(chain_id, collection, token_id);

--------------------------------------------------------------------------------
-- NFT Marketplace Stats (Aggregated daily/hourly)
--------------------------------------------------------------------------------

CREATE TABLE nft_marketplace_stats (
    id              BIGSERIAL PRIMARY KEY,
    chain_id        BIGINT NOT NULL REFERENCES chains(chain_id),
    period_type     VARCHAR(10) NOT NULL,  -- hour, day, week
    period_start    TIMESTAMPTZ NOT NULL,
    period_end      TIMESTAMPTZ NOT NULL,
    total_volume    NUMERIC(100) DEFAULT 0,
    total_sales     BIGINT DEFAULT 0,
    unique_buyers   BIGINT DEFAULT 0,
    unique_sellers  BIGINT DEFAULT 0,
    avg_price       NUMERIC(100),
    max_price       NUMERIC(100),
    volume_by_protocol JSONB,  -- {"seaport": "123", "blur": "456"}
    sales_by_protocol  JSONB,  -- {"seaport": 10, "blur": 20}
    top_collections    JSONB,  -- [{"address": "0x...", "volume": "123"}]
    created_at      TIMESTAMPTZ DEFAULT NOW(),

    UNIQUE(chain_id, period_type, period_start)
);

CREATE INDEX idx_nft_stats_period ON nft_marketplace_stats(chain_id, period_type, period_start DESC);

--------------------------------------------------------------------------------
-- NFT Royalty Settings
--------------------------------------------------------------------------------

CREATE TABLE nft_royalties (
    chain_id        BIGINT NOT NULL,
    collection      BYTEA NOT NULL,
    recipient       BYTEA NOT NULL,
    basis_points    INTEGER NOT NULL,
    source          VARCHAR(50),  -- eip2981, rarible, manifold, custom
    block_number    BIGINT,
    tx_hash         BYTEA,
    updated_at      TIMESTAMPTZ DEFAULT NOW(),

    PRIMARY KEY (chain_id, collection, recipient)
);

CREATE INDEX idx_nft_royalties_collection ON nft_royalties(chain_id, collection);

--------------------------------------------------------------------------------
-- Views for Common Queries
--------------------------------------------------------------------------------

-- Active listings by collection
CREATE VIEW v_nft_active_listings AS
SELECT
    o.chain_id,
    o.collection,
    o.token_id,
    o.price,
    o.payment_token,
    o.maker,
    o.protocol,
    o.end_time,
    o.created_at
FROM nft_orders o
WHERE o.status = 'active'
  AND o.order_type = 'listing'
  AND (o.end_time IS NULL OR o.end_time > NOW());

-- Floor prices by collection
CREATE VIEW v_nft_floor_prices AS
SELECT
    chain_id,
    collection,
    MIN(price) as floor_price,
    COUNT(*) as listing_count
FROM nft_orders
WHERE status = 'active'
  AND order_type = 'listing'
  AND (end_time IS NULL OR end_time > NOW())
GROUP BY chain_id, collection;

-- Recent sales
CREATE VIEW v_nft_recent_sales AS
SELECT
    s.chain_id,
    s.collection,
    s.token_id,
    s.seller,
    s.buyer,
    s.price,
    s.payment_token,
    s.protocol,
    s.tx_hash,
    s.timestamp,
    c.name as collection_name
FROM nft_sales s
LEFT JOIN nft_collections c ON s.chain_id = c.chain_id AND s.collection = c.address
ORDER BY s.timestamp DESC;

--------------------------------------------------------------------------------
-- Triggers for Auto-Updates
--------------------------------------------------------------------------------

-- Function to update collection stats on sale
CREATE OR REPLACE FUNCTION update_collection_on_sale()
RETURNS TRIGGER AS $$
BEGIN
    INSERT INTO nft_collections (chain_id, address, volume_total, sales_total)
    VALUES (NEW.chain_id, NEW.collection, NEW.price, 1)
    ON CONFLICT (chain_id, address) DO UPDATE
    SET volume_total = nft_collections.volume_total + NEW.price,
        sales_total = nft_collections.sales_total + 1,
        updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_nft_sale_update_collection
AFTER INSERT ON nft_sales
FOR EACH ROW
EXECUTE FUNCTION update_collection_on_sale();

-- Function to update listing counts
CREATE OR REPLACE FUNCTION update_collection_listings()
RETURNS TRIGGER AS $$
BEGIN
    IF TG_OP = 'INSERT' AND NEW.order_type = 'listing' AND NEW.status = 'active' THEN
        UPDATE nft_collections
        SET listing_count = listing_count + 1,
            updated_at = NOW()
        WHERE chain_id = NEW.chain_id AND address = NEW.collection;
    ELSIF TG_OP = 'UPDATE' AND OLD.status = 'active' AND NEW.status != 'active' THEN
        UPDATE nft_collections
        SET listing_count = GREATEST(listing_count - 1, 0),
            updated_at = NOW()
        WHERE chain_id = NEW.chain_id AND address = NEW.collection;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_nft_order_update_listings
AFTER INSERT OR UPDATE ON nft_orders
FOR EACH ROW
EXECUTE FUNCTION update_collection_listings();
