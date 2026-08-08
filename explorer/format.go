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
// nullableNum is fmtNum for figures where "we have no value" is a real and
// different answer from "the value is zero". fmtNum collapses both to "0",
// which is how an explorer ends up telling an investor that a contract
// holding 159 million LUX has a balance of zero.
func nullableNum(v any) any {
	if v == nil {
		return nil
	}
	return fmtNum(v)
}

func fmtNum(v any) string {
	if v == nil {
		return "0"
	}
	if s, ok := v.(string); ok {
		if strings.HasPrefix(s, "0x") || strings.HasPrefix(s, "0X") {
			trimmed := s[2:]
			if trimmed == "" {
				return "0"
			}
			if n, ok := new(big.Int).SetString(trimmed, 16); ok {
				return n.String()
			}
			return "0"
		}
		if s == "" {
			// un-ingested value/gas_price are stored as "" — emit "0" so the
			// SPA's BigNumber()/Number() never produces NaN.
			return "0"
		}
	}
	return fmt.Sprintf("%v", v)
}

// bigFromAny parses a numeric DB value (hex "0x…", decimal string, or a
// scanned integer) into a big.Int.
func bigFromAny(v any) (*big.Int, bool) {
	switch x := v.(type) {
	case nil:
		return nil, false
	case int64:
		return big.NewInt(x), true
	case int:
		return big.NewInt(int64(x)), true
	case uint64:
		return new(big.Int).SetUint64(x), true
	case float64:
		bi, _ := big.NewFloat(x).Int(nil)
		return bi, true
	case []byte:
		return bigFromStr(string(x))
	case string:
		return bigFromStr(x)
	}
	return nil, false
}

func bigFromStr(s string) (*big.Int, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, false
	}
	if strings.HasPrefix(s, "0x") || strings.HasPrefix(s, "0X") {
		return new(big.Int).SetString(s[2:], 16)
	}
	return new(big.Int).SetString(s, 10)
}

// mulBig returns a*b as a decimal string, or "0" when either operand is
// missing/unparseable. Used for block burnt_fees (gas_used × base_fee) and
// tx fee (gas_used × effective_gas_price).
func mulBig(a, b any) string {
	x, ok1 := bigFromAny(a)
	y, ok2 := bigFromAny(b)
	if !ok1 || !ok2 {
		return "0"
	}
	return new(big.Int).Mul(x, y).String()
}

// txFeeObj builds the Blockscout {type, value} transaction-fee object.
// value = gas_used × effective_gas_price (falling back to gas_price; for
// legacy/type-0 txs they are equal). Always returns an object with a decimal
// value ("0" when gas data is absent) so the SPA never reads .value off null.
func txFeeObj(t map[string]any) map[string]any {
	gasUsed, _ := bigFromAny(t["gas_used"])
	var price *big.Int
	// Price precedence: effective_gas_price (exact) → gas_price (submission
	// price) → block base_fee (the burnt rate, joined into the tx row via
	// txSelect). First positive wins, so historical rows with an empty
	// gas_price still get a real fee from the block's base_fee.
	for _, k := range []string{"effective_gas_price", "gas_price", "block_base_fee"} {
		if p, ok := bigFromAny(t[k]); ok && p.Sign() > 0 {
			price = p
			break
		}
	}
	val := "0"
	if gasUsed != nil && gasUsed.Sign() > 0 && price != nil {
		val = new(big.Int).Mul(gasUsed, price).String()
	}
	return map[string]any{"type": "actual", "value": val}
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
		// gas_used × base_fee is the Ethereum EIP-1559 burn. Lux does not
		// burn it: creditTxFee credits the whole fee to the block coinbase
		// (0x0100…0000, which has no code and no key) unless the fee split
		// is active, and the fee split has never fired on mainnet. Calling
		// the base fee "burnt" made every block report Burnt fees == Txn
		// fees, which the SPA renders as a 100% utilisation bar — a burn
		// that never happened, on a supply that is not deflationary.
		"burnt_fees": "0",
		"rewards":    []any{},
		"timestamp":  fmtTimestamp(b["timestamp"]),
		// The SPA reads `transactions_count` on a block — its Block type
		// declares that name and all nine render sites use it (block rows, the
		// home page's latest-blocks list, block details). This emitted the
		// column's own name, `tx_count`, so every REST-loaded row rendered the
		// Txn label with nothing after it, while rows pushed over the realtime
		// channel showed a number, because that path already renamed the field.
		// One name, and it is the reader's.
		"transactions_count": col(b, "tx_count", "transaction_count"),
		"state_root":         nil,
		"type":               "block",
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
	resp["fee"] = txFeeObj(t)

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
//
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
		"from": map[string]any{"hash": bytesToHex(col(t, "from_addr", "from_address_hash", "from_address"))},
		"to":   map[string]any{"hash": bytesToHex(col(t, "to_addr", "to_address_hash", "to_address"))},
		// Same TokenInfo contract as formatToken, narrowed to what a transfer
		// row carries — so the key is address_hash here too.
		"token":            map[string]any{"address_hash": bytesToHex(t["token_address"]), "type": t["token_type"]},
		"total":            map[string]any{"value": fmtNum(col(t, "value", "amount")), "decimals": nil},
		"log_index":        t["log_index"],
		"block_number":     t["block_number"],
		"transaction_hash": bytesToHex(col(t, "tx_hash", "transaction_hash")),
		"timestamp":        fmtTimestamp(t["timestamp"]),
	}
}

