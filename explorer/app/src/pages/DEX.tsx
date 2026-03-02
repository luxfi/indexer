import { colors } from '../theme'

export function DEX() {
  return (
    <div>
      <h1 style={{ fontSize: 20, fontWeight: 600, marginBottom: 20 }}>DEX</h1>

      <div
        style={{
          display: 'grid',
          gridTemplateColumns: '1fr 350px',
          gap: 16,
        }}
      >
        {/* Orderbook */}
        <div
          style={{
            background: colors.card,
            borderRadius: 8,
            padding: 20,
            border: `1px solid ${colors.border}`,
          }}
        >
          <h2 style={{ fontSize: 16, fontWeight: 600, marginBottom: 16 }}>Orderbook</h2>

          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 16 }}>
            <div>
              <div style={{ fontSize: 12, color: colors.textMuted, marginBottom: 8, fontWeight: 600 }}>
                BIDS
              </div>
              <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 13 }}>
                <thead>
                  <tr style={{ color: colors.textMuted }}>
                    <th style={{ textAlign: 'left', padding: '4px 0', fontWeight: 500 }}>Price</th>
                    <th style={{ textAlign: 'right', padding: '4px 0', fontWeight: 500 }}>Amount</th>
                  </tr>
                </thead>
                <tbody>
                  {Array.from({ length: 8 }).map((_, i) => (
                    <tr key={`bid-${i}`} style={{ borderBottom: `1px solid ${colors.border}` }}>
                      <td style={{ padding: '6px 0', fontFamily: colors.mono, color: colors.success }}>
                        --
                      </td>
                      <td style={{ padding: '6px 0', fontFamily: colors.mono, textAlign: 'right', color: colors.textMuted }}>
                        --
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>

            <div>
              <div style={{ fontSize: 12, color: colors.textMuted, marginBottom: 8, fontWeight: 600 }}>
                ASKS
              </div>
              <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 13 }}>
                <thead>
                  <tr style={{ color: colors.textMuted }}>
                    <th style={{ textAlign: 'left', padding: '4px 0', fontWeight: 500 }}>Price</th>
                    <th style={{ textAlign: 'right', padding: '4px 0', fontWeight: 500 }}>Amount</th>
                  </tr>
                </thead>
                <tbody>
                  {Array.from({ length: 8 }).map((_, i) => (
                    <tr key={`ask-${i}`} style={{ borderBottom: `1px solid ${colors.border}` }}>
                      <td style={{ padding: '6px 0', fontFamily: colors.mono, color: colors.error }}>
                        --
                      </td>
                      <td style={{ padding: '6px 0', fontFamily: colors.mono, textAlign: 'right', color: colors.textMuted }}>
                        --
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>
        </div>

        {/* Recent Trades */}
        <div
          style={{
            background: colors.card,
            borderRadius: 8,
            padding: 20,
            border: `1px solid ${colors.border}`,
          }}
        >
          <h2 style={{ fontSize: 16, fontWeight: 600, marginBottom: 16 }}>Recent Trades</h2>
          <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 13 }}>
            <thead>
              <tr style={{ color: colors.textMuted }}>
                <th style={{ textAlign: 'left', padding: '4px 0', fontWeight: 500 }}>Price</th>
                <th style={{ textAlign: 'right', padding: '4px 0', fontWeight: 500 }}>Amount</th>
                <th style={{ textAlign: 'right', padding: '4px 0', fontWeight: 500 }}>Time</th>
              </tr>
            </thead>
            <tbody>
              {Array.from({ length: 10 }).map((_, i) => (
                <tr key={`trade-${i}`} style={{ borderBottom: `1px solid ${colors.border}` }}>
                  <td style={{ padding: '6px 0', fontFamily: colors.mono, color: colors.textMuted }}>--</td>
                  <td style={{ padding: '6px 0', fontFamily: colors.mono, textAlign: 'right', color: colors.textMuted }}>--</td>
                  <td style={{ padding: '6px 0', fontFamily: colors.mono, textAlign: 'right', color: colors.textMuted }}>--</td>
                </tr>
              ))}
            </tbody>
          </table>
          <p style={{ color: colors.textMuted, fontSize: 12, marginTop: 16 }}>
            Trade data will appear when the DEX engine is connected.
          </p>
        </div>
      </div>

      {/* Market pairs */}
      <div
        style={{
          marginTop: 16,
          background: colors.card,
          borderRadius: 8,
          padding: 20,
          border: `1px solid ${colors.border}`,
        }}
      >
        <h2 style={{ fontSize: 16, fontWeight: 600, marginBottom: 16 }}>Markets</h2>
        <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 14 }}>
          <thead>
            <tr style={{ borderBottom: `1px solid ${colors.border}`, color: colors.textMuted }}>
              <th style={{ textAlign: 'left', padding: '8px 0', fontWeight: 500 }}>Pair</th>
              <th style={{ textAlign: 'right', padding: '8px 0', fontWeight: 500 }}>Price</th>
              <th style={{ textAlign: 'right', padding: '8px 0', fontWeight: 500 }}>24h Volume</th>
              <th style={{ textAlign: 'right', padding: '8px 0', fontWeight: 500 }}>24h Change</th>
            </tr>
          </thead>
          <tbody>
            <tr>
              <td colSpan={4} style={{ padding: 20, textAlign: 'center', color: colors.textMuted }}>
                No active markets.
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>
  )
}
