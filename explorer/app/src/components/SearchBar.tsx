import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { colors } from '../theme'

export function SearchBar() {
  const [query, setQuery] = useState('')
  const navigate = useNavigate()

  function submit(e: React.FormEvent) {
    e.preventDefault()
    const q = query.trim()
    if (!q) return

    // Route directly for known patterns
    if (/^0x[a-fA-F0-9]{64}$/.test(q)) {
      navigate(`/tx/${q}`)
    } else if (/^0x[a-fA-F0-9]{40}$/.test(q)) {
      navigate(`/address/${q}`)
    } else if (/^\d+$/.test(q)) {
      navigate(`/blocks/${q}`)
    } else {
      navigate(`/search?q=${encodeURIComponent(q)}`)
    }
    setQuery('')
  }

  return (
    <form onSubmit={submit} style={{ display: 'flex', flex: 1, maxWidth: 480 }}>
      <input
        type="text"
        value={query}
        onChange={(e) => setQuery(e.target.value)}
        placeholder="Search by address, tx hash, or block..."
        style={{
          flex: 1,
          padding: '8px 14px',
          borderRadius: 6,
          border: `1px solid ${colors.border}`,
          background: colors.card,
          color: colors.text,
          fontSize: 14,
          fontFamily: colors.mono,
          outline: 'none',
        }}
      />
    </form>
  )
}
