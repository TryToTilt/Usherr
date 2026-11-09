// Usherr Broadcast: the Broadcast guide, the Live TV guide's default style in
// the Broadcast look (TVGuideStyles.tsx lists it; Classic and Neu never see
// it). A grid for the remote: channels down the side (number, name in
// capitals, a star for favourites), two hours across, the programme with
// focus filled blue and a glowing line at now. Under it, a card for the
// focused programme. Up and Down land on what's on at the same time in the
// next channel, Left and Right go along a channel, and the hours on screen
// follow the focus at the edges (tvGuideNav.ts). OK watches what's on now,
// or opens a later programme (to set a reminder); Options (the ☰ key, or
// the button) has Remind me, Record, Start over and the channel's star.
import { useCallback, useEffect, useMemo, useRef, useState, type KeyboardEvent as RKeyboardEvent } from 'react'
import { createPortal } from 'react-dom'
import Icon from '../components/Icon'
import { locale, t } from '../i18n'
import { moveFocus, sendRemote, useRemoteOptions } from '../remote'
import { playChannel, playTitle } from './Player'
import { setTVPrefs, toggleIn, useTVPrefs } from './tvPrefs'
import { floorTo, focusTime, MIN, nextRow, pct, sameTimeIndex, SLOT, windowFor, type Span } from './tvGuideNav'
import './tvBroadcast.css'

export interface BChannel {
  id: number
  number: number
  name: string
  logoUrl?: string
}
export interface BProgram {
  start: string
  end: string
  title: string
  subtitle?: string
  kind: string
  itemId: number
}
export interface BGuide<C extends BChannel, P extends BProgram> {
  from?: string
  hours?: number
  channels: { channel: C; programs: P[] }[]
}

const clock = (ms: number) => new Date(ms).toLocaleTimeString(locale(), { hour: 'numeric', minute: '2-digit' })
const movieTitle = (p: BProgram) => (p.kind === 'movie' && /^\d{4}$/.test(p.subtitle ?? '') ? `${p.title} (${p.subtitle})` : p.title)
const playable = (p: BProgram) => p.kind === 'episode' || p.kind === 'movie'
const LAST_KEY = 'tv-guide-channel' // the classic guide's too: both start on the channel last looked at

function dayLabel(ms: number): string {
  const d = new Date(ms)
  const today = new Date()
  const days = Math.round((new Date(d.toDateString()).getTime() - new Date(today.toDateString()).getTime()) / 86_400_000)
  if (days === 0) return t('Today')
  if (days === 1) return t('Tomorrow')
  if (days === -1) return t('Yesterday')
  return d.toLocaleDateString(locale(), { weekday: 'long' })
}

type Focus = { ch: number; start: number } // a channel id and a programme's start

async function tvCall<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(`/api/tv${path}`, { method, credentials: 'include', headers: body ? { 'Content-Type': 'application/json' } : undefined, body: body ? JSON.stringify(body) : undefined })
  const data = await res.json().catch(() => null)
  if (!res.ok) throw new Error(data?.error ?? t('Something went wrong (error {status}).', { status: res.status }))
  return data as T
}

