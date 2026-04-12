import { useTokens } from '../api/hooks'
import { Hash } from '../components/Hash'
import { colors } from '../theme'

export function Tokens() {
  const { data, isLoading } = useTokens()

  return (
    <div>
      <h1 style={{ fontSize: 20, fontWeight: 600, marginBottom: 20 }}>Tokens</h1>
      <div
        style={{
          background: colors.card,
          borderRadius: 8,
          padding: 20,
          border: `1px solid ${colors.border}`,
        }}
      >
        {isLoading && <p style={{ color: colors.textMuted }}>Loading...</p>}
        <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 14 }}>
          <thead>
            <tr style={{ borderBottom: `1px solid ${colors.border}`, color: colors.textMuted }}>
              <th style={{ textAlign: 'left', padding: '8px 0', fontWeight: 500 }}>Token</th>
              <th style={{ textAlign: 'left', padding: '8px 0', fontWeight: 500 }}>Address</th>
              <th style={{ textAlign: 'right', padding: '8px 0', fontWeight: 500 }}>Type</th>
            </tr>
          </thead>
          <tbody>
            {data?.items.map((t) => (
              <tr key={t.address} style={{ borderBottom: `1px solid ${colors.border}` }}>
                <td style={{ padding: '10px 0' }}>
                  <span style={{ fontWeight: 600 }}>{t.name}</span>
                  <span style={{ color: colors.textMuted, marginLeft: 8 }}>{t.symbol}</span>
                </td>
                <td style={{ padding: '10px 0' }}>
                  <Hash hash={t.address} link={`/token/${t.address}`} />
                </td>
                <td style={{ padding: '10px 0', textAlign: 'right', color: colors.textMuted }}>
                  {t.type}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  )
}
