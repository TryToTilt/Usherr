// Usherr Broadcast: the More sheet (the mock-up's "More"): everything a
// title's page can do beyond its one main button and three more, in one
// list the remote goes down. The everyday things first, then an
// "Administrators" part. Rows either run something (and the sheet closes) or
// hold one of the page's own buttons (Rate, Re-Posterr…), which open their
// own windows over the sheet, so nothing is done twice.
//
// The remote's Options key opens it (useTitleMore), on a title's page and,
// through PosterOptions, on any poster that opens a title.
import { useContext, useEffect, useRef, useState, type ReactNode } from 'react'
import { createPortal } from 'react-dom'
import Icon, { type IconName } from '../components/Icon'
import { t } from '../i18n'
import { remoteOn, useRemote } from '../remote'
import { useFocusTrap } from '../useFocusTrap'
import { HeroTrailer } from './heroTrailer'
import { posterTitle, type PosterTitle } from './logic'
import './sheet.css'

export interface MoreRow {
  key: string
  label?: string
  hint?: string
  icon?: IconName
  /** Pressing the row: the sheet closes first. */
  run?: () => void
  /** One of the page's own buttons, kept as it is. */
  node?: ReactNode
  /** In the Administrators part. */
  admin?: boolean
  danger?: boolean
  /** Usherr Broadcast: its Show me how label (data-walk), so a walkthrough can point at it. */
  walk?: string
}

function RowView({ row, close }: { row: MoreRow; close: () => void }) {
  if (row.node)
    return (
      <div className="bc-row bc-row-own" data-walk={row.walk}>
        {row.label && (
          <span className="bc-row-label">
            {row.icon && <Icon name={row.icon} size={20} />}
            {row.label}
          </span>
        )}
        <span className="bc-row-node">{row.node}</span>
      </div>
    )
  return (
    <button
      type="button"
      className={`bc-row${row.danger ? ' danger' : ''}`}
      data-walk={row.walk}
      onClick={() => {
        close()
        // after the sheet is gone, so a dialog it opens gets the focus
        window.setTimeout(() => row.run?.(), 0)
      }}
    >
      <span className="bc-row-label">
        {row.icon && <Icon name={row.icon} size={20} />}
        {row.label}
      </span>
      {row.hint && <span className="bc-row-hint">{row.hint}</span>}
    </button>
  )
}

export function MoreSheet({ eyebrow, title, rows, onClose }: { eyebrow?: string; title?: string; rows: MoreRow[]; onClose: () => void }) {
  const box = useRef<HTMLElement>(null)
  useFocusTrap(box)
  const everyday = rows.filter((r) => !r.admin)
  const admin = rows.filter((r) => r.admin)
  return createPortal(
    <>
      <div className="bc-scrim" onClick={onClose} aria-hidden="true" />
      <section
        ref={box}
        tabIndex={-1}
        className="bc-sheet"
        data-walk="more-sheet"
        role="dialog"
        aria-modal="true"
        aria-label={eyebrow ? `${eyebrow} · ${title ?? t('More')}` : (title ?? t('More'))}
        onKeyDown={(e) => {
          // (a window opened from a row is in another part of the page: its own Escape is its own)
          if (e.key === 'Escape' && box.current?.contains(e.target as Node)) {
            e.preventDefault()
            e.stopPropagation()
            onClose()
          }
        }}
      >
        <div className="bc-sheet-head">
          {eyebrow && <span className="bc-eyebrow">{eyebrow}</span>}
          <h2>{title ?? t('More')}</h2>
        </div>
        <div className="bc-sheet-rows">
          {everyday.map((r) => (
            <RowView key={r.key} row={r} close={onClose} />
          ))}
          {admin.length > 0 && (
            <>
              <h3 className="bc-sheet-group">{t('Administrators')}</h3>
              {admin.map((r) => (
                <RowView key={r.key} row={r} close={onClose} />
              ))}
            </>
          )}
        </div>
        {remoteOn() && <p className="bc-sheet-hint">{t('Back or Options closes this.')}</p>}
        {/* last, so the first row has the focus when it opens; Back closes it from the remote */}
        <button type="button" className="bc-sheet-x" onClick={onClose} aria-label={t('Close')} data-remote-skip="">
          <Icon name="x" size={20} />
        </button>
      </section>
    </>,
    document.body,
  )
}

