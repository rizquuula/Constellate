import { useState } from 'react'
import { loginTOTP, loginRecovery, passkeyLogin } from '../../api/rest'

type Mode = 'totp' | 'recovery'

interface Props {
  onSuccess: () => void
}

const hasPasskeySupport = typeof window !== 'undefined' && !!window.PublicKeyCredential

export function Login({ onSuccess }: Props) {
  const [mode, setMode] = useState<Mode>('totp')
  const [code, setCode] = useState('')
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)
  const [passkeyLoading, setPasskeyLoading] = useState(false)

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    setError('')
    setLoading(true)
    try {
      if (mode === 'totp') {
        await loginTOTP(code)
      } else {
        await loginRecovery(code)
      }
      onSuccess()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Login failed')
    } finally {
      setLoading(false)
    }
  }

  async function handlePasskeyLogin() {
    setError('')
    setPasskeyLoading(true)
    try {
      await passkeyLogin()
      onSuccess()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Passkey login failed')
    } finally {
      setPasskeyLoading(false)
    }
  }

  return (
    <div className="login-overlay">
      <div className="login-card">
        <h2 className="login-title">Constellate</h2>
        {hasPasskeySupport && (
          <button
            className="login-passkey-btn"
            type="button"
            onClick={handlePasskeyLogin}
            disabled={passkeyLoading || loading}
          >
            {passkeyLoading ? 'Signing in…' : 'Sign in with a passkey'}
          </button>
        )}
        {hasPasskeySupport && <div className="login-divider"><span>or</span></div>}
        <div className="login-tabs" role="tablist">
          <button
            role="tab"
            aria-selected={mode === 'totp'}
            className={`login-tab${mode === 'totp' ? ' login-tab-active' : ''}`}
            onClick={() => { setMode('totp'); setCode(''); setError('') }}
          >
            TOTP Code
          </button>
          <button
            role="tab"
            aria-selected={mode === 'recovery'}
            className={`login-tab${mode === 'recovery' ? ' login-tab-active' : ''}`}
            onClick={() => { setMode('recovery'); setCode(''); setError('') }}
          >
            Recovery Code
          </button>
        </div>
        <form onSubmit={handleSubmit} className="login-form">
          <label className="login-label" htmlFor="auth-code">
            {mode === 'totp' ? '6-digit code' : 'Recovery code (xxxxx-xxxxx)'}
          </label>
          <input
            id="auth-code"
            className="login-input"
            type="text"
            inputMode={mode === 'totp' ? 'numeric' : 'text'}
            autoComplete={mode === 'totp' ? 'one-time-code' : 'off'}
            value={code}
            onChange={(e) => setCode(e.target.value)}
            placeholder={mode === 'totp' ? '000000' : 'aaaaa-bbbbb'}
            required
            autoFocus
          />
          <p className="login-error" role="alert" aria-live="assertive">{error || ' '}</p>
          <button className="login-submit" type="submit" disabled={loading || passkeyLoading || !code}>
            {loading ? 'Signing in…' : 'Sign in'}
          </button>
        </form>
      </div>
    </div>
  )
}
