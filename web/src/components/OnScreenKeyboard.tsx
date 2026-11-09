// Usherr: an on-screen keyboard for a TV with no keyboard of its own (or one
// that won't come up). Letters and numbers in a grid, then space, delete,
// clear and search, then the recent searches. Every key is a plain button, so
// arrows move between them: the keyboard moves focus itself inside the grid and
// lets an arrow at its edge go on to whatever handles the rest of the page.
//
//   <OnScreenKeyboard value={q} onChange={setQ} onSubmit={search} />
//
// Voice works through the TV's own keyboard (its microphone button) when the
// search box itself is focused; this is the fallback.
import { useRef, useState, type KeyboardEvent } from 'react'
import { t } from '../i18n'
import { KEY_ROWS, gridMove, readRecent, rememberSearch } from '../tvSignIn'
import Icon from './Icon'

export interface OnScreenKeyboardProps {
  value: string
  onChange: (value: string) => void
  /** Search (or OK) pressed, or a recent search picked. */
  onSubmit?: (value: string) => void
  /** Shows a Close key. */
  onClose?: () => void
  /** Keep and offer recent searches (default true). */
  recent?: boolean
  maxLength?: number
  /** What the Search key says ("Search" when left out). */
  submitLabel?: string
  label?: string
}

export function OnScreenKeyboard({ value, onChange, onSubmit, onClose, recent = true, maxLength = 200, submitLabel, label }: OnScreenKeyboardProps) {
  const root = useRef<HTMLDivElement>(null)
  const [recents, setRecents] = useState<string[]>(() => (recent ? readRecent() : []))

  function type(s: string) {
    if (value.length + s.length <= maxLength) onChange(value + s)
  }

  function submit(v: string) {
    if (recent && v.trim()) setRecents(rememberSearch(v))
    onSubmit?.(v)
  }

  // The rows as they are drawn: the letters, the action keys, the recent searches.
  const actions: { key: string; label: string; icon?: 'x' | 'search' | 'check'; run: () => void; wide?: boolean }[] = [
    { key: 'space', label: t('Space'), run: () => type(' '), wide: true },
    { key: 'delete', label: t('Delete'), run: () => onChange(value.slice(0, -1)) },
    { key: 'clear', label: t('Clear'), run: () => onChange('') },
  ]
  if (onSubmit) actions.push({ key: 'submit', label: submitLabel ?? t('Search'), icon: 'search', run: () => submit(value) })
  if (onClose) actions.push({ key: 'close', label: t('Close'), icon: 'x', run: onClose })
  const lengths = [...KEY_ROWS.map((r) => r.length), actions.length, ...(recents.length > 0 ? [recents.length] : [])]

  function onKey(e: KeyboardEvent<HTMLDivElement>) {
    const el = e.target as HTMLElement
    const r = Number(el.dataset.row)
    const c = Number(el.dataset.col)
    if (Number.isNaN(r) || Number.isNaN(c)) return
    if (e.key === 'Backspace') {
      e.preventDefault()
      onChange(value.slice(0, -1))
      return
    }
    const to = gridMove(lengths, r, c, e.key)
    if (!to) return // at the edge: let the page move on
    e.preventDefault()
    e.stopPropagation()
    root.current?.querySelector<HTMLElement>(`[data-row="${to[0]}"][data-col="${to[1]}"]`)?.focus()
  }

  const actionRow = KEY_ROWS.length
  return (
    <div ref={root} className="osk" role="group" aria-label={label ?? t('Keyboard on screen')} onKeyDown={onKey} style={{ display: 'grid', gap: 6 }}>
      <div className="osk-value" aria-live="polite" style={{ padding: '8px 12px', border: '1px solid var(--border)', borderRadius: 8, minHeight: '1.6em', fontSize: '1.2rem', overflowWrap: 'anywhere' }}>
        {value || <span style={{ color: 'var(--text-dim)' }}>{t('Type with the arrows and OK')}</span>}
      </div>
      {KEY_ROWS.map((row, r) => (
        <div key={r} className="osk-row" style={{ display: 'grid', gridTemplateColumns: `repeat(${row.length}, 1fr)`, gap: 6 }}>
          {row.map((k, c) => (
            <button key={k} type="button" className="osk-key" data-row={r} data-col={c} onClick={() => type(k)} aria-label={k === ' ' ? t('Space') : k} autoFocus={r === 0 && c === 0}>
              {k}
            </button>
          ))}
        </div>
      ))}
      <div className="osk-row" style={{ display: 'flex', gap: 6, flexWrap: 'wrap' }}>
        {actions.map((a, c) => (
          <button key={a.key} type="button" className={a.key === 'submit' ? 'primary osk-key' : 'osk-key'} data-row={actionRow} data-col={c} onClick={a.run} style={{ flex: a.wide ? 2 : 1 }}>
            {a.icon && <Icon name={a.icon} size={14} />} {a.label}
          </button>
        ))}
      </div>
      {recents.length > 0 && (
        <div className="osk-row osk-recent" style={{ display: 'flex', gap: 6, flexWrap: 'wrap', alignItems: 'center' }}>
          <span className="hint">{t('Recent searches')}</span>
          {recents.map((q, c) => (
            <button
              key={q}
              type="button"
              className="osk-key"
              data-row={actionRow + 1}
              data-col={c}
              onClick={() => {
                onChange(q)
                submit(q)
              }}
            >
              {q}
            </button>
          ))}
        </div>
      )}
    </div>
  )
}
