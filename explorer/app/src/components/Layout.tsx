import { useState, useRef, useEffect } from 'react'
import { Outlet, Link, useLocation, useParams, useNavigate } from 'react-router-dom'
import { useQueryClient } from '@tanstack/react-query'
import { setChainSlug } from '../api/chain'
import { SearchBar } from './SearchBar'
import { colors } from '../theme'

// Runtime config from env — zero hardcoded domains, names, or chain IDs
const configuredNetworks: { label: string; domain: string; chainId: number }[] = (() => {
  try { return JSON.parse(import.meta.env.VITE_NETWORKS || '[]') } catch { return [] }
})()

// Auto-detect localnet when running on localhost
const isLocal = typeof window !== 'undefined' && (window.location.hostname === 'localhost' || window.location.hostname === '127.0.0.1')
const localNetwork = isLocal ? { label: 'Localnet', domain: window.location.host, chainId: 0 } : null
const networks = localNetwork ? [localNetwork, ...configuredNetworks] : configuredNetworks

const chains: { label: string; slug: string }[] = (() => {
  try { return JSON.parse(import.meta.env.VITE_CHAINS || '[]') } catch { return [] }
})()

const coin = import.meta.env.VITE_COIN || 'ETH'

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
  const host = window.location.host // includes port
  const hostname = window.location.hostname
  for (let i = 0; i < networks.length; i++) {
    const d = networks[i].domain
    if (host === d || hostname === d || hostname.endsWith(`.${d}`)) return i
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
  const [localChainId, setLocalChainId] = useState(0)

  // Fetch actual chain ID for localnet
  useEffect(() => {
    if (!isLocal) return
    const base = import.meta.env.VITE_API_BASE || '/v1/indexer'
    fetch(`${base}/config/backend`).then(r => r.json()).then(d => {
      if (d?.chain_id) setLocalChainId(Number(d.chain_id))
    }).catch(() => {})
  }, [])

  function switchNetwork(idx: number) {
    if (idx === current) return
    const target = networks[idx]
    const proto = window.location.protocol
    window.location.href = `${proto}//${target.domain}${loc.pathname}${loc.search}`
  }

  return (
    <Dropdown label={networks[current]?.label || 'Network'}>
      {networks.map((n, i) => (
        <DropdownItem key={n.domain} onClick={() => switchNetwork(i)} active={i === current}>
          {n.label}
          <span style={{ marginLeft: 8, fontSize: 11, color: colors.textMuted, fontFamily: colors.mono }}>
            {n.label === 'Localnet' && localChainId ? localChainId : n.chainId || ''}
          </span>
        </DropdownItem>
      ))}
    </Dropdown>
  )
}

function ChainSwitcher() {
  const { chain } = useParams()
  const navigate = useNavigate()
  const loc = useLocation()

  const currentIdx = Math.max(0, chains.findIndex(c => c.slug === chain))

  function switchChain(idx: number) {
    const slug = chains[idx]?.slug || 'evm'
    // Replace /:chain/ prefix in current path
    const rest = loc.pathname.replace(/^\/[^/]+/, '')
    navigate(`/${slug}${rest || '/'}`)
  }

  if (chains.length === 0) return null

  return (
    <Dropdown label={chains[currentIdx]?.label || 'EVM'}>
      {chains.map((c, i) => (
        <DropdownItem key={c.slug} onClick={() => switchChain(i)} active={i === currentIdx}>
          {c.label}
        </DropdownItem>
      ))}
    </Dropdown>
  )
}

function NavGroupDropdown({ group }: { group: NavGroup }) {
  const loc = useLocation()
  const { chain } = useParams()
  const prefix = `/${chain || 'evm'}`
  const active = group.items.some((item) => loc.pathname === `${prefix}${item.to}`)

  return (
    <Dropdown label={group.label} active={active}>
      {group.items.map((item) => (
        <Link
          key={item.to}
          to={`${prefix}${item.to}`}
          style={{ textDecoration: 'none', display: 'block' }}
        >
          <DropdownItem active={loc.pathname === `${prefix}${item.to}`}>
            {item.label}
          </DropdownItem>
        </Link>
      ))}
    </Dropdown>
  )
}

// Unified logo bar: L icon + sliding wordmark OR switchers
// States: intro (wordmark visible) → collapsed (switchers visible) → hover (wordmark visible)
function LogoBar({ chainName, logoUrl }: { chainName: string; logoUrl: string }) {
  const [phase, setPhase] = useState<'intro' | 'collapsed'>('intro')
  const [hovered, setHovered] = useState(false)

  useEffect(() => {
    const t = setTimeout(() => setPhase('collapsed'), 2500)
    return () => clearTimeout(t)
  }, [])

  const expanded = phase === 'intro' || hovered
  const showSwitchers = phase === 'collapsed' && !hovered

  // No logo — plain text
  if (!logoUrl) {
    return (
      <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
        <Link to="/" style={{ fontSize: 16, fontWeight: 700, color: colors.text, textDecoration: 'none', whiteSpace: 'nowrap' }}>
          {chainName}
        </Link>
        <NetworkSwitcher />
        <span style={{ color: colors.border, fontSize: 12 }}>|</span>
        <ChainSwitcher />
      </div>
    )
  }

  // Single unified SVG logo — clips to icon-only when collapsed, full wordmark on hover
  return (
    <div style={{ display: 'flex', alignItems: 'center', height: 36 }}>
      <Link
        to="/"
        style={{ display: 'flex', alignItems: 'center', textDecoration: 'none', flexShrink: 0, overflow: 'hidden' }}
        onMouseEnter={() => setHovered(true)}
        onMouseLeave={() => setHovered(false)}
      >
        <img
          src={logoUrl}
          alt={chainName}
          style={{
            height: 28,
            maxWidth: expanded ? 300 : 28,
            objectFit: 'cover',
            objectPosition: 'left center',
            transition: 'max-width 0.35s ease',
          }}
        />
      </Link>

      {/* Switchers — fade in next to collapsed logo */}
      <div style={{
        display: 'flex', alignItems: 'center', gap: 6, marginLeft: 8,
        opacity: showSwitchers ? 1 : 0,
        transform: showSwitchers ? 'translateX(0)' : 'translateX(-12px)',
        transition: 'opacity 0.3s ease, transform 0.3s ease',
        pointerEvents: showSwitchers ? 'auto' : 'none',
        whiteSpace: 'nowrap',
      }}>
        <NetworkSwitcher />
        <span style={{ color: colors.border, fontSize: 12 }}>|</span>
        <ChainSwitcher />
      </div>
    </div>
  )
}

export function Layout() {
  const chainName = import.meta.env.VITE_CHAIN_NAME || 'Explorer'
  const logoUrl = import.meta.env.VITE_LOGO_URL || ''

  return (
    <div style={{ minHeight: '100vh', display: 'flex', flexDirection: 'column' }}>
      {/* Top bar: logo + switchers */}
      <header style={{ borderBottom: `1px solid ${colors.border}`, padding: '10px 20px' }}>
        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 12, maxWidth: 1200, margin: '0 auto' }}>
          {/* Left: logo + animated wordmark/switcher swap */}
          <LogoBar chainName={chainName} logoUrl={logoUrl} />

          {/* Right: nav + search */}
          <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
            <nav style={{ display: 'flex', gap: 6 }}>
              {navGroups.map((g) => (
                <NavGroupDropdown key={g.label} group={g} />
              ))}
            </nav>
            <SearchBar />
          </div>
        </div>
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
        {chainName}
      </footer>
    </div>
  )
}
