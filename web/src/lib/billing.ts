import { api } from '../api'

export interface LiveTier {
  id: string
  name: string
  priceCents: number
  maxDevices: number
  retentionDays: number
}

export interface LiveSubscription {
  id?: string
  planId?: string
  status?: string
  currentPeriodEnd?: string
  provider?: string
}

export interface BillingSnapshot {
  tiers: LiveTier[]
  subscription: LiveSubscription | null
  eventsToday: number
  devices: number
  checkoutAvailable: boolean
}

/** One live snapshot: tiers + subscription + usage, all from trace (which itself reads billing-service live). */
export async function fetchBillingSnapshot(): Promise<BillingSnapshot> {
  const [tiersRes, subRes, usageRes] = await Promise.all([
    api('/v1/billing/tiers').catch(() => ({ items: [], checkout_available: false })),
    api('/v1/billing/subscription').catch(() => null),
    api('/v1/usage').catch(() => ({ metrics: [] })),
  ])
  const metrics: Array<{ metric_name?: string; value?: number }> = usageRes.metrics || []
  return {
    tiers: tiersRes.items || tiersRes.tiers || [],
    subscription: subRes && subRes.planId ? subRes : null,
    eventsToday: metrics.reduce((sum, m) => sum + (m.value || 0), 0),
    devices: metrics.find((m) => m.metric_name === 'devices')?.value || 0,
    checkoutAvailable: !!tiersRes.checkout_available,
  }
}

/** Start a hosted checkout via trace (proxies billing-service prices). Returns the URL to open. */
export async function startCheckout(plan: 'pilot' | 'fleet' | 'enterprise'): Promise<string> {
  const res = await api('/v1/billing/checkout', { method: 'POST', body: { plan } })
  if (!res.checkout_url) throw new Error('checkout unavailable — contact the team')
  return res.checkout_url as string
}

/** Follow live billing changes over the existing trace event stream. Returns an unsubscribe fn. */
export function followBillingEvents(onChange: () => void): () => void {
  const src = new EventSource('/api/stream')
  const handler = (e: MessageEvent) => {
    try {
      const msg = JSON.parse(e.data)
      if (msg?.type?.startsWith?.('billing.') || msg?.type?.startsWith?.('entitlement.')) onChange()
    } catch {
      /* non-JSON heartbeat */
    }
  }
  src.addEventListener('message', handler as EventListener)
  return () => src.close()
}
