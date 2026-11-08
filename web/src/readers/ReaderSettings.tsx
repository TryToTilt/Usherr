// Usherr: the reading settings both picture readers share, the Manga reader and the
// Comics reader: one panel (direction, how pages show, and the shared options of
// readerOptions.ts, with room for each reader's own extras), the progress bar in
// its three styles and three places, the page number, tap and cursor hints, the
// button that brings a hidden header back, and the wide strip view.
import { forwardRef, useCallback, useEffect, useImperativeHandle, useLayoutEffect, useRef, useState, type CSSProperties, type MouseEvent as ReactMouseEvent, type ReactNode } from 'react'
import Icon from '../components/Icon'
import { t } from '../i18n'
import { backgroundColor, DEFAULT_OPTIONS, joinFilters, optionsFilter, progressParts, saveOptions, storedOptions, ticksFor, type ReaderOptions } from './readerOptions'
import './readers.css'

// useReaderOptions keeps a series' options, saving each change. scope names the
// series ("manga:12", "comic:4"); base is the reader's own defaults.
export function useReaderOptions(scope: string, base: ReaderOptions = DEFAULT_OPTIONS) {
  const [opts, setOpts] = useState<ReaderOptions>(() => storedOptions(scope, base))
  const baseRef = useRef(base)
  baseRef.current = base
  useEffect(() => setOpts(storedOptions(scope, baseRef.current)), [scope])
  const change = useCallback(
    (patch: Partial<ReaderOptions>) =>
      setOpts((o) => {
        const next = { ...o, ...patch }
        saveOptions(scope, next)
        return next
      }),
    [scope],
  )
  return [opts, change] as const
}

// readerStyle is what the options change on the reader itself: the background, and
// the filter the pages are drawn through (--rd-filter), with any of the reader's own.
export function readerStyle(opts: ReaderOptions, ownFilter?: string): CSSProperties {
  const style: Record<string, string> = {}
  const bg = backgroundColor(opts.background)
  if (bg) style.background = bg
  const filter = joinFilters(ownFilter, optionsFilter(opts))
  if (filter) style['--rd-filter'] = filter
  return style as CSSProperties
}

// readerClass is the reader's extra class names for the options.
export function readerClass(opts: ReaderOptions): string {
  return [opts.header ? '' : 'rd-no-header', opts.background === 'white' || (opts.background === 'theme' && !darkTheme()) ? 'rd-light' : ''].filter(Boolean).join(' ')
}

function darkTheme(): boolean {
  try {
    return getComputedStyle(document.documentElement).colorScheme !== 'light' && !window.matchMedia('(prefers-color-scheme: light)').matches
  } catch {
    return true
  }
}

// resolvedBackground is the colour behind the pages as a real colour, for the
// page-curl's canvas, or undefined to keep the reader's own.
export function resolvedBackground(opts: ReaderOptions): string | undefined {
  if (opts.background === 'theme') {
    try {
      return getComputedStyle(document.documentElement).getPropertyValue('--bg').trim() || undefined
    } catch {
      return undefined
    }
  }
  return backgroundColor(opts.background)
}

// ---- the panel ----

export interface LayoutChoice {
  id: string
  label: string
  title?: string
  disabled?: boolean
}

interface Props {
  direction: 'ltr' | 'rtl'
  onDirection: (d: 'ltr' | 'rtl') => void
  directionDisabled?: boolean
  directionFirst?: 'ltr' | 'rtl' // which button comes first, as each reader always had it
  layouts: LayoutChoice[]
  layout: string
  onLayout: (id: string) => void
  opts: ReaderOptions
  onOpts: (patch: Partial<ReaderOptions>) => void
  label: string
  rememberedHint?: string
  extraKeys?: { keys: string; what: string }[]
  children?: ReactNode // the reader's own extras, after how pages show
  more?: ReactNode // the reader's own extras inside "More"
}

