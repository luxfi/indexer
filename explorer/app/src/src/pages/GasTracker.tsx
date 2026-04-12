import { useGasPrice } from '../api/hooks'
import { colors } from '../theme'

export function GasTracker() {
  const { data: gas, isLoading } = useGasPrice()

  const tiers = gas
    ? [
        { label: 'Slow', gwei: gas.slow, color: colors.success },
        { label: 'Average', gwei: gas.average, color: colors.warning },
        { label: 'Fast', gwei: gas.fast, color: colors.error },
      ]
    : []

  return (
    <div>
      <h1 style={{ fontSize: 20, fontWeight: 600, marginBottom: 20 }}>Gas Tracker</h1>

      {isLoading && <p style={{ color: colors.textMuted }}>Loading...</p>}

      {!isLoading && !gas && (
        <div
          style={{
            background: colors.card,
            borderRadius: 8,
            padding: 40,
            border: `1px solid ${colors.border}`,
            textAlign: 'center',
          }}
        >
          <p style={{ color: colors.textMuted }}>Gas price data not available.</p>
        </div>
      )}

      {gas && (
        <div
          style={{
            display: 'grid',
            gridTemplateColumns: 'repeat(3, 1fr)',
            gap: 16,
          }}
        >
          {tiers.map((tier) => (
            <div
              key={tier.label}
              style={{
                background: colors.card,
                borderRadius: 8,
                padding: 24,
                border: `1px solid ${colors.border}`,
                textAlign: 'center',
              }}
            >
              <div style={{ fontSize: 14, color: colors.textMuted, marginBottom: 8 }}>{tier.label}</div>
              <div style={{ fontSize: 32, fontWeight: 700, fontFamily: colors.mono, color: tier.color }}>
                {tier.gwei}
              </div>
              <div style={{ fontSize: 13, color: colors.textMuted, marginTop: 4 }}>Gwei</div>
            </div>
          ))}
        </div>
      )}

      <div
        style={{
          marginTop: 24,
          background: colors.card,
          borderRadius: 8,
          padding: 20,
          border: `1px solid ${colors.border}`,
        }}
      >
        <h2 style={{ fontSize: 16, fontWeight: 600, marginBottom: 12 }}>Gas Price Guide</h2>
        <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 14 }}>
          <thead>
            <tr style={{ borderBottom: `1px solid ${colors.border}`, color: colors.textMuted }}>
              <th style={{ textAlign: 'left', padding: '8px 0', fontWeight: 500 }}>Transaction Type</th>
              <th style={{ textAlign: 'right', padding: '8px 0', fontWeight: 500 }}>Gas Units</th>
              <th style={{ textAlign: 'right', padding: '8px 0', fontWeight: 500 }}>Est. Cost (Avg)</th>
            </tr>
          </thead>
          <tbody>
            {[
              { name: 'Transfer', gasUnits: 21000 },
              { name: 'ERC-20 Transfer', gasUnits: 65000 },
              { name: 'ERC-20 Approve', gasUnits: 45000 },
              { name: 'DEX Swap', gasUnits: 150000 },
              { name: 'Contract Deploy', gasUnits: 500000 },
            ].map((row) => {
              const costGwei = gas ? row.gasUnits * gas.average : 0
              const costEth = costGwei / 1e9
              return (
                <tr key={row.name} style={{ borderBottom: `1px solid ${colors.border}` }}>
                  <td style={{ padding: '10px 0' }}>{row.name}</td>
                  <td style={{ padding: '10px 0', textAlign: 'right', fontFamily: colors.mono }}>
                    {row.gasUnits.toLocaleString()}
                  </td>
                  <td style={{ padding: '10px 0', textAlign: 'right', fontFamily: colors.mono, color: colors.textMuted }}>
                    {costEth.toFixed(6)} LQDTY
                  </td>
                </tr>
              )
            })}
          </tbody>
        </table>
      </div>
    </div>
  )
}
