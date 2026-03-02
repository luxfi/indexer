import { Link } from 'react-router-dom'
import type { Block } from '../api/types'
import { Hash } from './Hash'
import { TimeAgo } from './TimeAgo'
import { colors } from '../theme'

export function BlockCard({ block }: { block: Block }) {
  return (
    <div
      style={{
        display: 'flex',
        justifyContent: 'space-between',
        alignItems: 'center',
        padding: '14px 0',
        borderBottom: `1px solid ${colors.border}`,
      }}
    >
      <div style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
          <Link
            to={`/blocks/${block.height}`}
            style={{ color: colors.accent, fontWeight: 600, fontSize: 15, textDecoration: 'none' }}
          >
            #{block.height.toLocaleString()}
          </Link>
          <TimeAgo timestamp={block.timestamp} />
        </div>
        <div style={{ fontSize: 13, color: colors.textMuted }}>
          Miner <Hash hash={block.miner.hash} link={`/address/${block.miner.hash}`} />
        </div>
      </div>
      <div style={{ textAlign: 'right', display: 'flex', flexDirection: 'column', gap: 4 }}>
        <div style={{ fontSize: 13, fontFamily: colors.mono }}>
          {block.tx_count} txn{block.tx_count !== 1 ? 's' : ''}
        </div>
        <div style={{ fontSize: 12, color: colors.textMuted, fontFamily: colors.mono }}>
          {(Number(block.gas_used) / 1e6).toFixed(2)}M gas
        </div>
      </div>
    </div>
  )
}
