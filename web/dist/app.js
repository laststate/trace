const content = document.getElementById('content');
const crumb = document.getElementById('crumb');
const meta = document.getElementById('meta');

async function api(path) {
  const r = await fetch(path);
  if (!r.ok) throw new Error(await r.text());
  return r.json();
}

function badge(text, cls = '') {
  return `<span class="badge ${cls}">${esc(text)}</span>`;
}

function esc(s) {
  return String(s ?? '').replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
}

function fmtTime(t) {
  if (!t) return '—';
  try { return new Date(t).toLocaleString(); } catch { return t; }
}

async function showOverview() {
  crumb.textContent = 'Overview';
  const o = await api('/api/overview');
  meta.textContent = o.project ? `${o.project.name} · ${o.project.slug}` : '';
  content.innerHTML = `
    <div class="cards">
      <div class="card"><div class="label">Devices</div><div class="value">${o.devices ?? 0}</div></div>
      <div class="card"><div class="label">Open issues</div><div class="value">${o.open_issues ?? 0}</div></div>
      <div class="card"><div class="label">Events (24h)</div><div class="value">${o.events_today ?? 0}</div></div>
    </div>
    <div class="panel">
      <h2>Recent issues</h2>
      <div id="recent-issues" class="empty">Loading…</div>
    </div>`;
  const issues = await api('/api/issues');
  const box = document.getElementById('recent-issues');
  const items = (issues.items || []).slice(0, 8);
  if (!items.length) {
    box.innerHTML = '<div class="empty">No issues yet. Point a Relay at POST /v1/ingest.</div>';
    return;
  }
  box.innerHTML = tableIssues(items);
  bindIssueLinks();
}

function tableIssues(items) {
  return `<table>
    <thead><tr><th>Status</th><th>Severity</th><th>Title</th><th>Events</th><th>Last seen</th></tr></thead>
    <tbody>
      ${items.map(i => `<tr>
        <td>${badge(i.status, i.status)}</td>
        <td>${badge(i.severity, i.severity)}</td>
        <td><a data-issue="${i.id}">${esc(i.title || i.fingerprint)}</a></td>
        <td>${i.event_count}</td>
        <td>${fmtTime(i.last_seen)}</td>
      </tr>`).join('')}
    </tbody>
  </table>`;
}

async function showIssues() {
  crumb.textContent = 'Issues';
  const data = await api('/api/issues');
  const items = data.items || [];
  content.innerHTML = items.length ? tableIssues(items) : '<div class="empty">No issues</div>';
  bindIssueLinks();
}

function bindIssueLinks() {
  content.querySelectorAll('[data-issue]').forEach(a => {
    a.addEventListener('click', () => showIssue(a.getAttribute('data-issue')));
  });
}

async function showIssue(id) {
  crumb.textContent = 'Issue detail';
  const i = await api('/api/issues/' + id);
  content.innerHTML = `
    <div class="panel">
      <div class="row" style="justify-content:space-between">
        <h2 style="margin:0">${esc(i.title)}</h2>
        <div class="row">${badge(i.status, i.status)} ${badge(i.severity, i.severity)}</div>
      </div>
      <p class="mono" style="color:var(--muted)">${esc(i.fingerprint)}</p>
      <p>${esc(i.probable_cause || 'No probable cause yet')}</p>
      <div class="row">
        <span>Events: <b>${i.event_count}</b></span>
        <span>Devices: <b>${i.affected_devices}</b></span>
        <span>First: ${fmtTime(i.first_seen)}</span>
        <span>Last: ${fmtTime(i.last_seen)}</span>
      </div>
    </div>
    <div class="panel"><h2>Linked events</h2><div id="issue-events" class="empty">Loading…</div></div>`;
  const events = await api('/api/events');
  const linked = (events.items || []).filter(e => e.issue_id === id);
  const box = document.getElementById('issue-events');
  if (!linked.length) {
    box.innerHTML = '<div class="empty">No linked events (processing?)</div>';
    return;
  }
  box.innerHTML = tableEvents(linked);
  bindEventLinks();
}

