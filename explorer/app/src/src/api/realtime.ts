// Base SSE realtime client for live block/transaction updates.
// Connects to /v1/base/realtime (Base Functions SSE endpoint).
// No Phoenix WebSocket, no socket.io — just SSE.

const REALTIME_URL = import.meta.env.VITE_REALTIME_URL || '/v1/base/realtime'

type RealtimeEvent = {
  event: string
  data: unknown
}

type Handler = (data: unknown) => void

class RealtimeClient {
  private es: EventSource | null = null
  private handlers = new Map<string, Set<Handler>>()
  private reconnectMs = 1000
  private maxReconnectMs = 30000
  private disabled = false

  async connect() {
    if (this.es || this.disabled) return
    try {
      // Probe endpoint before opening EventSource (avoids console error spam)
      const url = new URL(REALTIME_URL, window.location.origin).toString()
      const probe = await fetch(url, { method: 'HEAD' }).catch(() => null)
      if (!probe || !probe.ok) {
        this.disabled = true // endpoint not available, don't retry
        return
      }

      this.es = new EventSource(url)
      this.reconnectMs = 1000

      this.es.onmessage = (e) => {
        try {
          const msg: RealtimeEvent = JSON.parse(e.data)
          const handlers = this.handlers.get(msg.event)
          if (handlers) {
            for (const h of handlers) h(msg.data)
          }
        } catch { /* ignore parse errors */ }
      }

      this.es.addEventListener('CONNECT', () => {
        // SSE handshake complete
      })

      this.es.onerror = () => {
        this.es?.close()
        this.es = null
        if (this.reconnectMs >= this.maxReconnectMs) {
          this.disabled = true // give up after max backoff
          return
        }
        setTimeout(() => this.connect(), this.reconnectMs)
        this.reconnectMs = Math.min(this.reconnectMs * 2, this.maxReconnectMs)
      }
    } catch {
      this.disabled = true
    }
  }

  disconnect() {
    this.es?.close()
    this.es = null
  }

  on(event: string, handler: Handler) {
    if (!this.handlers.has(event)) this.handlers.set(event, new Set())
    this.handlers.get(event)!.add(handler)
    return () => { this.handlers.get(event)?.delete(handler) }
  }
}

export const realtime = new RealtimeClient()
