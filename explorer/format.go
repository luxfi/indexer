package explorer

import (
	"database/sql"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"
	"time"
)

// col returns the first non-nil value from a map for the given key names.
// Handles schema variations (EVM tables use different column names than test/PG schema).
func col(m map[string]any, keys ...string) any {
	for _, k := range keys {
		if v, ok := m[k]; ok && v != nil {
			return v
		}
	}
	return nil
}

// scanMaps scans all rows from a query into []map[string]any.
func scanMaps(rows *sql.Rows) ([]map[string]any, error) {
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}

	var result []map[string]any
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		m := make(map[string]any, len(cols))
		for i, col := range cols {
			m[col] = vals[i]
		}
		result = append(result, m)
	}
	return result, rows.Err()
}

// bytesToHex converts a []byte to a 0x-prefixed hex string.
func bytesToHex(v any) string {
	switch b := v.(type) {
	case []byte:
		if len(b) == 0 {
			return ""
		}
		return "0x" + hex.EncodeToString(b)
	case string:
		if strings.HasPrefix(b, "0x") {
			return strings.ToLower(b)
		}
		return b
	default:
		return fmt.Sprintf("%v", v)
	}
}

// hexToBytes converts a 0x-prefixed hex string to []byte.
func hexToBytes(s string) []byte {
	s = strings.TrimPrefix(strings.ToLower(s), "0x")
	b, _ := hex.DecodeString(s)
	return b
}

// fmtNum formats a numeric value as a decimal string.
//
// Token-transfer value comes from the log's `data` field, which is a
// hex-encoded uint256 ("0x000…0de0b6b3a7640000" = 1e18). Same for stored
// block.base_fee + tx.value (hex from RPC). Detect "0x"-prefixed strings
// and convert via math/big so the SPA gets "1000000000000000000" instead
// of an unparseable hex string.
//
// Plain decimal strings (token total_supply, scanned int columns) pass
// through unchanged via fmt.Sprintf.
func fmtNum(v any) string {
	if v == nil {
		return "0"
	}
	if s, ok := v.(string); ok && strings.HasPrefix(s, "0x") {
		trimmed := strings.TrimPrefix(s, "0x")
		if trimmed == "" {
			return "0"
		}
		if n, ok := new(big.Int).SetString(trimmed, 16); ok {
			return n.String()
		}
	}
	return fmt.Sprintf("%v", v)
}

// fmtTimestamp formats a timestamp for the explorer API v2 response as ISO 8601.
func fmtTimestamp(v any) string {
	switch ts := v.(type) {
	case int64:
		if ts == 0 {
			return ""
		}
		return time.Unix(ts, 0).UTC().Format(time.RFC3339)
	case float64:
		if ts == 0 {
			return ""
		}
		return time.Unix(int64(ts), 0).UTC().Format(time.RFC3339)
	case time.Time:
		if ts.IsZero() {
			return ""
		}
		return ts.UTC().Format(time.RFC3339)
	case string:
		// Try parsing common Go time formats
		for _, layout := range []string{
			time.RFC3339,
			"2006-01-02 15:04:05 +0000 UTC",
			"2006-01-02T15:04:05Z",
			"2006-01-02 15:04:05",
		} {
			if t, err := time.Parse(layout, ts); err == nil {
				return t.UTC().Format(time.RFC3339)
			}
		}
		return ts
	default:
		if v == nil {
			return ""
		}
		return fmt.Sprintf("%v", v)
	}
}

// formatBlock formats a block row as an explorer v2 block response.
func formatBlock(b map[string]any) map[string]any {
	return map[string]any{
		"height":           b["number"],
		"hash":             bytesToHex(b["hash"]),
		"parent_hash":      bytesToHex(b["parent_hash"]),
		"nonce":            bytesToHex(b["nonce"]),
		"miner":            map[string]any{"hash": bytesToHex(col(b, "miner", "miner_hash"))},
		"difficulty":       fmtNum(b["difficulty"]),
		"total_difficulty": fmtNum(b["total_difficulty"]),
		"size":             b["size"],
		"gas_limit":        fmtNum(b["gas_limit"]),
		"gas_used":         fmtNum(b["gas_used"]),
		"base_fee_per_gas": fmtNum(b["base_fee"]),
		"timestamp":        fmtTimestamp(b["timestamp"]),
		"tx_count":         col(b, "tx_count", "transaction_count"),
		"state_root":       nil,
		"type":             "block",
	}
}

