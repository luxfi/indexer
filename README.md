# LUX Indexer

Go-based indexers for LUX Network's native chains (DAG and linear). Works alongside Blockscout for C-Chain EVM.

## Architecture

```
┌──────────────────────────────────────────────────────────────────────┐
│                     LUX Explorer Frontend                             │
│                     Next.js (explore.lux.network)                     │
└───────────────────────────────┬──────────────────────────────────────┘
                                │
          ┌─────────────────────┴─────────────────────┐
          │                                           │
          ▼                                           ▼
┌─────────────────────┐               ┌───────────────────────────────┐
│   Blockscout        │               │   LUX Indexer (this repo)     │
│   (Elixir)          │               │   (Go)                        │
│                     │               │                               │
│   C-Chain (EVM)     │               │   DAG: X, A, B, Q, T, Z       │
│   Port 4000         │               │   Linear: P                   │
│                     │               │   Ports 4100-4700             │
└─────────┬───────────┘               └───────────────┬───────────────┘
          │                                           │
          └─────────────────────┬─────────────────────┘
                                ▼
┌──────────────────────────────────────────────────────────────────────┐
│                         PostgreSQL                                    │
│  explorer_cchain │ explorer_xchain │ explorer_pchain │ explorer_*    │
└──────────────────────────────────────────────────────────────────────┘
                                │
                                ▼
┌──────────────────────────────────────────────────────────────────────┐
│                          LUX Node (luxd)                              │
│                            Port 9630                                  │
│  RPC: xvm.* │ pvm.* │ avm.* │ bvm.* │ qvm.* │ tvm.* │ zvm.*         │
└──────────────────────────────────────────────────────────────────────┘
```

## Chain Types

### DAG-based Chains (Vertex/Parent model)
Uses `luxfi/consensus/engine/dag/vertex` - multiple parents per vertex.
DAG enables fast consensus convergence through parallel vertex processing.

| Chain | Port | Database | Description |
|-------|------|----------|-------------|
| X-Chain | 4200 | explorer_xchain | Asset exchange, UTXOs |
| A-Chain | 4500 | explorer_achain | AI compute, attestations |
| B-Chain | 4600 | explorer_bchain | Cross-chain bridge |
| Q-Chain | 4300 | explorer_qchain | Quantum finality proofs |
| T-Chain | 4700 | explorer_tchain | MPC threshold signatures |
| Z-Chain | 4400 | explorer_zchain | Privacy, ZK transactions (DAG for fast finality) |

### Linear Chains (Block/Parent model)
Uses `luxfi/consensus/engine/chain/block` - single parent per block.
Platform chain requires strict ordering for validator/staking operations.

| Chain | Port | Database | Description |
|-------|------|----------|-------------|
| P-Chain | 4100 | explorer_pchain | Platform, validators, staking |

### EVM (Blockscout)
| Chain | Port | Database | Description |
|-------|------|----------|-------------|
| C-Chain | 4000 | explorer_cchain | Smart contracts (Blockscout/Elixir) |

## Directory Structure

```
indexer/
├── dag/               # Shared DAG indexer library
│   ├── dag.go         # DAG vertex/edge types
│   ├── websocket.go   # Live DAG streaming
│   └── http.go        # REST API handlers
├── chain/             # Shared linear chain library
│   ├── chain.go       # Block types
│   └── http.go        # REST API handlers
├── xchain/            # X-Chain (Exchange) - DAG
├── achain/            # A-Chain (AI) - DAG
├── bchain/            # B-Chain (Bridge) - DAG
├── qchain/            # Q-Chain (Quantum) - DAG
├── tchain/            # T-Chain (Teleport) - DAG
├── zchain/            # Z-Chain (Privacy) - DAG
├── pchain/            # P-Chain (Platform) - Linear
├── cmd/               # CLI entry points
│   └── indexer/       # Multi-chain indexer CLI
├── deploy/            # Deployment configs
│   ├── docker/
│   └── k8s/
└── test/              # Integration tests
```

## Quick Start

### Prerequisites

- Go 1.21+
- PostgreSQL 14+
- Running LUX node (luxd) on port 9630

### Build

```bash
# Build all indexers
make build

# Build specific chain
make build-xchain
make build-pchain

# Build multi-chain binary
make build-all-in-one
```

### Run

```bash
# Run X-Chain indexer
./bin/xchain \
  --rpc http://localhost:9630/ext/bc/X \
  --db "postgres://blockscout:blockscout@localhost:5432/explorer_xchain" \
  --port 4200

# Run all indexers (single binary)
./bin/indexer --config config.yaml
```

### Docker

