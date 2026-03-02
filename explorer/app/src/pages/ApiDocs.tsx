import { colors } from '../theme'

const endpoints = [
  { method: 'GET', path: '/stats', description: 'Network statistics (blocks, txs, addresses, gas prices)' },
  { method: 'GET', path: '/main-page/blocks', description: 'Latest blocks for the home page' },
  { method: 'GET', path: '/main-page/transactions', description: 'Latest transactions for the home page' },
  { method: 'GET', path: '/blocks', description: 'Paginated block list' },
  { method: 'GET', path: '/blocks/:height_or_hash', description: 'Block detail by height or hash' },
  { method: 'GET', path: '/blocks/:id/transactions', description: 'Transactions in a specific block' },
  { method: 'GET', path: '/transactions', description: 'Paginated transaction list' },
  { method: 'GET', path: '/transactions/:hash', description: 'Transaction detail by hash' },
  { method: 'GET', path: '/addresses/:hash', description: 'Address detail (balance, tx count, type)' },
  { method: 'GET', path: '/addresses/:hash/transactions', description: 'Transactions for an address' },
  { method: 'GET', path: '/tokens', description: 'Paginated token list' },
  { method: 'GET', path: '/tokens/:address', description: 'Token detail (name, symbol, supply, holders)' },
  { method: 'GET', path: '/token-transfers', description: 'Paginated token transfer list' },
  { method: 'GET', path: '/internal-transactions', description: 'Paginated internal transaction list' },
  { method: 'GET', path: '/smart-contracts/:address', description: 'Verified contract source, ABI, compiler info' },
  { method: 'GET', path: '/search', description: 'Search by address, tx hash, block, or token name' },
  { method: 'GET', path: '/stats/charts/transactions', description: 'Daily transaction count chart data' },
  { method: 'GET', path: '/stats/charts/market', description: 'Market price/cap chart data' },
]

export function ApiDocs() {
  const base = import.meta.env.VITE_API_BASE || '/v1/explorer'

  return (
    <div>
      <h1 style={{ fontSize: 20, fontWeight: 600, marginBottom: 8 }}>API Documentation</h1>
      <p style={{ color: colors.textMuted, fontSize: 14, marginBottom: 24 }}>
        Base URL: <code style={{ fontFamily: colors.mono, color: colors.accent }}>{base}</code>
      </p>

      <div
        style={{
          background: colors.card,
          borderRadius: 8,
          padding: 20,
          border: `1px solid ${colors.border}`,
        }}
      >
        <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 14 }}>
          <thead>
            <tr style={{ borderBottom: `1px solid ${colors.border}`, color: colors.textMuted }}>
              <th style={{ textAlign: 'left', padding: '8px 0', fontWeight: 500, width: 80 }}>Method</th>
              <th style={{ textAlign: 'left', padding: '8px 0', fontWeight: 500 }}>Endpoint</th>
              <th style={{ textAlign: 'left', padding: '8px 0', fontWeight: 500 }}>Description</th>
            </tr>
          </thead>
          <tbody>
            {endpoints.map((ep) => (
              <tr key={ep.path} style={{ borderBottom: `1px solid ${colors.border}` }}>
                <td style={{ padding: '10px 0' }}>
                  <span
                    style={{
                      fontFamily: colors.mono,
                      fontSize: 12,
                      fontWeight: 600,
                      color: colors.success,
                      background: `${colors.success}15`,
                      padding: '2px 8px',
                      borderRadius: 4,
                    }}
                  >
                    {ep.method}
                  </span>
                </td>
                <td style={{ padding: '10px 0', fontFamily: colors.mono, color: colors.accent }}>
                  {ep.path}
                </td>
                <td style={{ padding: '10px 0', color: colors.textMuted }}>
                  {ep.description}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      <div
        style={{
          marginTop: 24,
          background: colors.card,
          borderRadius: 8,
          padding: 20,
          border: `1px solid ${colors.border}`,
        }}
      >
        <h2 style={{ fontSize: 16, fontWeight: 600, marginBottom: 12 }}>Pagination</h2>
        <p style={{ color: colors.textMuted, fontSize: 14, lineHeight: 1.6 }}>
          List endpoints return <code style={{ fontFamily: colors.mono, color: colors.accent }}>{'{ items: T[], next_page_params: object | null }'}</code>.
          Pass <code style={{ fontFamily: colors.mono, color: colors.accent }}>next_page_params</code> as query parameters to fetch the next page.
          Common params: <code style={{ fontFamily: colors.mono, color: colors.accent }}>limit</code> (default 25).
        </p>
      </div>
    </div>
  )
}
