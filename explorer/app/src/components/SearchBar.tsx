import { useState, useRef, useEffect } from 'react'
import { useNavigate } from 'react-router-dom'
import { colors } from '../theme'

export function SearchBar() {
  const [query, setQuery] = useState('')
  const [focused, setFocused] = useState(false)
  const inputRef = useRef<HTMLInputElement>(null)
  const navigate = useNavigate()

  // Cmd+K / Ctrl+K to focus search
  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      if ((e.metaKey || e.ctrlKey) && e.key === 'k') {
        e.preventDefault()
        inputRef.current?.focus()
      }
      if (e.key === 'Escape') {
        inputRef.current?.blur()
        setFocused(false)
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [])

  function submit(e: React.FormEvent) {
    e.preventDefault()
    const q = query.trim()
    if (!q) return

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
    inputRef.current?.blur()
  }

  return (
    <form onSubmit={submit} style={{ position: 'relative', minWidth: 200, maxWidth: 360, flex: 1 }}>
      <input
        ref={inputRef}
        type="text"
        value={query}
        onChange={(e) => setQuery(e.target.value)}
        onFocus={() => setFocused(true)}
        onBlur={() => setFocused(false)}
        placeholder="Search..."
        style={{
          width: '100%',
          padding: '7px 12px',
          paddingRight: 52,
          borderRadius: 6,
          border: `1px solid ${focused ? colors.accent : colors.border}`,
          background: colors.card,
          color: colors.text,
          fontSize: 13,
          outline: 'none',
          transition: 'border-color 0.15s',
        }}
      />
      {!focused && (
        <span style={{
          position: 'absolute',
          right: 10,
          top: '50%',
          transform: 'translateY(-50%)',
          fontSize: 11,
          color: colors.textMuted,
          background: colors.bg,
          border: `1px solid ${colors.border}`,
          borderRadius: 4,
          padding: '1px 6px',
          pointerEvents: 'none',
          fontFamily: colors.mono,
        }}>
          ⌘K
        </span>
      )}
    </form>
  )
}
