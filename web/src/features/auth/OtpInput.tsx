import { useRef } from 'react'

interface Props {
  value: string
  onChange: (value: string) => void
  /** Called once the field holds `length` digits (typed or pasted). */
  onComplete: (value: string) => void
  length?: number
  disabled?: boolean
  autoFocus?: boolean
}

/**
 * Segmented one-time-code field: `length` single-digit boxes with auto-advance,
 * backspace-to-previous, arrow navigation and full-code paste. Fires `onComplete`
 * as soon as every box is filled so the caller can auto-submit.
 */
export function OtpInput({ value, onChange, onComplete, length = 6, disabled, autoFocus }: Props) {
  const inputs = useRef<Array<HTMLInputElement | null>>([])

  function focusBox(index: number) {
    inputs.current[index]?.focus()
    inputs.current[index]?.select()
  }

  function commit(next: string) {
    onChange(next)
    if (next.length === length) onComplete(next)
  }

  function handleChange(index: number, raw: string) {
    const digits = raw.replace(/\D/g, '')
    if (!digits) return
    // Spread the typed/dropped digits across this box and the ones after it.
    const chars = value.padEnd(length, ' ').split('')
    let cursor = index
    for (const d of digits) {
      if (cursor >= length) break
      chars[cursor] = d
      cursor++
    }
    commit(chars.join('').replace(/ /g, '').slice(0, length))
    focusBox(Math.min(cursor, length - 1))
  }

  function handleKeyDown(index: number, e: React.KeyboardEvent<HTMLInputElement>) {
    if (e.key === 'Backspace') {
      e.preventDefault()
      const chars = value.padEnd(length, ' ').split('')
      if (chars[index] && chars[index] !== ' ') {
        chars[index] = ' '
        commit(chars.join('').replace(/ /g, ''))
      } else if (index > 0) {
        chars[index - 1] = ' '
        commit(chars.join('').replace(/ /g, ''))
        focusBox(index - 1)
      }
    } else if (e.key === 'ArrowLeft' && index > 0) {
      e.preventDefault()
      focusBox(index - 1)
    } else if (e.key === 'ArrowRight' && index < length - 1) {
      e.preventDefault()
      focusBox(index + 1)
    }
  }

  function handlePaste(e: React.ClipboardEvent<HTMLInputElement>) {
    e.preventDefault()
    const digits = e.clipboardData.getData('text').replace(/\D/g, '').slice(0, length)
    if (!digits) return
    commit(digits)
    focusBox(Math.min(digits.length, length - 1))
  }

  return (
    <div className="otp-input" role="group" aria-label={`${length}-digit code`}>
      {Array.from({ length }, (_, i) => (
        <input
          key={i}
          ref={(el) => { inputs.current[i] = el }}
          className="otp-box"
          type="text"
          inputMode="numeric"
          autoComplete={i === 0 ? 'one-time-code' : 'off'}
          maxLength={1}
          value={value[i] ?? ''}
          disabled={disabled}
          autoFocus={autoFocus && i === 0}
          aria-label={`Digit ${i + 1}`}
          onChange={(e) => handleChange(i, e.target.value)}
          onKeyDown={(e) => handleKeyDown(i, e)}
          onPaste={handlePaste}
          onFocus={(e) => e.target.select()}
        />
      ))}
    </div>
  )
}
