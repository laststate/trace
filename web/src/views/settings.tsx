// Settings panel — real forms for retention, token management, notifications.
// Replaces the old prompt()-driven controls.
import React, { useEffect, useState } from 'react'
import { api } from '../api'
import { Badge } from '../components/ui/badge'
import { Button } from '../components/ui/button'
import { Bell, Shield, Trash2, Volume2, VolumeX } from '../icons'
import {
  getNotificationPermission,
  requestNotificationPermission,
  isNotificationsEnabled,
  setNotificationsEnabled,
  isSoundEnabled,
  setSoundEnabled,
  clearAlertHistory,
  getAlertHistory,
  sendTestNotification,
} from '../notifications'

interface TokenRow {
  id: string
  name: string
  prefix?: string
  scopes?: string[]
  created_at?: string
  last_used_at?: string | null
}

type Toast = { id: string; type: 'success' | 'error' | 'info'; message: string }

export default function SettingsPanel({ data, reload, dispatchToast = () => {} }: {
  data: any
  reload: () => void
  dispatchToast?: (t: Toast) => void
}) {
  const st = data.settings || {}
  const perm = getNotificationPermission()
  const notifOn = isNotificationsEnabled()
  const soundOn = isSoundEnabled()

  const [ret, setRet] = useState({
    events: String(st.retention_events_days ?? 90),
    health: String(st.retention_health_days ?? 30),
    logs: String(st.retention_logs_days ?? 14),
    metrics: String(st.retention_metrics_days ?? 30),
  })
  const [savingRet, setSavingRet] = useState(false)
  const [tokens, setTokens] = useState<TokenRow[]>([])
  const [tokenName, setTokenName] = useState('')
  const [creatingToken, setCreatingToken] = useState(false)
  const [newSecret, setNewSecret] = useState('')
  const [clearing, setClearing] = useState(0)

  async function refreshTokens() {
    try {
      const res = await api('/api/tokens')
      setTokens(res.items || [])
    } catch {
      setTokens([])
    }
  }

  useEffect(() => { refreshTokens() }, [])

  async function saveRetention(e: React.FormEvent) {
    e.preventDefault()
    if (savingRet) return
    setSavingRet(true)
    try {
      await api('/api/settings', {
        method: 'PUT',
        body: {
          retention_events_days: Number(ret.events) || 90,
          retention_health_days: Number(ret.health) || 30,
          retention_logs_days: Number(ret.logs) || 14,
          retention_metrics_days: Number(ret.metrics) || 30,
          analyzer_version_min: st.analyzer_version_min || 1,
        },
      })
      dispatchToast({ id: Math.random().toString(36).slice(2), type: 'success', message: 'Retention settings saved' })
      reload()
    } catch (err: any) {
      dispatchToast({ id: Math.random().toString(36).slice(2), type: 'error', message: err.message || 'Failed to save settings' })
    } finally {
      setSavingRet(false)
    }
  }

  async function createToken(e: React.FormEvent) {
    e.preventDefault()
    const name = tokenName.trim()
    if (!name || creatingToken) return
    setCreatingToken(true)
    try {
      const res = await api('/api/tokens', { method: 'POST', body: { name, scopes: ['event:write', 'event:read', 'artifact:write'] } })
      setNewSecret(res.secret || '')
      setTokenName('')
      await refreshTokens()
    } catch (err: any) {
      dispatchToast({ id: Math.random().toString(36).slice(2), type: 'error', message: err.message || 'Failed to create token' })
    } finally {
      setCreatingToken(false)
    }
  }

  async function revokeToken(id: string) {
    try {
      await api('/api/tokens/' + id, { method: 'DELETE' })
      dispatchToast({ id: Math.random().toString(36).slice(2), type: 'info', message: 'Token revoked' })
      await refreshTokens()
    } catch (err: any) {
      dispatchToast({ id: Math.random().toString(36).slice(2), type: 'error', message: err.message || 'Failed to revoke token' })
    }
  }

  return (
    <div className="panel" data-testid="settings-panel">
      <h2>Project settings</h2>
      <p className="meta">Open UI: {String(data.open_ui)} · Bootstrapped: {String(data.bootstrapped)}</p>

      {/* Browser notifications */}
      <div className="notif-widget" style={{ marginTop: '1.25rem', marginBottom: '1.25rem' }}>
        <div className="notif-widget-head">
          <h3><Bell size={16} /> Browser Notifications &amp; Real-Time Alerts</h3>
          <span className={`notif-status-badge ${perm}`}>
            {perm === 'granted' ? 'Permission Granted' : perm === 'denied' ? 'Blocked in Browser' : 'Permission Pending'}
          </span>
        </div>
        <p className="meta" style={{ marginTop: 0, marginBottom: '1rem' }}>
          Configure how Trace alerts your development environment about new Fatal errors, queue failures, and hardware anomalies.
        </p>
        <div className="grid-2">
          <div>
            <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: '0.75rem', padding: '0.5rem 0.75rem', background: 'rgba(255,255,255,0.02)', borderRadius: '0.375rem' }}>
              <div>
                <strong>Desktop Notifications (Push)</strong>
                <div className="meta" style={{ fontSize: '0.75rem' }}>Native OS cards when critical incidents are detected</div>
              </div>
              <Button
                type="button"
                variant={notifOn ? 'default' : 'secondary'}
                style={{ fontSize: '0.75rem', padding: '0.25rem 0.6rem' }}
                onClick={async () => {
                  if (perm !== 'granted') {
                    const granted = await requestNotificationPermission()
                    if (granted) setNotificationsEnabled(true)
                  } else {
                    setNotificationsEnabled(!notifOn)
                  }
                  setClearing(c => c + 1)
                }}
              >
                {notifOn ? 'Enabled' : 'Disabled'}
              </Button>
            </div>
            <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', padding: '0.5rem 0.75rem', background: 'rgba(255,255,255,0.02)', borderRadius: '0.375rem' }}>
              <div>
                <strong>Sound Effects</strong>
                <div className="meta" style={{ fontSize: '0.75rem' }}>Subtle chime via Web Audio API on critical events</div>
              </div>
              <Button
                type="button"
                variant={soundOn ? 'default' : 'secondary'}
                style={{ fontSize: '0.75rem', padding: '0.25rem 0.6rem' }}
                onClick={() => { setSoundEnabled(!soundOn); setClearing(c => c + 1) }}
              >
                {soundOn ? <><Volume2 size={13} style={{ marginRight: 4, verticalAlign: '-2px' }} /> Sound On</> : <><VolumeX size={13} style={{ marginRight: 4, verticalAlign: '-2px' }} /> Muted</>}
              </Button>
            </div>
          </div>
          <div style={{ display: 'flex', flexDirection: 'column', gap: '0.5rem' }}>
            <div style={{ padding: '0.5rem 0.75rem', background: 'rgba(255,255,255,0.02)', borderRadius: '0.375rem' }}>
              <strong>Quick test actions</strong>
              <div className="meta" style={{ fontSize: '0.75rem', marginBottom: '0.5rem' }}>Validate sound and OS alert functionality</div>
              <div className="row gap">
                <Button type="button" variant="secondary" size="sm" onClick={() => sendTestNotification()}>
                  <Bell size={14} style={{ marginRight: 4 }} /> Test Alert
                </Button>
                <Button type="button" variant="ghost" size="sm" onClick={() => { clearAlertHistory(); setClearing(c => c + 1) }}>
                  Clear History ({getAlertHistory().length})
                </Button>
              </div>
            </div>
          </div>
        </div>
      </div>

      <div className="grid-2" style={{ marginTop: '1rem' }}>
        {/* Retention form */}
        <div>
          <h3><Shield size={14} style={{ verticalAlign: '-2px', marginRight: 4 }} />Retention</h3>
          <form className="settings-form" onSubmit={saveRetention}>
            {([
              ['events', 'Events (days)', 90],
              ['health', 'Health (days)', 30],
              ['logs', 'Logs (days)', 14],
              ['metrics', 'Metrics (days)', 30],
            ] as const).map(([key, label, dflt]) => (
              <label key={key}>
                {label}
                <input type="number" min={1} max={3650} placeholder={String(dflt)}
                  value={ret[key]} onChange={e => setRet({ ...ret, [key]: e.target.value })} />
              </label>
            ))}
            <Button type="submit" disabled={savingRet}>{savingRet ? 'Saving…' : 'Save retention'}</Button>
          </form>
          <details style={{ marginTop: '0.75rem' }}>
            <summary className="meta">Raw settings JSON</summary>
            <pre>{JSON.stringify(st, null, 2)}</pre>
          </details>
        </div>

        {/* Tokens */}
        <div>
          <h3>Tokens &amp; tools</h3>
          <form className="settings-form" onSubmit={createToken} style={{ marginBottom: '0.75rem' }}>
            <label>
              New token name
              <input type="text" placeholder="relay" required minLength={2}
                value={tokenName} onChange={e => setTokenName(e.target.value)} />
            </label>
            <Button type="submit" disabled={creatingToken}>{creatingToken ? 'Creating…' : 'Create token'}</Button>
          </form>
          {newSecret && (
            <div className="auth-alert success" data-testid="token-secret" style={{ marginBottom: '0.75rem' }}>
              <strong>Copy your token now — it won't be shown again:</strong>
              <pre className="mono" style={{ margin: '0.35rem 0 0', userSelect: 'all', whiteSpace: 'pre-wrap', wordBreak: 'break-all' }}>{newSecret}</pre>
            </div>
          )}
          {tokens.length > 0 && (
            <div className="table-wrap" style={{ marginBottom: '0.75rem' }}>
              <table>
                <thead><tr><th>Name</th><th>Prefix</th><th>Scopes</th><th /></tr></thead>
                <tbody>
                  {tokens.map((t: TokenRow) => (
                    <tr key={t.id}>
                      <td>{t.name}</td>
                      <td className="mono">{t.prefix || t.id.slice(0, 8) + '…'}</td>
                      <td>{(t.scopes || []).map(s => <Badge key={s} variant="outline" style={{ marginRight: 4 }}>{s}</Badge>)}</td>
                      <td>
                        <Button type="button" variant="ghost" size="sm" title="Revoke" onClick={() => revokeToken(t.id)}>
                          <Trash2 size={14} />
                        </Button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
          <div className="row gap">
            <Button type="button" variant="secondary" onClick={async () => {
              try {
                const res = await api('/api/events/reprocess-stale', { method: 'POST' })
                dispatchToast({ id: Math.random().toString(36).slice(2), type: 'info', message: `Queued ${res.queued} events` })
              } catch (err: any) {
                dispatchToast({ id: Math.random().toString(36).slice(2), type: 'error', message: err.message || 'Failed' })
              }
            }}>Reprocess stale</Button>
          </div>
          <p className="meta" style={{ marginTop: 12 }}>
            <a href="/metrics" target="_blank" rel="noreferrer">/metrics</a>
            {' · '}
            <a href="/openapi.json" target="_blank" rel="noreferrer">OpenAPI</a>
          </p>
        </div>
      </div>
    </div>
  )
}
