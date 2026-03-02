import { useStats } from '../api/hooks'
import { colors } from '../theme'

export function StatsBar() {
  const { data, isLoading } = useStats()

  if (isLoading || !data) return null

  const items = [
    { label: 'Blocks', value: Number(data.total_blocks).toLocaleString() },
    { label: 'Transactions', value: Number(data.total_transactions).toLocaleString() },
    { label: 'Addresses', value: Number(data.total_addresses).toLocaleString() },
    { label: 'Avg Block Time', value: `${data.average_block_time.toFixed(1)}s` },
  ]

  return (
    <div
      style={{
        display: 'flex',
        gap: 1,
        background: colors.border,
        borderRadius: 8,
        overflow: 'hidden',
      }}
    >
      {items.map((item) => (
        <div
          key={item.label}
          style={{
            flex: 1,
            background: colors.card,
            padding: '16px 20px',
            textAlign: 'center',
          }}
        >
          <div style={{ fontSize: 12, color: colors.textMuted, marginBottom: 4 }}>
            {item.label}
          </div>
          <div style={{ fontSize: 18, fontWeight: 600, fontFamily: colors.mono }}>
            {item.value}
          </div>
        </div>
      ))}
    </div>
  )
}
