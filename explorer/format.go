package explorer

import (
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

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

// fmtNum formats a numeric value as string.
func fmtNum(v any) string {
	if v == nil {
		return "0"
	}
	return fmt.Sprintf("%v", v)
}

// fmtTimestamp formats a unix timestamp for the explorer API v2 response.
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
	default:
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
		"miner":            map[string]any{"hash": bytesToHex(b["miner"])},
		"difficulty":       fmtNum(b["difficulty"]),
		"total_difficulty": fmtNum(b["total_difficulty"]),
		"size":             b["size"],
		"gas_limit":        fmtNum(b["gas_limit"]),
		"gas_used":         fmtNum(b["gas_used"]),
		"base_fee_per_gas": fmtNum(b["base_fee"]),
		"timestamp":        fmtTimestamp(b["timestamp"]),
		"tx_count":         b["transaction_count"],
		"state_root":       bytesToHex(b["state_root"]),
		"type":             "block",
	}
}

// formatTx formats a transaction row as an explorer v2 transaction response.
func formatTx(t map[string]any) map[string]any {
	resp := map[string]any{
		"hash":                       bytesToHex(t["hash"]),
		"block_number":              t["block_number"],
		"block_hash":                bytesToHex(t["block_hash"]),
		"from":                      map[string]any{"hash": bytesToHex(t["from_address_hash"])},
		"to":                        nil,
		"value":                     fmtNum(t["value"]),
		"gas_limit":                 fmtNum(t["gas"]),
		"gas_price":                 fmtNum(t["gas_price"]),
		"gas_used":                  fmtNum(t["gas_used"]),
		"max_fee_per_gas":           fmtNum(t["max_fee_per_gas"]),
		"max_priority_fee_per_gas":  fmtNum(t["max_priority_fee_per_gas"]),
		"nonce":                     t["nonce"],
		"position":                  t["transaction_index"],
		"type":                      t["type"],
		"status":                    txStatusStr(t["status"]),
		"timestamp":                 fmtTimestamp(t["block_timestamp"]),
		"method":                    txMethodStr(t["input"]),
		"result":                    txResultStr(t),
	}

	if to := t["to_address_hash"]; to != nil {
		if s := bytesToHex(to); s != "" {
			resp["to"] = map[string]any{"hash": s}
		}
	}
	if ca := t["created_contract_address_hash"]; ca != nil {
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
		"from":             map[string]any{"hash": bytesToHex(t["from_address_hash"])},
		"to":               nil,
		"value":            fmtNum(t["value"]),
		"gas_limit":        fmtNum(t["gas"]),
		"gas_used":         fmtNum(t["gas_used"]),
		"input":            bytesToHex(t["input"]),
		"output":           bytesToHex(t["output"]),
		"error":            errField,
		"success":          success,
		"timestamp":        fmtTimestamp(t["block_timestamp"]),
	}
	if to := t["to_address_hash"]; to != nil {
		if s := bytesToHex(to); s != "" {
			resp["to"] = map[string]any{"hash": s}
		}
	}
	return resp
}

// formatLog formats a log row.
func formatLog(l map[string]any) map[string]any {
	topics := []string{}
	for _, key := range []string{"first_topic", "second_topic", "third_topic", "fourth_topic"} {
		if v := l[key]; v != nil {
			if s := bytesToHex(v); s != "" {
				topics = append(topics, s)
			}
		}
	}
	return map[string]any{
		"address":          map[string]any{"hash": bytesToHex(l["address_hash"])},
		"data":             bytesToHex(l["data"]),
		"topics":           topics,
		"index":            l["index"],
		"block_number":     l["block_number"],
		"transaction_hash": bytesToHex(l["transaction_hash"]),
		"decoded":          nil,
	}
}

// formatTokenTransfer formats a token transfer row.
func formatTokenTransfer(t map[string]any) map[string]any {
	return map[string]any{
		"from":             map[string]any{"hash": bytesToHex(t["from_address_hash"])},
		"to":               map[string]any{"hash": bytesToHex(t["to_address_hash"])},
		"token":            map[string]any{"address": bytesToHex(t["token_contract_address_hash"]), "type": t["token_type"]},
		"total":            map[string]any{"value": fmtNum(t["amount"]), "decimals": nil},
		"log_index":        t["log_index"],
		"block_number":     t["block_number"],
		"transaction_hash": bytesToHex(t["transaction_hash"]),
		"timestamp":        fmtTimestamp(t["block_timestamp"]),
	}
}

// formatToken formats a token row.
func formatToken(t map[string]any) map[string]any {
	return map[string]any{
		"address":                bytesToHex(t["contract_address_hash"]),
		"name":                   t["name"],
		"symbol":                 t["symbol"],
		"total_supply":           fmtNum(t["total_supply"]),
		"decimals":               fmtNum(t["decimals"]),
		"type":                   t["type"],
		"holders":                fmtNum(t["holder_count"]),
		"exchange_rate":          t["fiat_value"],
		"circulating_market_cap": fmtNum(t["circulating_market_cap"]),
		"icon_url":               t["icon_url"],
	}
}

// formatContract formats a smart contract row.
func formatContract(c map[string]any) map[string]any {
	return map[string]any{
		"address":            map[string]any{"hash": bytesToHex(c["address_hash"])},
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
	}
}

// formatAddress formats an address row.
func formatAddress(a map[string]any) map[string]any {
	return map[string]any{
		"hash":                                bytesToHex(a["hash"]),
		"coin_balance":                        fmtNum(a["fetched_coin_balance"]),
		"block_number_balance_was_fetched_at": a["fetched_coin_balance_block_number"],
		"transactions_count":                  a["transactions_count"],
		"token_transfers_count":               a["token_transfers_count"],
		"is_contract":                         a["contract_code"] != nil,
		"is_verified":                         a["verified"],
		"has_token_balances":                  false,
		"exchange_rate":                       nil,
	}
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
