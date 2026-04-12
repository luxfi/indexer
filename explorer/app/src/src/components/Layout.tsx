import { useState, useRef, useEffect } from 'react'
import { Outlet, Link, useLocation } from 'react-router-dom'
import { SearchBar } from './SearchBar'
import { colors } from '../theme'

const networks = [
  { label: 'Devnet', domain: 'explore.dev.satschel.com', chainId: 8675311 },
  { label: 'Testnet', domain: 'explore.test.satschel.com', chainId: 8675310 },
  { label: 'Mainnet', domain: 'explore.satschel.com', chainId: 8675309 },
] as const

const chains = [
  { label: 'Liquid EVM', slug: 'evm' },
  { label: 'Liquid DEX', slug: 'dex' },
  { label: 'Liquid FHE', slug: 'fhe' },
] as const

interface NavGroup {
  label: string
  items: { label: string; to: string }[]
}

const navGroups: NavGroup[] = [
  {
    label: 'Blockchain',
    items: [
      { label: 'Blocks', to: '/blocks' },
      { label: 'Transactions', to: '/txs' },
      { label: 'Internal Txs', to: '/internal-txs' },
    ],
  },
  {
    label: 'Tokens',
    items: [
      { label: 'Token List', to: '/tokens' },
      { label: 'Transfers', to: '/token-transfers' },
    ],
  },
  {
    label: 'Tools',
    items: [
      { label: 'Gas Tracker', to: '/gas-tracker' },
      { label: 'Statistics', to: '/stats' },
      { label: 'Validators', to: '/validators' },
      { label: 'API Docs', to: '/api-docs' },
      { label: 'GraphQL', to: '/graphql' },
    ],
  },
  {
    label: 'Cross-Chain',
    items: [
      { label: 'DEX', to: '/dex' },
      { label: 'Bridge', to: '/bridge' },
    ],
  },
]

function detectNetwork(): number {
  const host = window.location.hostname
  for (const n of networks) {
    if (host === n.domain || host.endsWith(`.${n.domain}`)) return networks.indexOf(n)
  }
  return 0
}

function Dropdown({ label, children, active }: { label: string; children: React.ReactNode; active?: boolean }) {
  const [open, setOpen] = useState(false)
  const ref = useRef<HTMLDivElement>(null)

  useEffect(() => {
    function handleClick(e: MouseEvent) {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false)
    }
    document.addEventListener('mousedown', handleClick)
    return () => document.removeEventListener('mousedown', handleClick)
  }, [])

  return (
    <div ref={ref} style={{ position: 'relative' }}>
      <button
        onClick={() => setOpen(!open)}
        style={{
          background: 'none',
          border: 'none',
          color: active ? colors.text : colors.textMuted,
          cursor: 'pointer',
          fontSize: 14,
          display: 'flex',
          alignItems: 'center',
          gap: 4,
          padding: '4px 0',
        }}
      >
        {label}
        <span style={{ fontSize: 10 }}>{open ? '\u25B2' : '\u25BC'}</span>
      </button>
      {open && (
        <div
          style={{
            position: 'absolute',
            top: '100%',
            left: 0,
            marginTop: 4,
            background: colors.card,
            border: `1px solid ${colors.border}`,
            borderRadius: 8,
            padding: '4px 0',
            minWidth: 160,
            zIndex: 100,
            boxShadow: '0 4px 12px rgba(0,0,0,0.5)',
          }}
          onClick={() => setOpen(false)}
        >
          {children}
        </div>
      )}
    </div>
  )
}

function DropdownItem({ children, onClick, active }: { children: React.ReactNode; onClick?: () => void; active?: boolean }) {
  return (
    <div
      onClick={onClick}
      style={{
        padding: '8px 16px',
        fontSize: 14,
        color: active ? colors.accent : colors.text,
        cursor: 'pointer',
        fontWeight: active ? 600 : 400,
      }}
      onMouseEnter={(e) => { (e.target as HTMLElement).style.background = colors.cardHover }}
      onMouseLeave={(e) => { (e.target as HTMLElement).style.background = 'transparent' }}
    >
      {children}
    </div>
  )
}

function NetworkSwitcher() {
  const current = detectNetwork()
  const loc = useLocation()

  function switchNetwork(idx: number) {
    if (idx === current) return
    const target = networks[idx]
    const proto = window.location.protocol
    window.location.href = `${proto}//${target.domain}${loc.pathname}${loc.search}`
  }

  return (
    <Dropdown label={networks[current].label}>
      {networks.map((n, i) => (
        <DropdownItem key={n.domain} onClick={() => switchNetwork(i)} active={i === current}>
          {n.label}
          <span style={{ marginLeft: 8, fontSize: 11, color: colors.textMuted, fontFamily: colors.mono }}>
            {n.chainId}
          </span>
        </DropdownItem>
      ))}
    </Dropdown>
  )
}

function ChainSwitcher() {
  const [current, setCurrent] = useState(0)

  return (
    <Dropdown label={chains[current].label}>
      {chains.map((c, i) => (
        <DropdownItem key={c.slug} onClick={() => setCurrent(i)} active={i === current}>
          {c.label}
        </DropdownItem>
      ))}
    </Dropdown>
  )
}

function NavGroupDropdown({ group }: { group: NavGroup }) {
  const loc = useLocation()
  const active = group.items.some((item) => loc.pathname === item.to)

  return (
    <Dropdown label={group.label} active={active}>
      {group.items.map((item) => (
        <Link
          key={item.to}
          to={item.to}
          style={{ textDecoration: 'none', display: 'block' }}
        >
          <DropdownItem active={loc.pathname === item.to}>
            {item.label}
          </DropdownItem>
        </Link>
      ))}
    </Dropdown>
  )
}

export function Layout() {
  const chainName = import.meta.env.VITE_CHAIN_NAME || 'Lux Explorer'

  return (
    <div style={{ minHeight: '100vh', display: 'flex', flexDirection: 'column' }}>
      <header
        style={{
          borderBottom: `1px solid ${colors.border}`,
          padding: '12px 24px',
          display: 'flex',
          alignItems: 'center',
          gap: 16,
          flexWrap: 'wrap',
        }}
      >
        <Link
          to="/"
          style={{
            fontSize: 18,
            fontWeight: 700,
            color: colors.text,
            textDecoration: 'none',
            whiteSpace: 'nowrap',
          }}
        >
          {chainName}
        </Link>

        <div
          style={{
            display: 'flex',
            alignItems: 'center',
            gap: 8,
            padding: '2px 0',
          }}
        >
          <NetworkSwitcher />
          <span style={{ color: colors.border }}>|</span>
          <ChainSwitcher />
        </div>

        <nav style={{ display: 'flex', gap: 12 }}>
          {navGroups.map((g) => (
            <NavGroupDropdown key={g.label} group={g} />
          ))}
        </nav>

        <SearchBar />
      </header>

      <main style={{ flex: 1, padding: 24, maxWidth: 1200, width: '100%', margin: '0 auto' }}>
        <Outlet />
      </main>

      <footer
        style={{
          borderTop: `1px solid ${colors.border}`,
          padding: '16px 24px',
          fontSize: 12,
          color: colors.textMuted,
          textAlign: 'center',
        }}
      >
        {chainName} Explorer
      </footer>
    </div>
  )
}
