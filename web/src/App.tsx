import { useCallback, useEffect, useState } from 'react'
import { Link, Navigate, Route, Routes, useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { api, setToken, token } from './api'
import {
  AreaChart, DualAreaChart, DualLineChart, StackedBarChart, MiniBarSpark,
  Donut, HBarList, Sparkline, sevClass, issueCode, COLORS, fmtCompact,
  seriesToCSV, downloadText,
  type StackedBar,
} from './charts'
import { ChevronRight, LogIn, LogOut, NavIcon, Search } from './icons'
import { BootSplash, BrandLogo, Loading } from './Loading'
import { NAV, type View, isView } from './nav'

export default function App() {
  return (
    <Routes>
      <Route path="/" element={<Navigate to="/overview" replace />} />
      <Route path="/:view" element={<Shell />} />
      <Route path="/:view/:id" element={<Shell />} />
      <Route path="*" element={<Navigate to="/overview" replace />} />
    </Routes>
  )
}

function Shell() {
  const { view: viewParam, id } = useParams()
  const view: View = isView(viewParam) ? viewParam : 'overview'
  const detailId = id || ''
  const navigate = useNavigate()
  const [searchParams, setSearchParams] = useSearchParams()

  const [who, setWho] = useState('guest')
  const [data, setData] = useState<any>(null)
  const [err, setErr] = useState('')
  const [detail, setDetail] = useState<any>(null)
  const [loading, setLoading] = useState(true)
  const [q, setQ] = useState(searchParams.get('q') || '')
  const [filter, setFilter] = useState({
    status: searchParams.get('status') || '',
    severity: searchParams.get('severity') || '',
    q: searchParams.get('filter') || '',
  })
  const [page, setPage] = useState(Number(searchParams.get('page') || 0))
  const [search, setSearch] = useState<any>(null)
  const [loginOpen, setLoginOpen] = useState(false)
  const [loginEmail, setLoginEmail] = useState('admin@localhost')
  const [loginPass, setLoginPass] = useState('')
  /** True until first successful payload (or hard error) — full-screen brand splash. */
  const [booting, setBooting] = useState(true)
  const [liveAt, setLiveAt] = useState<number>(0)
  const limit = 25

  const go = useCallback((v: View, itemId?: string) => {
    navigate(itemId ? `/${v}/${itemId}` : `/${v}`)
  }, [navigate])

  async function refreshAuth() {
    if (!token()) { setWho('guest'); return }
    try {
      const me = await api('/api/me')
      setWho(`${me.email} · ${me.role}`)
    } catch { setToken(''); setWho('guest') }
  }

  useEffect(() => { refreshAuth() }, [])

  // Sync filters → URL (shareable)
  useEffect(() => {
    const sp = new URLSearchParams()
    if (page > 0) sp.set('page', String(page))
    if (filter.status) sp.set('status', filter.status)
    if (filter.severity) sp.set('severity', filter.severity)
    if (filter.q) sp.set('filter', filter.q)
    setSearchParams(sp, { replace: true })
  }, [page, filter.status, filter.severity, filter.q])

  // Initial + navigation load (full splash only while booting / no data yet)
  useEffect(() => {
    let cancelled = false
    setErr(''); setSearch(null)
    const hasContent = !!(data || detail)
    if (!hasContent) setLoading(true)
    const started = Date.now()
    ;(async () => {
      try {
        if (detailId) {
          const d = await loadDetail(view, detailId)
          if (!cancelled) { setDetail(d); setData(null); setLiveAt(Date.now()) }
        } else {
          const d = await load(view, page, filter, limit)
          if (!cancelled) { setData(d); setDetail(null); setLiveAt(Date.now()) }
        }
      } catch (e: any) {
        if (!cancelled) setErr(String(e.message || e))
      } finally {
        if (!cancelled) {
          // Keep splash visible at least ~700ms so GIF doesn't flash
          const wait = Math.max(0, 700 - (Date.now() - started))
          if (wait > 0) await new Promise(r => setTimeout(r, wait))
          if (!cancelled) {
            setLoading(false)
            setBooting(false)
          }
        }
      }
    })()
    return () => { cancelled = true }
  }, [view, detailId, page, filter.status, filter.severity, filter.q])

  // Live polling — refresh overview (and current list) without full-screen splash
  useEffect(() => {
    if (booting || search || loginOpen) return
    const intervalMs = view === 'overview' ? 5000 : 15000
    const t = window.setInterval(async () => {
      try {
        if (detailId) {
          const d = await loadDetail(view, detailId)
          setDetail(d)
        } else {
          const d = await load(view, page, filter, limit)
          setData(d)
        }
        setLiveAt(Date.now())
        setErr('')
      } catch {
        // keep previous data on poll failure
      }
    }, intervalMs)
    return () => window.clearInterval(t)
  }, [booting, view, detailId, page, filter.status, filter.severity, filter.q, search, loginOpen])

  async function doLogin() {
    try {
      const res = await api('/api/auth/login', { method: 'POST', body: { email: loginEmail, password: loginPass } })
      setToken(res.token)
      setLoginOpen(false)
      setLoginPass('')
      await refreshAuth()
      // reload current view with session
      setPage(p => p)
      navigate(0)
    } catch (e: any) { alert(e.message) }
  }

  async function doSearch() {
    if (!q.trim()) return
    try { setSearch(await api('/api/search?q=' + encodeURIComponent(q.trim()))) }
    catch (e: any) { setErr(e.message) }
  }

  const total = data?.total ?? 0
  const pages = Math.max(1, Math.ceil(total / limit))
  const showBoot = booting || (loading && !data && !detail && !err)

  if (showBoot) {
    return <BootSplash label="Loading dashboard…" />
  }

  return (
    <div className="app">
      <a className="skip-link" href="#main">Skip to content</a>
      <aside className="sidebar" aria-label="Main">
        <div className="brand">
          <BrandLogo size={28} />
          <span className="brand-text">Last State <em>Trace</em></span>
        </div>
        <nav>
          {NAV.map(item => (
            <div key={item.id}>
              {item.section && <div className="nav-section">{item.section}</div>}
              <Link
                to={`/${item.id}`}
                className={view === item.id && !detailId ? 'nav-link active' : 'nav-link'}
                aria-current={view === item.id ? 'page' : undefined}
                onClick={() => setPage(0)}
              >
                <NavIcon view={item.id} />
                <span>{item.label}</span>
              </Link>
            </div>
          ))}
        </nav>
        <div className="side-foot">{who}</div>
      </aside>
      <main id="main">
        <header className="topbar">
          <nav className="breadcrumbs" aria-label="Breadcrumb">
            <Link to="/overview">Trace</Link>
            <ChevronRight size={14} strokeWidth={1.75} className="bc-sep" aria-hidden />
            <Link to={`/${view}`} style={{ color: detailId ? 'var(--muted)' : 'var(--text)' }}>{view}</Link>
            {detailId && (
              <>
                <ChevronRight size={14} strokeWidth={1.75} className="bc-sep" aria-hidden />
                <span className="mono">{detailId.slice(0, 8)}…</span>
              </>
            )}
          </nav>
          <div className="row search-row">
            <div className="search-field">
              <Search size={15} strokeWidth={1.75} aria-hidden />
              <input data-testid="global-search" aria-label="Search" placeholder="Search issues, events, devices…" value={q}
                onChange={e => setQ(e.target.value)} onKeyDown={e => e.key === 'Enter' && doSearch()} />
            </div>
            <button type="button" className="btn secondary" onClick={doSearch}>Search</button>
            <button type="button" className="btn secondary" data-testid="auth-btn" onClick={async () => {
              if (token()) {
                try { await api('/api/auth/logout', { method: 'POST' }) } catch { /* */ }
                setToken(''); await refreshAuth(); navigate('/overview')
              } else setLoginOpen(true)
            }}>
              {token() ? <><LogOut size={15} strokeWidth={1.75} /> Logout</> : <><LogIn size={15} strokeWidth={1.75} /> Login</>}
            </button>
            <a className="btn ghost" href="/api/auth/oidc/login">OIDC</a>
          </div>
        </header>
        <section className="content">
          {loginOpen && (
            <div className="panel login-panel" role="dialog" aria-label="Login" data-testid="login-dialog">
              <h2>Sign in to Trace</h2>
              <label>Email <input data-testid="login-email" type="email" value={loginEmail} onChange={e => setLoginEmail(e.target.value)} /></label>
              <label>Password <input data-testid="login-password" type="password" value={loginPass} onChange={e => setLoginPass(e.target.value)} onKeyDown={e => e.key === 'Enter' && doLogin()} /></label>
              <div className="row gap">
                <button type="button" className="btn" data-testid="login-submit" onClick={doLogin}>Continue</button>
                <button type="button" className="btn secondary" onClick={() => setLoginOpen(false)}>Cancel</button>
              </div>
              <p className="meta">First-boot password: <code>data/bootstrap-admin.txt</code></p>
            </div>
          )}
          {liveAt > 0 && (
            <div className="live-pill" title="Auto-refresh enabled">
              <span className="live-dot" aria-hidden />
              Live · {new Date(liveAt).toLocaleTimeString()}
            </div>
          )}
          {err && <div className="empty" role="alert" data-testid="error-banner">Error: {err}</div>}
          {search && (
            <div className="panel" style={{ marginBottom: '1rem' }}>
              <div className="panel-head"><h2>Search results</h2>
                <button type="button" className="btn secondary" onClick={() => setSearch(null)}>Close</button>
              </div>
              <SearchResults data={search} onOpen={(v, itemId) => { setSearch(null); go(v, itemId) }} />
            </div>
          )}
          {loading && !search && !data && !detail && <Loading label="Updating…" />}
          {!err && !search && detail && (
            <Detail view={view} data={detail} onBack={() => go(view)} onNavigate={go}
              reload={async () => { setLoading(true); try { setDetail(await loadDetail(view, detailId)) } catch (e: any) { setErr(e.message) } finally { setLoading(false) } }} />
          )}
          {!err && !search && !detail && data && (
            <>
              {['issues', 'events', 'devices'].includes(view) && (
                <div className="filters" role="search">
                  <label>Status <input value={filter.status} onChange={e => setFilter({ ...filter, status: e.target.value })} placeholder="open" /></label>
                  <label>Severity <input value={filter.severity} onChange={e => setFilter({ ...filter, severity: e.target.value })} placeholder="fatal" /></label>
                  <label>Filter <input value={filter.q} onChange={e => setFilter({ ...filter, q: e.target.value })} placeholder="text" /></label>
                  <button type="button" className="btn secondary" onClick={() => setPage(0)}>Apply</button>
                </div>
              )}
              <ViewBody view={view} data={data} onOpen={(itemId) => go(view, itemId)} go={go}
                reload={async () => {
                  try {
                    const d = await load(view, page, filter, limit)
                    setData(d)
                    setLiveAt(Date.now())
                  } catch (e: any) { setErr(e.message) }
                }} />
              {total > 0 && (
                <div className="pager" aria-label="Pagination">
                  <button type="button" className="btn secondary" disabled={page <= 0} onClick={() => setPage(p => p - 1)}>Prev</button>
                  <span>Page {page + 1} / {pages} · {total} total</span>
                  <button type="button" className="btn secondary" disabled={page + 1 >= pages} onClick={() => setPage(p => p + 1)}>Next</button>
                </div>
              )}
            </>
          )}
        </section>
      </main>
    </div>
  )
}

async function load(v: View, pageN: number, f: { status: string; severity: string; q: string }, limit: number) {
  const qs = new URLSearchParams({ limit: String(limit), offset: String(pageN * limit) })
  if (f.status) qs.set('status', f.status)
  if (f.severity) qs.set('severity', f.severity)
  if (f.q) qs.set('q', f.q)
  switch (v) {
    case 'overview': return api('/api/overview')
    case 'issues': return api('/api/issues?' + qs)
    case 'events': return api('/api/events?' + qs)
    case 'devices': return api('/api/devices?' + qs)
    case 'releases': return api('/api/releases')
    case 'artifacts': return api('/api/artifacts')
    case 'alerts': return api('/api/alerts')
    case 'channels': return api('/api/channels')
    case 'relays': return api('/api/relays')
    case 'projects': return api('/api/projects')
    case 'hardware': return api('/api/hardware/compare')
    case 'boots': return api('/api/boots')
    case 'dead': return api('/api/jobs/dead')
    case 'audit': return api('/api/audit')
    case 'settings': return Promise.all([api('/api/bootstrap'), api('/api/settings').catch(() => null)]).then(([b, s]) => ({ ...b, settings: s }))
  }
}

async function loadDetail(v: View, id: string) {
  switch (v) {
    case 'issues': return api('/api/issues/' + id)
    case 'events': return api('/api/events/' + id)
    case 'devices': {
      const d = await api('/api/devices/' + id)
      const hist = await api('/api/devices/' + id + '/firmware-history').catch(() => ({ items: [] }))
      return { ...d, firmware_history: hist.items }
    }
    case 'releases': {
      const r = await api('/api/releases/' + id)
      const stats = await api('/api/releases/' + id + '/stats').catch(() => null)
      return { release: r, stats }
    }
    default: return null
  }
}

function SearchResults({ data, onOpen }: { data: any; onOpen: (v: View, id: string) => void }) {
  return (
    <div className="search-grid">
      {(['issues', 'events', 'devices', 'artifacts'] as const).map(key => (
        <section key={key}>
          <h3 style={{ marginTop: 0 }}>{key}</h3>
          {(data[key] || data[key + '_fts'] || []).map((row: any) => (
            <div key={row.id} style={{ marginBottom: '.35rem' }}>
              <Link to={`/${key === 'artifacts' ? 'events' : key}/${row.id}`}
                onClick={e => { e.preventDefault(); onOpen((key === 'artifacts' ? 'events' : key) as View, row.id) }}>
                {row.title || row.event_id || row.device_id || row.build_id || row.id}
              </Link>
            </div>
          ))}
        </section>
      ))}
    </div>
  )
}

function ViewBody({ view, data, onOpen, go, reload }: {
  view: View; data: any; onOpen: (id: string) => void; go: (v: View, id?: string) => void; reload: () => void
}) {
  if (view === 'overview') return <Overview data={data} go={go} />
  if (view === 'issues') return <IssuesList items={data.items || []} onOpen={onOpen} />
  if (view === 'events') {
    return (
      <div className="table-wrap" data-testid="events-table">
        <table>
          <thead><tr><th>State</th><th>Pipeline</th><th>Event</th><th>Sev</th><th>Received</th></tr></thead>
          <tbody>
            {(data.items || []).map((e: any) => (
              <tr key={e.id} style={{ cursor: 'pointer' }} onClick={() => onOpen(e.id)}>
                <td><span className="badge">{e.state}</span></td>
                <td><span className="tag">{e.pipeline || 'issue'}</span></td>
                <td className="mono"><Link to={`/events/${e.id}`} onClick={ev => { ev.preventDefault(); onOpen(e.id) }}>{e.event_id}</Link></td>
                <td><span className={`badge sev-${e.severity}`}>{e.severity}</span></td>
                <td className="meta">{fmtTime(e.received_at)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    )
  }
  if (view === 'devices') {
    return (
      <div className="table-wrap">
        <table>
          <thead><tr><th>Device</th><th>Status</th><th>Product</th><th>Firmware</th><th>Last seen</th></tr></thead>
          <tbody>
            {(data.items || []).map((d: any) => (
              <tr key={d.id} style={{ cursor: 'pointer' }} onClick={() => onOpen(d.id)}>
                <td className="mono">{d.device_id}</td>
                <td><span className={`badge ${sevClass(d.status)}`}>{d.status}</span></td>
                <td>{d.product || '—'}</td>
                <td className="mono">{d.firmware_version || '—'}</td>
                <td className="meta">{fmtTime(d.last_seen)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    )
  }
  if (view === 'boots' || view === 'dead') {
    const items = data.items || []
    return (
      <div className="table-wrap">
        <table>
          <thead>
            <tr>
              {view === 'boots' && <><th>Boot ID</th><th>Device</th><th>Events</th><th>Last</th></>}
              {view === 'dead' && <><th>Type</th><th>Attempts</th><th>Error</th><th /></>}
            </tr>
          </thead>
          <tbody>
            {items.map((row: any) => (
              <tr key={row.id}>
                {view === 'boots' && <>
                  <td className="mono">{row.boot_id}</td>
                  <td className="mono">{row.device_id || '—'}</td>
                  <td>{row.event_count}</td>
                  <td className="meta">{fmtTime(row.last_event_at)}</td>
                </>}
                {view === 'dead' && <>
                  <td>{row.type}</td><td>{row.attempts}</td>
                  <td className="meta">{row.last_error}</td>
                  <td><button type="button" className="btn secondary" onClick={async () => {
                    await api('/api/jobs/dead/' + row.id + '/requeue', { method: 'POST' }); reload()
                  }}>Requeue</button></td>
                </>}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    )
  }
  if (['releases', 'artifacts', 'alerts', 'channels', 'relays', 'projects', 'audit', 'hardware'].includes(view)) {
    const items = data.items || []
    return (
      <div className="table-wrap">
        <table>
          <thead>
            <tr>
              {view === 'releases' && <><th>Version</th><th>Build</th><th>Commit</th><th>Status</th></>}
              {view === 'artifacts' && <><th>Build</th><th>Arch</th><th>Status</th><th>SHA</th></>}
              {view === 'alerts' && <><th>Name</th><th>Kind</th><th>Channel</th><th>Cooldown</th></>}
              {view === 'channels' && <><th>Name</th><th>Kind</th><th>Enabled</th></>}
              {view === 'relays' && <><th>Relay</th><th>Version</th><th>Status</th><th>Heartbeat</th></>}
              {view === 'projects' && <><th>Name</th><th>Slug</th><th>Description</th></>}
              {view === 'audit' && <><th>When</th><th>Actor</th><th>Action</th><th>Target</th></>}
              {view === 'hardware' && <><th>Revision</th><th>Fatal</th><th>Events</th></>}
            </tr>
          </thead>
          <tbody>
            {items.map((row: any, idx: number) => (
              <tr key={row.id || idx}>
                {view === 'releases' && <><td><Link to={`/releases/${row.id}`}>{row.version}</Link></td><td className="mono">{row.build_id}</td><td className="mono">{row.git_commit || '—'}</td><td><span className="badge">{row.status}</span></td></>}
                {view === 'artifacts' && <><td className="mono">{row.build_id}</td><td>{row.architecture}</td><td><span className="badge">{row.status}</span></td><td className="mono">{(row.sha256 || '').slice(0, 12)}</td></>}
                {view === 'alerts' && <><td>{row.name}</td><td>{row.kind}</td><td>{row.channel}</td><td>{row.cooldown_sec}s</td></>}
                {view === 'channels' && <><td>{row.name}</td><td>{row.kind}</td><td>{String(row.enabled)}</td></>}
                {view === 'relays' && <><td className="mono">{row.relay_id}</td><td>{row.version}</td><td><span className="badge">{row.status}</span></td><td className="meta">{fmtTime(row.last_heartbeat)}</td></>}
                {view === 'projects' && <><td>{row.name}</td><td className="mono">{row.slug}</td><td className="meta">{row.description}</td></>}
                {view === 'audit' && <><td className="meta">{fmtTime(row.created_at)}</td><td>{row.actor || '—'}</td><td>{row.action}</td><td className="mono">{row.target_type}</td></>}
                {view === 'hardware' && <><td>{row.revision}</td><td>{row.fatal_events}</td><td>{row.events}</td></>}
              </tr>
            ))}
          </tbody>
        </table>
        {view === 'channels' && (
          <button type="button" className="btn" style={{ marginTop: 12 }} onClick={async () => {
            const kind = prompt('kind: slack|discord|webhook|email', 'slack') || 'slack'
            const url = prompt('webhook_url')
            if (!url) return
            await api('/api/channels', { method: 'POST', body: { kind, name: kind, config: { webhook_url: url } } })
            reload()
          }}>Add channel</button>
        )}
        {view === 'projects' && (
          <button type="button" className="btn" style={{ marginTop: 12 }} onClick={async () => {
            const name = prompt('Project name')
            if (!name) return
            await api('/api/projects', { method: 'POST', body: { name, slug: name.toLowerCase().replace(/\s+/g, '-') } })
            reload()
          }}>New project</button>
        )}
      </div>
    )
  }
  if (view === 'settings') {
    const st = data.settings || {}
    return (
      <div className="panel" data-testid="settings-panel">
        <h2>Project settings</h2>
        <p className="meta">Open UI: {String(data.open_ui)} · Bootstrapped: {String(data.bootstrapped)}</p>
        <div className="grid-2" style={{ marginTop: '1rem' }}>
          <div>
            <h3>Retention</h3>
            <pre>{JSON.stringify(st, null, 2)}</pre>
            <button type="button" className="btn secondary" onClick={async () => {
              await api('/api/settings', { method: 'PUT', body: {
                retention_events_days: Number(prompt('events days', String(st.retention_events_days || 90))),
                retention_health_days: Number(prompt('health days', String(st.retention_health_days || 30))),
                retention_logs_days: Number(prompt('logs days', String(st.retention_logs_days || 14))),
                retention_metrics_days: Number(prompt('metrics days', String(st.retention_metrics_days || 30))),
                analyzer_version_min: st.analyzer_version_min || 1,
              }})
              reload()
            }}>Edit retention</button>
          </div>
          <div>
            <h3>Tokens & tools</h3>
            <pre>{JSON.stringify(data.project || {}, null, 2)}</pre>
            <div className="row gap">
              <button type="button" className="btn" onClick={async () => {
                const res = await api('/api/tokens', { method: 'POST', body: { name: 'relay', scopes: ['event:write', 'event:read', 'artifact:write'] } })
                alert('Secret (once): ' + res.secret)
              }}>Create token</button>
              <button type="button" className="btn secondary" onClick={async () => {
                const res = await api('/api/events/reprocess-stale', { method: 'POST' })
                alert('Queued ' + res.queued)
              }}>Reprocess stale</button>
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
  return null
}

function Overview({ data, go }: { data: any; go: (v: View, id?: string) => void }) {
  const [range, setRange] = useState<'7d' | '14d' | '24h'>('14d')
  const hourly = (data.events_hourly || []).map((h: any) => Number(h.count) || 0)
  let eventsSeries = (data.events_trend || []).map((d: any) => ({ x: d.date, y: Number(d.count) || 0 }))
  let fatalSeries = (data.fatal_trend || []).map((d: any) => ({ x: d.date, y: Number(d.count) || 0 }))
  let issuesSeries = (data.issues_trend || []).map((d: any) => ({ x: d.date, y: Number(d.count) || 0 }))
  if (range === '7d') {
    eventsSeries = eventsSeries.slice(-7)
    fatalSeries = fatalSeries.slice(-7)
    issuesSeries = issuesSeries.slice(-7)
  }
  const hourlySeries = (data.events_hourly || []).map((h: any) => ({
    x: (h.hour || '').slice(11, 16),
    y: Number(h.count) || 0,
  }))

  const sev24 = data.severity_24h || { ok: 0, warn: 0, err: 0 }
  const severityBars: StackedBar[] = (data.severity_hourly || []).map((h: any) => ({
    x: (h.hour || '').slice(11, 16),
    segments: [
      { key: 'ok', y: Number(h.ok) || 0 },
      { key: 'warn', y: Number(h.warn) || 0 },
      { key: 'err', y: Number(h.err) || 0 },
    ],
  }))
  const exceptionBars: StackedBar[] = (data.exception_hourly || []).map((h: any) => ({
    x: (h.hour || '').slice(11, 16),
    segments: [
      { key: 'handled', y: Number(h.handled) || 0 },
      { key: 'unhandled', y: Number(h.unhandled) || 0 },
    ],
  }))
  // Dual line: total events (avg-like) vs fatal (max-like) over 14d
  const dualA = eventsSeries
  const dualB = fatalSeries.length ? fatalSeries : eventsSeries.map((s: { x: string }) => ({ x: s.x, y: 0 }))
  const openCount = Number(data.open_issues) || 0
  const resolvedCount = Number(data.resolved_issues) || 0
  const eventsToday = Number(data.events_today) || 0

  return (
    <div data-testid="overview" className="nw-dashboard">
      <header className="nw-hero">
        <div>
          <h1 className="nw-hero-title">System health</h1>
          <p className="meta">{data.project?.name || 'Project'} · <span className="mono">{data.project?.slug}</span></p>
        </div>
        <div className="nw-hero-meta">
          <div className="range-toggle" role="group" aria-label="Time range">
            {(['24h', '7d', '14d'] as const).map(r => (
              <button key={r} type="button" className={range === r ? 'active' : ''} onClick={() => setRange(r)}>{r}</button>
            ))}
          </div>
          <button type="button" className="btn secondary" onClick={() => downloadText('events-trend.csv', seriesToCSV(eventsSeries, 'events'))}>Export CSV</button>
          <span className="tag">{fmtCompact(data.events_total ?? 0)} events</span>
          <span className="tag">{fmtCompact(data.issues_total ?? 0)} issues</span>
        </div>
      </header>

      <div className="nw-twin">
        <div className="panel nw-panel nw-metric-card">
          <div className="nw-metric-head">
            <div>
              <div className="nw-metric-label">Events</div>
              <div className="nw-metric-big">{fmtCompact(eventsToday)}</div>
            </div>
            <div className="nw-metric-stats">
              <div className="nw-stat">
                <span className="dot" style={{ background: COLORS.nwOk }} />
                <span className="meta">ok</span>
                <strong>{fmtCompact(Number(sev24.ok) || 0)}</strong>
              </div>
              <div className="nw-stat">
                <span className="dot" style={{ background: COLORS.nwWarn }} />
                <span className="meta">warn</span>
                <strong>{fmtCompact(Number(sev24.warn) || 0)}</strong>
              </div>
              <div className="nw-stat">
                <span className="dot" style={{ background: COLORS.nwErr }} />
                <span className="meta">err</span>
                <strong style={{ color: COLORS.nwErr }}>{fmtCompact(Number(sev24.err) || 0)}</strong>
              </div>
            </div>
          </div>
          <div className="nw-chart-slot">
            <StackedBarChart
              bars={severityBars}
              colors={{ ok: COLORS.nwOk, warn: COLORS.nwWarn, err: COLORS.nwErr }}
              height={168}
            />
          </div>
        </div>

        <div className="panel nw-panel nw-metric-card">
          <div className="nw-metric-head">
            <div>
              <div className="nw-metric-label">Fatal trend</div>
              <div className="nw-metric-big" style={{ fontSize: '1.4rem' }}>
                {fmtCompact(Number(data.fatal_open) || 0)}
                <span className="meta" style={{ fontSize: '.8rem', fontWeight: 500 }}> open fatal</span>
              </div>
            </div>
            <div className="nw-metric-stats">
              <div className="nw-stat">
                <span className="dot" style={{ background: COLORS.nwAvg }} />
                <span className="meta">all</span>
                <strong>{fmtCompact(eventsToday)}</strong>
              </div>
              <div className="nw-stat">
                <span className="dot" style={{ background: COLORS.nwMax }} />
                <span className="meta">fatal</span>
                <strong style={{ color: COLORS.nwMax }}>{fmtCompact((fatalSeries as { y: number }[]).reduce((s, p) => s + p.y, 0))}</strong>
              </div>
            </div>
          </div>
          <div className="nw-chart-slot">
            <DualLineChart
              a={dualA}
              b={dualB}
              labelA="All events"
              labelB="Fatal"
              height={168}
              colorA={COLORS.nwAvg}
              colorB={COLORS.nwMax}
            />
          </div>
        </div>
      </div>

      <div className="nw-exc-row">
        <div className="panel nw-panel nw-metric-card nw-exc-hero">
          <div className="nw-metric-head">
            <div>
              <div className="nw-metric-label">Exceptions</div>
              <div className="nw-exc-headline">
                <strong>{openCount}</strong> exceptions open
                {eventsToday > 0 ? ` · ${fmtCompact(eventsToday)} events in 24h` : ''}
              </div>
              <p className="meta" style={{ margin: '0.2rem 0 0' }}>
                Impacted <strong style={{ color: 'var(--text)', fontWeight: 600 }}>{data.devices ?? 0}</strong> devices
                · {resolvedCount} handled · {openCount} unhandled
                {data.fatal_open ? ` · ${data.fatal_open} fatal` : ''}
              </p>
            </div>
            <Link className="btn secondary" to="/issues">View</Link>
          </div>
          <div className="nw-chart-slot tall">
            <StackedBarChart
              bars={exceptionBars.length ? exceptionBars : severityBars}
              colors={exceptionBars.length
                ? { handled: COLORS.nwHandled, unhandled: COLORS.nwUnhandled }
                : { ok: COLORS.nwOk, warn: COLORS.nwWarn, err: COLORS.nwErr }}
              height={180}
            />
          </div>
          <div className="nw-legend nw-legend-footer">
            <span><i style={{ background: COLORS.nwHandled }} />{resolvedCount} handled</span>
            <span><i style={{ background: COLORS.nwUnhandled }} />{openCount} unhandled</span>
          </div>
        </div>

        <div className="nw-mini-stack">
          <div className="panel nw-panel nw-metric-card nw-mini">
            <div className="nw-metric-label">Devices</div>
            <div className="nw-metric-big sm">{data.devices ?? 0}</div>
            <div className="nw-chart-slot mini">
              <MiniBarSpark values={hourly.length ? hourly : [2, 4, 3, 6, 4, 5, 3, 7, 4, 5]} />
            </div>
            <div className="delta">{data.unhealthy_devices ?? 0} unhealthy</div>
          </div>
          <div className="panel nw-panel nw-metric-card nw-mini">
            <div className="nw-metric-label">New issues</div>
            <div className="nw-metric-big sm">
              {fmtCompact((issuesSeries as { y: number }[]).reduce((s, p) => s + p.y, 0))}
            </div>
            <div className="nw-chart-slot mini">
              <Sparkline
                data={(issuesSeries as { y: number }[]).map(s => s.y).length
                  ? (issuesSeries as { y: number }[]).map(s => s.y)
                  : [1, 2, 1, 3, 2, 4, 2]}
                color={COLORS.nwMax}
              />
            </div>
            <div className="delta">First-seen · 14 days</div>
          </div>
          <div className="panel nw-panel nw-metric-card nw-mini">
            <div className="nw-metric-label">Regressions</div>
            <div className="nw-metric-big sm">{data.regressions ?? 0}</div>
            <div className="nw-chart-slot mini">
              <Sparkline data={hourly.length ? hourly.map((v: number, i: number) => (i % 3 === 0 ? v : 0)) : [0, 1, 0, 2, 0, 1]} color={COLORS.pink} />
            </div>
            <div className="delta">Reopened after resolve</div>
          </div>
        </div>
      </div>

      <div className="grid-2">
        <div className="panel nw-panel nw-chart-panel">
          <div className="panel-head">
            <div>
              <h2>Event volume</h2>
              <p className="meta" style={{ margin: 0 }}>All events vs fatal · 14 days</p>
            </div>
          </div>
          <div className="nw-chart-slot">
            <DualAreaChart a={eventsSeries} b={fatalSeries} labelA="All events" labelB="Fatal" height={200} />
          </div>
        </div>
        <div className="panel nw-panel nw-chart-panel">
          <div className="panel-head">
            <div>
              <h2>Ingest · last 24h</h2>
              <p className="meta" style={{ margin: 0 }}>Hourly density</p>
            </div>
          </div>
          <div className="nw-chart-slot">
            <AreaChart series={hourlySeries} color={COLORS.cyan} fillId="hourFill" height={200} showDots />
          </div>
        </div>
      </div>

      <div className="grid-3">
        <div className="panel nw-panel">
          <div className="panel-head"><h2>Severity</h2></div>
          <Donut items={data.by_severity || []} nameKey="severity" valueKey="count" />
        </div>
        <div className="panel nw-panel">
          <div className="panel-head"><h2>Architecture</h2></div>
          <HBarList items={data.by_architecture || []} nameKey="name" valueKey="count" />
        </div>
        <div className="panel nw-panel">
          <div className="panel-head"><h2>Pipelines</h2></div>
          <HBarList items={data.by_pipeline || []} nameKey="pipeline" valueKey="count" />
        </div>
      </div>

      {/* Exception feed — Nightwatch SKY-### style (TRC-###) */}
      <div className="panel nw-panel">
        <div className="panel-head">
          <div>
            <h2>Exceptions</h2>
            <p className="meta" style={{ margin: 0 }}>
              {openCount} open · impacting devices across releases
            </p>
          </div>
          <Link className="btn secondary" to="/issues">View all</Link>
        </div>
        <div className="nw-exception-list">
          {(data.top_issues || []).map((i: any) => (
            <button type="button" key={i.id} className="nw-exception" onClick={() => go('issues', i.id)}>
              <div className="nw-exception-id mono">{issueCode(i.id)}</div>
              <div className="nw-exception-body">
                <div className="nw-exception-type">
                  <span className={`badge sev-${i.severity || 'error'}`}>{(i.severity || 'error').toUpperCase()}</span>
                  <span className={`badge ${sevClass(i.status)}`}>{i.status}</span>
                </div>
                <div className="nw-exception-title">{i.title}</div>
                <div className="meta">{fmtTime(i.last_seen)} · {i.event_count} events · {i.affected_devices} devices</div>
              </div>
              <div className="nw-exception-spark">
                <Sparkline data={[1, 2, 1, Number(i.event_count) || 1, 2, 3, Number(i.event_count) || 1]} color={i.severity === 'fatal' ? COLORS.red : COLORS.purple} />
              </div>
            </button>
          ))}
          {!(data.top_issues || []).length && (
            <div className="empty">No exceptions yet — point Relay at Trace and send a crash LEP</div>
          )}
        </div>
      </div>

      <div className="panel nw-panel nw-chart-panel">
        <div className="panel-head">
          <div>
            <h2>New issues</h2>
            <p className="meta" style={{ margin: 0 }}>First-seen count · 14 days</p>
          </div>
        </div>
        <div className="nw-chart-slot">
          <AreaChart series={issuesSeries} color={COLORS.pink} fillId="issFill" height={160} />
        </div>
      </div>
    </div>
  )
}

function IssuesList({ items, onOpen }: { items: any[]; onOpen: (id: string) => void }) {
  if (!items.length) return <div className="panel empty" data-testid="issues-empty">No issues match filters</div>
  return (
    <div className="panel nw-panel" style={{ padding: 0, overflow: 'hidden' }} data-testid="issues-list">
      <div className="nw-exception-list">
        {items.map((i: any) => (
          <button type="button" key={i.id} className="nw-exception" onClick={() => onOpen(i.id)}>
            <div className="nw-exception-id mono">{issueCode(i.id)}</div>
            <div className="nw-exception-body">
              <div className="nw-exception-type">
                <span className={`badge sev-${i.severity}`}>{i.severity}</span>
                <span className={`badge ${sevClass(i.status)}`}>{i.status}</span>
                {i.regression_count > 0 && <span className="badge warn">reg ×{i.regression_count}</span>}
              </div>
              <div className="nw-exception-title">{i.title}</div>
              <div className="meta mono">{(i.fingerprint || '').slice(0, 20)} · {fmtTime(i.last_seen)}</div>
            </div>
            <div className="issue-stats" style={{ textAlign: 'right' }}>
              <strong style={{ display: 'block', color: 'var(--text)' }}>{i.event_count}</strong>
              <span className="meta">{i.affected_devices} devices</span>
            </div>
          </button>
        ))}
      </div>
    </div>
  )
}

function IssueDetail({ data, onBack, reload }: { data: any; onBack: () => void; reload: () => void }) {
  const i = data.issue
  const latest = (data.events || [])[0]
  const frames = safeJSON(latest?.frames) || []
  const analysis = safeJSON(latest?.analysis) || {}
  const [suspects, setSuspects] = useState<any[]>([])
  const [replay, setReplay] = useState<any[]>([])
  const [replayIdx, setReplayIdx] = useState(0)

  useEffect(() => {
    let cancelled = false
    ;(async () => {
      try {
        const [s, r] = await Promise.all([
          api('/api/issues/' + i.id + '/suspect-commits').catch(() => ({ items: [] })),
          api('/api/issues/' + i.id + '/replay').catch(() => ({ frames: [] })),
        ])
        if (!cancelled) {
          setSuspects(s.items || [])
          setReplay(r.frames || [])
        }
      } catch { /* */ }
    })()
    return () => { cancelled = true }
  }, [i.id])

  const breadcrumbs = replay.length
    ? replay
    : Array.isArray(analysis.breadcrumbs) ? analysis.breadcrumbs
      : Array.isArray(safeJSON(latest?.decoded)?.breadcrumbs) ? safeJSON(latest?.decoded).breadcrumbs
        : (data.activity || []).map((a: any) => ({
          type: a.action, message: a.body || a.action, timestamp: a.created_at, category: 'activity',
        }))

  return (
    <div data-testid="issue-detail">
      <button type="button" className="btn secondary" onClick={onBack} style={{ marginBottom: 12 }}>← Issues</button>
      <div className="panel nw-panel" style={{ marginBottom: 12 }}>
        <div className="row gap" style={{ justifyContent: 'space-between', alignItems: 'flex-start' }}>
          <div>
            <div className="meta mono" style={{ marginBottom: 6 }}>{issueCode(i.id)}</div>
            <h2 style={{ marginTop: 0 }}>{i.title}</h2>
          </div>
          <div className="row gap">
            <span className={`badge ${sevClass(i.status)}`}>{i.status}</span>
            <span className={`badge sev-${i.severity}`}>{i.severity}</span>
            {i.regression_count > 0 && <span className="badge warn">reg ×{i.regression_count}</span>}
          </div>
        </div>
        <div className="issue-summary">
          <strong>Summary</strong>
          <p style={{ margin: '0.35rem 0 0' }}>{i.probable_cause || analysis.summary || 'No probable cause yet — waiting for symbolicated frames.'}</p>
        </div>
        <div className="kv" style={{ marginTop: 12 }}>
          <div className="kv-row"><span className="k">Owner</span><span className="v">{i.assignee_email || i.assignee || 'Unassigned'}</span></div>
          <div className="kv-row"><span className="k">Fingerprint</span><span className="v mono">{i.fingerprint}</span></div>
          <div className="kv-row"><span className="k">Impact</span><span className="v">{i.event_count} events · {i.affected_devices} devices</span></div>
          <div className="kv-row"><span className="k">First / last</span><span className="v">{fmtTime(i.first_seen)} → {fmtTime(i.last_seen)}</span></div>
          {analysis.unwind_method && <div className="kv-row"><span className="k">Unwind</span><span className="v mono">{analysis.unwind_method}</span></div>}
        </div>
        <div className="row gap" style={{ marginTop: 12 }}>
          {['open', 'investigating', 'resolved', 'ignored'].map(st => (
            <button key={st} type="button" className="btn secondary" onClick={async () => {
              await api('/api/issues/' + i.id + '/status', { method: 'POST', body: { status: st } })
              reload()
            }}>{st}</button>
          ))}
          <button type="button" className="btn secondary" onClick={async () => {
            const email = prompt('Assign to (email)')
            if (!email) return
            await api('/api/issues/' + i.id + '/assign', { method: 'POST', body: { email } })
            reload()
          }}>Assign</button>
        </div>
      </div>
      <div className="detail-grid">
        <div>
          <div className="panel nw-panel" style={{ marginBottom: 12 }}>
            <h3>Stack (latest event)</h3>
            <StackFrames frames={Array.isArray(frames) ? frames : []} />
          </div>
          <div className="panel nw-panel" style={{ marginBottom: 12 }}>
            <div className="panel-head">
              <h3 style={{ margin: 0 }}>Breadcrumb replay</h3>
              {breadcrumbs.length > 0 && (
                <div className="row gap">
                  <button type="button" className="btn secondary" disabled={replayIdx <= 0} onClick={() => setReplayIdx(x => Math.max(0, x - 1))}>Prev</button>
                  <span className="meta">{Math.min(replayIdx + 1, breadcrumbs.length)} / {breadcrumbs.length}</span>
                  <button type="button" className="btn secondary" disabled={replayIdx >= breadcrumbs.length - 1} onClick={() => setReplayIdx(x => Math.min(breadcrumbs.length - 1, x + 1))}>Next</button>
                </div>
              )}
            </div>
            {breadcrumbs.length ? (
              <>
                <div className="issue-summary" style={{ marginBottom: 12 }}>
                  <strong>Frame {replayIdx + 1}</strong>
                  <p style={{ margin: '0.35rem 0 0' }}>
                    {breadcrumbs[replayIdx]?.message || breadcrumbs[replayIdx]?.body || breadcrumbs[replayIdx]?.type || '—'}
                  </p>
                  <div className="meta">{fmtTime(breadcrumbs[replayIdx]?.timestamp || breadcrumbs[replayIdx]?.ts)} · {breadcrumbs[replayIdx]?.category || breadcrumbs[replayIdx]?.type || 'event'}</div>
                </div>
                <div className="timeline">
                  {breadcrumbs.slice(0, 40).map((b: any, idx: number) => (
                    <button type="button" key={idx} className={'timeline-item' + (idx === replayIdx ? ' active' : '')} onClick={() => setReplayIdx(idx)} style={{ width: '100%', textAlign: 'left', background: idx === replayIdx ? 'rgba(255,255,255,0.04)' : 'transparent', border: 0, color: 'inherit', font: 'inherit', cursor: 'pointer' }}>
                      <div className="timeline-dot" />
                      <div>
                        <div className="meta">{fmtTime(b.timestamp || b.ts || b.created_at)} · <span className="tag">{b.category || b.type || 'event'}</span></div>
                        <div>{b.message || b.body || b.type || JSON.stringify(b).slice(0, 120)}</div>
                      </div>
                    </button>
                  ))}
                </div>
              </>
            ) : <div className="meta">No breadcrumbs on latest event</div>}
          </div>
          <div className="panel nw-panel">
            <h3>Events</h3>
            <ul style={{ paddingLeft: '1.1rem' }}>
              {(data.events || []).map((e: any) => (
                <li key={e.id}><Link className="mono" to={`/events/${e.id}`}>{e.event_id}</Link>
                  <span className={`badge sev-${e.severity}`} style={{ marginLeft: 8 }}>{e.severity}</span>
                </li>
              ))}
            </ul>
          </div>
        </div>
        <div>
          <div className="panel nw-panel" style={{ marginBottom: 12 }}>
            <h3>Suspect commits</h3>
            {suspects.length ? (
              <ul style={{ paddingLeft: '1.1rem', margin: 0 }}>
                {suspects.map((c: any, idx: number) => (
                  <li key={idx} style={{ marginBottom: 8 }}>
                    <span className="mono">{(c.sha || '').slice(0, 8)}</span>
                    <span className="meta"> · {c.release || c.build_id}</span>
                    <div>{c.message || 'release head'}</div>
                    {c.author && <div className="meta">{c.author}</div>}
                  </li>
                ))}
              </ul>
            ) : <div className="meta">No commits linked — POST /api/releases/:id/commits from CI</div>}
          </div>
          <div className="panel nw-panel" style={{ marginBottom: 12 }}>
            <h3>Activity</h3>
            <ul style={{ paddingLeft: '1.1rem', margin: 0 }}>
              {(data.activity || []).map((a: any) => (
                <li key={a.id} className="meta" style={{ marginBottom: 6 }}>{fmtTime(a.created_at)} · <strong>{a.action}</strong> {a.body}</li>
              ))}
              {!(data.activity || []).length && <li className="meta">No activity</li>}
            </ul>
          </div>
          <div className="panel nw-panel">
            <h3>Comments</h3>
            {(data.comments || []).map((c: any) => (
              <div key={c.id} style={{ marginBottom: 8 }}><strong>{c.author || 'anon'}</strong><div className="meta">{c.body}</div></div>
            ))}
            <button type="button" className="btn" onClick={async () => {
              const body = prompt('Comment')
              if (!body) return
              await api('/api/issues/' + i.id + '/comments', { method: 'POST', body: { body } })
              reload()
            }}>Add comment</button>
          </div>
        </div>
      </div>
    </div>
  )
}

function Detail({ view, data, onBack, onNavigate, reload }: {
  view: View; data: any; onBack: () => void; onNavigate: (v: View, id?: string) => void; reload: () => void
}) {
  if (view === 'issues' && data.issue) {
    return <IssueDetail data={data} onBack={onBack} reload={reload} />
  }
  if (view === 'events') {
    const e = data.event || data
    const analysis = safeJSON(e.analysis) || {}
    const frames = safeJSON(e.frames) || []
    const decoded = safeJSON(e.decoded) || {}
    return (
      <div data-testid="event-detail">
        <button type="button" className="btn secondary" onClick={onBack} style={{ marginBottom: 12 }}>← Events</button>
        <div className="panel" style={{ marginBottom: 12 }}>
          <h2 className="mono" style={{ marginTop: 0 }}>{e.event_id}</h2>
          <div className="row gap">
            <span className="badge">{e.state}</span>
            <span className={`badge sev-${e.severity}`}>{e.severity}</span>
            <span className="tag">{e.pipeline || 'issue'}</span>
            {analysis.architecture_name && <span className="tag">{analysis.architecture_name}</span>}
          </div>
          {analysis.summary && <p style={{ margin: '.75rem 0' }}>{analysis.summary}</p>}
          <div className="row gap">
            <a className="btn secondary" href={'/api/events/' + e.id + '/raw'}>Download raw</a>
            <button type="button" className="btn" onClick={async () => {
              await api('/api/events/' + e.id + '/reprocess', { method: 'POST' })
              alert('Reprocess queued'); reload()
            }}>Reprocess</button>
          </div>
        </div>
        <div className="detail-grid">
          <div className="panel">
            <h3>Stack frames {analysis.unwind_method && <span className="meta">· {analysis.unwind_method}</span>}</h3>
            <StackFrames frames={Array.isArray(frames) ? frames : []} />
            {analysis.fault_details && (
              <>
                <h3 style={{ marginTop: '1rem' }}>Fault details</h3>
                <div className="kv">
                  {Object.entries(analysis.fault_details).filter(([, v]) => typeof v !== 'object').map(([k, v]) => (
                    <div key={k} className="kv-row"><span className="k">{k}</span><span className="v mono">{String(v)}</span></div>
                  ))}
                </div>
              </>
            )}
            {analysis.stacked_frame && (
              <>
                <h3 style={{ marginTop: '1rem' }}>Stacked frame</h3>
                <pre>{JSON.stringify(analysis.stacked_frame, null, 2)}</pre>
              </>
            )}
            {analysis.causes?.length > 0 && (
              <>
                <h3 style={{ marginTop: '1rem' }}>Causes</h3>
                <ul>{analysis.causes.map((c: string, i: number) => <li key={i}>{c}</li>)}</ul>
              </>
            )}
          </div>
          <div>
            <div className="panel" style={{ marginBottom: 12 }}>
              <h3>Decoded</h3>
              <pre>{JSON.stringify(decoded, null, 2)}</pre>
            </div>
            <div className="panel" style={{ marginBottom: 12 }}>
              <h3>Hex</h3>
              <pre className="hex">{hexDump(JSON.stringify(decoded).slice(0, 256))}</pre>
            </div>
            <div className="panel">
              <h3>History</h3>
              <ul style={{ margin: 0, paddingLeft: '1.1rem' }}>
                {(data.history || []).map((h: any, idx: number) => (
                  <li key={idx} className="meta">{fmtTime(h.created_at)} · {h.from || '∅'} → {h.to}</li>
                ))}
              </ul>
            </div>
          </div>
        </div>
      </div>
    )
  }
  if (view === 'devices' && data.device) {
    const d = data.device
    return (
      <div>
        <button type="button" className="btn secondary" onClick={onBack} style={{ marginBottom: 12 }}>← Devices</button>
        <div className="panel">
          <h2 className="mono" style={{ marginTop: 0 }}>{d.device_id}</h2>
          <div className="row gap">
            <span className={`badge ${sevClass(d.status)}`}>{d.status}</span>
            <span className="meta">{d.product} · rev {d.hardware_revision || '—'}</span>
          </div>
          <div className="kv" style={{ marginTop: 12 }}>
            <div className="kv-row"><span className="k">Firmware</span><span className="v mono">{d.firmware_version || '—'}</span></div>
            <div className="kv-row"><span className="k">Build</span><span className="v mono">{d.build_id || '—'}</span></div>
            <div className="kv-row"><span className="k">Last seen</span><span className="v">{fmtTime(d.last_seen)}</span></div>
          </div>
          <h3>Firmware history</h3>
          <ul>{(data.firmware_history || []).map((h: any, i: number) => (
            <li key={i} className="meta">{h.firmware_version} · {h.build_id} · {fmtTime(h.last_seen)}</li>
          ))}</ul>
          <h3>Recent events</h3>
          <ul>{(data.events || []).map((e: any) => (
            <li key={e.id}><Link className="mono" to={`/events/${e.id}`}>{e.event_id}</Link></li>
          ))}</ul>
        </div>
      </div>
    )
  }
  if (view === 'releases' && data.release) {
    return (
      <div>
        <button type="button" className="btn secondary" onClick={onBack} style={{ marginBottom: 12 }}>← Releases</button>
        <div className="panel">
          <h2 style={{ marginTop: 0 }}>{data.release.version}</h2>
          <p className="mono meta">{data.release.build_id} · {data.release.git_commit || 'no commit'}</p>
          {data.stats && (
            <div className="cards" style={{ marginTop: 16 }}>
              <div className="card ok"><div className="label">Crash-free</div><div className="value">{Number(data.stats.crash_free_rate).toFixed(1)}%</div></div>
              <div className="card"><div className="label">Sessions</div><div className="value">{data.stats.sessions}</div></div>
              <div className="card fatal"><div className="label">Crash sessions</div><div className="value">{data.stats.crash_sessions}</div></div>
            </div>
          )}
        </div>
      </div>
    )
  }
  return (
    <div className="panel">
      <button type="button" className="btn secondary" onClick={onBack}>← Back</button>
      <pre>{JSON.stringify(data, null, 2)}</pre>
    </div>
  )
}

function StackFrames({ frames }: { frames: any[] }) {
  if (!frames?.length) return <p className="meta">No frames</p>
  return (
    <div className="stack">
      {frames.map((f, i) => (
        <div key={i} className="stack-frame">
          <span className="n">{i}</span>
          <span>
            {f.function ? <span className="fn">{f.function}</span> : <span className="meta">unknown</span>}
            {f.file && <span className="meta"> · {f.file}{f.line ? `:${f.line}` : ''}</span>}
          </span>
          <span className="addr">0x{(f.address || 0).toString(16)}</span>
        </div>
      ))}
    </div>
  )
}

function fmtTime(v: any) {
  if (!v) return '—'
  try { return new Date(v).toLocaleString() } catch { return String(v) }
}
function safeJSON(v: any) {
  if (typeof v === 'string') { try { return JSON.parse(v) } catch { return v } }
  return v
}
function hexDump(s: string) {
  const bytes = Array.from(s).map(c => c.charCodeAt(0))
  const lines: string[] = []
  for (let i = 0; i < bytes.length; i += 16) {
    const chunk = bytes.slice(i, i + 16)
    const hex = chunk.map(b => b.toString(16).padStart(2, '0')).join(' ')
    const asc = chunk.map(b => (b >= 32 && b < 127 ? String.fromCharCode(b) : '.')).join('')
    lines.push(i.toString(16).padStart(4, '0') + '  ' + hex.padEnd(48) + '  ' + asc)
  }
  return lines.join('\n')
}
