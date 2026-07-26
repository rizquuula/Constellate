import { useCallback, useEffect, useRef, useState } from 'react'
import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import { openTerminalSocket, sendResize } from '../../api/ws'
import {
  backoffDelay,
  isStable,
  isStale,
  shouldRetry,
  subscribeWake,
  WAKE_STAGGER_MS,
  WATCHDOG_TICK_MS,
} from '../../api/reconnect'
import { applyModifiers, specialKeySeq } from './keys'
import type { KeyMods, SpecialKey } from './keys'
import { imeAttrsFor, type InputMode } from './inputMode'
import { attachTouchScroll, dispatchWheelLines } from './touchScroll'

// Imperative handle returned by useTerminal, so out-of-tree controls (the touch
// Keypad) can drive the live terminal without prop-drilling the xterm instance.
// Methods dereference the hook's live refs, so the handle stays valid across a
// reloadKey teardown + reattach.
export interface TerminalHandle {
  sendKey(key: SpecialKey): void
  sendText(text: string): void
  toggleModifier(mod: 'ctrl' | 'alt'): void
  getModifiers(): KeyMods
  subscribeModifiers(cb: (m: KeyMods) => void): () => void
  subscribeSelection(cb: (hasSelection: boolean) => void): () => void
  hasSelection(): boolean
  focus(): void
  copySelection(): Promise<boolean>
  paste(): Promise<void>
  setFontSize(px: number): void
  getFontSize(): number
  refit(): void
  setInputMode(mode: InputMode): void
  /** Scroll by whole lines; positive moves toward newer output. */
  scrollLines(lines: number): void
}

// Connection state of the pane's terminal socket, surfaced so the pane can show
// a reconnecting/disconnected badge. 'connecting' covers the very first attach
// (no badge — a brief blank pane is the expected first paint); 'reconnecting'
// and 'stopped' are the states a user needs told about. 'stopped' is reached
// only on a terminal close code — retryable failures reconnect forever.
export type ConnStatus = 'connecting' | 'open' | 'reconnecting' | 'stopped'

export interface ConnState {
  status: ConnStatus
  /** Consecutive failed attempts; 0 while connecting or healthy. */
  attempt: number
}

export interface TerminalSession {
  handle: TerminalHandle
  conn: ConnState
  /** Reconnect immediately, cancelling any pending backoff. Works from 'stopped'. */
  retryNow(): void
}

// Stable identity so re-running the connection effect in its disabled branch
// doesn't churn React state.
const CONN_IDLE: ConnState = { status: 'connecting', attempt: 0 }

const FONT_SIZE_KEY = 'constellate.fontSize'
const FONT_SIZE_MIN = 8
const FONT_SIZE_MAX = 32
const DEFAULT_FONT_SIZE = 14

function clampFontSize(px: number): number {
  return Math.max(FONT_SIZE_MIN, Math.min(FONT_SIZE_MAX, px))
}

function readFontSize(): number {
  try {
    const raw = typeof window !== 'undefined' ? window.localStorage.getItem(FONT_SIZE_KEY) : null
    const parsed = raw !== null ? Number(raw) : NaN
    return Number.isFinite(parsed) ? clampFontSize(parsed) : DEFAULT_FONT_SIZE
  } catch {
    return DEFAULT_FONT_SIZE
  }
}

// Applies an input mode to xterm's helper textarea — the one DOM write in the
// input-mode path; the decision of *what* to write lives in inputMode.ts.
function applyImeAttrs(ta: HTMLTextAreaElement, mode: InputMode): void {
  const { attrs, readOnly } = imeAttrsFor(mode)
  for (const [name, value] of Object.entries(attrs)) {
    if (value === null) ta.removeAttribute(name)
    else ta.setAttribute(name, value)
  }
  ta.readOnly = readOnly
}

function writeFontSize(px: number): void {
  try {
    if (typeof window !== 'undefined') window.localStorage.setItem(FONT_SIZE_KEY, String(px))
  } catch {
    // ignore — persistence is best-effort (private mode / disabled storage)
  }
}

