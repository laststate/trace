/**
 * Pure frontend helpers — run with: npx tsx src/helpers.test.ts
 */

import { isView } from './nav'

export function pathFromParts(view: string, id?: string): string {
  if (!isView(view)) return '/overview'
  return id ? `/${view}/${id}` : `/${view}`
}

export function hexDump(s: string): string {
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

export function buildQuery(params: Record<string, string | number | undefined>): string {
  const qs = new URLSearchParams()
  for (const [k, v] of Object.entries(params)) {
    if (v !== undefined && v !== '') qs.set(k, String(v))
  }
  return qs.toString()
}

export function pageCount(total: number, limit: number): number {
  if (limit <= 0) return 1
  return Math.max(1, Math.ceil(total / limit))
}

function assert(cond: unknown, msg: string) {
  if (!cond) throw new Error(msg)
}

function run() {
  assert(isView('issues'), 'isView issues')
  assert(!isView('nope'), 'isView nope')
  assert(pathFromParts('issues', 'abc') === '/issues/abc', 'path detail')
  assert(pathFromParts('overview') === '/overview', 'path list')
  assert(pathFromParts('bad') === '/overview', 'path fallback')
  assert(hexDump('AB').includes('41 42'), 'hex')
  assert(buildQuery({ limit: 25, offset: 0, status: 'open' }) === 'limit=25&offset=0&status=open', 'qs')
  assert(pageCount(100, 25) === 4, 'pages')
  console.log('helpers.test.ts: ok')
}

run()
