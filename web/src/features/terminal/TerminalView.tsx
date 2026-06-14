import { useRef } from 'react'
import { useStore } from '../../store'
import { useTerminal } from './useTerminal'

export function TerminalView() {
  const activeSessionId = useStore((s) => s.activeSessionId)
  const containerRef = useRef<HTMLDivElement>(null)

  useTerminal(containerRef, activeSessionId)

  return (
    <div className="terminal-area">
      {!activeSessionId && (
        <div className="terminal-empty">
          Select a machine and open a shell
        </div>
      )}
      <div
        ref={containerRef}
        className="terminal-container"
        style={{ display: activeSessionId ? 'block' : 'none' }}
      />
    </div>
  )
}