export default function BroadcastGuide<C extends BChannel, P extends BProgram>({ guide, onOpen }: { guide: BGuide<C, P>; onOpen: (p: P, c: C) => void }) {
  const prefs = useTVPrefs()
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    const tm = window.setInterval(() => setNow(Date.now()), 30_000)
    return () => window.clearInterval(tm)
  }, [])
  const [narrow, setNarrow] = useState(() => typeof window !== 'undefined' && window.innerWidth < 760)
  useEffect(() => {
    const on = () => setNarrow(window.innerWidth < 760)
    window.addEventListener('resize', on)
    return () => window.removeEventListener('resize', on)
  }, [])
  const winLen = (narrow ? 60 : 120) * MIN
  const from = guide.from ? new Date(guide.from).getTime() : floorTo(now) - 3 * 60 * MIN
  const min = floorTo(from)
  const max = Math.max(min, from + (guide.hours ?? 24) * 60 * MIN - winLen)
  const [winStart, setWinStart] = useState(() => floorTo(Date.now()))

  const rows = useMemo<Span[][]>(() => guide.channels.map((c) => c.programs.map((p) => ({ start: new Date(p.start).getTime(), end: new Date(p.end).getTime() }))), [guide])
  const [focus, setFocus] = useState<Focus | null>(null)
  // Start on the channel last looked at (else the first), on what's on now.
  useEffect(() => {
    if (focus || guide.channels.length === 0) return
    let last = 0
    try {
      last = Number(localStorage.getItem(LAST_KEY)) || 0
    } catch {
      /* private mode */
    }
    let r = guide.channels.findIndex((c) => c.channel.id === last)
    if (r < 0 || rows[r].length === 0) r = rows.findIndex((x) => x.length > 0)
    if (r < 0) return
    const i = sameTimeIndex(rows[r], Date.now())
    setFocus({ ch: guide.channels[r].channel.id, start: rows[r][i].start })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [guide])

  // Where the focus is now; after the guide is read again a programme that's gone falls back to the one on at the same time.
  let row = focus ? guide.channels.findIndex((c) => c.channel.id === focus.ch) : -1
  if (row < 0 && focus) row = rows.findIndex((x) => x.length > 0)
  let idx = row >= 0 && focus ? rows[row].findIndex((s) => s.start === focus.start) : -1
  if (row >= 0 && idx < 0) idx = sameTimeIndex(rows[row], focus ? focus.start : now)
  const cur = row >= 0 && idx >= 0 ? { channel: guide.channels[row].channel, program: guide.channels[row].programs[idx], span: rows[row][idx] } : null

  const cells = useRef(new Map<string, HTMLButtonElement>())
  const grid = useRef<HTMLDivElement>(null)
  const want = useRef(false) // the remote moved: focus follows
  useEffect(() => {
    if (!want.current || !cur) return
    want.current = false
    const el = cells.current.get(`${cur.channel.id}-${cur.span.start}`)
    if (el) {
      el.focus({ preventScroll: true })
      el.scrollIntoView({ block: 'nearest', inline: 'nearest' })
    }
  })

  const pick = useCallback(
    (r: number, i: number, keyboard: boolean) => {
      const s = rows[r][i]
      if (!s) return
      want.current = keyboard
      setFocus({ ch: guide.channels[r].channel.id, start: s.start })
      setWinStart((w) => windowFor(w, winLen, s, min, max))
      try {
        localStorage.setItem(LAST_KEY, String(guide.channels[r].channel.id))
      } catch {
        /* private mode */
      }
    },
    [rows, guide, winLen, min, max],
  )

  // OK on a programme: watch what's on now; a later (or earlier) one opens, to set a reminder or play it.
  const activate = (c: C, p: P) => {
    const s = new Date(p.start).getTime()
    if (s <= Date.now() && new Date(p.end).getTime() > Date.now()) playChannel(c.id, c.name)
    else onOpen(p, c)
  }

  const onKey = (e: RKeyboardEvent<HTMLDivElement>) => {
    const dir = e.key === 'ArrowUp' ? 'up' : e.key === 'ArrowDown' ? 'down' : e.key === 'ArrowLeft' ? 'left' : e.key === 'ArrowRight' ? 'right' : null
    if (!dir || !cur || e.altKey || e.ctrlKey || e.metaKey) return
    e.preventDefault()
    if (dir === 'up' || dir === 'down') {
      const r = nextRow(rows, row, dir === 'up' ? -1 : 1)
      if (r < 0) {
        moveFocus(dir) // out of the guide: the options above, the card below
        return
      }
      pick(r, sameTimeIndex(rows[r], focusTime(cur.span, winStart)), true)
      return
    }
    const i = idx + (dir === 'left' ? -1 : 1)
    if (i < 0 || rows[row][i].end <= min) {
      if (dir === 'left' && !moveFocus('left')) sendRemote('menu', e.currentTarget)
      return
    }
    if (i >= rows[row].length) return
    pick(row, i, true)
  }

  // The window by the buttons (a mouse): the focus stays in the same channel, at the new window's start.
  const page = (to: number) => {
    const w = Math.max(min, Math.min(max, to))
    setWinStart(w)
    if (row >= 0 && rows[row].length) {
      const i = sameTimeIndex(rows[row], w)
      setFocus({ ch: guide.channels[row].channel.id, start: rows[row][i].start })
    }
  }

  const [options, setOptions] = useState(false)
  useRemoteOptions((target) => {
    if (!cur || !target || !grid.current?.contains(target)) return
    setOptions(true)
    return true
  })

  const slots: number[] = []
  for (let tm = winStart; tm < winStart + winLen; tm += SLOT) slots.push(tm)
  const nowAt = now >= winStart && now < winStart + winLen ? pct(now, winStart, winLen) : null
  const wasSel = useRef(false)

  return (
    <div className="btg" data-walk="tv-guide">{/* Usherr Broadcast: the data-walk labels here are for Show me how */}
      <div className="btg-head">
        <span className="btg-day">
          {dayLabel(winStart)} <b>{clock(now)}</b>
        </span>
        <div className="btg-times" aria-hidden="true">
          {slots.map((tm) => (
            <span key={tm} style={{ left: `${pct(tm, winStart, winLen)}%` }}>
              {clock(tm)}
            </span>
          ))}
        </div>
        <span className="btg-page">
          <button type="button" className="btg-pg" onClick={() => page(winStart - SLOT)} aria-label={t('Half an hour earlier')} title={t('Half an hour earlier')}>
            <Icon name="chevron-left" size={18} />
          </button>
          <button type="button" className="btg-pg" onClick={() => page(floorTo(Date.now()))}>
            {t('Now')}
          </button>
          <button type="button" className="btg-pg" onClick={() => page(winStart + SLOT)} aria-label={t('Half an hour later')} title={t('Half an hour later')}>
            <Icon name="chevron-right" size={18} />
          </button>
        </span>
      </div>
      <div className="btg-grid" ref={grid} data-remote-own-arrows onKeyDown={onKey} role="grid" aria-label={t('Guide')} style={{ ['--btg-now' as string]: nowAt === null ? undefined : String(nowAt) }}>
        {guide.channels.map(({ channel, programs }, r) => {
          const fav = prefs.favs.includes(channel.id)
          const here = r === row
          return (
            <div key={channel.id} className={`btg-row${here ? ' here' : ''}`} role="row">
              <div className="btg-ch" role="rowheader">
                <button type="button" tabIndex={-1} className="btg-chname" onClick={() => playChannel(channel.id, channel.name)} title={t('Watch {name}', { name: channel.name })}>
                  <b className="btg-num">{channel.number}</b>
                  {channel.logoUrl ? <img className="btg-logo" src={channel.logoUrl} alt="" loading="lazy" /> : null}
                  <span className="btg-name">{channel.name}</span>
                </button>
                <button type="button" tabIndex={-1} className={`btg-star${fav ? ' on' : ''}`} aria-pressed={fav} aria-label={t('Favourite')} title={t('Favourite')} onClick={() => setTVPrefs({ favs: toggleIn(prefs.favs, channel.id) })}>
                  <Icon name="star" size={16} />
                </button>
              </div>
              <div className="btg-cells">
                {programs.map((p, i) => {
                  const s = rows[r][i]
                  if (s.end <= winStart || s.start >= winStart + winLen) return null
                  const sel = here && i === idx
                  const onAir = s.start <= now && s.end > now
                  const left = pct(s.start, winStart, winLen)
                  const width = pct(s.end, winStart, winLen) - left
                  return (
                    <button
                      key={p.start}
                      ref={(el) => {
                        const k = `${channel.id}-${s.start}`
                        if (el) cells.current.set(k, el)
                        else cells.current.delete(k)
                      }}
                      type="button"
                      role="gridcell"
                      tabIndex={sel ? 0 : -1}
                      data-autofocus={sel ? '' : undefined}
                      aria-selected={sel}
                      className={`btg-cell${sel ? ' sel' : ''}${onAir ? ' on' : ''}${s.end <= now ? ' past' : ''}${s.start < winStart ? ' cut' : ''}`}
                      style={{ left: `${left}%`, width: `calc(${width}% - 6px)` }}
                      title={p.subtitle ? `${p.title} · ${p.subtitle}` : p.title}
                      onPointerDown={() => (wasSel.current = sel)}
                      onFocus={() => !sel && pick(r, i, false)}
                      onClick={(e) => {
                        // a click picks, a second click (or OK) watches
                        if (e.detail > 0 && !wasSel.current) {
                          wasSel.current = true
                          return
                        }
                        activate(channel, p)
                      }}
                    >
                      <strong>{movieTitle(p)}</strong>
                      <span>{p.kind === 'episode' && p.subtitle ? p.subtitle : `${clock(s.start)} – ${clock(s.end)}`}</span>
                    </button>
                  )
                })}
              </div>
            </div>
          )
        })}
        {nowAt !== null && <span className="btg-now" aria-hidden="true" />}
      </div>
      {cur && <PreviewCard channel={cur.channel} program={cur.program} now={now} onOptions={() => setOptions(true)} />}
      {options && cur && createPortal(<OptionsSheet channel={cur.channel} program={cur.program} onClose={() => setOptions(false)} onOpen={() => (setOptions(false), activate(cur.channel, cur.program))} />, document.body)}
    </div>
  )
}

