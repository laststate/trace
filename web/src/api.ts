const TOKEN_KEY = 'trace_session'

export function token() {
  return localStorage.getItem(TOKEN_KEY) || ''
}
export function setToken(t: string) {
  if (t) localStorage.setItem(TOKEN_KEY, t)
  else localStorage.removeItem(TOKEN_KEY)
}

export async function api(path: string, opts: RequestInit & { body?: any } = {}) {
  const headers: Record<string, string> = { ...(opts.headers as any) }
  if (token()) headers.Authorization = 'Bearer ' + token()
  let body = opts.body
  if (body && !(body instanceof Uint8Array) && typeof body !== 'string' && !(body instanceof ArrayBuffer)) {
    headers['Content-Type'] = 'application/json'
    body = JSON.stringify(body)
  }
  const r = await fetch(path, { ...opts, headers, body })
  if (!r.ok) throw new Error(await r.text())
  const ct = r.headers.get('content-type') || ''
  if (ct.includes('application/json')) return r.json()
  return r
}
