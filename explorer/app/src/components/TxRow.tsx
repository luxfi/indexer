import type { Tx } from '../api/types'
import { Hash } from './Hash'
import { TimeAgo } from './TimeAgo'
import { colors } from '../theme'

function formatValue(wei: string): string {
  const n = Number(wei) / 1e18
  if (n === 0) return '0'
  if (n < 0.0001) return '<0.0001'
  return n.toFixed(4)
}

export function TxRow({ tx }: { tx: Tx }) {
  const coin = import.meta.env.VITE_COIN || 'ETH'
  const statusColor = tx.status === 'ok' ? colors.success : colors.error

  return (
    <div
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
          <Hash hash={tx.hash} link={`/tx/${tx.hash}`} />
          <span style={{ fontSize: 11, color: statusColor, fontWeight: 600 }}>
            {tx.status === 'ok' ? 'OK' : 'FAIL'}
          </span>
        </div>
        <div style={{ fontSize: 13, color: colors.textMuted }}>
          From <Hash hash={tx.from.hash} link={`/address/${tx.from.hash}`} />
          {tx.to ? (
            <>
              {' '}To <Hash hash={tx.to.hash} link={`/address/${tx.to.hash}`} />
            </>
          ) : tx.created_contract ? (
            <>
              {' '}Created <Hash hash={tx.created_contract.hash} link={`/address/${tx.created_contract.hash}`} />
            </>
          ) : null}
        </div>
      </div>
      <div style={{ textAlign: 'right', display: 'flex', flexDirection: 'column', gap: 4, flexShrink: 0 }}>
        <div style={{ fontSize: 13, fontFamily: colors.mono }}>
          {formatValue(tx.value)} {coin}
        </div>
        <TimeAgo timestamp={tx.timestamp} />
      </div>
    </div>
  )
}