// formatTx formats a transaction row as an explorer v2 transaction response.
func formatTx(t map[string]any) map[string]any {
	resp := map[string]any{
		"hash":                     bytesToHex(t["hash"]),
		"block_number":             t["block_number"],
		"block_hash":               bytesToHex(t["block_hash"]),
		"from":                     map[string]any{"hash": bytesToHex(col(t, "from_addr", "from_address_hash", "from_address"))},
		"to":                       nil,
		"value":                    fmtNum(t["value"]),
		"gas_limit":                fmtNum(t["gas"]),
		"gas_price":                fmtNum(t["gas_price"]),
		"gas_used":                 fmtNum(t["gas_used"]),
		"max_fee_per_gas":          fmtNum(t["max_fee_per_gas"]),
		"max_priority_fee_per_gas": fmtNum(t["max_priority_fee_per_gas"]),
		"nonce":                    t["nonce"],
		"position":                 t["tx_index"],
		"type":                     t["type"],
		"status":                   txStatusStr(t["status"]),
		"timestamp":                fmtTimestamp(t["timestamp"]),
		"method":                   txMethodStr(t["input"]),
		"input":                    bytesToHex(t["input"]),
		"result":                   txResultStr(t),
	}

	if to := col(t, "to_addr", "to_address_hash", "to_address"); to != nil {
		if s := bytesToHex(to); s != "" {
			resp["to"] = map[string]any{"hash": s}
		}
	}
	if ca := col(t, "contract_addr", "created_contract_address_hash", "created_contract_address"); ca != nil {
		if s := bytesToHex(ca); s != "" {
			resp["created_contract"] = map[string]any{"hash": s}
		}
	}
	if e := t["error"]; e != nil && fmt.Sprintf("%v", e) != "" {
		resp["error"] = e
	}
	if r := t["revert_reason"]; r != nil && fmt.Sprintf("%v", r) != "" {
		resp["revert_reason"] = r
	}

	return resp
}

// formatInternalTx formats an internal transaction row.
func formatInternalTx(t map[string]any) map[string]any {
	// For pending transactions (no block), success and error are null.
	var success any
	var errField any
	if t["block_number"] != nil {
		hasError := t["error"] != nil && fmt.Sprintf("%v", t["error"]) != ""
		success = !hasError
		if hasError {
			errField = t["error"]
		}
	}

	resp := map[string]any{
		"block_number":     t["block_number"],
		"index":            t["index"],
		"transaction_hash": bytesToHex(t["transaction_hash"]),
		"type":             t["type"],
		"call_type":        t["call_type"],
		"from":             map[string]any{"hash": bytesToHex(col(t, "from_addr", "from_address_hash", "from_address"))},
		"to":               nil,
		"value":            fmtNum(t["value"]),
		"gas_limit":        fmtNum(t["gas"]),
		"gas_used":         fmtNum(t["gas_used"]),
		"input":            bytesToHex(t["input"]),
		"output":           bytesToHex(t["output"]),
		"error":            errField,
		"success":          success,
		"timestamp":        fmtTimestamp(t["timestamp"]),
	}
	if to := col(t, "to_addr", "to_address_hash", "to_address"); to != nil {
		if s := bytesToHex(to); s != "" {
			resp["to"] = map[string]any{"hash": s}
		}
	}
	return resp
}