export default function ReaderSettings({ direction, onDirection, directionDisabled, directionFirst = 'ltr', layouts, layout, onLayout, opts, onOpts, label, rememberedHint, extraKeys = [], children, more }: Props) {
  const [moreOpen, setMoreOpen] = useState(false)
  const [keysOpen, setKeysOpen] = useState(false)
  const dirs = directionFirst === 'rtl' ? (['rtl', 'ltr'] as const) : (['ltr', 'rtl'] as const)
  const parts = progressParts(opts)
  return (
    <div className="cx-marks-panel cx-reader-opts" aria-label={label}>
      <h3>{t('Reading direction')}</h3>
      <div className="seg" role="group" aria-label={t('Reading direction')}>
        {dirs.map((d) => (
          <button key={d} className={direction === d ? 'active' : ''} aria-pressed={direction === d} disabled={directionDisabled} onClick={() => onDirection(d)}>
            {d === 'rtl' ? t('Right to left') : t('Left to right')}
          </button>
        ))}
      </div>
      <h3>{t('How pages show')}</h3>
      <div className="seg cx-reader-layouts" role="group" aria-label={t('How pages show')}>
        {layouts.map((l) => (
          <button key={l.id} className={layout === l.id ? 'active' : ''} aria-pressed={layout === l.id} title={l.title} disabled={l.disabled} onClick={() => onLayout(l.id)}>
            {l.label}
          </button>
        ))}
      </div>
      {rememberedHint && <p className="hint">{rememberedHint}</p>}
      {children}
      <details className="mr-more" open={moreOpen} onToggle={(e) => setMoreOpen(e.currentTarget.open)}>
        <summary>{t('More reading options')}</summary>
        {more}
        <h3>{t('Progress bar')}</h3>
        <div className="seg" role="group" aria-label={t('Progress bar')}>
          {(
            [
              ['normal', t('Normal')],
              ['thin', t('Thin line')],
              ['hidden', t('Hidden')],
            ] as const
          ).map(([id, text]) => (
            <button key={id} className={opts.progress === id ? 'active' : ''} aria-pressed={opts.progress === id} onClick={() => onOpts({ progress: id })}>
              {text}
            </button>
          ))}
        </div>
        {parts.bar && (
          <>
            <div className="seg" role="group" aria-label={t('Where the progress bar goes')}>
              {(
                [
                  ['bottom', t('Bottom')],
                  ['left', t('Left')],
                  ['right', t('Right')],
                ] as const
              ).map(([id, text]) => (
                <button key={id} className={opts.side === id ? 'active' : ''} aria-pressed={opts.side === id} onClick={() => onOpts({ side: id })}>
                  {text}
                </button>
              ))}
            </div>
            <label className="rd-row">
              <span>{t('Thickness')}</span>
              <input type="range" min={1} max={4} value={opts.size} onChange={(e) => onOpts({ size: Number(e.target.value) })} aria-label={t('Thickness')} />
            </label>
          </>
        )}
        {!parts.bar && (
          <label className="mr-check">
            <input type="checkbox" checked={opts.pageNumber} onChange={(e) => onOpts({ pageNumber: e.target.checked })} />
            <span>{t('Show the page number')}</span>
          </label>
        )}
        <label className="mr-check">
          <input type="checkbox" checked={!opts.header} onChange={(e) => onOpts({ header: !e.target.checked })} />
          <span>
            {t('Hide the header')}
            <small className="hint">{t('A small button in the corner brings it back, and so does the M key.')}</small>
          </span>
        </label>
        <h3>{t('Tap and click hints')}</h3>
        <div className="seg" role="group" aria-label={t('Tap and click hints')}>
          {(
            [
              ['overlay', t('Show once')],
              ['cursor', t('Pointer arrows')],
              ['none', t('None')],
            ] as const
          ).map(([id, text]) => (
            <button key={id} className={opts.hints === id ? 'active' : ''} aria-pressed={opts.hints === id} onClick={() => onOpts({ hints: id })}>
              {text}
            </button>
          ))}
        </div>
        <h3>{t('Pages')}</h3>
        <label className="mr-check">
          <input type="checkbox" checked={opts.greyscale} onChange={(e) => onOpts({ greyscale: e.target.checked })} />
          <span>{t('Greyscale')}</span>
        </label>
        <label className="mr-check">
          <input type="checkbox" checked={opts.dim} onChange={(e) => onOpts({ dim: e.target.checked })} />
          <span>{t('Dim the pages')}</span>
        </label>
        <h3>{t('Background')}</h3>
        <div className="seg" role="group" aria-label={t('Background')}>
          {(
            [
              ['reader', t('Dark')],
              ['theme', t('Usherr’s theme')],
              ['white', t('White')],
              ['black', t('Black')],
            ] as const
          ).map(([id, text]) => (
            <button key={id} className={opts.background === id ? 'active' : ''} aria-pressed={opts.background === id} onClick={() => onOpts({ background: id })}>
              {text}
            </button>
          ))}
        </div>
      </details>
      <details className="mr-more" open={keysOpen} onToggle={(e) => setKeysOpen(e.currentTarget.open)}>
        <summary>{t('Keyboard shortcuts')}</summary>
        <dl className="rd-keys">
          <dt>← →</dt>
          <dd>{t('Turn the page, the way the book reads')}</dd>
          <dt>{t('Space, Page Down')}</dt>
          <dd>{t('Next page')}</dd>
          <dt>Page Up</dt>
          <dd>{t('Previous page')}</dd>
          <dt>↑ ↓</dt>
          <dd>{t('Scroll a long strip')}</dd>
          <dt>M</dt>
          <dd>{t('Show or hide the header')}</dd>
          {extraKeys.map((k) => (
            <KeyRow key={k.keys} keys={k.keys} what={k.what} />
          ))}
          <dt>Esc</dt>
          <dd>{t('Close the reader')}</dd>
        </dl>
      </details>
    </div>
  )
}

