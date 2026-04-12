import { useState } from 'react'
import { useParams } from 'react-router-dom'
import { useAddress, useAddressTransactions, useContract } from '../api/hooks'
import { Hash } from '../components/Hash'
import { TxRow } from '../components/TxRow'
import { colors } from '../theme'

function Row({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div style={{ display: 'flex', gap: 16, padding: '10px 0', borderBottom: `1px solid ${colors.border}` }}>
      <div style={{ width: 160, flexShrink: 0, color: colors.textMuted, fontSize: 14 }}>{label}</div>
      <div style={{ fontSize: 14, minWidth: 0, overflow: 'hidden' }}>{children}</div>
    </div>
  )
}

type Tab = 'transactions' | 'contract'

export function Address() {
  const { hash } = useParams<{ hash: string }>()
  const { data: addr, isLoading } = useAddress(hash!)
  const { data: txs } = useAddressTransactions(hash!)
  const { data: contract } = useContract(hash!)
  const coin = import.meta.env.VITE_COIN || 'LQDTY'
  const [tab, setTab] = useState<Tab>('transactions')

  if (isLoading) return <p style={{ color: colors.textMuted }}>Loading...</p>
  if (!addr) return <p style={{ color: colors.error }}>Address not found</p>

  const balance = (Number(addr.balance) / 1e18).toFixed(6)
  const hasContract = addr.is_contract && contract

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
        {addr.is_contract ? 'Contract' : 'Address'}
      </h1>

      <div
        style={{
          background: colors.card,
          borderRadius: 8,
          padding: 20,
          border: `1px solid ${colors.border}`,
        }}
      >
        <Row label="Address"><Hash hash={addr.hash} full /></Row>
        {addr.name && <Row label="Name">{addr.name}</Row>}
        <Row label="Balance">
          <span style={{ fontFamily: colors.mono }}>{balance} {coin}</span>
        </Row>
        <Row label="Transactions">{addr.tx_count.toLocaleString()}</Row>
        {addr.token_transfers_count > 0 && (
          <Row label="Token Transfers">{addr.token_transfers_count.toLocaleString()}</Row>
        )}
        <Row label="Type">{addr.is_contract ? 'Contract' : 'EOA'}</Row>
      </div>

      <div style={{ display: 'flex', gap: 8 }}>
        <button style={tabStyle(tab === 'transactions')} onClick={() => setTab('transactions')}>
          Transactions
        </button>
        {addr.is_contract && (
          <button style={tabStyle(tab === 'contract')} onClick={() => setTab('contract')}>
            Contract
          </button>
        )}
      </div>

      {tab === 'transactions' && txs && txs.items.length > 0 && (
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
      )}

      {tab === 'transactions' && txs && txs.items.length === 0 && (
        <div
          style={{
            background: colors.card,
            borderRadius: 8,
            padding: 20,
            border: `1px solid ${colors.border}`,
          }}
        >
          <p style={{ color: colors.textMuted }}>No transactions found.</p>
        </div>
      )}

      {tab === 'contract' && hasContract && (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
          <div
            style={{
              background: colors.card,
              borderRadius: 8,
              padding: 20,
              border: `1px solid ${colors.border}`,
            }}
          >
            <Row label="Contract Name">{contract.name || 'Unknown'}</Row>
            <Row label="Compiler">{contract.compiler_version}</Row>
            <Row label="Verified">
              <span style={{ color: contract.is_verified ? colors.success : colors.textMuted, fontWeight: 600 }}>
                {contract.is_verified ? 'Yes' : 'No'}
              </span>
            </Row>
            {contract.optimization_enabled && (
              <Row label="Optimization">
                Enabled ({contract.optimization_runs} runs)
              </Row>
            )}
          </div>

          {contract.source_code && (
            <div>
              <h3 style={{ fontSize: 14, fontWeight: 600, marginBottom: 8 }}>Source Code</h3>
              <pre
                style={{
                  background: colors.card,
                  borderRadius: 8,
                  padding: 16,
                  border: `1px solid ${colors.border}`,
                  fontSize: 12,
                  fontFamily: colors.mono,
                  lineHeight: 1.6,
                  overflow: 'auto',
                  maxHeight: 500,
                  whiteSpace: 'pre-wrap',
                  wordBreak: 'break-word',
                }}
              >
                {contract.source_code}
              </pre>
            </div>
          )}

          {contract.abi && contract.abi.length > 0 && (
            <div>
              <h3 style={{ fontSize: 14, fontWeight: 600, marginBottom: 8 }}>ABI</h3>
              <pre
                style={{
                  background: colors.card,
                  borderRadius: 8,
                  padding: 16,
                  border: `1px solid ${colors.border}`,
                  fontSize: 12,
                  fontFamily: colors.mono,
                  lineHeight: 1.6,
                  overflow: 'auto',
                  maxHeight: 400,
                  whiteSpace: 'pre-wrap',
                  wordBreak: 'break-word',
                }}
              >
                {JSON.stringify(contract.abi, null, 2)}
              </pre>
            </div>
          )}
        </div>
      )}

      {tab === 'contract' && !hasContract && (
        <div
          style={{
            background: colors.card,
            borderRadius: 8,
            padding: 20,
            border: `1px solid ${colors.border}`,
          }}
        >
          <p style={{ color: colors.textMuted }}>Contract source code not verified.</p>
        </div>
      )}
    </div>
  )
}
