import React, { useState } from 'react'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'
import { api, setToken } from '../api'
import { BrandLogo } from '../Loading'
import { Button } from '../components/ui/button'
import './auth.css'

type AuthToast = { type: 'success' | 'error' | 'info'; message: string } | null

function AuthShell({ title, subtitle, children, footer }: {
  title: string
  subtitle?: string
  children: React.ReactNode
  footer?: React.ReactNode
}) {
  return (
    <div className="auth-page" data-testid="auth-page">
      <div className="auth-card" data-testid="login-dialog">
        <div className="auth-brand">
          <BrandLogo size={36} />
          <span className="auth-brand-text">Last State <em>Trace</em></span>
        </div>
        <h1>{title}</h1>
        {subtitle && <p className="auth-subtitle">{subtitle}</p>}
        {children}
        {footer && <div className="auth-footer">{footer}</div>}
      </div>
      <Link to="/overview" className="auth-back">← Back to dashboard</Link>
    </div>
  )
}

function AuthError({ toast }: { toast: AuthToast }) {
  if (!toast) return null
  return <div className={`auth-alert ${toast.type}`} role="alert">{toast.message}</div>
}

export function LoginPage() {
  const navigate = useNavigate()
  const [email, setEmail] = useState('admin@localhost')
  const [password, setPassword] = useState('')
  const [busy, setBusy] = useState(false)
  const [toast, setToast] = useState<AuthToast>(null)

  async function submit(e?: React.FormEvent) {
    e?.preventDefault()
    if (busy) return
    setBusy(true)
    setToast(null)
    try {
      const res = await api('/api/auth/login', { method: 'POST', body: { email, password } })
      setToken(res.token)
      window.location.assign('/overview')
    } catch (err: any) {
      setToast({ type: 'error', message: err.message || 'Login failed' })
      setBusy(false)
    }
  }

  return (
    <AuthShell
      title="Sign in to Trace"
      subtitle="Firmware observability for your whole fleet."
      footer={(
        <>
          <span>No account? <Link to="/register">Create one</Link></span>
          <span>·</span>
          <Link to="/forgot-password">Forgot password?</Link>
        </>
      )}
    >
      <form className="auth-form" onSubmit={submit}>
        <label>
          Email
          <input data-testid="login-email" type="email" autoComplete="username" required
            value={email} onChange={e => setEmail(e.target.value)} />
        </label>
        <label>
          Password
          <input data-testid="login-password" type="password" autoComplete="current-password" required
            value={password} onChange={e => setPassword(e.target.value)} />
        </label>
        <AuthError toast={toast} />
        <Button type="submit" data-testid="login-submit" disabled={busy}>
          {busy ? 'Signing in…' : 'Sign in'}
        </Button>
      </form>
      <div className="auth-divider"><span>or</span></div>
      <a className="btn secondary auth-oidc" href="/api/auth/oidc/login">Continue with OIDC</a>
      <p className="auth-hint">First-boot password: <code>data/bootstrap-admin.txt</code></p>
      <button type="button" className="auth-link-btn" onClick={() => navigate('/landing')}>What is Trace? →</button>
    </AuthShell>
  )
}

export function RegisterPage() {
  const [name, setName] = useState('')
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [busy, setBusy] = useState(false)
  const [toast, setToast] = useState<AuthToast>(null)

  async function submit(e?: React.FormEvent) {
    e?.preventDefault()
    if (busy) return
    setBusy(true)
    setToast(null)
    try {
      await api('/api/auth/signup', { method: 'POST', body: { name, email, password } })
      setToast({ type: 'success', message: 'Account created — check your email to verify, then sign in.' })
    } catch (err: any) {
      // Backend may require the invitation flow; surface its guidance verbatim.
      setToast({ type: 'error', message: err.message || 'Signup failed' })
    } finally {
      setBusy(false)
    }
  }

  return (
    <AuthShell
      title="Create your account"
      subtitle="Self-service signup may require an organization invitation."
      footer={<span>Already have an account? <Link to="/login">Sign in</Link></span>}
    >
      <form className="auth-form" onSubmit={submit}>
        <label>
          Name
          <input type="text" autoComplete="name" required minLength={2}
            value={name} onChange={e => setName(e.target.value)} />
        </label>
        <label>
          Email
          <input type="email" autoComplete="username" required
            value={email} onChange={e => setEmail(e.target.value)} />
        </label>
        <label>
          Password <span className="auth-req">(min 12 chars)</span>
          <input type="password" autoComplete="new-password" required minLength={12}
            value={password} onChange={e => setPassword(e.target.value)} />
        </label>
        <AuthError toast={toast} />
        <Button type="submit" disabled={busy}>{busy ? 'Creating…' : 'Create account'}</Button>
      </form>
    </AuthShell>
  )
}

