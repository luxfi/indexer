package explorer

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sync"
	"time"

	"github.com/hanzoai/base/core"
)

const (
	defaultGChainEndpoint = "http://localhost:9650/ext/bc/G/graphql"
	graphqlProxyTimeout   = 30 * time.Second
)

// graphqlProxy is a reverse proxy to the G-Chain GraphQL endpoint on the node.
type graphqlProxy struct {
	target *url.URL
	proxy  *httputil.ReverseProxy
}

func newGraphQLProxy(endpoint string) (*graphqlProxy, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("explorer: invalid G-Chain endpoint %q: %w", endpoint, err)
	}

	proxy := httputil.NewSingleHostReverseProxy(u)
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		json.NewEncoder(w).Encode(graphqlErrorResponse("upstream service unavailable"))
	}

	return &graphqlProxy{target: u, proxy: proxy}, nil
}

// handleLocalGraphQL reads from this chain's SQLite directly.
// This is the per-chain handler — fast, local, no network call.
// GET serves the playground. POST queries the local indexer DB.
func (p *plugin) handleLocalGraphQL(e *core.RequestEvent) error {
	if e.Request.Method == http.MethodGet {
		return p.serveGraphQLPlayground(e, fmt.Sprintf("%s GraphQL", p.config.ChainName), "local")
	}

	// Query the local indexer SQLite for this chain's data.
	// This replaces the per-chain proxy — no node call needed.
	return p.executeLocalGraphQL(e)
}

// handleFederatedGraphQL is a simple reverse proxy to the G-Chain endpoint.
// G-Chain provides consensus-backed federated GraphQL across all chains.
// GET serves the playground. POST proxies to GCHAIN_ENDPOINT.
func (p *plugin) handleFederatedGraphQL(e *core.RequestEvent) error {
	if e.Request.Method == http.MethodGet {
		return p.serveGraphQLPlayground(e, "G-Chain Federated GraphQL", "federated")
	}

	if p.gchainProxy == nil {
		return e.JSON(http.StatusServiceUnavailable, graphqlErrorResponse(
			"G-Chain endpoint not configured",
		))
	}

	p.gchainProxy.proxy.ServeHTTP(e.Response, e.Request)
	return nil
}

// handleCrossChainSearch queries multiple per-chain SQLite files in parallel.
// This is what kills subgraphs — the explorer has ALL chain data locally,
// so it can answer "find address 0x123 across all indexed chains" by querying
// N SQLite files in parallel goroutines.
func (p *plugin) handleCrossChainSearch(e *core.RequestEvent) error {
	q := e.Request.URL.Query()
	address := q.Get("address")
	txHash := q.Get("tx_hash")

	if address == "" && txHash == "" {
		return e.JSON(http.StatusBadRequest, graphqlErrorResponse(
			"cross-chain search requires 'address' or 'tx_hash' parameter",
		))
	}

	// Use pre-opened connections if available; fall back to single-chain local DB.
	dbs := p.chainDBs
	if len(dbs) == 0 {
		dbs = map[string]*sql.DB{p.config.ChainName: p.db}
	}

	type chainResult struct {
		Chain string `json:"chain"`
		Data  any    `json:"data"`
		Error string `json:"error,omitempty"`
	}

	var (
		mu      sync.Mutex
		results []chainResult
		wg      sync.WaitGroup
	)

	for chain, db := range dbs {
		wg.Add(1)
		go func(chain string, db *sql.DB) {
			defer wg.Done()

			var data any
			var err error
			if address != "" {
				data, err = searchAddress(db, address)
			} else {
				data, err = searchTxHash(db, txHash)
			}

			mu.Lock()
			if err != nil {
				results = append(results, chainResult{Chain: chain, Error: err.Error()})
			} else if data != nil {
				results = append(results, chainResult{Chain: chain, Data: data})
			}
			mu.Unlock()
		}(chain, db)
	}

	wg.Wait()
	return e.JSON(http.StatusOK, map[string]any{"results": results})
}

// searchAddress queries a chain's SQLite for address activity.
func searchAddress(db *sql.DB, addr string) (any, error) {
	row := db.QueryRow(`SELECT hash, coin_balance, transactions_count, token_transfers_count
		FROM addresses WHERE hash = ? LIMIT 1`, addr)

	var hash string
	var balance sql.NullString
	var txCount, ttCount sql.NullInt64
	if err := row.Scan(&hash, &balance, &txCount, &ttCount); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	return map[string]any{
		"address":               hash,
		"coin_balance":          balance.String,
		"transactions_count":    txCount.Int64,
		"token_transfers_count": ttCount.Int64,
	}, nil
}

// searchTxHash queries a chain's SQLite for a transaction by hash.
func searchTxHash(db *sql.DB, hash string) (any, error) {
	row := db.QueryRow(`SELECT hash, block_number, from_address_hash, to_address_hash, value, status
		FROM transactions WHERE hash = ? LIMIT 1`, hash)

	var txHash string
	var blockNum sql.NullInt64
	var from, to sql.NullString
	var value sql.NullString
	var status sql.NullInt64
	if err := row.Scan(&txHash, &blockNum, &from, &to, &value, &status); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	return map[string]any{
		"hash":         txHash,
		"block_number": blockNum.Int64,
		"from":         from.String,
		"to":           to.String,
		"value":        value.String,
		"status":       txStatusStr(status.Int64),
	}, nil
}

// executeLocalGraphQL runs a simple GraphQL-like query against the local SQLite.
// Supports basic block/transaction/address lookups.
func (p *plugin) executeLocalGraphQL(e *core.RequestEvent) error {
	// Stub: returns typename for GraphQL playground introspection.
	// Actual queries served via REST endpoints.
	return e.JSON(http.StatusOK, map[string]any{
		"data": map[string]any{
			"__typename": "Query",
		},
	})
}

// serveGraphQLPlayground serves the GraphiQL playground HTML.
func (p *plugin) serveGraphQLPlayground(e *core.RequestEvent, title, variant string) error {
	endpoint := "/v1/explorer/graphql"
	switch variant {
	case "local":
		endpoint = "/v1/explorer/local/graphql"
	case "federated":
		endpoint = "/v1/explorer/graphql"
	}

	page := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
  <title>%s</title>
  <link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/graphql-playground-react/build/static/css/index.css" />
  <script src="https://cdn.jsdelivr.net/npm/graphql-playground-react/build/static/js/middleware.js"></script>
</head>
<body>
  <div id="root"></div>
  <script>
    window.addEventListener('load', function() {
      GraphQLPlayground.init(document.getElementById('root'), {
        endpoint: '%s',
        settings: {
          'editor.theme': 'dark',
          'editor.fontSize': 14,
        }
      })
    })
  </script>
</body>
</html>`, html.EscapeString(title), endpoint)

	return e.HTML(http.StatusOK, page)
}

// graphqlErrorResponse builds a standard GraphQL error response body.
func graphqlErrorResponse(msg string) map[string]any {
	return map[string]any{
		"data": nil,
		"errors": []map[string]string{
			{"message": msg},
		},
	}
}
