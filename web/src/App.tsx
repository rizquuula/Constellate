import { useEffect, useState, useCallback } from 'react'
import {
  DndContext,
  DragOverlay,
  PointerSensor,
  KeyboardSensor,
  useSensor,
  useSensors,
  type DragEndEvent,
  type DragStartEvent,
} from '@dnd-kit/core'
import { ProjectTree } from './features/sidebar/ProjectTree'
import { TerminalView } from './features/terminal/TerminalView'
import { OverviewGrid } from './features/overview/OverviewGrid'
import { DashboardView } from './features/dashboard/DashboardView'
import { Login } from './features/auth/Login'
import { Snackbar, type SnackbarVariant } from './components/Snackbar'
import { useStore, hashToView } from './store'
import { authStatus, logout, passkeyRegister } from './api/rest'
import { parsePaneDropId } from './features/terminal/dnd'
import type { SessionDragData } from './features/terminal/dnd'

const hasPasskeySupport = typeof window !== 'undefined' && !!window.PublicKeyCredential

type AuthState = 'loading' | 'setup' | 'login' | 'authed'

/** Map a passkey-registration failure to a concise, user-facing message. */
function passkeyErrorMessage(err: unknown): string {
  // Cancelling or letting the prompt time out throws NotAllowedError with a
  // verbose spec URL — show something readable instead.
  if (err instanceof DOMException && err.name === 'NotAllowedError') {
    return 'Passkey registration was cancelled.'
  }
  if (err instanceof DOMException && err.name === 'InvalidStateError') {
    return 'A passkey is already registered for this device.'
  }
  return err instanceof Error ? err.message : 'Registration failed.'
}