// formatLog formats a log row.
//
// Column-spelling variants:
//   - luxfi/indexer evm_logs:  topic0/topic1/topic2/topic3, log_index, tx_hash
//   - Blockscout-legacy logs:  first_topic/.../fourth_topic, index, transaction_hash
// Fall through both sets so the response shape works against either.
func formatLog(l map[string]any) map[string]any {
	topics := []string{}
	for _, key := range []string{
		"first_topic", "second_topic", "third_topic", "fourth_topic",
		"topic0", "topic1", "topic2", "topic3",
	} {
		if v := l[key]; v != nil {
			if s := bytesToHex(v); s != "" {
				topics = append(topics, s)
				// stop after we collect 4 — the two naming schemes shouldn't
				// both populate the same physical row, but cap defensively.
				if len(topics) >= 4 {
					break
				}
			}
		}
	}
	return map[string]any{
		"address":          map[string]any{"hash": bytesToHex(col(l, "address", "address_hash"))},
		"data":             bytesToHex(l["data"]),
		"topics":           topics,
		"index":            col(l, "log_index", "index"),
		"block_number":     l["block_number"],
		"transaction_hash": bytesToHex(col(l, "tx_hash", "transaction_hash")),
		"decoded":          nil,
	}
}

// formatTokenTransfer formats a token transfer row.
func formatTokenTransfer(t map[string]any) map[string]any {
	// Column-spelling variants between luxfi/indexer evm_token_transfers
	// ("tx_hash", "value") and Blockscout-legacy ("transaction_hash",
	// "amount"). Fall through both so the response works against either.
	return map[string]any{
		"from":             map[string]any{"hash": bytesToHex(col(t, "from_addr", "from_address_hash", "from_address"))},
		"to":               map[string]any{"hash": bytesToHex(col(t, "to_addr", "to_address_hash", "to_address"))},
		"token":            map[string]any{"address": bytesToHex(t["token_address"]), "type": t["token_type"]},
		"total":            map[string]any{"value": fmtNum(col(t, "value", "amount")), "decimals": nil},
		"log_index":        t["log_index"],
		"block_number":     t["block_number"],
		"transaction_hash": bytesToHex(col(t, "tx_hash", "transaction_hash")),
		"timestamp":        fmtTimestamp(t["timestamp"]),
	}
}

// formatToken formats a token row.
func formatToken(t map[string]any) map[string]any {
	// Schema variants:
	//   - luxfi/indexer evm_tokens:        column "address",   "token_type"
	//   - Blockscout legacy tokens:        "contract_address", "type"
	//   - blockscout-derivative variants:  "contract_addr", "created_contract_address_hash",
	//                                      "address_hash"
	// Fall through every known spelling so the response shape works against
	// any schema this binary is bolted onto.
	return map[string]any{
		"address":                bytesToHex(col(t, "address", "contract_addr", "created_contract_address_hash", "address_hash", "contract_address")),
		"name":                   t["name"],
		"symbol":                 t["symbol"],
		"total_supply":           fmtNum(t["total_supply"]),
		"decimals":               fmtNum(t["decimals"]),
		"type":                   col(t, "token_type", "type"),
		"holders":                fmtNum(t["holder_count"]),
		"exchange_rate":          t["fiat_value"],
		"circulating_market_cap": fmtNum(t["circulating_market_cap"]),
		"icon_url":               t["icon_url"],
		"trust_score":            computeTokenScore(t),
	}
}

// formatContract formats a smart contract row.
//
// Returns the full verified-contract envelope used by the SPA contract
// panel. `secondary_sources` carries multi-file source bundles (one entry
// per import the deployer ships alongside the main `source_code`), with
// the standard Blockscout/Sourcify shape: an array of
// `{ file_path, contract_source_code }` objects.
func formatContract(c map[string]any) map[string]any {
	return map[string]any{
		"address":            map[string]any{"hash": bytesToHex(c["address"])},
		"name":               c["name"],
		"compiler_version":   c["compiler_version"],
		"optimization":       c["optimization"],
		"optimization_runs":  c["optimization_runs"],
		"source_code":        c["contract_source_code"],
		"abi":                c["abi"],
		"constructor_args":   c["constructor_arguments"],
		"evm_version":        c["evm_version"],
		"is_verified":        true,
		"is_vyper_contract":  c["is_vyper_contract"],
		"license_type":       c["license_type"],
		"external_libraries": c["external_libraries"],
		"secondary_sources":  c["secondary_sources"],
		"file_path":          c["file_path"],
		"verified_via":       c["verified_via"],
	}
}

