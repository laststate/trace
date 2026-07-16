const content = document.getElementById('content');
const crumb = document.getElementById('crumb');
const meta = document.getElementById('meta');
const authFoot = document.getElementById('auth-foot');
const TOKEN_KEY = 'trace_session';

function token() { return localStorage.getItem(TOKEN_KEY) || ''; }
function setToken(t) { if (t) localStorage.setItem(TOKEN_KEY, t); else localStorage.removeItem(TOKEN_KEY); }

async function api(path, opts = {}) {
  const headers = Object.assign({}, opts.headers || {});
  if (token()) headers['Authorization'] = 'Bearer ' + token();
  if (opts.body && !(opts.body instanceof FormData) && !(opts.body instanceof ArrayBuffer) && !(opts.body instanceof Uint8Array) && typeof opts.body !== 'string') {
    headers['Content-Type'] = 'application/json';
    opts.body = JSON.stringify(opts.body);
  }
  const r = await fetch(path, Object.assign({}, opts, { headers }));
  if (!r.ok) {
    const t = await r.text();
    throw new Error(t || r.statusText);
  }
  const ct = r.headers.get('content-type') || '';
  if (ct.includes('application/json')) return r.json();
  return r;
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

async function refreshAuth() {
  if (!token()) {
    authFoot.textContent = 'v0.2 · guest';
    return;
  }
  try {
    const me = await api('/api/me');
    authFoot.textContent = `${me.email} · ${me.role}`;
  } catch {
    setToken('');
    authFoot.textContent = 'v0.2 · guest';
  }
}

document.getElementById('login-btn').onclick = async () => {
  if (token()) {
    setToken('');
    await refreshAuth();
    return;
  }
  const email = prompt('Email', 'admin@localhost');
  const password = prompt('Password', 'admin');
  if (!email) return;
  try {
    const res = await api('/api/auth/login', { method: 'POST', body: { email, password } });
    setToken(res.token);
    await refreshAuth();
    alert('Logged in as ' + res.user.email);
  } catch (e) {
    alert('Login failed: ' + e.message);
  }
};

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
    <div class="panel"><h2>Recent issues</h2><div id="recent-issues" class="empty">Loading…</div></div>`;
  const issues = await api('/api/issues');
  const box = document.getElementById('recent-issues');
  const items = (issues.items || []).slice(0, 8);
  box.innerHTML = items.length ? tableIssues(items) : '<div class="empty">No issues yet.</div>';
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
  content.innerHTML = (data.items || []).length ? tableIssues(data.items) : '<div class="empty">No issues</div>';
  bindIssueLinks();
}

function bindIssueLinks() {
  content.querySelectorAll('[data-issue]').forEach(a => a.addEventListener('click', () => showIssue(a.getAttribute('data-issue'))));
}

async function showIssue(id) {
  crumb.textContent = 'Issue detail';
  const data = await api('/api/issues/' + id);
  const i = data.issue || data;
  const events = data.events || [];
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
      <div class="row" style="margin-top:.75rem">
        ${['open','investigating','resolved','ignored','archived'].map(s =>
          `<button class="btn secondary" data-status="${s}">${s}</button>`).join('')}
      </div>
    </div>
    <div class="panel"><h2>Events</h2><div id="issue-events"></div></div>
    <div class="panel"><h2>Latest analysis</h2><pre id="issue-analysis">—</pre></div>`;
  document.getElementById('issue-events').innerHTML = events.length ? tableEvents(events) : '<div class="empty">No events</div>';
  bindEventLinks();
  if (events[0] && events[0].analysis) {
    try {
      const a = typeof events[0].analysis === 'string' ? JSON.parse(events[0].analysis) : events[0].analysis;
      document.getElementById('issue-analysis').textContent = JSON.stringify(a, null, 2);
    } catch {}
  }
  content.querySelectorAll('[data-status]').forEach(btn => {
    btn.onclick = async () => {
      try {
        await api('/api/issues/' + id + '/status', { method: 'POST', body: { status: btn.getAttribute('data-status') } });
        showIssue(id);
      } catch (e) { alert(e.message); }
    };
  });
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
  content.innerHTML = (data.items || []).length ? tableEvents(data.items) : '<div class="empty">No events</div>';
  bindEventLinks();
}
function bindEventLinks() {
  content.querySelectorAll('[data-event]').forEach(a => a.addEventListener('click', () => showEvent(a.getAttribute('data-event'))));
}

