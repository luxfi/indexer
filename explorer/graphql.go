package explorer

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
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
	client *http.Client
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
		json.NewEncoder(w).Encode(graphqlErrorResponse(
			fmt.Sprintf("G-Chain upstream error: %v", err),
		))
	}

	return &graphqlProxy{
		target: u,
		proxy:  proxy,
		client: &http.Client{Timeout: graphqlProxyTimeout},
	}, nil
}

// handleGChainGraphQL proxies GraphQL requests to the G-Chain node endpoint.
// GET serves the playground. POST proxies the query.
func (p *plugin) handleGChainGraphQL(e *core.RequestEvent) error {
	if e.Request.Method == http.MethodGet {
		return p.serveGraphQLPlayground(e, "G-Chain Federated GraphQL", "")
	}

	if p.gchainProxy == nil {
		return e.JSON(http.StatusServiceUnavailable, graphqlErrorResponse(
			"G-Chain endpoint not configured",
		))
	}

	p.gchainProxy.proxy.ServeHTTP(e.Response, e.Request)
	return nil
}

// handlePerChainGraphQL proxies GraphQL requests to a per-chain local endpoint.
// The chain name comes from the {chain} path parameter.
// GET serves the playground. POST proxies to the node's per-chain GraphQL.
func (p *plugin) handlePerChainGraphQL(e *core.RequestEvent) error {
	chain := e.Request.PathValue("chain")
	if chain == "" {
		return e.JSON(http.StatusBadRequest, graphqlErrorResponse("missing chain parameter"))
	}

	if e.Request.Method == http.MethodGet {
		return p.serveGraphQLPlayground(e, fmt.Sprintf("%s GraphQL", chain), chain)
	}

	// Build the per-chain endpoint URL on the node.
	// Standard Lux node path: /ext/bc/{chain}/graphql
	// For C-chain, this would be /ext/bc/C/graphql, etc.
	nodeBase := p.config.NodeEndpoint
	if nodeBase == "" {
		nodeBase = "http://localhost:9650"
	}

	target := fmt.Sprintf("%s/ext/bc/%s/graphql", strings.TrimRight(nodeBase, "/"), chain)

	// Forward the request body to the per-chain endpoint.
	body, err := io.ReadAll(e.Request.Body)
	if err != nil {
		return e.JSON(http.StatusBadRequest, graphqlErrorResponse("failed to read request body"))
	}

	req, err := http.NewRequestWithContext(
		e.Request.Context(),
		http.MethodPost,
		target,
		strings.NewReader(string(body)),
	)
	if err != nil {
		return e.JSON(http.StatusInternalServerError, graphqlErrorResponse(
			fmt.Sprintf("failed to create upstream request: %v", err),
		))
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: graphqlProxyTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return e.JSON(http.StatusBadGateway, graphqlErrorResponse(
			fmt.Sprintf("per-chain upstream error: %v", err),
		))
	}
	defer resp.Body.Close()

	// Forward the upstream response as-is.
	e.Response.Header().Set("Content-Type", "application/json")
	e.Response.WriteHeader(resp.StatusCode)
	io.Copy(e.Response, resp.Body)
	return nil
}

// serveGraphQLPlayground serves the GraphiQL playground HTML.
// If chain is empty, the endpoint is the federated G-Chain route.
func (p *plugin) serveGraphQLPlayground(e *core.RequestEvent, title, chain string) error {
	endpoint := "/v1/explorer/graphql"
	if chain != "" {
		endpoint = fmt.Sprintf("/v1/explorer/%s/graphql", chain)
	}

	html := fmt.Sprintf(`<!DOCTYPE html>
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
</html>`, title, endpoint)

	return e.HTML(http.StatusOK, html)
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