function KeyRow({ keys, what }: { keys: string; what: string }) {
  return (
    <>
      <dt>{keys}</dt>
      <dd>{what}</dd>
    </>
  )
}

// ---- the progress bar ----

// ReaderProgress is the bar in the style and place chosen. Normal at the bottom is
// the slider the readers always had, now with a tick a page.
export function ReaderProgress({ opts, page, total, rtl, onJump }: { opts: ReaderOptions; page: number; total: number; rtl: boolean; onJump: (n: number) => void }) {
  if (total <= 0) return null
  const parts = progressParts(opts)
  if (parts.number) {
    return (
      <span className="rd-page-number" aria-live="polite">
        {t('{n} / {total}', { n: page, total })}
      </span>
    )
  }
  if (!parts.bar) return null
  const vertical = opts.side !== 'bottom'
  const sideClass = vertical ? ` rd-side rd-${opts.side}` : ''
  const size = { '--rd-size': `${opts.size * 2}px` } as CSSProperties
  if (!parts.slider) {
    const fill = total > 1 ? (page - 1) / (total - 1) : 1
    const jump = (e: ReactMouseEvent<HTMLDivElement>) => {
      const r = e.currentTarget.getBoundingClientRect()
      let at = vertical ? (e.clientY - r.top) / r.height : (e.clientX - r.left) / r.width
      if (rtl && !vertical) at = 1 - at
      onJump(Math.min(total, Math.max(1, Math.round(at * (total - 1)) + 1)))
    }
    return (
      <div className={`rd-thin${sideClass}${rtl && !vertical ? ' rd-rtl' : ''}`} style={size} onClick={jump} role="progressbar" aria-valuemin={1} aria-valuemax={total} aria-valuenow={page} aria-label={t('Page')}>
        <i style={vertical ? { height: `${fill * 100}%` } : { width: `${fill * 100}%` }} />
      </div>
    )
  }
  const ticks = ticksFor(total)
  const listId = ticks.length ? `rd-ticks-${total}` : undefined
  return (
    <>
      <input
        className={`cx-reader-slider${sideClass}`}
        style={vertical ? size : undefined}
        type="range"
        dir={rtl && !vertical ? 'rtl' : 'ltr'}
        min={1}
        max={total}
        value={page}
        list={listId}
        onChange={(e) => onJump(Number(e.target.value))}
        aria-label={t('Page')}
      />
      {listId && (
        <datalist id={listId}>
          {ticks.map((_, i) => (
            <option key={i} value={i + 1} />
          ))}
        </datalist>
      )}
    </>
  )
}

