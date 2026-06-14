import { useRef } from 'react'
import { useStore } from '../../store'
import { useTerminal } from './useTerminal'

export function TerminalView() {
  const activeSessionId = useStore((s) => s.activeSessionId)
  const sessions = useStore((s) => s.sessions)
  const containerRef = useRef<HTMLDivElement>(null)

  useTerminal(containerRef, activeSessionId)

  const activeSession = activeSessionId
    ? sessions.find((s) => s.id === activeSessionId)
    : undefined
  const sessionEnded =
    activeSession !== undefined && activeSession.status !== 'running'

  return (
    <div className="terminal-area">
      {!activeSessionId && (
        <div className="terminal-empty">
          Select a machine and open a shell
        </div>
      )}
      {sessionEnded && activeSession && (
        <div className="terminal-ended">
          Session {activeSession.status} — terminal closed
        </div>
      )}
      <div
        ref={containerRef}
        className="terminal-container"
        style={{ display: activeSessionId && !sessionEnded ? 'block' : 'none' }}
      />
    </div>
  )
}
