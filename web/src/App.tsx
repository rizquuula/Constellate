import { useEffect, useState } from 'react'
import { ProjectTree } from './features/sidebar/ProjectTree'
import { TerminalView } from './features/terminal/TerminalView'
import { OverviewGrid } from './features/overview/OverviewGrid'
import { Login } from './features/auth/Login'
import { useStore } from './store'
import { authStatus, logout } from './api/rest'

type AuthState = 'loading' | 'setup' | 'login' | 'authed'

export function App() {
  const refreshMachines = useStore((s) => s.refreshMachines)
  const refreshProjects = useStore((s) => s.refreshProjects)
  const refreshSessions = useStore((s) => s.refreshSessions)
  const viewMode = useStore((s) => s.viewMode)
  const setViewMode = useStore((s) => s.setViewMode)

  const [authState, setAuthState] = useState<AuthState>('loading')

  async function checkAuth() {
    try {
      const status = await authStatus()
      if (!status.hasOperator) {
        setAuthState('setup')
      } else if (!status.authenticated) {
        setAuthState('login')
      } else {
        setAuthState('authed')
      }
    } catch {
      // If auth check fails (e.g. server not yet up), treat as loading.
      setAuthState('loading')
    }
  }

  useEffect(() => {
    checkAuth()
  }, [])

  useEffect(() => {
    if (authState !== 'authed') return
    const tick = () => {
      refreshMachines().catch(console.error)
      refreshProjects().catch(console.error)
      refreshSessions().catch(console.error)
    }
    tick()
    const id = setInterval(tick, 2000)
    return () => clearInterval(id)
  }, [authState, refreshMachines, refreshProjects, refreshSessions])

  async function handleLogout() {
    await logout().catch(console.error)
    setAuthState('login')
  }

  if (authState === 'loading') {
    return (
      <div className="login-overlay">
        <div className="login-card">
          <p className="login-title">Connecting…</p>
        </div>
      </div>
    )
  }

  if (authState === 'setup') {
    return (
      <div className="login-overlay">
        <div className="login-card">
          <h2 className="login-title">Setup Required</h2>
          <p className="login-label">
            No operator account configured. Run{' '}
            <code>constellate-hub operator add</code> to bootstrap TOTP.
          </p>
        </div>
      </div>
    )
  }

  if (authState === 'login') {
    return <Login onSuccess={() => setAuthState('authed')} />
  }

  return (
    <div className="app-root">
      <header className="app-header">
        <span className="app-wordmark" aria-hidden="true">Constellate</span>
        <h1 className="sr-only">Constellate</h1>
        <div className="view-toggle" role="group" aria-label="View mode">
          <button
            className={`view-toggle-btn${viewMode === 'workspace' ? ' view-toggle-active' : ''}`}
            onClick={() => setViewMode('workspace')}
            aria-pressed={viewMode === 'workspace'}
          >
            Workspace
          </button>
          <button
            className={`view-toggle-btn${viewMode === 'overview' ? ' view-toggle-active' : ''}`}
            onClick={() => setViewMode('overview')}
            aria-pressed={viewMode === 'overview'}
          >
            Overview
          </button>
        </div>
        <button className="logout-btn" onClick={handleLogout} title="Sign out">
          Sign out
        </button>
      </header>

      {viewMode === 'overview' ? (
        <div className="overview-shell">
          <OverviewGrid />
        </div>
      ) : (
        <div className="layout">
          <ProjectTree />
          <TerminalView />
        </div>
      )}
    </div>
  )
}