// HeaderButton brings a hidden header back.
export function HeaderButton({ onShow }: { onShow: () => void }) {
  return (
    <button className="btn-sm rd-header-btn" onClick={onShow} title={t('Show the header (M)')} aria-label={t('Show the header (M)')}>
      <Icon name="sliders" size={14} />
    </button>
  )
}

// cursorHint points the mouse the way a click there turns: the third behind you
// goes back, the rest on, as tapTurn has it.
export function cursorHint(e: ReactMouseEvent<HTMLElement>, rtl: boolean) {
  const r = e.currentTarget.getBoundingClientRect()
  const at = (e.clientX - r.left) / r.width
  const back = rtl ? at > 2 / 3 : at < 1 / 3
  const left = back !== rtl // back in a left-to-right book is to the left
  e.currentTarget.style.cursor = left ? 'w-resize' : 'e-resize'
}

// TapZones shows where a tap goes back and where it goes on, for a few seconds.
export function TapZones({ rtl, onDone }: { rtl: boolean; onDone: () => void }) {
  return (
    <div className="rd-zones" aria-hidden="true" onPointerDown={onDone}>
      <div className={`rd-zone back ${rtl ? 'right' : 'left'}`}>{t('Tap here to go back')}</div>
      <div className={`rd-zone on ${rtl ? 'left' : 'right'}`}>{t('Tap here to go on')}</div>
    </div>
  )
}

// ---- the wide strip ----

export interface WideStripHandle {
  scroll: (screens: number) => void // on (or back, negative) by that share of the screen
  toPage: (page: number) => void
}

interface StripProps {
  pageUrl: (n: number) => string
  total: number
  start: number
  rtl: boolean
  onPage: (page: number) => void
}

// WideStrip is every page side by side in one strip you scroll across, the way a
// long strip goes down: right to left when the book reads that way. The wheel
// scrolls it too.
export const WideStrip = forwardRef<WideStripHandle, StripProps>(function WideStrip({ pageUrl, total, start, rtl, onPage }, ref) {
  const box = useRef<HTMLDivElement>(null)
  const pin = useRef(start)
  const onPageRef = useRef(onPage)
  onPageRef.current = onPage

  const showPinned = useCallback(() => {
    const el = box.current?.querySelector<HTMLElement>(`[data-page="${pin.current}"]`)
    el?.scrollIntoView({ inline: 'center', block: 'nearest' })
  }, [])
  useLayoutEffect(showPinned, [showPinned])

  useEffect(() => {
    const el = box.current
    if (!el) return
    const seen = new IntersectionObserver(
      (entries) => {
        if (pin.current !== 0) return
        for (const e of entries) if (e.isIntersecting) onPageRef.current(Number((e.target as HTMLElement).dataset.page))
      },
      { root: el, rootMargin: '0px -50% 0px -50%' },
    )
    el.querySelectorAll('[data-page]').forEach((im) => seen.observe(im))
    return () => seen.disconnect()
  }, [total])

  useImperativeHandle(
    ref,
    () => ({
      scroll: (screens: number) => {
        pin.current = 0
        const el = box.current
        if (el) el.scrollBy({ left: el.clientWidth * screens * (rtl ? -1 : 1) })
      },
      toPage: (page: number) => {
        pin.current = page
        showPinned()
      },
    }),
    [showPinned, rtl],
  )

  const letGo = () => {
    pin.current = 0
  }
  return (
    <div
      ref={box}
      className="rd-wide"
      dir={rtl ? 'rtl' : 'ltr'}
      onWheel={(e) => {
        letGo()
        if (Math.abs(e.deltaY) > Math.abs(e.deltaX) && box.current) box.current.scrollLeft += rtl ? -e.deltaY : e.deltaY
      }}
      onTouchStart={letGo}
      onPointerDown={letGo}
    >
      {Array.from({ length: total }, (_, i) => i + 1).map((n) => (
        <img
          key={n}
          loading={Math.abs(n - start) <= 2 ? 'eager' : 'lazy'}
          data-page={n}
          className="rd-wide-page"
          src={pageUrl(n)}
          alt={t('Page {n}', { n })}
          draggable={false}
          onLoad={(e) => {
            e.currentTarget.style.minWidth = '0'
            if (pin.current !== 0) showPinned()
          }}
        />
      ))}
    </div>
  )
})
