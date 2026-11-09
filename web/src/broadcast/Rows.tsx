// Usherr Broadcast: rows made for a remote, as in the mock-up's Your settings
// and sheets. A choice row changes with Left and Right (and with a click, or
// the small arrows for a mouse); a row that would reload the page (the look,
// the language) changes with Left and Right and takes effect on OK. A link
// row opens a page. Up and Down are the remote's, from row to row.
import { useState, type KeyboardEvent, type ReactNode } from 'react'
import { Link } from 'react-router-dom'
import { t } from '../i18n'
import { arrowDir, cycleChoice, type Choice } from './choice'
import './screens.css'

interface ChoiceRowProps<T extends string> {
  label: string
  choices: readonly Choice<T>[]
  value: T
  onChoose: (v: T) => void
  /** Left and Right only pick; OK makes it so (for a choice that reloads the page). */
  applyOnOk?: boolean
  onFocus?: () => void
  autoFocus?: boolean
  /** Usherr Broadcast: its Show me how label (data-walk). */
  walk?: string
}

export function ChoiceRow<T extends string>({ label, choices, value, onChoose, applyOnOk, onFocus, autoFocus, walk }: ChoiceRowProps<T>) {
  const [pending, setPending] = useState<T | null>(null)
  const shown = applyOnOk && pending !== null ? pending : value
  const step = (dir: 1 | -1) => {
    const next = cycleChoice(choices, shown, dir)
    if (applyOnOk) setPending(next === value ? null : next)
    else if (next !== value) onChoose(next)
  }
  const press = () => {
    if (applyOnOk) {
      if (pending !== null && pending !== value) onChoose(pending)
      else step(1)
    } else step(1)
  }
  const onKeyDown = (e: KeyboardEvent<HTMLButtonElement>) => {
    const d = arrowDir(e.key)
    if (!d || e.altKey || e.ctrlKey || e.metaKey || e.shiftKey) return
    e.preventDefault() // the remote leaves a key that's been used
    step(d)
  }
  const label_ = choices.find((c) => c.id === shown)?.label ?? String(shown)
  const waiting = applyOnOk && pending !== null && pending !== value
  return (
    <button
      type="button"
      className={`b-row b-choice${waiting ? ' b-waiting' : ''}`}
      data-b-row=""
      data-autofocus={autoFocus ? '' : undefined}
      data-walk={walk}
      onClick={press}
      onKeyDown={onKeyDown}
      onFocus={onFocus}
      onBlur={() => setPending(null)}
      aria-label={t('{label}: {value}. Left and Right change it.', { label, value: label_ })}
    >
      <span className="b-row-label">{label}</span>
      <span className="b-row-val" aria-hidden="true">
        <span
          className="b-row-arrow"
          onClick={(e) => {
            e.stopPropagation()
            step(-1)
          }}
        >
          ◀
        </span>
        <span className="b-row-now">{label_}</span>
        <span
          className="b-row-arrow"
          onClick={(e) => {
            e.stopPropagation()
            step(1)
          }}
        >
          ▶
        </span>
        {waiting && <small className="b-row-ok">{t('OK to switch')}</small>}
      </span>
    </button>
  )
}

/**
 * A choice that reloads the page (the language), kept a real list box so a
 * mouse and the TV's own picker still work: Left and Right pick, OK makes it
 * so; a pick from the list takes effect at once, as it always has.
 */
export function SelectRow<T extends string>({ label, ariaLabel, choices, value, onChoose, onFocus }: { label: string; ariaLabel: string; choices: readonly Choice<T>[]; value: T; onChoose: (v: T) => void; onFocus?: () => void }) {
  const [pending, setPending] = useState<T | null>(null)
  const shown = pending ?? value
  const onKeyDown = (e: KeyboardEvent<HTMLSelectElement>) => {
    if (e.altKey || e.ctrlKey || e.metaKey || e.shiftKey) return
    const d = arrowDir(e.key)
    if (d) {
      e.preventDefault()
      const next = cycleChoice(choices, shown, d)
      setPending(next === value ? null : next)
    } else if (e.key === 'Enter' && pending !== null) {
      e.preventDefault()
      onChoose(pending)
    }
  }
  return (
    <label className={`b-row b-choice${pending !== null ? ' b-waiting' : ''}`} data-b-row="">
      <span className="b-row-label">{label}</span>
      <span className="b-row-val">
        <select aria-label={ariaLabel} value={shown} onChange={(e) => onChoose(e.target.value as T)} onKeyDown={onKeyDown} onFocus={onFocus} onBlur={() => setPending(null)}>
          {choices.map((c) => (
            <option key={c.id} value={c.id}>
              {c.label}
            </option>
          ))}
        </select>
        {pending !== null && <small className="b-row-ok">{t('OK to switch')}</small>}
      </span>
    </label>
  )
}

export function LinkRow({ label, value, to, onFocus }: { label: string; value?: ReactNode; to: string; onFocus?: () => void }) {
  return (
    <Link className="b-row" data-b-row="" to={to} onFocus={onFocus}>
      <span className="b-row-label">{label}</span>
      <span className="b-row-val">{value ?? '›'}</span>
    </Link>
  )
}

export function PressRow({ label, value, onPress, danger, disabled, onFocus }: { label: string; value?: ReactNode; onPress: () => void; danger?: boolean; disabled?: boolean; onFocus?: () => void }) {
  return (
    <button type="button" className={`b-row${danger ? ' b-danger' : ''}`} data-b-row="" onClick={onPress} disabled={disabled} onFocus={onFocus}>
      <span className="b-row-label">{label}</span>
      {value !== undefined && <span className="b-row-val">{value}</span>}
    </button>
  )
}
