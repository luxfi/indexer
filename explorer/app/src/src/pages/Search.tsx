import { useSearchParams, Link } from 'react-router-dom'
import { useSearch } from '../api/hooks'
import { colors } from '../theme'

export function Search() {
  const [params] = useSearchParams()
  const q = params.get('q') || ''
  const { data, isLoading } = useSearch(q)

  return (
    <div>
      <h1 style={{ fontSize: 20, fontWeight: 600, marginBottom: 20 }}>
        Search results for "{q}"
      </h1>
      <div
        style={{
          background: colors.card,
          borderRadius: 8,
          padding: 20,
          border: `1px solid ${colors.border}`,
        }}
      >
        {isLoading && <p style={{ color: colors.textMuted }}>Loading...</p>}
        {data && data.items.length === 0 && (
          <p style={{ color: colors.textMuted }}>No results found.</p>
        )}
        {data?.items.map((r, i) => {
          let link = '#'
          let label = r.type
          let detail = ''

          if (r.type === 'address' && r.address) {
            link = `/address/${r.address}`
            detail = r.address
          } else if (r.type === 'block' && r.block_number !== undefined) {
            link = `/blocks/${r.block_number}`
            detail = `Block #${r.block_number}`
          } else if (r.type === 'transaction' && r.tx_hash) {
            link = `/tx/${r.tx_hash}`
            detail = r.tx_hash
          }

          return (
            <div
              key={i}
              style={{
                padding: '12px 0',
                borderBottom: `1px solid ${colors.border}`,
              }}
            >
              <span
                style={{
                  fontSize: 11,
                  color: colors.accent,
                  textTransform: 'uppercase',
                  fontWeight: 600,
                  marginRight: 12,
                }}
              >
                {label}
              </span>
              <Link
                to={link}
                style={{
                  color: colors.text,
                  textDecoration: 'none',
                  fontFamily: colors.mono,
                  fontSize: 14,
                }}
              >
                {r.name || detail}
              </Link>
            </div>
          )
        })}
      </div>
    </div>
  )
}
