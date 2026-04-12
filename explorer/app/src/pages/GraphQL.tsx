import { useState } from 'react'
import { colors } from '../theme'

const EXAMPLE_QUERY = `{
  blocks(first: 5) {
    id
    number
    timestamp
    gasUsed
    transactions {
      id
      from
      to
      value
    }
  }
}`

export function GraphQL() {
  const [query, setQuery] = useState(EXAMPLE_QUERY)
  const [result, setResult] = useState('')
  const [loading, setLoading] = useState(false)

  async function execute() {
    setLoading(true)
    setResult('')
    try {
      const endpoint = '/v1/subgraph/graphql'
      const res = await fetch(endpoint, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ query }),
      })
      const json = await res.json()
      setResult(JSON.stringify(json, null, 2))
    } catch (err) {
      setResult(`Error: ${err instanceof Error ? err.message : 'Request failed'}`)
    } finally {
      setLoading(false)
    }
  }

  return (
    <div>
      <h1 style={{ fontSize: 20, fontWeight: 600, marginBottom: 8 }}>GraphQL Explorer</h1>
      <p style={{ color: colors.textMuted, fontSize: 14, marginBottom: 20 }}>
        Query the chain subgraph via GraphQL.
      </p>

      <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 16 }}>
        <div>
          <div
            style={{
              display: 'flex',
              justifyContent: 'space-between',
              alignItems: 'center',
              marginBottom: 8,
            }}
          >
            <label style={{ fontSize: 13, color: colors.textMuted, fontWeight: 600 }}>Query</label>
            <button
              onClick={execute}
              disabled={loading}
              style={{
                padding: '6px 16px',
                background: loading ? colors.border : colors.accent,
                color: loading ? colors.textMuted : '#fff',
                border: 'none',
                borderRadius: 6,
                cursor: loading ? 'not-allowed' : 'pointer',
                fontSize: 13,
                fontWeight: 600,
              }}
            >
              {loading ? 'Running...' : 'Execute'}
            </button>
          </div>
          <textarea
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            spellCheck={false}
            style={{
              width: '100%',
              height: 400,
              padding: 16,
              borderRadius: 8,
              border: `1px solid ${colors.border}`,
              background: colors.card,
              color: colors.text,
              fontSize: 13,
              fontFamily: colors.mono,
              lineHeight: 1.6,
              resize: 'vertical',
              outline: 'none',
            }}
          />
        </div>

        <div>
          <label style={{ fontSize: 13, color: colors.textMuted, fontWeight: 600, display: 'block', marginBottom: 8 }}>
            Result
          </label>
          <pre
            style={{
              width: '100%',
              height: 400,
              padding: 16,
              borderRadius: 8,
              border: `1px solid ${colors.border}`,
              background: colors.card,
              color: result.startsWith('Error') ? colors.error : colors.text,
              fontSize: 13,
              fontFamily: colors.mono,
              lineHeight: 1.6,
              overflow: 'auto',
              margin: 0,
              whiteSpace: 'pre-wrap',
              wordBreak: 'break-word',
            }}
          >
            {result || 'Click Execute to run the query.'}
          </pre>
        </div>
      </div>

      <div
        style={{
          marginTop: 24,
          background: colors.card,
          borderRadius: 8,
          padding: 20,
          border: `1px solid ${colors.border}`,
        }}
      >
        <h2 style={{ fontSize: 16, fontWeight: 600, marginBottom: 12 }}>Available Entities</h2>
        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(150px, 1fr))', gap: 8 }}>
          {['blocks', 'transactions', 'accounts', 'tokens', 'transfers', 'approvals', 'pairs', 'swaps'].map((entity) => (
            <div
              key={entity}
              style={{
                padding: '8px 12px',
                background: colors.bg,
                borderRadius: 6,
                fontSize: 13,
                fontFamily: colors.mono,
                color: colors.accent,
              }}
            >
              {entity}
            </div>
          ))}
        </div>
      </div>
    </div>
  )
}
