import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createBrowserRouter, RouterProvider, Navigate } from 'react-router-dom'
import { Layout } from './components/Layout'
import { Home } from './pages/Home'
import { Blocks } from './pages/Blocks'
import { BlockDetail } from './pages/BlockDetail'
import { Transactions } from './pages/Transactions'
import { TxDetail } from './pages/TxDetail'
import { Address } from './pages/Address'
import { Tokens } from './pages/Tokens'
import { TokenDetail } from './pages/TokenDetail'
import { Search } from './pages/Search'
import { InternalTxs } from './pages/InternalTxs'
import { TokenTransfers } from './pages/TokenTransfers'
import { GasTracker } from './pages/GasTracker'
import { StatsPage } from './pages/Stats'
import { Validators } from './pages/Validators'
import { ApiDocs } from './pages/ApiDocs'
import { DEX } from './pages/DEX'
import { Bridge } from './pages/Bridge'
import { GraphQL } from './pages/GraphQL'
import { ChainRedirect } from './components/ChainRedirect'

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 10_000,
      retry: 1,
    },
  },
})

// All explorer routes live under /:chain (evm, dex, fhe)
// / redirects to /evm
const explorerRoutes = [
  { index: true, element: <Home /> },
  { path: 'blocks', element: <Blocks /> },
  { path: 'blocks/:id', element: <BlockDetail /> },
  { path: 'txs', element: <Transactions /> },
  { path: 'tx/:hash', element: <TxDetail /> },
  { path: 'address/:hash', element: <Address /> },
  { path: 'tokens', element: <Tokens /> },
  { path: 'token/:address', element: <TokenDetail /> },
  { path: 'search', element: <Search /> },
  { path: 'internal-txs', element: <InternalTxs /> },
  { path: 'token-transfers', element: <TokenTransfers /> },
  { path: 'gas-tracker', element: <GasTracker /> },
  { path: 'stats', element: <StatsPage /> },
  { path: 'validators', element: <Validators /> },
  { path: 'api-docs', element: <ApiDocs /> },
  { path: 'dex', element: <DEX /> },
  { path: 'bridge', element: <Bridge /> },
  { path: 'graphql', element: <GraphQL /> },
]

const router = createBrowserRouter([
  // Redirect / → /evm
  { path: '/', element: <Navigate to="/evm" replace /> },
  {
    path: '/:chain',
    element: <><ChainRedirect /><Layout /></>,
    children: explorerRoutes,
  },
])

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  </StrictMode>,
)
