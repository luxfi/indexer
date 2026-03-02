import { useState } from 'react'
import { Link } from 'react-router-dom'
import { colors } from '../theme'

function truncate(hash: string, chars = 8): string {
  if (hash.length <= chars * 2 + 2) return hash
  return `${hash.slice(0, chars + 2)}...${hash.slice(-chars)}`
}

interface HashProps {
  hash: string
  link?: string
  full?: boolean
}

export function Hash({ hash, link, full }: HashProps) {
  const [copied, setCopied] = useState(false)

  const display = full ? hash : truncate(hash)

  function copy() {
    navigator.clipboard.writeText(hash)
    setCopied(true)
    setTimeout(() => setCopied(false), 1500)
  }

  const style: React.CSSProperties = {
    fontFamily: colors.mono,
    fontSize: 13,
    color: link ? colors.accent : colors.text,
    cursor: 'pointer',
  }

  const inner = link ? (
    <Link to={link} style={{ ...style, textDecoration: 'none' }}>
      {display}
    </Link>
  ) : (
    <span style={style}>{display}</span>
  )

  return (
    <span style={{ display: 'inline-flex', alignItems: 'center', gap: 6 }}>
      {inner}
      <button
        onClick={copy}
        style={{
          background: 'none',
          border: 'none',
          color: colors.textMuted,
          cursor: 'pointer',
          fontSize: 11,
          padding: 0,
        }}
        title="Copy"
      >
        {copied ? 'ok' : 'cp'}
      </button>
    </span>
  )
}