// formatToken formats a token row.
//
// Emitted key names are the TokenInfo contract in luxfi/explore
// (types/api/token.ts), the only consumer of this surface. It reads
// address_hash/holders_count; the earlier address/holders spelling keyed every
// row of the tokens table on undefined and crashed the page.
func formatToken(t map[string]any) map[string]any {
	// Input schema variants:
	//   - luxfi/indexer evm_tokens:        column "address",   "token_type"
	//   - Blockscout legacy tokens:        "contract_address", "type"
	//   - blockscout-derivative variants:  "contract_addr", "created_contract_address_hash",
	//                                      "address_hash"
	// Fall through every known spelling so the response shape works against
	// any schema this binary is bolted onto.
	return map[string]any{
		"address_hash": bytesToHex(col(t, "address", "contract_addr", "created_contract_address_hash", "address_hash", "contract_address")),
		"name":         t["name"],
		"symbol":       t["symbol"],
		"total_supply": fmtNum(t["total_supply"]),
		"decimals":     fmtNum(t["decimals"]),
		"type":         col(t, "token_type", "type"),
		// Counted from the balance rows by tokenSelect, falling back to the
		// stored column on schemas that maintain it. luxfi/indexer never
		// writes evm_tokens.holder_count, so reading it alone printed
		// "Holders 0" over a populated holder list. Absent => null, so the
		// page can say "unknown" instead of asserting nobody holds it.
		"holders_count":          nullableNum(firstNonNil(t, "holder_count_live", "holder_count")),
		"exchange_rate":          t["fiat_value"],
		"circulating_market_cap": fmtNum(t["circulating_market_cap"]),
		"icon_url":               t["icon_url"],
		// TokenReputation is 'ok' | 'scam' — a fraud flag, not a quality
		// score. We run no scam oracle, so the honest value is null
		// ("unknown"); the page renders no badge for it. Deriving it from
		// the old numeric completeness score would have labelled any token
		// lacking an icon a scam.
		"reputation": nil,
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
//  1. boolean column `is_contract` (the indexer's address upsert
//     populates this via the per-block eth_getCode resolve loop in
//     luxfi/indexer#10).
//  2. non-empty contract_code / code column (legacy Postgres shape).
//
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
	// Only `fetched_coin_balance` is a measurement — it travels with a
	// fetched-at block number, so it has provenance. `evm_addresses.balance`
	// is a placeholder: the indexer writes the literal '0' on first sight
	// and never updates it, so reading it reported a balance of zero for
	// every address on the chain, WLUX's 159,126,795.518 LUX included.
	// A placeholder is not evidence; unknown is null.
	// StandaloneServer.withNativeBalance overlays the node's live answer.
	balance := nullableNum(firstNonNil(a, "fetched_coin_balance"))
	return map[string]any{
		"hash":         bytesToHex(a["hash"]),
		"coin_balance": balance,
		// SPA reads `balance` directly in `Number(i.balance)/1e18`; alias to
		// `coin_balance` so the Blockscout-derived `coin_balance` and the
		// downstream-tenant SPA's `balance` both work without a SPA rebuild.
		"balance":                             balance,
		"block_number_balance_was_fetched_at": firstNonNil(a, "fetched_coin_balance_block_number"),
		"transactions_count":                  txCount,
		// SPA reads `tx_count` directly (the field on block rows). Alias so the
		// SPA's `i.tx_count.toLocaleString()` on the address-detail page works.
		"tx_count":              txCount,
		"token_transfers_count": ttCount,
		"is_contract":           isContract,
		"is_verified":           firstNonNil(a, "verified"),
		"has_token_balances":    false,
		"exchange_rate":         nil,
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
