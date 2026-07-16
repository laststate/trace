// Lightweight unit checks (tsx runner)
import { isView } from './nav'

if (!isView('overview') || isView('xyz')) {
  throw new Error('nav isView failed')
}
console.log('App.test.ts: ok')
