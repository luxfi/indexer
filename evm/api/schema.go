// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

package api

import (
	"database/sql"
	"fmt"
	"log"
)

// EnsureTables creates all tables required by the API layer if they don't exist.
// prefix is the chain slug (e.g. "cchain", "zoo", "hanzo", "spc", "pars").
// This is idempotent -- safe to call on every startup.
func EnsureTables(db *sql.DB, prefix string) error {
	tbl := func(name string) string { return prefix + "_" + name }
	idx := func(name string) string { return "idx_" + prefix + "_" + name }

	stmts := []string{
		// blocks table (linear chain block storage)
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
			id TEXT PRIMARY KEY,
			parent_id TEXT,
			height BIGINT NOT NULL,
			timestamp TIMESTAMP WITH TIME ZONE NOT NULL,
			status TEXT DEFAULT 'pending',
			tx_count INTEGER DEFAULT 0,
			tx_ids JSONB DEFAULT '[]',
			data JSONB,
			metadata JSONB,
			created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
		)`, tbl("blocks")),
		fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s ON %s (height)`, idx("blocks_height"), tbl("blocks")),
		fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s ON %s (status)`, idx("blocks_status"), tbl("blocks")),
		fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s ON %s (timestamp)`, idx("blocks_timestamp"), tbl("blocks")),

		// transactions
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
			hash TEXT PRIMARY KEY,
			block_hash TEXT NOT NULL,
			block_number BIGINT NOT NULL,
			tx_from TEXT NOT NULL,
			tx_to TEXT,
			value TEXT DEFAULT '0',
			gas BIGINT NOT NULL,
			gas_price TEXT,
			gas_used BIGINT,
			nonce BIGINT NOT NULL,
			input TEXT,
			tx_index INTEGER NOT NULL,
			tx_type INTEGER DEFAULT 0,
			status INTEGER DEFAULT 1,
			contract_address TEXT,
			timestamp TIMESTAMP WITH TIME ZONE NOT NULL
		)`, tbl("transactions")),
		fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s ON %s (block_number)`, idx("tx_block"), tbl("transactions")),
		fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s ON %s (tx_from)`, idx("tx_from"), tbl("transactions")),
		fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s ON %s (tx_to)`, idx("tx_to"), tbl("transactions")),
		fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s ON %s (timestamp)`, idx("tx_timestamp"), tbl("transactions")),

		// addresses
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
			hash TEXT PRIMARY KEY,
			balance TEXT DEFAULT '0',
			tx_count BIGINT DEFAULT 0,
			is_contract BOOLEAN DEFAULT false,
			contract_code TEXT,
			contract_creator TEXT,
			contract_tx_hash TEXT,
			created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
		)`, tbl("addresses")),
		fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s ON %s (balance)`, idx("addr_balance"), tbl("addresses")),

		// token_transfers
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
			id TEXT PRIMARY KEY,
			tx_hash TEXT NOT NULL,
			log_index BIGINT NOT NULL,
			block_number BIGINT NOT NULL,
			token_address TEXT NOT NULL,
			token_type TEXT NOT NULL,
			tx_from TEXT NOT NULL,
			tx_to TEXT NOT NULL,
			value TEXT NOT NULL,
			token_id TEXT,
			timestamp TIMESTAMP WITH TIME ZONE NOT NULL
		)`, tbl("token_transfers")),
		fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s ON %s (token_address)`, idx("transfer_token"), tbl("token_transfers")),
		fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s ON %s (tx_from)`, idx("transfer_from"), tbl("token_transfers")),
		fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s ON %s (tx_to)`, idx("transfer_to"), tbl("token_transfers")),
		fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s ON %s (block_number)`, idx("transfer_block"), tbl("token_transfers")),

		// tokens
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
			address TEXT PRIMARY KEY,
			name TEXT,
			symbol TEXT,
			decimals INTEGER DEFAULT 18,
			total_supply TEXT,
			token_type TEXT NOT NULL,
			holder_count BIGINT DEFAULT 0,
			tx_count BIGINT DEFAULT 0,
			created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
		)`, tbl("tokens")),
		fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s ON %s (token_type)`, idx("token_type"), tbl("tokens")),

		// logs
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
			tx_hash TEXT NOT NULL,
			log_index BIGINT NOT NULL,
			block_number BIGINT NOT NULL,
			address TEXT NOT NULL,
			topics TEXT NOT NULL,
			data TEXT,
			removed BOOLEAN DEFAULT false
		)`, tbl("logs")),
		fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s ON %s (address)`, idx("logs_address"), tbl("logs")),
		fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s ON %s (block_number)`, idx("logs_block"), tbl("logs")),

		// internal_transactions
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
			id TEXT PRIMARY KEY,
			tx_hash TEXT NOT NULL,
			block_number BIGINT NOT NULL,
			trace_index BIGINT NOT NULL,
			trace_address TEXT DEFAULT '',
			call_type TEXT NOT NULL,
			tx_from TEXT NOT NULL,
			tx_to TEXT,
			value TEXT DEFAULT '0',
			gas BIGINT,
			gas_used BIGINT,
			input TEXT,
			output TEXT,
			error TEXT,
			created_contract_address TEXT,
			created_contract_code TEXT,
			init TEXT,
			timestamp TIMESTAMP WITH TIME ZONE NOT NULL
		)`, tbl("internal_transactions")),
		fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s ON %s (tx_hash)`, idx("internal_tx"), tbl("internal_transactions")),
		fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s ON %s (tx_from)`, idx("internal_from"), tbl("internal_transactions")),
		fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s ON %s (tx_to)`, idx("internal_to"), tbl("internal_transactions")),
		fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s ON %s (block_number)`, idx("internal_block"), tbl("internal_transactions")),

		// token_balances
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
			token_address TEXT NOT NULL,
			holder_address TEXT NOT NULL,
			balance TEXT DEFAULT '0',
			token_id TEXT,
			block_number BIGINT DEFAULT 0,
			updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
		)`, tbl("token_balances")),
		fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s ON %s (holder_address)`, idx("balance_holder"), tbl("token_balances")),
		fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s ON %s (token_address)`, idx("balance_token"), tbl("token_balances")),

		// extended_stats
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
			id INTEGER PRIMARY KEY,
			total_transactions BIGINT DEFAULT 0,
			total_addresses BIGINT DEFAULT 0,
			total_contracts BIGINT DEFAULT 0,
			total_tokens BIGINT DEFAULT 0,
			total_token_transfers BIGINT DEFAULT 0,
			total_internal_transactions BIGINT DEFAULT 0,
			total_gas_used BIGINT DEFAULT 0,
			avg_gas_price TEXT DEFAULT '0',
			avg_block_time DOUBLE PRECISION DEFAULT 0,
			tps_24h DOUBLE PRECISION DEFAULT 0,
			updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
		)`, tbl("extended_stats")),

		// Seed the stats row
		fmt.Sprintf(`INSERT INTO %s (id) VALUES (1) ON CONFLICT DO NOTHING`, tbl("extended_stats")),
	}

	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("ensure tables (%s): %w", prefix, err)
		}
	}

	log.Printf("[%s] Schema ensured (9 tables)", prefix)
	return nil
}
