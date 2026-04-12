import { useTokenTransfers } from '../api/hooks'
import { Hash } from '../components/Hash'
import { TimeAgo } from '../components/TimeAgo'
import { colors } from '../theme'

function formatTokenValue(value: string, decimals: string): string {
  const d = Number(decimals)
  if (d === 0) return value
  const n = Number(value) / Math.pow(10, d)
  if (n === 0) return '0'
  if (n < 0.0001) return '<0.0001'
  return n.toLocaleString(undefined, { maximumFractionDigits: 4 })
}

export function TokenTransfers() {
  const { data, isLoading } = useTokenTransfers()

  return (
    <div>
      <h1 style={{ fontSize: 20, fontWeight: 600, marginBottom: 20 }}>Token Transfers</h1>
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
          <p style={{ color: colors.textMuted }}>No token transfers found.</p>
        )}
        {data?.items.map((t, i) => (
          <div
            key={`${t.transaction_hash}-${t.log_index}-${i}`}
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
                <Hash hash={t.transaction_hash} link={`/tx/${t.transaction_hash}`} />
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
                  {t.token.type}
                </span>
              </div>
              <div style={{ fontSize: 13, color: colors.textMuted }}>
                From <Hash hash={t.from.hash} link={`/address/${t.from.hash}`} />
                {' '}To <Hash hash={t.to.hash} link={`/address/${t.to.hash}`} />
              </div>
              <div style={{ fontSize: 12, color: colors.textMuted }}>
                Token <Hash hash={t.token.address} link={`/token/${t.token.address}`} />
              </div>
            </div>
            <div style={{ textAlign: 'right', display: 'flex', flexDirection: 'column', gap: 4, flexShrink: 0 }}>
              <div style={{ fontSize: 13, fontFamily: colors.mono }}>
                {formatTokenValue(t.total.value, t.total.decimals)}
              </div>
              <TimeAgo timestamp={t.timestamp} />
            </div>
          </div>
        ))}
      </div>
    </div>
  )
}
