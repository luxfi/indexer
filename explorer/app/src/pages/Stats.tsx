import { useStats, useTxChart, useMarketChart } from '../api/hooks'
import { colors } from '../theme'
import type { ChartItem } from '../api/types'

function MiniChart({ data, label, color }: { data: ChartItem[]; label: string; color: string }) {
  if (data.length === 0) return null

  const values = data.map((d) => Number(d.value))
  const max = Math.max(...values)
  const min = Math.min(...values)
  const range = max - min || 1
  const barWidth = Math.max(2, Math.floor(600 / data.length) - 1)

  return (
    <div>
      <h3 style={{ fontSize: 14, fontWeight: 600, marginBottom: 12 }}>{label}</h3>
      <div
        style={{
          display: 'flex',
          alignItems: 'flex-end',
          gap: 1,
          height: 120,
          padding: '0 4px',
        }}
      >
        {data.map((d, i) => {
          const h = Math.max(2, ((Number(d.value) - min) / range) * 100)
          return (
            <div
              key={i}
              title={`${d.date}: ${Number(d.value).toLocaleString()}`}
              style={{
                width: barWidth,
                height: `${h}%`,
                background: color,
                borderRadius: '2px 2px 0 0',
                opacity: 0.8,
              }}
            />
          )
        })}
      </div>
      <div
        style={{
          display: 'flex',
          justifyContent: 'space-between',
          fontSize: 11,
          color: colors.textMuted,
          marginTop: 4,
        }}
      >
        <span>{data[0]?.date}</span>
        <span>{data[data.length - 1]?.date}</span>
      </div>
    </div>
  )
}

export function StatsPage() {
  const { data: stats, isLoading: statsLoading } = useStats()
  const { data: txChart } = useTxChart()
  const { data: marketChart } = useMarketChart()

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 24 }}>
      <h1 style={{ fontSize: 20, fontWeight: 600 }}>Network Statistics</h1>

      {statsLoading && <p style={{ color: colors.textMuted }}>Loading...</p>}

      {stats && (
        <div
          style={{
            display: 'grid',
            gridTemplateColumns: 'repeat(auto-fit, minmax(200px, 1fr))',
            gap: 16,
          }}
        >
          {[
            { label: 'Total Blocks', value: Number(stats.total_blocks).toLocaleString() },
            { label: 'Total Transactions', value: Number(stats.total_transactions).toLocaleString() },
            { label: 'Total Addresses', value: Number(stats.total_addresses).toLocaleString() },
            { label: 'Avg Block Time', value: `${stats.average_block_time.toFixed(1)}s` },
            { label: 'Coin Price', value: stats.coin_price ? `$${stats.coin_price}` : '--' },
            { label: 'Market Cap', value: stats.market_cap ? `$${Number(stats.market_cap).toLocaleString()}` : '--' },
          ].map((item) => (
            <div
              key={item.label}
              style={{
                background: colors.card,
                borderRadius: 8,
                padding: 20,
                border: `1px solid ${colors.border}`,
                textAlign: 'center',
              }}
            >
              <div style={{ fontSize: 12, color: colors.textMuted, marginBottom: 6 }}>{item.label}</div>
              <div style={{ fontSize: 20, fontWeight: 600, fontFamily: colors.mono }}>{item.value}</div>
            </div>
          ))}
        </div>
      )}

      <div
        style={{
          display: 'grid',
          gridTemplateColumns: 'repeat(auto-fit, minmax(400px, 1fr))',
          gap: 24,
        }}
      >
        <div
          style={{
            background: colors.card,
            borderRadius: 8,
            padding: 20,
            border: `1px solid ${colors.border}`,
          }}
        >
          {txChart?.chart_data ? (
            <MiniChart data={txChart.chart_data} label="Daily Transactions" color={colors.accent} />
          ) : (
            <p style={{ color: colors.textMuted }}>Transaction chart data not available.</p>
          )}
        </div>

        <div
          style={{
            background: colors.card,
            borderRadius: 8,
            padding: 20,
            border: `1px solid ${colors.border}`,
          }}
        >
          {marketChart?.chart_data ? (
            <MiniChart data={marketChart.chart_data} label="Market Data" color={colors.success} />
          ) : (
            <p style={{ color: colors.textMuted }}>Market chart data not available.</p>
          )}
        </div>
      </div>
    </div>
  )
}
