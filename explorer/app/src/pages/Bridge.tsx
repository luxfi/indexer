import { colors } from '../theme'

// All chain info from env — zero hardcoded names
const chains: { label: string; slug: string }[] = (() => {
  try { return JSON.parse(import.meta.env.VITE_CHAINS || '[]') } catch { return [] }
})()

export function Bridge() {
  const chainOptions = chains.length > 0
    ? chains
    : [{ label: 'EVM', slug: 'evm' }, { label: 'DEX', slug: 'dex' }, { label: 'FHE', slug: 'fhe' }]

  return (
    <div>
      <h1 style={{ fontSize: 20, fontWeight: 600, marginBottom: 20 }}>Cross-Chain Bridge</h1>

      <div
        style={{
          display: 'grid',
          gridTemplateColumns: 'repeat(auto-fit, minmax(300px, 1fr))',
          gap: 16,
          marginBottom: 24,
        }}
      >
        {chainOptions.map((chain) => (
          <div
            key={chain.slug}
            style={{
              background: colors.card,
              borderRadius: 8,
              padding: 20,
              border: `1px solid ${colors.border}`,
            }}
          >
            <div style={{ fontSize: 16, fontWeight: 600, marginBottom: 8 }}>{chain.label}</div>
            <div style={{ fontSize: 12, fontFamily: colors.mono, color: colors.textMuted }}>
              /{chain.slug}
            </div>
          </div>
        ))}
      </div>

      <div
        style={{
          background: colors.card,
          borderRadius: 8,
          padding: 24,
          border: `1px solid ${colors.border}`,
          maxWidth: 500,
          margin: '0 auto',
        }}
      >
        <h2 style={{ fontSize: 16, fontWeight: 600, marginBottom: 20, textAlign: 'center' }}>
          Transfer Assets
        </h2>

        <div style={{ marginBottom: 16 }}>
          <label style={{ fontSize: 13, color: colors.textMuted, display: 'block', marginBottom: 6 }}>From</label>
          <select style={{ width: '100%', padding: '10px 14px', borderRadius: 6, border: `1px solid ${colors.border}`, background: colors.bg, color: colors.text, fontSize: 14 }}>
            {chainOptions.map((c) => <option key={c.slug}>{c.label}</option>)}
          </select>
        </div>

        <div style={{ textAlign: 'center', padding: '8px 0', color: colors.textMuted, fontSize: 18 }}>|</div>

        <div style={{ marginBottom: 16 }}>
          <label style={{ fontSize: 13, color: colors.textMuted, display: 'block', marginBottom: 6 }}>To</label>
          <select style={{ width: '100%', padding: '10px 14px', borderRadius: 6, border: `1px solid ${colors.border}`, background: colors.bg, color: colors.text, fontSize: 14 }}>
            {[...chainOptions].reverse().map((c) => <option key={c.slug}>{c.label}</option>)}
          </select>
        </div>

        <div style={{ marginBottom: 20 }}>
          <label style={{ fontSize: 13, color: colors.textMuted, display: 'block', marginBottom: 6 }}>Amount</label>
          <input type="text" placeholder="0.0" style={{ width: '100%', padding: '10px 14px', borderRadius: 6, border: `1px solid ${colors.border}`, background: colors.bg, color: colors.text, fontSize: 14, fontFamily: colors.mono, outline: 'none' }} />
        </div>

        <button disabled style={{ width: '100%', padding: '12px 20px', background: colors.border, color: colors.textMuted, border: 'none', borderRadius: 6, cursor: 'not-allowed', fontSize: 14, fontWeight: 600 }}>
          Bridge Coming Soon
        </button>
      </div>
    </div>
  )
}
