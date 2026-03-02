import { useParams } from 'react-router-dom'
import { useTransaction } from '../api/hooks'
import { Hash } from '../components/Hash'
import { TimeAgo } from '../components/TimeAgo'
import { colors } from '../theme'

function Row({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div style={{ display: 'flex', gap: 16, padding: '10px 0', borderBottom: `1px solid ${colors.border}` }}>
      <div style={{ width: 160, flexShrink: 0, color: colors.textMuted, fontSize: 14 }}>{label}</div>
      <div style={{ fontSize: 14, minWidth: 0, overflow: 'hidden' }}>{children}</div>
    </div>
  )
}

export function TxDetail() {
  const { hash } = useParams<{ hash: string }>()
  const { data: tx, isLoading } = useTransaction(hash!)
  const coin = import.meta.env.VITE_COIN || 'LQDTY'

  if (isLoading) return <p style={{ color: colors.textMuted }}>Loading...</p>
  if (!tx) return <p style={{ color: colors.error }}>Transaction not found</p>

  const statusColor = tx.status === 'ok' ? colors.success : colors.error
  const value = (Number(tx.value) / 1e18).toFixed(6)
  const gasPrice = (Number(tx.gas_price) / 1e9).toFixed(2)

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 24 }}>
      <h1 style={{ fontSize: 20, fontWeight: 600 }}>Transaction Details</h1>

      <div
        style={{
          background: colors.card,
          borderRadius: 8,
          padding: 20,
          border: `1px solid ${colors.border}`,
        }}
      >
        <Row label="Hash"><Hash hash={tx.hash} full /></Row>
        <Row label="Status">
          <span style={{ color: statusColor, fontWeight: 600 }}>
            {tx.status === 'ok' ? 'Success' : 'Failed'}
          </span>
        </Row>
        <Row label="Block">
          <a
            href={`/blocks/${tx.block_number}`}
            style={{ color: colors.accent, textDecoration: 'none' }}
          >
            {tx.block_number.toLocaleString()}
          </a>
        </Row>
        <Row label="Timestamp"><TimeAgo timestamp={tx.timestamp} /> ({new Date(tx.timestamp).toUTCString()})</Row>
        <Row label="From"><Hash hash={tx.from.hash} link={`/address/${tx.from.hash}`} /></Row>
        {tx.to && (
          <Row label="To"><Hash hash={tx.to.hash} link={`/address/${tx.to.hash}`} /></Row>
        )}
        {tx.created_contract && (
          <Row label="Created">
            <Hash hash={tx.created_contract.hash} link={`/address/${tx.created_contract.hash}`} />
          </Row>
        )}
        <Row label="Value">
          <span style={{ fontFamily: colors.mono }}>{value} {coin}</span>
        </Row>
        <Row label="Gas Price">
          <span style={{ fontFamily: colors.mono }}>{gasPrice} Gwei</span>
        </Row>
        <Row label="Gas Used">
          <span style={{ fontFamily: colors.mono }}>
            {Number(tx.gas_used).toLocaleString()} / {Number(tx.gas_limit).toLocaleString()}
          </span>
        </Row>
        <Row label="Nonce">
          <span style={{ fontFamily: colors.mono }}>{tx.nonce}</span>
        </Row>
        {tx.method && (
          <Row label="Method">
            <span style={{ fontFamily: colors.mono, color: colors.accent }}>{tx.method}</span>
          </Row>
        )}
      </div>
    </div>
  )
}