// formatAddress formats an address row.
//
// Some installs use the legacy `transactions_count` / `token_transfers_count`
// / `contract_code` columns; the SQLite path in evm/indexer.go writes the
// shorter `tx_count` / `code` columns. Read whichever is present and never
// emit a JSON `null` for the counters — the Blockscout-derived SPA does
// `transactions_count.toLocaleString()` without a guard and crashes on null.
//
// is_contract resolution order, most authoritative first:
//   1. boolean column `is_contract` (the indexer's address upsert
//      populates this via the per-block eth_getCode resolve loop in
//      luxfi/indexer#10).
//   2. non-empty contract_code / code column (legacy Postgres shape).
// EOAs end up as false in both — no false positives.
func formatAddress(a map[string]any) map[string]any {
	txCount := firstNonNil(a, "transactions_count", "tx_count")
	if txCount == nil {
		txCount = int64(0)
	}
	ttCount := firstNonNil(a, "token_transfers_count")
	if ttCount == nil {
		ttCount = int64(0)
	}
	isContract := toBool(a["is_contract"])
	if !isContract {
		// Fallback: derive from a non-empty code column (legacy Postgres
		// shape where the boolean column doesn't exist).
		switch v := firstNonNil(a, "contract_code", "code").(type) {
		case string:
			isContract = v != "" && v != "0x"
		case []byte:
			isContract = len(v) > 0
		}
	}
	balance := fmtNum(firstNonNil(a, "fetched_coin_balance", "balance"))
	return map[string]any{
		"hash":                                bytesToHex(a["hash"]),
		"coin_balance":                        balance,
		// SPA reads `balance` directly in `Number(i.balance)/1e18`; alias to
		// `coin_balance` so the Blockscout-derived `coin_balance` and the
		// downstream-tenant SPA's `balance` both work without a SPA rebuild.
		"balance":                             balance,
		"block_number_balance_was_fetched_at": firstNonNil(a, "fetched_coin_balance_block_number"),
		"transactions_count":                  txCount,
		// SPA reads `tx_count` directly (the field on block rows). Alias so the
		// SPA's `i.tx_count.toLocaleString()` on the address-detail page works.
		"tx_count":                            txCount,
		"token_transfers_count":               ttCount,
		"is_contract":                         isContract,
		"is_verified":                         firstNonNil(a, "verified"),
		"has_token_balances":                  false,
		"exchange_rate":                       nil,
	}
}

// toBool is defined in tokenscore.go in this same package.

// firstNonNil returns the first non-nil value at any of the given map keys,
// or nil if all are missing/nil. Lets callers tolerate schema drift between
// the legacy Postgres column layout and the trimmed SQLite one.
func firstNonNil(a map[string]any, keys ...string) any {
	for _, k := range keys {
		if v, ok := a[k]; ok && v != nil {
			return v
		}
	}
	return nil
}

func txStatusStr(v any) string {
	switch s := v.(type) {
	case int64:
		if s == 1 {
			return "ok"
		}
		return "error"
	case float64:
		if s == 1 {
			return "ok"
		}
		return "error"
	case nil:
		return "pending"
	default:
		return "pending"
	}
}

func txResultStr(t map[string]any) string {
	if txStatusStr(t["status"]) == "ok" {
		return "success"
	}
	if e := t["error"]; e != nil {
		return fmt.Sprintf("%v", e)
	}
	return ""
}

func txMethodStr(input any) string {
	s := bytesToHex(input)
	if len(s) >= 10 {
		return s[:10]
	}
	return ""
}