```bash
# Build image
docker build -t luxfi/indexer .

# Run
docker run -d \
  -e RPC_ENDPOINT=http://host.docker.internal:9630 \
  -e DATABASE_URL=postgres://... \
  -p 4200:4200 \
  luxfi/indexer xchain
```

## API Endpoints

All indexers expose Blockscout-compatible `/api/v2/` endpoints:

### DAG Chains (X, A, B, Q, T, Z)

```
GET  /api/v2/stats                    # Chain statistics
GET  /api/v2/vertices                 # List DAG vertices
GET  /api/v2/vertices/:id             # Get vertex by ID
GET  /api/v2/edges                    # List DAG edges
WS   /api/v2/dag/subscribe            # Live DAG stream
GET  /health                          # Health check
```

### Linear Chains (P)

```
GET  /api/v2/stats                    # Chain statistics
GET  /api/v2/blocks                   # List blocks
GET  /api/v2/blocks/:id               # Get block by ID
GET  /api/v2/blocks/height/:height    # Get block by height
WS   /api/v2/blocks/subscribe         # Live block stream
GET  /health                          # Health check
```

### Chain-Specific

See individual chain READMEs for chain-specific endpoints.

## Live DAG Visualization

WebSocket endpoint for real-time DAG updates:

```javascript
const ws = new WebSocket('ws://localhost:4200/api/v2/dag/subscribe');

ws.onmessage = (event) => {
  const msg = JSON.parse(event.data);
  switch (msg.type) {
    case 'vertex_added':
      console.log('New vertex:', msg.data.vertex);
      break;
    case 'edge_added':
      console.log('New edge:', msg.data.edge);
      break;
    case 'vertex_accepted':
      console.log('Accepted:', msg.data.vertex.id);
      break;
  }
};
```

## Database Schema

### DAG Vertices

```sql
CREATE TABLE {chain}_vertices (
  id TEXT PRIMARY KEY,
  type TEXT NOT NULL,
  parent_ids JSONB DEFAULT '[]',
  timestamp TIMESTAMPTZ NOT NULL,
  status TEXT DEFAULT 'pending',
  data JSONB,
  created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE {chain}_edges (
  source TEXT NOT NULL,
  target TEXT NOT NULL,
  type TEXT NOT NULL,
  PRIMARY KEY (source, target, type)
);
```

### Linear Blocks

```sql
CREATE TABLE {chain}_blocks (
  id TEXT PRIMARY KEY,
  parent_id TEXT,
  height BIGINT NOT NULL,
  timestamp TIMESTAMPTZ NOT NULL,
  status TEXT DEFAULT 'pending',
  data JSONB,
  created_at TIMESTAMPTZ DEFAULT NOW()
);
```

## Testing

```bash
# Unit tests
make test

# Integration tests (requires running node)
make test-integration

# Coverage
make test-coverage
```

## CI/CD

GitHub Actions builds and pushes Docker images on:
- Push to `main` → `luxfi/indexer:latest`
- Tags `v*` → `luxfi/indexer:v1.2.3`

```yaml
# .github/workflows/build.yml
name: Build and Push
on:
  push:
    branches: [main]
    tags: ['v*']
```

## Configuration

### Environment Variables

```bash
RPC_ENDPOINT=http://localhost:9630/ext/bc/X
DATABASE_URL=postgres://blockscout:blockscout@localhost:5432/explorer_xchain
HTTP_PORT=4200
POLL_INTERVAL=30s
LOG_LEVEL=info
```

### Config File

```yaml
# config.yaml
chains:
  - name: xchain
    type: dag
    rpc: http://localhost:9630/ext/bc/X
    database: postgres://...@localhost:5432/explorer_xchain
    port: 4200
  - name: pchain
    type: linear
    rpc: http://localhost:9630/ext/bc/P
    database: postgres://...@localhost:5432/explorer_pchain
    port: 4100
```

## Network Configs

### Mainnet
- RPC: `http://api.lux.network:9630/ext/bc/{X,P,A,...}`
- Databases: `explorer_{chain}`

### Testnet
- RPC: `http://api-test.lux.network:9630/ext/bc/{X,P,A,...}`
- Databases: `explorer_{chain}_testnet`

## Related

- [LUX Consensus](https://github.com/luxfi/consensus) - DAG/Chain data structures
- [LUX Node](https://github.com/luxfi/node) - Blockchain node
- [LUX Explorer](https://github.com/luxfi/explore) - Frontend
- [LP-0038: Explorer Architecture](https://github.com/luxfi/LPs/blob/main/LPs/lp-0038-explorer-indexer-architecture.md)

## License

MIT - see [LICENSE](LICENSE)
