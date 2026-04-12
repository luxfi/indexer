const UNITS: [string, number][] = [
  ['y', 31536000],
  ['mo', 2592000],
  ['d', 86400],
  ['h', 3600],
  ['m', 60],
  ['s', 1],
]

export function timeAgo(ts: string): string {
  const seconds = Math.floor((Date.now() - new Date(ts).getTime()) / 1000)
  if (seconds < 1) return 'just now'
  for (const [unit, divisor] of UNITS) {
    const val = Math.floor(seconds / divisor)
    if (val >= 1) return `${val}${unit} ago`
  }
  return 'just now'
}

export function TimeAgo({ timestamp }: { timestamp: string }) {
  return <span style={{ color: '#737373', fontSize: 13 }}>{timeAgo(timestamp)}</span>
}
