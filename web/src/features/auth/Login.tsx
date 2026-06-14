import { useState } from 'react'
import { loginTOTP, loginRecovery } from '../../api/rest'

type Mode = 'totp' | 'recovery'

interface Props {
  onSuccess: () => void
}

export function Login({ onSuccess }: Props) {
  const [mode, setMode] = useState<Mode>('totp')
  const [code, setCode] = useState('')
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)

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

  return (
    <div className="login-overlay">
      <div className="login-card">
        <h2 className="login-title">Constellate</h2>
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
            type={mode === 'totp' ? 'text' : 'text'}
            inputMode={mode === 'totp' ? 'numeric' : 'text'}
            autoComplete="one-time-code"
            value={code}
            onChange={(e) => setCode(e.target.value)}
            placeholder={mode === 'totp' ? '000000' : 'aaaaa-bbbbb'}
            required
            autoFocus
          />
          {error && <p className="login-error">{error}</p>}
          <button className="login-submit" type="submit" disabled={loading || !code}>
            {loading ? 'Signing in…' : 'Sign in'}
          </button>
        </form>
      </div>
    </div>
  )
}