// Attaches an xterm.js terminal to `containerRef` for the given `sessionId` and
// returns a stable TerminalHandle for imperative control, plus live connection
// state and a manual retry.
// Each call is fully independent — multiple panes can call this hook concurrently.
// Tears down its xterm instance and WebSocket on unmount or when sessionId changes.
// Bumping `reloadKey` forces a full teardown + reattach (fresh socket, scrollback
// replayed on attach) — used by the pane's reload button to recover a wedged term.
// `enabled` gates the socket only: pass false for a session that has exited or
// been lost, otherwise the reconnect loop would hammer the hub forever for a PTY
// that is never coming back.
export function useTerminal(
  containerRef: React.RefObject<HTMLDivElement | null>,
  sessionId: string | null,
  reloadKey = 0,
  enabled = true,
): TerminalSession {
  const termRef = useRef<Terminal | null>(null)
  const wsRef = useRef<WebSocket | null>(null)
  const fitRef = useRef<FitAddon | null>(null)

  // Whether the *current* xterm instance has already taken an attach replay.
  // It lives at hook level, not inside the connection effect, because that
  // effect remounts whenever `enabled` flips (session goes lost, then running
  // again) while Effect A's terminal — still holding the pre-blip screen —
  // survives. A per-effect counter would call the next attach "the first one"
  // and append the replay to content already on screen, doubling every line.
  const replayedRef = useRef(false)

  const [conn, setConn] = useState<ConnState>(CONN_IDLE)

  // The connection effect owns the live retry closure; the returned callback is
  // a stable indirection into whichever effect instance is current.
  const requestRetryRef = useRef<() => void>(() => {})
  const retryNow = useCallback(() => { requestRetryRef.current() }, [])

  // One-shot modifier state lives outside the effect so it survives a reloadKey
  // teardown. Subscribers (the Keypad) are notified on every change.
  const modsRef = useRef<KeyMods>({ ctrl: false, alt: false })
  // Which input mode the helper textarea should be in. Also at hook level so a
  // reloadKey teardown + reattach re-applies the caller's choice to the new
  // textarea instead of silently reverting.
  //
  // Starts 'native' — the permissive mode — rather than at the *preference*
  // default: the caller decides whether this device suppresses its keyboard,
  // and until it says so the terminal must not run with a readOnly textarea.
  const inputModeRef = useRef<InputMode>('native')
  const modSubsRef = useRef<Set<(m: KeyMods) => void>>(new Set())
  const selSubsRef = useRef<Set<(hasSelection: boolean) => void>>(new Set())

  const notifyMods = useCallback(() => {
    const snapshot = { ...modsRef.current }
    modSubsRef.current.forEach((cb) => cb(snapshot))
  }, [])

  const notifySelection = useCallback(() => {
    const has = termRef.current?.hasSelection() ?? false
    selSubsRef.current.forEach((cb) => cb(has))
  }, [])

  // Reset the one-shot modifiers after they are consumed (or on teardown), but
  // only notify when something actually changed — keeps the no-modifier path free.
  const clearMods = useCallback(() => {
    if (!modsRef.current.ctrl && !modsRef.current.alt) return
    modsRef.current = { ctrl: false, alt: false }
    notifyMods()
  }, [notifyMods])

  const sendBytes = useCallback((data: string) => {
    const ws = wsRef.current
    if (ws?.readyState === WebSocket.OPEN) {
      ws.send(new TextEncoder().encode(data))
    }
  }, [])

  // Built once; its methods dereference the live refs above so the same handle
  // instance keeps working across reattaches.
  const handleRef = useRef<TerminalHandle | null>(null)
  if (handleRef.current === null) {
    handleRef.current = {
      // Special keys send their raw sequence as-is (modifiers are not folded into
      // a special-key sequence) but still consume the armed one-shot modifiers.
      sendKey: (key) => {
        const appCursor = termRef.current?.modes.applicationCursorKeysMode ?? false
        sendBytes(specialKeySeq(key, appCursor))
        clearMods()
      },
      sendText: (text) => {
        sendBytes(applyModifiers(text, modsRef.current))
        clearMods()
      },
      toggleModifier: (mod) => {
        modsRef.current = { ...modsRef.current, [mod]: !modsRef.current[mod] }
        notifyMods()
      },
      getModifiers: () => ({ ...modsRef.current }),
      subscribeModifiers: (cb) => {
        modSubsRef.current.add(cb)
        return () => { modSubsRef.current.delete(cb) }
      },
      subscribeSelection: (cb) => {
        selSubsRef.current.add(cb)
        return () => { selSubsRef.current.delete(cb) }
      },
      hasSelection: () => termRef.current?.hasSelection() ?? false,
      focus: () => { termRef.current?.focus() },
      copySelection: async () => {
        const selection = termRef.current?.getSelection()
        if (!selection || !navigator.clipboard) return false
        try {
          await navigator.clipboard.writeText(selection)
          return true
        } catch {
          return false
        }
      },
      paste: async () => {
        const term = termRef.current
        if (!term || !navigator.clipboard) return
        try {
          const text = await navigator.clipboard.readText()
          if (text) term.paste(text)
        } catch {
          // silent — clipboard read may be denied; mirrors the Ctrl+Shift+V path
        }
      },
      setFontSize: (px) => {
        const term = termRef.current
        if (!term) return
        const clamped = clampFontSize(px)
        term.options.fontSize = clamped
        writeFontSize(clamped)
        fitRef.current?.fit()
        const ws = wsRef.current
        if (ws) sendResize(ws, term.cols, term.rows)
      },
      getFontSize: () => termRef.current?.options.fontSize ?? readFontSize(),
      refit: () => {
        const term = termRef.current
        if (!term || !fitRef.current) return
        fitRef.current.fit()
        const ws = wsRef.current
        if (ws) sendResize(ws, term.cols, term.rows)
      },
      setInputMode: (mode) => {
        inputModeRef.current = mode
        const ta = termRef.current?.textarea
        if (!ta) return
        applyImeAttrs(ta, mode)
        // Changing inputmode on an already-focused element does not re-negotiate
        // the virtual keyboard, so bounce focus to make the switch take effect
        // on the spot rather than on the next tap.
        if (document.activeElement === ta) {
          ta.blur()
          termRef.current?.focus()
        }
      },
      // Routed through the wheel pipeline rather than term.scrollLines(), which
      // is a no-op in the alternate screen and under mouse tracking.
      //
      // The synthetic wheel is aimed at the *centre* of the terminal element:
      // with mouse tracking on, xterm turns the wheel into a mouse report for
      // the cell under the pointer, and the nub sits at the far-right edge — so
      // using the nub's own x would scroll the rightmost tmux split instead of
      // the one the user is reading.
      scrollLines: (lines) => {
        const term = termRef.current
        const element = term?.element
        if (!term || !element || lines === 0) return
        const rect = element.getBoundingClientRect()
        dispatchWheelLines(term, lines, rect.left + rect.width / 2, rect.top + rect.height / 2)
      },
    }
  }

  // ── Effect A: terminal lifecycle ──────────────────────────────────────────
  // Owns the xterm instance and everything bound to the DOM node. Deliberately
  // captures no socket — anything that needs to send reads wsRef.current — so a
  // reconnect can swap the socket underneath a terminal that keeps its scrollback.
  useEffect(() => {
    if (!sessionId || !containerRef.current) return

    const container = containerRef.current

    const term = new Terminal({
      cursorBlink: true,
      fontSize: readFontSize(),
      scrollback: 5000,
      theme: {
        background: '#0f0f11',
        foreground: '#e0e0e0',
      },
    })
    // A brand-new terminal has no scrollback of its own, so the next attach
    // replay is the first one and must not be preceded by a reset().
    replayedRef.current = false
    const fitAddon = new FitAddon()
    term.loadAddon(fitAddon)
    term.open(container)
    fitAddon.fit()

    // Before anything can focus the new terminal: re-apply the chosen input
    // mode, since open() creates a fresh helper textarea with default attrs.
    if (term.textarea) applyImeAttrs(term.textarea, inputModeRef.current)

    // Ctrl+Shift+C / Ctrl+Shift+V → copy the selection / paste from the system
    // clipboard, instead of the browser default (Ctrl+Shift+C opens DevTools
    // "inspect element"). Returning false stops xterm from also processing the
    // key; preventDefault stops the browser shortcut. Runs while the terminal
    // is focused. Plain Ctrl+C/Ctrl+V are left untouched so SIGINT and shells
    // that read the literal keystroke still work.
    term.attachCustomKeyEventHandler((e) => {
      if (e.type !== 'keydown' || !e.ctrlKey || !e.shiftKey || e.altKey || e.metaKey) return true
      if (e.code === 'KeyC') {
        const selection = term.getSelection()
        if (selection) navigator.clipboard?.writeText(selection).catch(() => {})
        e.preventDefault()
        return false
      }
      if (e.code === 'KeyV') {
        navigator.clipboard?.readText().then((text) => {
          if (text) term.paste(text)
        }).catch(() => {})
        e.preventDefault()
        return false
      }
      return true
    })

    termRef.current = term
    fitRef.current = fitAddon

    // Bridge mobile vertical swipes to wheel events so full-screen TUIs (alt
    // screen / mouse-tracking apps), where xterm's native touch scroll is dead,
    // still scroll on touch devices.
    const detachTouch = attachTouchScroll(term, container)

    // Fold any armed one-shot modifiers into the typed data, then consume them.
    // With no modifier armed applyModifiers is the identity and clearMods is a
    // no-op, so this path stays byte-identical to a plain passthrough.
    const dataSub = term.onData((data) => {
      sendBytes(applyModifiers(data, modsRef.current))
      clearMods()
    })

    const selectionSub = term.onSelectionChange(() => notifySelection())

    let rafId: number | null = null
    const observer = new ResizeObserver(() => {
      if (rafId !== null) return
      rafId = requestAnimationFrame(() => {
        rafId = null
        fitAddon.fit()
        const ws = wsRef.current
        if (ws) sendResize(ws, term.cols, term.rows)
      })
    })
    observer.observe(container)

    return () => {
      if (rafId !== null) cancelAnimationFrame(rafId)
      observer.disconnect()
      dataSub.dispose()
      selectionSub.dispose()
      detachTouch()
      term.dispose()
      termRef.current = null
      fitRef.current = null
      clearMods()
    }
  }, [sessionId, containerRef, reloadKey, sendBytes, clearMods, notifySelection])

  // ── Effect B: connection + reconnect state machine ────────────────────────
  // Owns the WebSocket. Runs after Effect A in the same commit, so termRef holds
  // the freshly created terminal by the time the first socket opens.
  useEffect(() => {
    if (!enabled || !sessionId) {
      setConn(CONN_IDLE)
      return
    }

    let disposed = false
    let unstableStreak = 0
    let status: ConnStatus = 'connecting'
    let retryTimer: number | null = null
    let wakeTimer: number | null = null
    let watchdog: number | null = null
    let lastRxMs = Date.now()

    const setStatus = (next: ConnStatus, attempt: number) => {
      status = next
      setConn({ status: next, attempt })
    }

    const clearWatchdog = () => {
      if (watchdog === null) return
      window.clearInterval(watchdog)
      watchdog = null
    }

    const clearRetryTimer = () => {
      if (retryTimer === null) return
      window.clearTimeout(retryTimer)
      retryTimer = null
    }

    // Drop the current socket without letting its onclose schedule a competing
    // reconnect — used on teardown and before a manually forced reconnect.
    const closeActive = () => {
      clearWatchdog()
      const ws = wsRef.current
      wsRef.current = null
      if (!ws) return
      ws.onopen = null
      ws.onmessage = null
      ws.onclose = null
      ws.onerror = null
      ws.close()
    }

    const connect = () => {
      clearRetryTimer()
      closeActive()

      const ws = openTerminalSocket(sessionId)
      wsRef.current = ws
      let openedAtMs: number | null = null
      let pendingReset = false

      // Silence past the staleness threshold means a zombie socket — common on
      // mobile sleep and NAT timeouts, where no close event ever arrives. The
      // watchdog arms at open rather than on the first heartbeat: waiting for
      // one leaves a ~15s blind window on every single connect, and a socket
      // that black-holes immediately would never arm it at all. Any received
      // frame (PTY bytes or an 'hb') refreshes lastRxMs.
      const armWatchdog = () => {
        if (watchdog !== null) return
        watchdog = window.setInterval(() => {
          if (!isStale(lastRxMs, Date.now())) return
          // Not ws.close(): a black-holed socket may never fire onclose, which
          // would leave the pane stuck at 'open' — no badge, keystrokes
          // silently dropped, and the wake handler declining to retry. Drive
          // the reconnect directly instead.
          const wasStable = isStable(openedAtMs ?? lastRxMs, Date.now())
          closeActive()
          scheduleReconnect(wasStable)
        }, WATCHDOG_TICK_MS)
      }

      ws.onopen = () => {
        openedAtMs = Date.now()
        lastRxMs = openedAtMs
        // The agent replays its whole scrollback ring on every attach. Into a
        // fresh terminal that is exactly right; into one that already took a
        // replay the pre-blip screen is still there and the replay would double
        // it. reset() (not clear(): modes and the alt screen must go too) right
        // before the first replayed byte fixes that.
        pendingReset = replayedRef.current
        const term = termRef.current
        if (term) sendResize(ws, term.cols, term.rows)
        armWatchdog()
        setStatus('open', 0)
      }

      ws.onmessage = (ev: MessageEvent) => {
        lastRxMs = Date.now()
        // Text frames are control frames ('hb' liveness heartbeats today); they
        // carry no terminal state, so timestamping their arrival above is the
        // whole of their handling. Arrival is timestamped locally rather than
        // read off the frame, since the hub clock may skew from the browser's.
        if (!(ev.data instanceof ArrayBuffer)) return
        if (pendingReset) {
          termRef.current?.reset()
          pendingReset = false
        }
        replayedRef.current = true
        termRef.current?.write(new Uint8Array(ev.data))
      }

      ws.onclose = (ev: CloseEvent) => {
        clearWatchdog()
        if (disposed) return
        if (wsRef.current === ws) wsRef.current = null

        const wasStable = isStable(openedAtMs ?? Date.now(), Date.now())
        if (!shouldRetry(ev.code)) {
          setStatus('stopped', unstableStreak)
          return
        }
        scheduleReconnect(wasStable)
      }

      // onclose always follows onerror, so the retry decision lives there alone.
      ws.onerror = () => {}
    }

    // A close after a stable run opens a *new* outage at attempt 1 — that is
    // what the badge counts, and what makes the first retry a ~300ms blink
    // rather than a resumption of some earlier outage's backoff. The exponent
    // trails the attempt number by one so attempt 1 waits BACKOFF_BASE_MS.
    const scheduleReconnect = (wasStable: boolean) => {
      unstableStreak = wasStable ? 1 : unstableStreak + 1
      setStatus('reconnecting', unstableStreak)
      retryTimer = window.setTimeout(connect, backoffDelay(unstableStreak - 1, Math.random))
    }

    const requestRetry = () => {
      if (disposed) return
      clearRetryTimer()
      unstableStreak = 0
      setStatus('connecting', 0)
      connect()
    }
    requestRetryRef.current = requestRetry

    // Network return / tab refocus are strong hints the outage is over, and a
    // backgrounded tab may have had its backoff timer frozen for the duration.
    // Stagger the retries so a workspace full of panes doesn't hit the hub with
    // simultaneous scrollback replays.
    const unsubscribeWake = subscribeWake(() => {
      if (status !== 'reconnecting' && status !== 'stopped') return
      if (wakeTimer !== null) return
      wakeTimer = window.setTimeout(() => {
        wakeTimer = null
        requestRetry()
      }, Math.random() * WAKE_STAGGER_MS)
    })

    setStatus('connecting', 0)
    connect()

    return () => {
      disposed = true
      unsubscribeWake()
      clearRetryTimer()
      if (wakeTimer !== null) window.clearTimeout(wakeTimer)
      closeActive()
      requestRetryRef.current = () => {}
    }
  }, [sessionId, reloadKey, enabled])

  return { handle: handleRef.current!, conn, retryNow }
}