interface Info {
  title: string
  episode?: string
  overview?: string
  poster?: string
  backdrop?: string
}

// The focused programme, under the grid: its poster, channel, title and times, and what OK does.
function PreviewCard({ channel, program, now, onOptions }: { channel: BChannel; program: BProgram; now: number; onOptions: () => void }) {
  const [info, setInfo] = useState<Info | null>(null)
  const s = new Date(program.start).getTime()
  const e = new Date(program.end).getTime()
  useEffect(() => {
    let gone = false
    setInfo(null)
    // after a moment, so moving through the guide doesn't ask for every programme passed over
    const tm = window.setTimeout(() => {
      tvCall<{ info: Info }>('GET', `/channels/${channel.id}/info?at=${s + 1000}`)
        .then((r) => !gone && setInfo(r.info))
        .catch(() => undefined)
    }, 250)
    return () => {
      gone = true
      window.clearTimeout(tm)
    }
  }, [channel.id, s])
  const onAir = s <= now && e > now
  const left = Math.max(0, Math.round((e - now) / MIN))
  const pic = info?.poster || info?.backdrop || channel.logoUrl
  return (
    <section className="btg-card" data-walk="tv-guide-card" aria-live="polite">
      {pic ? <img className={`btg-poster${info?.poster ? '' : ' wide'}`} src={pic} alt="" /> : <span className="btg-poster none" aria-hidden="true" />}
      <div className="btg-about">
        <span className="btg-eyebrow">
          {channel.number} · {channel.name}
          {onAir && <span className="btg-live">{t('On now')}</span>}
        </span>
        <h2>{movieTitle(program)}</h2>
        {(info?.episode || (program.kind === 'episode' && program.subtitle)) && <span className="btg-ep">{info?.episode || program.subtitle}</span>}
        <span className="btg-when">
          {dayLabel(s)} {clock(s)} – {clock(e)}
          {onAir ? ` · ${t('{n} min left', { n: left })}` : ''}
        </span>
        {onAir && (
          <span className="btg-track" aria-hidden="true">
            <span style={{ width: `${Math.min(100, ((now - s) / (e - s)) * 100)}%` }} />
          </span>
        )}
        {info?.overview && <p className="btg-overview">{info.overview}</p>}
        <span className="btg-hint">
          {onAir ? t('OK to watch') : t('OK to see it and set a reminder')} · <b>☰</b> {t('Remind me, Record, Start over')}
        </span>
      </div>
      <button type="button" className="btg-opt" data-walk="tv-guide-options" onClick={onOptions}>
        <Icon name="sliders" size={18} /> {t('Options')}
      </button>
    </section>
  )
}