export function ForgotPasswordPage() {
  const [email, setEmail] = useState('')
  const [busy, setBusy] = useState(false)
  const [sent, setSent] = useState(false)

  async function submit(e?: React.FormEvent) {
    e?.preventDefault()
    if (busy) return
    setBusy(true)
    try {
      await api('/api/auth/forgot-password', { method: 'POST', body: { email } })
    } catch { /* server responds success regardless — keep anti-enumeration UX */ }
    setSent(true)
    setBusy(false)
  }

  return (
    <AuthShell
      title="Reset your password"
      subtitle="We'll email you a reset link if the address exists."
      footer={<span>Remembered it? <Link to="/login">Sign in</Link></span>}
    >
      {sent ? (
        <div className="auth-alert success" role="status">
          If the email exists, a reset link has been sent. Check your inbox.
        </div>
      ) : (
        <form className="auth-form" onSubmit={submit}>
          <label>
            Email
            <input type="email" autoComplete="username" required
              value={email} onChange={e => setEmail(e.target.value)} />
          </label>
          <Button type="submit" disabled={busy}>{busy ? 'Sending…' : 'Send reset link'}</Button>
        </form>
      )}
    </AuthShell>
  )
}

export function ResetPasswordPage() {
  const [searchParams] = useSearchParams()
  const [token, setToken] = useState(searchParams.get('token') || '')
  const [password, setPassword] = useState('')
  const [confirm, setConfirm] = useState('')
  const [busy, setBusy] = useState(false)
  const [toast, setToast] = useState<AuthToast>(null)
  const [done, setDone] = useState(false)

  async function submit(e?: React.FormEvent) {
    e?.preventDefault()
    if (password !== confirm) {
      setToast({ type: 'error', message: 'Passwords do not match' })
      return
    }
    if (busy) return
    setBusy(true)
    setToast(null)
    try {
      await api('/api/auth/reset-password', { method: 'POST', body: { token, new_password: password } })
      setDone(true)
    } catch (err: any) {
      setToast({ type: 'error', message: err.message || 'Reset failed' })
    } finally {
      setBusy(false)
    }
  }

  return (
    <AuthShell
      title="Choose a new password"
      footer={<span>Token expired? <Link to="/forgot-password">Request a new one</Link></span>}
    >
      {done ? (
        <div className="auth-alert success" role="status">
          Password reset successful. <Link to="/login">Sign in with your new password →</Link>
        </div>
      ) : (
        <form className="auth-form" onSubmit={submit}>
          <label>
            Reset token
            <input type="text" required className="mono" placeholder="from the reset email"
              value={token} onChange={e => setToken(e.target.value)} />
          </label>
          <label>
            New password <span className="auth-req">(min 12 chars)</span>
            <input type="password" autoComplete="new-password" required minLength={12}
              value={password} onChange={e => setPassword(e.target.value)} />
          </label>
          <label>
            Confirm password
            <input type="password" autoComplete="new-password" required minLength={12}
              value={confirm} onChange={e => setConfirm(e.target.value)} />
          </label>
          <AuthError toast={toast} />
          <Button type="submit" disabled={busy}>{busy ? 'Saving…' : 'Set new password'}</Button>
        </form>
      )}
    </AuthShell>
  )
}
