import { useInternalTxs } from '../api/hooks'
import { Hash } from '../components/Hash'
import { colors } from '../theme'

function formatValue(wei: string): string {
  const n = Number(wei) / 1e18
  if (n === 0) return '0'
  if (n < 0.0001) return '<0.0001'
  return n.toFixed(4)
}

export function InternalTxs() {
  const { data, isLoading } = useInternalTxs()
  const coin = import.meta.env.VITE_COIN || 'ETH'

  return (
    <div>
      <h1 style={{ fontSize: 20, fontWeight: 600, marginBottom: 20 }}>Internal Transactions</h1>
      <div
        style={{
          background: colors.card,
          borderRadius: 8,
          padding: 20,
          border: `1px solid ${colors.border}`,
        }}
      >
        {isLoading && <p style={{ color: colors.textMuted }}>Loading...</p>}
        {data?.items.length === 0 && (
          <p style={{ color: colors.textMuted }}>No internal transactions found.</p>
        )}
        {data?.items.map((itx, i) => (
          <div
            key={`${itx.transaction_hash}-${itx.index}-${i}`}
            style={{
              display: 'flex',
              justifyContent: 'space-between',
              alignItems: 'center',
              padding: '12px 0',
              borderBottom: `1px solid ${colors.border}`,
              gap: 12,
              flexWrap: 'wrap',
            }}
          >
            <div style={{ display: 'flex', flexDirection: 'column', gap: 4, minWidth: 0 }}>
              <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                <Hash hash={itx.transaction_hash} link={`/tx/${itx.transaction_hash}`} />
                <span
                  style={{
                    fontSize: 11,
                    color: itx.success ? colors.success : colors.error,
                    fontWeight: 600,
                  }}
                >
                  {itx.success ? 'OK' : 'FAIL'}
                </span>
                <span
                  style={{
                    fontSize: 11,
                    color: colors.textMuted,
                    fontFamily: colors.mono,
                    background: colors.bg,
                    padding: '2px 6px',
                    borderRadius: 4,
                  }}
                >
                  {itx.call_type || itx.type}
                </span>
              </div>
              <div style={{ fontSize: 13, color: colors.textMuted }}>
                From <Hash hash={itx.from.hash} link={`/address/${itx.from.hash}`} />
                {' '}To <Hash hash={itx.to.hash} link={`/address/${itx.to.hash}`} />
              </div>
              {itx.error && (
                <div style={{ fontSize: 12, color: colors.error }}>{itx.error}</div>
              )}
            </div>
            <div style={{ textAlign: 'right', display: 'flex', flexDirection: 'column', gap: 4, flexShrink: 0 }}>
              <div style={{ fontSize: 13, fontFamily: colors.mono }}>
                {formatValue(itx.value)} {coin}
              </div>
              <div style={{ fontSize: 12, color: colors.textMuted, fontFamily: colors.mono }}>
                Block #{itx.block_number.toLocaleString()}
              </div>
            </div>
          </div>
        ))}
      </div>
    </div>
  )
}