export function App() {
  const refreshMachines = useStore((s) => s.refreshMachines)
  const refreshProjects = useStore((s) => s.refreshProjects)
  const refreshSessions = useStore((s) => s.refreshSessions)
  const refreshDashboard = useStore((s) => s.refreshDashboard)
  const viewMode = useStore((s) => s.viewMode)
  const setViewMode = useStore((s) => s.setViewMode)
  const assignSessionToPane = useStore((s) => s.assignSessionToPane)
  const doSplitPaneWithSession = useStore((s) => s.splitPaneWithSession)
  const sidebarOpen = useStore((s) => s.sidebarOpen)
  const setSidebarOpen = useStore((s) => s.setSidebarOpen)

  const [authState, setAuthState] = useState<AuthState>('loading')
  const [activeDragLabel, setActiveDragLabel] = useState<string | null>(null)

  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 5 } }),
    useSensor(KeyboardSensor),
  )

  const handleDragStart = useCallback((event: DragStartEvent) => {
    const data = event.active.data.current as SessionDragData | undefined
    setActiveDragLabel(data?.label ?? null)
  }, [])

  const handleDragEnd = useCallback((event: DragEndEvent) => {
    setActiveDragLabel(null)
    const source = event.active.data.current as SessionDragData | undefined
    if (!source || source.kind !== 'session') return
    if (!event.over) return
    const parsed = parsePaneDropId(String(event.over.id))
    if (!parsed) return
    const { paneId, zone } = parsed
    if (zone === 'center') {
      assignSessionToPane(paneId, source.sessionId)
    } else {
      doSplitPaneWithSession(paneId, zone, source.sessionId)
    }
  }, [assignSessionToPane, doSplitPaneWithSession])

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

  // Keep viewMode in sync with the URL hash so the browser back/forward buttons
  // and manual hash edits switch views. setViewMode writes the hash; this only
  // reacts to external changes (guarded against the self-write feedback loop).
  useEffect(() => {
    const onHashChange = () => {
      const next = hashToView(window.location.hash)
      if (next !== useStore.getState().viewMode) setViewMode(next)
    }
    window.addEventListener('hashchange', onHashChange)
    return () => window.removeEventListener('hashchange', onHashChange)
  }, [setViewMode])

  // Alt/Cmd + 1/2/3 jump straight to a view. Use e.code (physical key) so it
  // works regardless of the character Alt produces on some keyboard layouts.
  useEffect(() => {
    if (authState !== 'authed') return
    const views = { Digit1: 'workspace', Digit2: 'overview', Digit3: 'dashboard' } as const
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.ctrlKey || e.shiftKey) return
      if (!e.altKey && !e.metaKey) return
      const mode = views[e.code as keyof typeof views]
      if (!mode) return
      e.preventDefault()
      setViewMode(mode)
    }
    window.addEventListener('keydown', onKeyDown)
    return () => window.removeEventListener('keydown', onKeyDown)
  }, [authState, setViewMode])

  // Shift+Alt pane controls act on the focused pane: split H (−), split V (=),
  // close (W), detach (E). Physical e.code keeps them layout-independent (Alt
  // often rewrites the produced character). Capture phase + stop/prevent so the
  // shortcut wins even while a terminal is focused and xterm would otherwise
  // swallow the keystroke. State is read live via getState() so the handler
  // always targets the currently focused pane without re-registering.
  useEffect(() => {
    if (authState !== 'authed') return
    const onKeyDown = (e: KeyboardEvent) => {
      if (!e.shiftKey || !e.altKey || e.ctrlKey || e.metaKey) return
      const { viewMode, focusedPaneId, splitPane, closePane, detachPane } = useStore.getState()
      if (viewMode !== 'workspace') return
      switch (e.code) {
        case 'Minus': splitPane(focusedPaneId, 'horizontal'); break
        case 'Equal': splitPane(focusedPaneId, 'vertical'); break
        case 'KeyW':  closePane(focusedPaneId); break
        case 'KeyE':  detachPane(focusedPaneId); break
        default: return
      }
      e.preventDefault()
      e.stopPropagation()
    }
    window.addEventListener('keydown', onKeyDown, { capture: true })
    return () => window.removeEventListener('keydown', onKeyDown, { capture: true })
  }, [authState])

  useEffect(() => {
    if (authState !== 'authed') return
    const tick = () => {
      if (viewMode === 'dashboard') {
        refreshDashboard().catch(console.error)
      } else {
        refreshMachines().catch(console.error)
        refreshProjects().catch(console.error)
        refreshSessions().catch(console.error)
      }
    }
    tick()
    const id = setInterval(tick, 2000)
    return () => clearInterval(id)
  }, [authState, viewMode, refreshMachines, refreshProjects, refreshSessions, refreshDashboard])

  const [snackMsg, setSnackMsg] = useState('')
  const [snackVariant, setSnackVariant] = useState<SnackbarVariant>('info')

  async function handleLogout() {
    await logout().catch(console.error)
    setAuthState('login')
  }

  async function handleAddPasskey() {
    setSnackMsg('')
    try {
      await passkeyRegister()
      setSnackVariant('success')
      setSnackMsg('Passkey added.')
    } catch (err) {
      setSnackVariant('error')
      setSnackMsg(passkeyErrorMessage(err))
    }
  }

  if (authState === 'loading') {
    return (
      <div className="login-overlay">
        <div className="login-card">
          <h2 className="login-title">Connecting…</h2>
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
        {viewMode === 'workspace' && (
          <button
            className="menu-btn"
            onClick={() => setSidebarOpen(!sidebarOpen)}
            aria-label={sidebarOpen ? 'Close sidebar' : 'Open sidebar'}
            aria-expanded={sidebarOpen}
          >
            ☰
          </button>
        )}
        <span className="app-wordmark" aria-hidden="true">Constellate</span>
        <h1 className="sr-only">Constellate</h1>
        <div className="view-toggle" role="group" aria-label="View mode">
          <button
            className={`view-toggle-btn${viewMode === 'workspace' ? ' view-toggle-active' : ''}`}
            onClick={() => setViewMode('workspace')}
            aria-pressed={viewMode === 'workspace'}
            title="Workspace (Alt/⌘+1)"
          >
            Workspace
            <kbd className="view-toggle-kbd" aria-hidden="true">1</kbd>
          </button>
          <button
            className={`view-toggle-btn${viewMode === 'overview' ? ' view-toggle-active' : ''}`}
            onClick={() => setViewMode('overview')}
            aria-pressed={viewMode === 'overview'}
            title="Overview (Alt/⌘+2)"
          >
            Overview
            <kbd className="view-toggle-kbd" aria-hidden="true">2</kbd>
          </button>
          <button
            className={`view-toggle-btn${viewMode === 'dashboard' ? ' view-toggle-active' : ''}`}
            onClick={() => setViewMode('dashboard')}
            aria-pressed={viewMode === 'dashboard'}
            title="Dashboard (Alt/⌘+3)"
          >
            Dashboard
            <kbd className="view-toggle-kbd" aria-hidden="true">3</kbd>
          </button>
        </div>
        {hasPasskeySupport && (
          <button className="logout-btn" onClick={handleAddPasskey} title="Register a passkey for this device">
            Add passkey
          </button>
        )}
        <button className="logout-btn" onClick={handleLogout} title="Sign out">
          Sign out
        </button>
      </header>

      <Snackbar
        message={snackMsg}
        variant={snackVariant}
        duration={snackVariant === 'error' ? 6000 : 3000}
        onDismiss={() => setSnackMsg('')}
      />

      {viewMode === 'overview' ? (
        <div className="overview-shell">
          <OverviewGrid />
        </div>
      ) : viewMode === 'dashboard' ? (
        <DashboardView />
      ) : (
        <DndContext
          sensors={sensors}
          onDragStart={handleDragStart}
          onDragEnd={handleDragEnd}
        >
          <div className={`layout${sidebarOpen ? ' drawer-open' : ''}`}>
            {sidebarOpen && (
              <div className="sidebar-scrim" onClick={() => setSidebarOpen(false)} aria-hidden="true" />
            )}
            <ProjectTree />
            <TerminalView />
          </div>
          <DragOverlay>
            {activeDragLabel && (
              <div className="drag-chip">{activeDragLabel}</div>
            )}
          </DragOverlay>
        </DndContext>
      )}
    </div>
  )
}