// Cards whose link is a title: their Options key is that title's More sheet (PosterOptions).
export const CARD = '.pcard, .cx-card, .sc-card, [data-remote-card]'

/** The title a focused thing's poster card opens, if it is in one. */
export function cardTitle(el: Element | null): PosterTitle | null {
  const card = el?.closest(CARD)
  if (!card) return null
  const a = card.matches('a[href]') ? (card as HTMLAnchorElement) : card.querySelector<HTMLAnchorElement>('a[href]')
  return posterTitle(a?.getAttribute('href'))
}

/**
 * A title page's sheet: open or closed, and the remote's keys for it. The
 * Options key opens and closes it (a poster on the page keeps its own: that
 * title's sheet), and Play plays the page's title when `play` is given.
 */
export function useTitleMore(enabled: boolean) {
  const [open, setOpen] = useState(false)
  // what Play plays, set by the page once it knows (it can change as the page loads)
  const playRef = useRef<(() => void) | undefined>(undefined)
  useRemote((action, target) => {
    const play = playRef.current
    if (action === 'options') {
      if (open) {
        setOpen(false)
        return true
      }
      if (cardTitle(target)) return // PosterOptions has it
      if (target?.closest('[aria-modal="true"]') || document.querySelector('.hp-player')) return // another window, or the player, is open
      setOpen(true)
      return true
    }
    // (never while a player or another window is open: its keys are its own)
    if ((action === 'play' || action === 'playpause') && play && !open && !cardTitle(target) && !document.querySelector('[aria-modal="true"]')) {
      play()
      return true
    }
  }, enabled)
  return { open, setOpen, playRef }
}

/** Mark watched: today, in your diary (home_diary.go), as the Diary's own "I watched" does. */
export async function markWatched(kind: 'movie' | 'tv', tmdbId: number): Promise<void> {
  const d = new Date()
  const day = `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
  const res = await fetch('/api/home/diary', { method: 'POST', credentials: 'include', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ kind, tmdbId, watchedOn: day, stars: 0, note: '' }) })
  if (!res.ok) {
    const data = (await res.json().catch(() => null)) as { error?: string } | null
    throw new Error(data?.error ?? t('Something went wrong (error {status}).', { status: res.status }))
  }
}

/**
 * A title's buttons in Broadcast: one main action (it gets the remote's first
 * focus), then at most three more: Stack, Trailer and More.
 */
export function BroadcastTitleActions({ primary, stack, onMore }: { primary?: ReactNode; stack?: ReactNode; onMore: () => void }) {
  const trailer = useContext(HeroTrailer)
  const row = useRef<HTMLDivElement>(null)
  useEffect(() => {
    // the page's most likely next step has the focus when the remote is in use
    const now = document.activeElement
    if (!remoteOn() || (now && now !== document.body)) return
    const first = row.current?.querySelector<HTMLElement>('[data-autofocus]') ?? row.current?.querySelector<HTMLElement>('button, a[href]')
    first?.focus({ preventScroll: true })
  }, [])
  return (
    <div className="bc-title-actions" ref={row}>
      {primary}
      {stack}
      {trailer && (
        <button type="button" className="btn-with-icon big" onClick={trailer}>
          <Icon name="film" size={18} /> {t('Trailer')}
        </button>
      )}
      <button type="button" className="btn-with-icon big" data-walk="title-more" onClick={onMore} aria-haspopup="dialog" title={t('Everything else you can do with it (the Options key opens this too)')}>
        <Icon name="more" size={18} /> {t('More')}
      </button>
    </div>
  )
}

/** The main button: big, and first for the remote. */
export function PrimaryButton({ icon, label, onClick, walk, title }: { icon: IconName; label: string; onClick: () => void; walk?: string; title?: string }) {
  return (
    <button type="button" className="primary btn-with-icon big" data-autofocus="" data-walk={walk} onClick={onClick} title={title}>
      <Icon name={icon} size={20} /> {label}
    </button>
  )
}