function tableEvents(items) {
  return `<table>
    <thead><tr><th>State</th><th>Type</th><th>Severity</th><th>Event ID</th><th>Received</th></tr></thead>
    <tbody>
      ${items.map(e => `<tr>
        <td>${badge(e.state, e.state)}</td>
        <td>${e.type}</td>
        <td>${badge(e.severity, e.severity)}</td>
        <td class="mono"><a data-event="${e.id}">${esc(e.event_id)}</a></td>
        <td>${fmtTime(e.received_at)}</td>
      </tr>`).join('')}
    </tbody>
  </table>`;
}

async function showEvents() {
  crumb.textContent = 'Events';
  const data = await api('/api/events');
  const items = data.items || [];
  content.innerHTML = items.length ? tableEvents(items) : '<div class="empty">No events</div>';
  bindEventLinks();
}

function bindEventLinks() {
  content.querySelectorAll('[data-event]').forEach(a => {
    a.addEventListener('click', () => showEvent(a.getAttribute('data-event')));
  });
}

async function showEvent(id) {
  crumb.textContent = 'Event detail';
  const e = await api('/api/events/' + id);
  let analysis = {};
  let decoded = {};
  try { analysis = typeof e.analysis === 'string' ? JSON.parse(e.analysis) : (e.analysis || {}); } catch {}
  try { decoded = typeof e.decoded === 'string' ? JSON.parse(e.decoded) : (e.decoded || {}); } catch {}
  const frames = analysis.frames || e.frames || [];
  content.innerHTML = `
    <div class="panel">
      <div class="row" style="justify-content:space-between">
        <h2 style="margin:0" class="mono">${esc(e.event_id)}</h2>
        <div class="row">
          ${badge(e.state, e.state)} ${badge(e.severity, e.severity)}
          <a class="btn secondary" href="/api/events/${e.id}/raw">Download LEP</a>
        </div>
      </div>
      <div class="row" style="margin-top:.75rem">
        <span>Type: ${e.type}</span>
        <span>Arch: ${e.architecture}</span>
        <span>Seq: ${e.sequence}</span>
        <span>Dup: ${e.duplicate_count}</span>
        <span>FP: <span class="mono">${esc(e.fingerprint || '—')}</span></span>
      </div>
    </div>
    <div class="panel">
      <h2>Analysis</h2>
      <p>${esc(analysis.summary || analysis.probable_cause || '—')}</p>
      <pre>${esc(JSON.stringify(analysis, null, 2))}</pre>
    </div>
    <div class="panel">
      <h2>Stack / frames</h2>
      <pre>${esc(JSON.stringify(frames, null, 2))}</pre>
    </div>
    <div class="panel">
      <h2>Decoded</h2>
      <pre>${esc(JSON.stringify(decoded, null, 2))}</pre>
    </div>`;
}

async function showDevices() {
  crumb.textContent = 'Devices';
  const data = await api('/api/devices');
  const items = data.items || [];
  if (!items.length) {
    content.innerHTML = '<div class="empty">No devices</div>';
    return;
  }
  content.innerHTML = `<table>
    <thead><tr><th>Status</th><th>Device</th><th>Product</th><th>Firmware</th><th>Build</th><th>Last seen</th></tr></thead>
    <tbody>
      ${items.map(d => `<tr>
        <td>${badge(d.status)}</td>
        <td class="mono">${esc(d.device_id)}</td>
        <td>${esc(d.product)}</td>
        <td>${esc(d.firmware_version)}</td>
        <td class="mono">${esc(d.build_id)}</td>
        <td>${fmtTime(d.last_seen)}</td>
      </tr>`).join('')}
    </tbody>
  </table>`;
}

const views = {
  overview: showOverview,
  issues: showIssues,
  events: showEvents,
  devices: showDevices,
};

document.querySelectorAll('.sidebar button').forEach(btn => {
  btn.addEventListener('click', () => {
    document.querySelectorAll('.sidebar button').forEach(b => b.classList.remove('active'));
    btn.classList.add('active');
    const v = btn.getAttribute('data-view');
    views[v]().catch(err => {
      content.innerHTML = `<div class="empty">Error: ${esc(err.message)}</div>`;
    });
  });
});

showOverview().catch(err => {
  content.innerHTML = `<div class="empty">Error: ${esc(err.message)}</div>`;
});