// Options for the focused programme: Remind me, Record, Start over, and the channel's star.
function OptionsSheet({ channel, program, onClose, onOpen }: { channel: BChannel; program: BProgram; onClose: () => void; onOpen: () => void }) {
  const prefs = useTVPrefs()
  const s = new Date(program.start).getTime()
  const e = new Date(program.end).getTime()
  const now = Date.now()
  const onAir = s <= now && e > now
  const future = s > now
  const [reminded, setReminded] = useState(false)
  const [note, setNote] = useState('')
  useEffect(() => {
    if (!future) return
    tvCall<{ channelId: number; start: string }[]>('GET', '/reminders')
      .then((list) => setReminded(list.some((x) => x.channelId === channel.id && new Date(x.start).getTime() === s)))
      .catch(() => undefined)
  }, [future, channel.id, s])
  const remind = () =>
    tvCall<{ on: boolean; message: string }>('POST', '/remind', { channelId: channel.id, start: program.start })
      .then((r) => {
        setReminded(r.on)
        setNote(r.message)
      })
      .catch((err: Error) => setNote(err.message))
  const fromStart = () => {
    onClose()
    playTitle(program.kind as 'episode' | 'movie', program.itemId, program.subtitle ? `${program.title} · ${program.subtitle}` : program.title)
  }
  // Nothing to record from the library: it's already there, and plays from the start any time.
  const record = () =>
    setNote(playable(program) ? t('{title} is in your library already: Start over plays it from the beginning, any time.', { title: program.title }) : t('A live stream can’t be recorded yet.'))
  const fav = prefs.favs.includes(channel.id)
  return (
    <div className="btg-scrim" onClick={onClose}>
      <div
        className="btg-sheet"
        role="dialog"
        aria-modal="true"
        aria-label={program.title}
        onClick={(ev) => ev.stopPropagation()}
        onKeyDown={(ev) => {
          if (ev.key === 'Escape') {
            ev.preventDefault()
            onClose()
          }
        }}
      >
        <span className="btg-eyebrow">
          {channel.number} · {channel.name} · {clock(s)} – {clock(e)}
        </span>
        <h2>{movieTitle(program)}</h2>
        {(onAir || playable(program)) && (
          <button type="button" className="btg-row-btn" autoFocus onClick={onOpen}>
            <Icon name="play" size={18} /> {onAir ? t('Watch {name}', { name: channel.name }) : t('See it')}
          </button>
        )}
        {future && (
          <button type="button" className="btg-row-btn" aria-pressed={reminded} onClick={() => void remind()} autoFocus={!playable(program)}>
            <Icon name="clock" size={18} /> {reminded ? t('Reminder on') : t('Remind me')}
            <span className="btg-val">{t('A message to your phone five minutes before')}</span>
          </button>
        )}
        <button type="button" className="btg-row-btn" onClick={record}>
          <Icon name="bookmark" size={18} /> {t('Record')}
        </button>
        {onAir && playable(program) && (
          <button type="button" className="btg-row-btn" onClick={fromStart}>
            <Icon name="refresh" size={18} /> {t('Start over')}
          </button>
        )}
        <button type="button" className="btg-row-btn" aria-pressed={fav} onClick={() => setTVPrefs({ favs: toggleIn(prefs.favs, channel.id) })}>
          <Icon name="star" size={18} /> {fav ? t('A favourite channel') : t('Add the channel to favourites')}
        </button>
        {note && <p className="btg-note">{note}</p>}
        <button type="button" className="btg-row-btn" onClick={onClose} autoFocus={!onAir && !playable(program) && !future}>
          <Icon name="x" size={18} /> {t('Close')}
        </button>
      </div>
    </div>
  )
}
