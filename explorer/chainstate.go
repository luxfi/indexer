package explorer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"time"
)

// Current state belongs to the chain, not to an index of it.
//
// An indexer's job is history: blocks, transactions, transfers — facts that
// are only knowable by having watched. A native-coin balance is not history;
// the node answers it in one call and is never stale. Keeping a second copy
// in SQLite means keeping a copy that is wrong between updates, and the
// evm_addresses.balance column was never updated at all — every address page
// read "0" against a chain where WLUX alone holds 159,126,795.518 LUX.
//
// So we ask the node. One batched JSON-RPC round trip serves a whole page of
// addresses. When the node cannot be reached the balance is *unavailable*,
// and unavailable is reported as null — never as zero.

const balanceRPCTimeout = 4 * time.Second

// nativeBalances returns wei balances keyed by the lowercase address, for
// the addresses the node answered for. Absent key = unavailable; the caller
// must render null, not 0.
func (s *StandaloneServer) nativeBalances(ctx context.Context, addrs []string) map[string]string {
	if s.cfg.RPCEndpoint == "" || len(addrs) == 0 {
		return nil
	}

	type rpcReq struct {
		JSONRPC string `json:"jsonrpc"`
		ID      int    `json:"id"`
		Method  string `json:"method"`
		Params  []any  `json:"params"`
	}
	batch := make([]rpcReq, 0, len(addrs))
	order := make([]string, 0, len(addrs))
	seen := make(map[string]struct{}, len(addrs))
	for _, a := range addrs {
		a = strings.ToLower(a)
		if !isValidHexAddr(a) {
			continue
		}
		if _, dup := seen[a]; dup {
			continue
		}
		seen[a] = struct{}{}
		batch = append(batch, rpcReq{JSONRPC: "2.0", ID: len(order), Method: "eth_getBalance", Params: []any{a, "latest"}})
		order = append(order, a)
	}
	if len(batch) == 0 {
		return nil
	}

	body, err := json.Marshal(batch)
	if err != nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, balanceRPCTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.cfg.RPCEndpoint, bytes.NewReader(body))
	if err != nil {
		return nil
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}

	var out []struct {
		ID     int    `json:"id"`
		Result string `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil
	}

	balances := make(map[string]string, len(out))
	for _, r := range out {
		if r.ID < 0 || r.ID >= len(order) || r.Result == "" {
			continue
		}
		n, ok := new(big.Int).SetString(strings.TrimPrefix(r.Result, "0x"), 16)
		if !ok {
			continue
		}
		balances[order[r.ID]] = n.String()
	}
	return balances
}

// withNativeBalance overlays live balances onto formatted address maps.
// A formatted address whose balance the node did not answer for keeps
// whatever the DB knew; when the DB knew nothing either, formatAddress has
// already written null.
func (s *StandaloneServer) withNativeBalance(ctx context.Context, items []map[string]any) {
	addrs := make([]string, 0, len(items))
	for _, it := range items {
		if h, ok := it["hash"].(string); ok {
			addrs = append(addrs, h)
		}
	}
	balances := s.nativeBalances(ctx, addrs)
	if len(balances) == 0 {
		return
	}
	for _, it := range items {
		h, _ := it["hash"].(string)
		if bal, ok := balances[strings.ToLower(h)]; ok {
			it["coin_balance"] = bal
			it["balance"] = bal
		}
	}
}

// countRows returns a COUNT(*) and whether it could be taken at all. A count
// that could not be taken is unavailable — the caller reports null, not 0.
func (s *StandaloneServer) countRows(ctx context.Context, query string, args ...any) (int64, bool) {
	var n int64
	if err := s.db.QueryRowContext(ctx, query, args...).Scan(&n); err != nil {
		return 0, false
	}
	return n, true
}

// tokenHolderCount counts holders with a non-zero balance of one token.
func (s *StandaloneServer) tokenHolderCount(ctx context.Context, token string) (int64, bool) {
	if s.t.balTokenCol == "" {
		return 0, false
	}
	return s.countRows(ctx, fmt.Sprintf(
		"SELECT COUNT(*) FROM %s WHERE LOWER(%s) = ? AND value != '0' AND value != ''", s.t.balances, s.t.balTokenCol),
		strings.ToLower(token))
}

// tokenTransferCount counts transfers of one token.
func (s *StandaloneServer) tokenTransferCount(ctx context.Context, token string) (int64, bool) {
	if s.t.transferTokenCol == "" {
		return 0, false
	}
	return s.countRows(ctx, fmt.Sprintf(
		"SELECT COUNT(*) FROM %s WHERE LOWER(%s) = ?", s.t.transfers, s.t.transferTokenCol),
		strings.ToLower(token))
}
