// Usherr Broadcast: a sheet over the page, as in the mock-up: a short list of
// rows, the first one focused. Up and Down go from row to row, Left and Right
// change a choice row, OK presses, Back, Escape or the Options key close it,
// and focus goes back to what opened it. Used by the readers (their settings),
// Activity (a download's actions) and music (an album's actions).
import { useEffect, useRef, type KeyboardEvent, type ReactNode } from 'react'
import { createPortal } from 'react-dom'
import { t } from '../i18n'
import type { Choice } from './choice'
import { ChoiceRow, PressRow } from './Rows'
import { useRemoteFirst } from './useRemoteFirst'
import './screens.css'

export interface SheetRow {
  key: string
  label: string
  /** A row Left and Right change. */
  choices?: readonly Choice[]
  value?: string
  onChoose?: (id: string) => void
  /** A row OK presses. */
  onPress?: () => void
  shown?: ReactNode // what a pressed row shows on its right
  keepOpen?: boolean // a pressed row closes the sheet, unless this
  danger?: boolean
  disabled?: boolean
}

interface Props {
  title: string
  eyebrow?: string
  text?: string
  rows: SheetRow[]
  onClose: () => void
  /** Where it sits: along the right edge (settings) or in the middle (actions). */
  place?: 'side' | 'middle'
  /** Kept out of a reader's own key handling (see readers/useBroadcastReader.ts). */
  label?: string
  /** Usherr Broadcast: its Show me how label (data-walk). */
  walk?: string
}

export default function Sheet({ title, eyebrow, text, rows, onClose, place = 'middle', label, walk }: Props) {
  const box = useRef<HTMLDivElement>(null)
  const close = useRef(onClose)
  close.current = onClose

  // Focus the first row; on closing, go back to what opened the sheet.
  useEffect(() => {
    const opener = document.activeElement instanceof HTMLElement ? document.activeElement : null
    box.current?.querySelector<HTMLElement>('[data-b-row]:not(:disabled)')?.focus({ preventScroll: true })
    return () => {
      if (opener?.isConnected) setTimeout(() => opener.focus({ preventScroll: true }), 0)
    }
  }, [])

  useRemoteFirst((action) => {
    if (action === 'back' || action === 'options') {
      close.current()
      return true
    }
    if (action === 'menu') return true // the menu stays shut while a sheet is open
    return undefined
  })

  const onKeyDown = (e: KeyboardEvent<HTMLDivElement>) => {
    if (e.altKey || e.ctrlKey || e.metaKey) return
    if (e.key === 'Escape') {
      e.preventDefault()
      e.stopPropagation()
      close.current()
      return
    }
    if (e.key === 'ArrowUp' || e.key === 'ArrowDown') {
      const list = Array.from(box.current?.querySelectorAll<HTMLElement>('[data-b-row]:not(:disabled)') ?? [])
      const at = list.indexOf(document.activeElement as HTMLElement)
      const next = list[Math.max(0, Math.min(list.length - 1, at + (e.key === 'ArrowUp' ? -1 : 1)))]
      next?.focus({ preventScroll: true })
      next?.scrollIntoView({ block: 'nearest' })
      e.preventDefault()
      e.stopPropagation()
      return
    }
    // Left and Right never leave the sheet: a choice row has used them already
    if (e.key === 'ArrowLeft' || e.key === 'ArrowRight') {
      e.preventDefault()
      e.stopPropagation()
    }
  }

  return createPortal(
    <div className="b-sheet-scrim" data-bsheet="" onClick={() => close.current()}>
      <div
        ref={box}
        className={`b-sheet b-sheet-${place}`}
        data-walk={walk}
        role="dialog"
        aria-modal="true"
        aria-label={label ?? title}
        onClick={(e) => e.stopPropagation()}
        onKeyDown={onKeyDown}
      >
        {eyebrow && <span className="b-sheet-eyebrow">{eyebrow}</span>}
        <h2 className="b-sheet-title">{title}</h2>
        {text && <p className="b-sheet-text">{text}</p>}
        <div className="b-sheet-rows">
          {rows.map((r) =>
            r.choices && r.onChoose && !r.disabled ? (
              <ChoiceRow key={r.key} label={r.label} choices={r.choices} value={r.value ?? ''} onChoose={r.onChoose} />
            ) : (
              <PressRow
                key={r.key}
                label={r.label}
                value={r.shown ?? r.choices?.find((c) => c.id === r.value)?.label}
                danger={r.danger}
                disabled={r.disabled}
                onPress={() => {
                  if (!r.keepOpen) close.current()
                  r.onPress?.()
                }}
              />
            ),
          )}
          <PressRow label={t('Done')} onPress={() => close.current()} />
        </div>
      </div>
    </div>,
    document.body,
  )
}
