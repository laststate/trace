import { useEffect, useState } from 'react'
import { api, setToken, token } from './api'

type View = 'overview' | 'issues' | 'events' | 'devices' | 'releases' | 'artifacts' | 'alerts' | 'audit' | 'settings'

export default function App() {
  const [view, setView] = useState<View>('overview')
  const [who, setWho] = useState('guest')
  const [data, setData] = useState<any>(null)
  const [err, setErr] = useState('')
  const [detail, setDetail] = useState<any>(null)

  async function refreshAuth() {
    if (!token()) { setWho('guest'); return }
    try {
      const me = await api('/api/me')
      setWho(`${me.email} · ${me.role}`)
    } catch { setToken(''); setWho('guest') }
  }

  useEffect(() => { refreshAuth() }, [])

  useEffect(() => {
    setErr(''); setDetail(null)
    load(view).then(setData).catch(e => setErr(String(e.message || e)))
  }, [view])

  async function load(v: View) {
    switch (v) {
      case 'overview': return api('/api/overview')
      case 'issues': return api('/api/issues')
      case 'events': return api('/api/events')
      case 'devices': return api('/api/devices')
      case 'releases': return api('/api/releases')
      case 'artifacts': return api('/api/artifacts')
      case 'alerts': return api('/api/alerts')
      case 'audit': return api('/api/audit')
      case 'settings': return api('/api/bootstrap')
    }
  }

  async function login() {
    const email = prompt('Email', 'admin@localhost')
    const password = prompt('Password', 'admin')
    if (!email) return
    try {
      const res = await api('/api/auth/login', { method: 'POST', body: { email, password } })
      setToken(res.token)
      await refreshAuth()
    } catch (e: any) { alert(e.message) }
  }

  const nav: View[] = ['overview', 'issues', 'events', 'devices', 'releases', 'artifacts', 'alerts', 'audit', 'settings']

  return (
    <div className="app">
      <aside className="sidebar">
        <div className="brand">Last State <span>Trace</span></div>
        <nav>
          {nav.map(v => (
            <button key={v} className={view === v ? 'active' : ''} onClick={() => setView(v)}>{v}</button>
          ))}
        </nav>
        <div className="side-foot">{who}</div>
      </aside>
      <main>
        <header className="topbar">
          <div>{view}</div>
          <div className="row">
            <button className="btn secondary" onClick={() => token() ? (setToken(''), refreshAuth()) : login()}>
              {token() ? 'Logout' : 'Login'}
            </button>
            <a className="btn secondary" href="/api/auth/oidc/login">OIDC</a>
          </div>
        </header>
        <section className="content">
          {err && <div className="empty">Error: {err}</div>}
          {!err && detail && <Detail data={detail} onBack={() => setDetail(null)} />}
          {!err && !detail && data && (
            <ViewBody view={view} data={data} onOpen={setDetail} reload={() => load(view).then(setData)} />
          )}
        </section>
      </main>
    </div>
  )
}

function ViewBody({ view, data, onOpen, reload }: { view: View; data: any; onOpen: (d: any) => void; reload: () => void }) {
  if (view === 'overview') {
    return (
      <>
        <div className="cards">
          <div className="card"><div className="label">Devices</div><div className="value">{data.devices ?? 0}</div></div>
          <div className="card"><div className="label">Open issues</div><div className="value">{data.open_issues ?? 0}</div></div>
          <div className="card"><div className="label">Events 24h</div><div className="value">{data.events_today ?? 0}</div></div>
        </div>
        <p className="meta">{data.project?.name} · {data.project?.slug}</p>
      </>
    )
  }
  if (view === 'issues') {
    const items = data.items || []
    return (
      <table>
        <thead><tr><th>Status</th><th>Sev</th><th>Title</th><th>Events</th></tr></thead>
        <tbody>
          {items.map((i: any) => (
            <tr key={i.id}>
              <td><span className="badge">{i.status}</span></td>
              <td><span className="badge">{i.severity}</span></td>
              <td><a onClick={async () => onOpen(await api('/api/issues/' + i.id))}>{i.title}</a></td>
              <td>{i.event_count}</td>
            </tr>
          ))}
        </tbody>
      </table>
    )
  }
  if (view === 'events') {
    const items = data.items || []
    return (
      <table>
        <thead><tr><th>State</th><th>ID</th><th>Sev</th></tr></thead>
        <tbody>
          {items.map((e: any) => (
            <tr key={e.id}>
              <td><span className="badge">{e.state}</span></td>
              <td className="mono"><a onClick={async () => onOpen(await api('/api/events/' + e.id))}>{e.event_id}</a></td>
              <td>{e.severity}</td>
            </tr>
          ))}
        </tbody>
      </table>
    )
  }
  if (view === 'devices' || view === 'releases' || view === 'artifacts' || view === 'alerts' || view === 'audit') {
    return <pre>{JSON.stringify(data.items || data, null, 2)}</pre>
  }
  if (view === 'settings') {
    return (
      <div className="panel">
        <h2>Settings</h2>
        <pre>{JSON.stringify(data, null, 2)}</pre>
        <button className="btn" onClick={async () => {
          const name = prompt('token name', 'relay') || 'relay'
          const res = await api('/api/tokens', { method: 'POST', body: { name } })
          alert('Secret (once): ' + res.secret)
          reload()
        }}>Create ingest token</button>
        <p><a href="/metrics" target="_blank">/metrics</a></p>
        <button className="btn secondary" onClick={async () => {
          const body = {
            name: 'fatal-webhook',
            kind: 'new_fatal_issue',
            channel: 'webhook',
            target_url: prompt('Webhook URL') || '',
            secret: prompt('HMAC secret', 'dev') || 'dev',
          }
          if (!body.target_url) return
          await api('/api/alerts', { method: 'POST', body })
          alert('alert rule created')
        }}>Add fatal webhook alert</button>
      </div>
    )
  }
  return null
}

function Detail({ data, onBack }: { data: any; onBack: () => void }) {
  return (
    <div className="panel">
      <button className="btn secondary" onClick={onBack}>Back</button>
      <pre>{JSON.stringify(data, null, 2)}</pre>
      {data.issue && (
        <div className="row">
          {['open', 'investigating', 'resolved', 'ignored', 'archived'].map(s => (
            <button key={s} className="btn secondary" onClick={async () => {
              await api('/api/issues/' + data.issue.id + '/status', { method: 'POST', body: { status: s } })
              onBack()
            }}>{s}</button>
          ))}
        </div>
      )}
      {data.event_id && (
        <a className="btn secondary" href={`/api/events/${data.id}/raw`}>Download LEP</a>
      )}
    </div>
  )
}
