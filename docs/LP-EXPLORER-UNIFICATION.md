# Explorer unification — `luxfi/explorer`

Target: one binary, one Docker image, one deploy — serving the indexer +
graph + frontend for any number of chains (Lux, Zoo, Hanzo, SPC, Pars,
Liquidity, local 1337s), white-labeled by hostname at the ingress.

Owner: `lux/cli` drives config; `luxfi/explorer` is the runtime.

## Current state (as of this doc)

| Component | Location | Status |
|---|---|---|
| Indexer + FE server | `lux/indexer/cmd/explorer/` (Go, 587 LoC main) | working, multi-chain, `go:embed` Vite assets |
| Graph engine | `lux/graph/cmd/graph/` (Go, separate binary) | working, GraphQL subgraph queries |
| Explorer FE | `lux/explore/` (Next.js 16) | working, stateless SSR (see commit 275285e26) |
| Hanzo Base coupling | 7 files under `lux/indexer/explorer/` | **to remove** |
| CLI | `lux/cli/cmd/explorecmd/explore.go` | wraps the indexer binary |

## Plan

### 1. Rename & relocate

Move `lux/indexer/cmd/explorer/` → new repo `luxfi/explorer`. Keep
`lux/indexer/` as the pure indexer library (chain-agnostic ingestion
primitives) that `luxfi/explorer` depends on.

### 2. Merge graph engine in-process

`lux/graph/engine` + `lux/graph/indexer` become modules imported by
`luxfi/explorer` main.go. The GraphQL handler mounts on
`/v1/graph/{chain}/*`. No second binary, no second deploy.

### 3. Disconnect Hanzo Base

Files importing `github.com/hanzoai/base/core`:

- `explorer.go` — replace `base/tools/router` with `net/http` + `chi/v5`.
- `handlers.go` — replace `core.App` with plain `http.Handler`; SSE via `net/http.Flusher`.
- `graphql.go` — keep `graphql-go`, drop `base/core` request hooks.
- `collections.go`, `notifications.go`, `concentration.go` — same.
- `graphql_test.go` — rewrite against plain `httptest.Server`.

Rationale: `hanzoai/base` is an application framework (PocketBase fork) —
overkill for a stateless per-chain indexer. Adds a 40 MB dep, a DB layer
we don't use (SQLite chain DBs are per-chain, not Base's central DB),
and blocks embedding in CI-built binaries.

### 4. Embed the real FE

Replace the stub Vite bundle in `cmd/explorer/static/` with the output of
`next build && next export` from `lux/explore/`. Requires:

- Next.js `output: 'export'` in `next.config.js` (or a build flag).
- Ensure every route the FE uses is statically exportable (no SSR-only
  data fetching in `/app`, `/pages`). The chainRegistry already runs
  client-side via `window.location.hostname` — that part is fine.
- `lux/explore/scripts/export-embed.sh` runs `next build && next export`,
  stages output into `lux/indexer/cmd/explorer/static/`, and the Go
  `//go:embed` directive picks it up.

### 5. Lux CLI orchestration

New subcommand tree under `lux dev`:

```
lux dev stack up [--chains N]          # spawn N local chains + explorer + bridge + exchange + safe + dao + wallet + faucet
lux dev stack down
lux dev stack status
lux dev stack logs <app>
```

Implementation: a single `stack.yaml` in `~/.lux/dev/` lists the apps
and their binaries. `lux dev stack up` reads it and spawns each with
the right env vars. Ports deconflict automatically (3000-3999 reserved
for the dev stack; +100 per chain instance for the N-chain case).

Default apps:

| App | Repo | Port base |
|---|---|---|
| luxd (chain) | `lux/node` | 9650 + i |
| explorer | `luxfi/explorer` | 3001 |
| bridge | `lux/bridge` | 3002 |
| exchange | `liquidity/app` | 3003 |
| safe | `lux/safe` | 3004 |
| dao | `lux/dao` | 3005 |
| wallet | `lux/wallet` | 3006 |
| faucet | `lux/faucet` | 3007 |

Each app reads `~/.lux/dev/chains.json` to discover the currently-running
chains. Hot reload: when a chain is added/removed, apps re-read the
registry (no restart needed for the chain-selector dropdown).

### 6. Production: one deploy, many hostnames

With stateless SSR (already merged in `lux/explore@275285e26`) and the
merged binary, production is one Deployment in `lux-k8s`:

- 2+ replicas, HPA 2-8
- Ingress routes `explore.{lux,zoo.network,hanzo.ai,zoo.ngo}` →
  same Service
- chainRegistry auto-brands per-request via the Host header
- Cost: one set of pods for 6+ branded explorers

## Migration order

1. ✅ FE stateless SSR (`lux/explore@275285e26`)
2. Disconnect `hanzoai/base/core` (this repo, 7 files, ~3 days)
3. Merge `lux/graph/engine` into `cmd/explorer/main.go` (~1 day)
4. Rename to `luxfi/explorer` on GitHub; update `lux/cli` import
5. Static-export `lux/explore`, embed via `go:embed`
6. `lux dev stack` subcommand in `lux/cli`
7. Production: swap lux-k8s Deployment to the unified image

## What we delete on completion

- `lux/graph/cmd/graph/` (engine code stays in `lux/graph/engine/`)
- `lux/indexer/cmd/explorer/` (moves to `luxfi/explorer`)
- `hanzoai/base/core` dep in indexer
- Individual Dockerfiles for each branded explorer (one image covers them all)
