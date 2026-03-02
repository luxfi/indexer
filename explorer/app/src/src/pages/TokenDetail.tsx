import { useState } from 'react'
import { useParams } from 'react-router-dom'
import { useTokenDetail } from '../api/hooks'
import { Hash } from '../components/Hash'
import { colors } from '../theme'

function Row({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div style={{ display: 'flex', gap: 16, padding: '10px 0', borderBottom: `1px solid ${colors.border}` }}>
      <div style={{ width: 160, flexShrink: 0, color: colors.textMuted, fontSize: 14 }}>{label}</div>
      <div style={{ fontSize: 14, minWidth: 0, overflow: 'hidden' }}>{children}</div>
    </div>
  )
}

type Tab = 'transfers' | 'holders'

export function TokenDetail() {
  const { address } = useParams<{ address: string }>()
  const { data: token, isLoading } = useTokenDetail(address!)
  const [tab, setTab] = useState<Tab>('transfers')

  if (isLoading) return <p style={{ color: colors.textMuted }}>Loading...</p>
  if (!token) return <p style={{ color: colors.error }}>Token not found</p>

  const decimals = Number(token.decimals)
  const supply = decimals > 0
    ? (Number(token.total_supply) / Math.pow(10, decimals)).toLocaleString()
    : Number(token.total_supply).toLocaleString()

  const tabStyle = (active: boolean): React.CSSProperties => ({
    padding: '8px 16px',
    background: active ? colors.accent : 'transparent',
    color: active ? '#fff' : colors.textMuted,
    border: 'none',
    borderRadius: 6,
    cursor: 'pointer',
    fontSize: 14,
    fontWeight: active ? 600 : 400,
  })

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 24 }}>
      <h1 style={{ fontSize: 20, fontWeight: 600 }}>
        Token: {token.name} ({token.symbol})
      </h1>

      <div
        style={{
          background: colors.card,
          borderRadius: 8,
          padding: 20,
          border: `1px solid ${colors.border}`,
        }}
      >
        <Row label="Name">{token.name}</Row>
        <Row label="Symbol">{token.symbol}</Row>
        <Row label="Address"><Hash hash={token.address} full /></Row>
        <Row label="Decimals">
          <span style={{ fontFamily: colors.mono }}>{token.decimals}</span>
        </Row>
        <Row label="Total Supply">
          <span style={{ fontFamily: colors.mono }}>{supply}</span>
        </Row>
        {token.holders && (
          <Row label="Holders">
            <span style={{ fontFamily: colors.mono }}>{Number(token.holders).toLocaleString()}</span>
          </Row>
        )}
        <Row label="Type">{token.type}</Row>
      </div>

      <div style={{ display: 'flex', gap: 8 }}>
        <button style={tabStyle(tab === 'transfers')} onClick={() => setTab('transfers')}>
          Transfers
        </button>
        <button style={tabStyle(tab === 'holders')} onClick={() => setTab('holders')}>
          Holders
        </button>
      </div>

      <div
        style={{
          background: colors.card,
          borderRadius: 8,
          padding: 20,
          border: `1px solid ${colors.border}`,
        }}
      >
        {tab === 'transfers' && (
          <p style={{ color: colors.textMuted }}>Token transfer history will appear here once indexed.</p>
        )}
        {tab === 'holders' && (
          <p style={{ color: colors.textMuted }}>Token holder list will appear here once indexed.</p>
        )}
      </div>
    </div>
  )
}
