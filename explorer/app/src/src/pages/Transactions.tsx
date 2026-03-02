import { useState } from 'react'
import { useTransactions } from '../api/hooks'
import { TxRow } from '../components/TxRow'
import { colors } from '../theme'

export function Transactions() {
  const [pageParams, setPageParams] = useState<Record<string, string>>({})
  const { data, isLoading } = useTransactions(pageParams)

  return (
    <div>
      <h1 style={{ fontSize: 20, fontWeight: 600, marginBottom: 20 }}>Transactions</h1>
      <div
        style={{
          background: colors.card,
          borderRadius: 8,
          padding: 20,
          border: `1px solid ${colors.border}`,
        }}
      >
        {isLoading && <p style={{ color: colors.textMuted }}>Loading...</p>}
        {data?.items.map((tx) => <TxRow key={tx.hash} tx={tx} />)}
        {data?.next_page_params && (
          <button
            onClick={() => setPageParams(data.next_page_params!)}
            style={{
              marginTop: 16,
              padding: '8px 20px',
              background: colors.accent,
              color: '#fff',
              border: 'none',
              borderRadius: 6,
              cursor: 'pointer',
              fontSize: 14,
            }}
          >
            Next Page
          </button>
        )}
      </div>
    </div>
  )
}
