import { useParams } from 'react-router-dom'
import { useBlock, useBlockTransactions } from '../api/hooks'
import { Hash } from '../components/Hash'
import { TxRow } from '../components/TxRow'
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

export function BlockDetail() {
  const { id } = useParams<{ id: string }>()
  const { data: block, isLoading } = useBlock(id!)
  const { data: txs } = useBlockTransactions(id!)

  if (isLoading) return <p style={{ color: colors.textMuted }}>Loading...</p>
  if (!block) return <p style={{ color: colors.error }}>Block not found</p>

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 24 }}>
      <h1 style={{ fontSize: 20, fontWeight: 600 }}>Block #{block.height.toLocaleString()}</h1>

      <div
        style={{
          background: colors.card,
          borderRadius: 8,
          padding: 20,
          border: `1px solid ${colors.border}`,
        }}
      >
        <Row label="Height">{block.height.toLocaleString()}</Row>
        <Row label="Hash"><Hash hash={block.hash} full /></Row>
        <Row label="Parent"><Hash hash={block.parent_hash} link={`/blocks/${block.height - 1}`} /></Row>
        <Row label="Timestamp"><TimeAgo timestamp={block.timestamp} /> ({new Date(block.timestamp).toUTCString()})</Row>
        <Row label="Miner"><Hash hash={block.miner.hash} link={`/address/${block.miner.hash}`} /></Row>
        <Row label="Transactions">{block.tx_count}</Row>
        <Row label="Gas Used">
          <span style={{ fontFamily: colors.mono }}>
            {Number(block.gas_used).toLocaleString()} / {Number(block.gas_limit).toLocaleString()}
          </span>
        </Row>
        <Row label="Size">
          <span style={{ fontFamily: colors.mono }}>{block.size.toLocaleString()} bytes</span>
        </Row>
      </div>

      {txs && txs.items.length > 0 && (
        <div>
          <h2 style={{ fontSize: 16, fontWeight: 600, marginBottom: 12 }}>Transactions</h2>
          <div
            style={{
              background: colors.card,
              borderRadius: 8,
              padding: 20,
              border: `1px solid ${colors.border}`,
            }}
          >
            {txs.items.map((tx) => <TxRow key={tx.hash} tx={tx} />)}
          </div>
        </div>
      )}
    </div>
  )
}