async function showEvent(id) {
  crumb.textContent = 'Event detail';
  const e = await api('/api/events/' + id);
  let analysis = {}, decoded = {}, frames = [];
  try { analysis = typeof e.analysis === 'string' ? JSON.parse(e.analysis) : (e.analysis || {}); } catch {}
  try { decoded = typeof e.decoded === 'string' ? JSON.parse(e.decoded) : (e.decoded || {}); } catch {}
  try { frames = typeof e.frames === 'string' ? JSON.parse(e.frames) : (e.frames || analysis.frames || []); } catch {}
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
        <span>Type: ${e.type}</span><span>Arch: ${e.architecture}</span>
        <span>Seq: ${e.sequence}</span><span>Dup: ${e.duplicate_count}</span>
        <span>FP: <span class="mono">${esc(e.fingerprint || '—')}</span></span>
      </div>
    </div>
    <div class="panel"><h2>Analysis</h2><p>${esc(analysis.summary || analysis.probable_cause || '—')}</p><pre>${esc(JSON.stringify(analysis, null, 2))}</pre></div>
    <div class="panel"><h2>Stack / frames</h2><pre>${esc(JSON.stringify(frames, null, 2))}</pre></div>
    <div class="panel"><h2>Decoded</h2><pre>${esc(JSON.stringify(decoded, null, 2))}</pre></div>`;
}

async function showDevices() {
  crumb.textContent = 'Devices';
  const data = await api('/api/devices');
  const items = data.items || [];
  if (!items.length) { content.innerHTML = '<div class="empty">No devices</div>'; return; }
  content.innerHTML = `<table>
    <thead><tr><th>Status</th><th>Device</th><th>Product</th><th>Firmware</th><th>Build</th><th>Last seen</th></tr></thead>
    <tbody>
      ${items.map(d => `<tr>
        <td>${badge(d.status)}</td>
        <td class="mono"><a data-device="${d.id}">${esc(d.device_id)}</a></td>
        <td>${esc(d.product)}</td>
        <td>${esc(d.firmware_version)}</td>
        <td class="mono">${esc(d.build_id)}</td>
        <td>${fmtTime(d.last_seen)}</td>
      </tr>`).join('')}
    </tbody>
  </table>`;
  content.querySelectorAll('[data-device]').forEach(a => a.addEventListener('click', async () => {
    const d = await api('/api/devices/' + a.getAttribute('data-device'));
    crumb.textContent = 'Device detail';
    content.innerHTML = `<div class="panel"><h2 class="mono">${esc(d.device.device_id)}</h2>
      <pre>${esc(JSON.stringify(d.device, null, 2))}</pre></div>
      <div class="panel"><h2>Events</h2>${(d.events||[]).length ? tableEvents(d.events) : '<div class="empty">None</div>'}</div>`;
    bindEventLinks();
  }));
}

async function showReleases() {
  crumb.textContent = 'Releases';
  const data = await api('/api/releases');
  const items = data.items || [];
  if (!items.length) { content.innerHTML = '<div class="empty">No releases</div>'; return; }
  content.innerHTML = `<table>
    <thead><tr><th>Status</th><th>Version</th><th>Build ID</th></tr></thead>
    <tbody>${items.map(r => `<tr>
      <td>${badge(r.status)}</td><td>${esc(r.version)}</td><td class="mono">${esc(r.build_id)}</td>
    </tr>`).join('')}</tbody></table>`;
}

async function showArtifacts() {
  crumb.textContent = 'Artifacts';
  const data = await api('/api/artifacts');
  const items = data.items || [];
  content.innerHTML = `
    <div class="panel">
      <h2>Upload ELF</h2>
      <input type="file" id="art-file" />
      <button class="btn" id="art-upload" type="button">Upload</button>
      <p class="meta" style="color:var(--muted)">Needs session with maintainer+ (or set TRACE_OPEN_UI writes via login).</p>
    </div>
    <div class="panel"><h2>Catalog</h2>
      ${items.length ? `<table>
        <thead><tr><th>Status</th><th>Build ID</th><th>Arch</th><th>SHA256</th></tr></thead>
        <tbody>${items.map(a => `<tr>
          <td>${badge(a.status, a.status)}</td>
          <td class="mono">${esc(a.build_id)}</td>
          <td>${esc(a.architecture)}</td>
          <td class="mono">${esc((a.sha256||'').slice(0,16))}…</td>
        </tr>`).join('')}</tbody></table>` : '<div class="empty">No artifacts</div>'}
    </div>`;
  document.getElementById('art-upload').onclick = async () => {
    const f = document.getElementById('art-file').files[0];
    if (!f) return alert('pick a file');
    const buf = await f.arrayBuffer();
    try {
      await api('/api/artifacts', {
        method: 'POST',
        headers: { 'Content-Type': 'application/octet-stream' },
        body: new Uint8Array(buf),
      });
      showArtifacts();
    } catch (e) { alert(e.message); }
  };
}

async function showAudit() {
  crumb.textContent = 'Audit';
  try {
    const data = await api('/api/audit');
    const items = data.items || [];
    content.innerHTML = items.length ? `<table>
      <thead><tr><th>When</th><th>Actor</th><th>Action</th><th>Target</th></tr></thead>
      <tbody>${items.map(a => `<tr>
        <td>${fmtTime(a.created_at)}</td>
        <td>${esc(a.actor || '—')}</td>
        <td>${esc(a.action)}</td>
        <td class="mono">${esc(a.target_type)} ${esc(a.target_id)}</td>
      </tr>`).join('')}</tbody></table>` : '<div class="empty">No audit entries</div>';
  } catch (e) {
    content.innerHTML = `<div class="empty">Audit requires admin session. ${esc(e.message)}</div>`;
  }
}

async function showSettings() {
  crumb.textContent = 'Settings';
  content.innerHTML = `
    <div class="panel">
      <h2>Create ingest token</h2>
      <input id="tok-name" placeholder="name" value="relay" />
      <button class="btn" id="tok-create" type="button">Create</button>
      <pre id="tok-out"></pre>
    </div>
    <div class="panel">
      <h2>Metrics</h2>
      <a href="/metrics" target="_blank">/metrics</a>
    </div>`;
  document.getElementById('tok-create').onclick = async () => {
    try {
      const res = await api('/api/tokens', { method: 'POST', body: { name: document.getElementById('tok-name').value || 'relay' } });
      document.getElementById('tok-out').textContent = 'Secret (once): ' + res.secret + '\n' + JSON.stringify(res.token, null, 2);
    } catch (e) { alert(e.message); }
  };
}

const views = {
  overview: showOverview, issues: showIssues, events: showEvents, devices: showDevices,
  releases: showReleases, artifacts: showArtifacts, audit: showAudit, settings: showSettings,
};

document.querySelectorAll('.sidebar button').forEach(btn => {
  btn.addEventListener('click', () => {
    document.querySelectorAll('.sidebar button').forEach(b => b.classList.remove('active'));
    btn.classList.add('active');
    views[btn.getAttribute('data-view')]().catch(err => {
      content.innerHTML = `<div class="empty">Error: ${esc(err.message)}</div>`;
    });
  });
});

refreshAuth();
showOverview().catch(err => {
  content.innerHTML = `<div class="empty">Error: ${esc(err.message)}</div>`;
});
