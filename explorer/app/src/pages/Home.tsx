import { useEffect } from 'react'
import { Link } from 'react-router-dom'
import { useQueryClient } from '@tanstack/react-query'
import { useMainPageBlocks, useMainPageTransactions } from '../api/hooks'
import { realtime } from '../api/realtime'
import { StatsBar } from '../components/StatsBar'
import { BlockCard } from '../components/BlockCard'
import { TxRow } from '../components/TxRow'
import { colors } from '../theme'
import type { Block, Tx } from '../api/types'

function SectionHeader({ title, linkTo }: { title: string; linkTo: string }) {
  return (
    <div
      style={{
        display: 'flex',
        justifyContent: 'space-between',
        alignItems: 'center',
        marginBottom: 8,
      }}
    >
      <h2 style={{ fontSize: 16, fontWeight: 600 }}>{title}</h2>
      <Link to={linkTo} style={{ color: colors.accent, fontSize: 13, textDecoration: 'none' }}>
        View All
      </Link>
    </div>
  )
}

export function Home() {
  const blocks = useMainPageBlocks()
  const txs = useMainPageTransactions()
  const qc = useQueryClient()

  // Base SSE realtime: invalidate queries on new block/tx events
  useEffect(() => {
    realtime.connect()
    const unsub1 = realtime.on('new_block', (data) => {
      // Prepend new block to cache
      qc.setQueryData<Block[]>(['main-page-blocks'], (prev) =>
        prev ? [data as Block, ...prev].slice(0, 8) : [data as Block]
      )
      qc.invalidateQueries({ queryKey: ['stats'] })
    })
    const unsub2 = realtime.on('new_tx', (data) => {
      qc.setQueryData<Tx[]>(['main-page-txs'], (prev) =>
        prev ? [data as Tx, ...prev].slice(0, 8) : [data as Tx]
      )
    })
    return () => { unsub1(); unsub2() }
  }, [qc])

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 32 }}>
      <StatsBar />

      <div
        style={{
          display: 'grid',
          gridTemplateColumns: 'repeat(auto-fit, minmax(400px, 1fr))',
          gap: 24,
        }}
      >
        <section
          style={{ background: colors.card, borderRadius: 8, padding: 20, border: `1px solid ${colors.border}` }}
        >
          <SectionHeader title="Latest Blocks" linkTo="/blocks" />
          {blocks.isLoading && <p style={{ color: colors.textMuted }}>Loading...</p>}
          {blocks.data?.slice(0, 8).map((b) => <BlockCard key={b.hash} block={b} />)}
        </section>

        <section
          style={{ background: colors.card, borderRadius: 8, padding: 20, border: `1px solid ${colors.border}` }}
        >
          <SectionHeader title="Latest Transactions" linkTo="/txs" />
          {txs.isLoading && <p style={{ color: colors.textMuted }}>Loading...</p>}
          {txs.data?.slice(0, 8).map((tx) => <TxRow key={tx.hash} tx={tx} />)}
        </section>
      </div>
    </div>
  )
}
