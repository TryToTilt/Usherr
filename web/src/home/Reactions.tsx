// Usherr: linked screenings, step 4 (internal/api/home_link_chat.go).
// Reactions pinned to the moment: HA, EEK, WOW, NOPE and CLAP. Sending one
// floats it up over the picture in every house at once; each is kept at its
// second of the film, so it floats up again for anyone who plays that part
// later. Small bars above the progress show where the crew reacted.
import { useEffect, useRef, useState } from 'react'
import { t } from '../i18n'
import { subscribeParty } from './partyStream'
import { BUCKET, dueReplays, filmClock, heatOf, isWord, WORDS, type HeatBar, type Reaction, type Word } from './reactionMath'
import './reactions.css'

export interface Moment {
  from: number
  to: number
  peak: number
  total: number
  words: Partial<Record<Word, number>>
  top: Word
}

export interface ReactionsAnswer {
  bucket: number
  runtime?: number // seconds
  words: Word[]
  heat: HeatBar[] | null
  top: Moment | null
  reactions: Reaction[] | null
}

async function call<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(`/api/home${path}`, {
    method,
    credentials: 'include',
    cache: 'no-store',
    headers: body === undefined ? undefined : { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  const data = (await res.json().catch(() => null)) as (T & { error?: string }) | null
  if (!res.ok) throw new Error(data?.error ?? t('Something went wrong (error {status}).', { status: res.status }))
  return data as T
}

export const sendReaction = (showtimeId: number, word: Word, pos?: number) =>
  call<{ reaction: Reaction }>('POST', `/showtimes/${showtimeId}/react`, pos === undefined || !Number.isFinite(pos) ? { word } : { word, pos }).then((d) => d.reaction)

export type FilmRef = { tmdb?: number; movie?: number }

const loadReactions = (src: { showtimeId?: number; film?: FilmRef }) => {
  if (src.showtimeId) return call<ReactionsAnswer>('GET', `/showtimes/${src.showtimeId}/reactions`)
  const q = new URLSearchParams()
  if (src.film?.tmdb) q.set('tmdb', String(src.film.tmdb))
  else if (src.film?.movie) q.set('movie', String(src.film.movie))
  return call<ReactionsAnswer>('GET', `/reactions/film?${q.toString()}`)
}

// useReactions keeps a screening's (or a film's) reactions: read once, and
// for a screening, live as they come. onLive hears each new one as it
// arrives (to float it at once).
export function useReactions(src: { showtimeId?: number; film?: FilmRef } | null, onLive?: (r: Reaction) => void): { list: Reaction[]; runtime: number } {
  const [list, setList] = useState<Reaction[]>([])
  const [runtime, setRuntime] = useState(0)
  const live = useRef(onLive)
  useEffect(() => {
    live.current = onLive
  })
  const showtimeId = src?.showtimeId
  const tmdb = src?.film?.tmdb
  const movie = src?.film?.movie
  useEffect(() => {
    if (!showtimeId && !tmdb && !movie) return
    let on = true
    const load = () =>
      loadReactions({ showtimeId, film: { tmdb, movie } })
        .then((d) => {
          if (!on) return
          setList(d.reactions ?? [])
          setRuntime(d.runtime ?? 0)
        })
        .catch(() => undefined)
    void load()
    if (!showtimeId) return () => void (on = false)
    const off = subscribeParty(
      showtimeId,
      (ev) => {
        if (ev.kind !== 'react') return
        const r = ev.message as Reaction
        if (!r || !isWord(r.word)) return
        setList((l) => (l.some((x) => x.id === r.id) ? l : [...l, r]))
        live.current?.(r)
      },
      () => void load(),
    )
    return () => {
      on = false
      off()
    }
  }, [showtimeId, tmdb, movie])
  return { list, runtime }
}

const errText = (e: unknown) => (e instanceof Error ? e.message : String(e))

// ReactButtons: the five reactions. position is where the film is here
// (seconds; the server's room is used when it isn't given).
export function ReactButtons({ showtimeId, position, big, onSent }: { showtimeId: number; position?: () => number; big?: boolean; onSent?: (r: Reaction) => void }) {
  const [error, setError] = useState('')
  const [pops, setPops] = useState<{ id: number; word: Word; left: number }[]>([])
  const n = useRef(0)
  const send = (word: Word, i: number) => {
    setError('')
    const id = ++n.current
    setPops((p) => [...p, { id, word, left: 4 + i * 19 }])
    window.setTimeout(() => setPops((p) => p.filter((x) => x.id !== id)), 1300)
    sendReaction(showtimeId, word, position?.())
      .then((r) => onSent?.(r))
      .catch((e: unknown) => setError(errText(e)))
  }
  return (
    <div className={`rx-bar${big ? ' rx-big' : ''}`} role="group" aria-label={t('Reactions')}>
      {pops.map((p) => (
        <span key={p.id} className={`rx-pop rx-${p.word}`} style={{ left: `${p.left}%` }} aria-hidden="true">
          {p.word}
        </span>
      ))}
      {WORDS.map((w, i) => (
        <button key={w} type="button" className={`rx-btn rx-${w}`} onClick={() => send(w, i)} aria-label={t('React: {word}', { word: w })} title={t('React: {word}', { word: w })}>
          {w}
        </button>
      ))}
      {error && <span className="rx-error" role="status">{error}</span>}
    </div>
  )
}

// HeatStrip: small bars over the film's length where the crew reacted
// (taller where more did), and where the film is now.
export function HeatStrip({ list, length, position, label }: { list: Reaction[]; length: number; position?: () => number; label?: boolean }) {
  const [, setTick] = useState(0)
  const ticking = !!position
  useEffect(() => {
    if (!ticking) return
    const id = window.setInterval(() => setTick((n) => n + 1), 1000)
    return () => window.clearInterval(id)
  }, [ticking])
  const heat = heatOf(list)
  const end = Math.max(length, heat.length ? heat[heat.length - 1].pos + BUCKET : 0)
  if (end <= 0) return null
  const most = Math.max(1, ...heat.map((h) => h.count))
  const now = position ? Math.max(0, Math.min(end, position())) : -1
  return (
    <div className="rx-heat-wrap">
      <div className="rx-heat" role="img" aria-label={heat.length ? t('Where the crew reacted') : t('No reactions yet')}>
        {heat.map((h) => (
          <span
            key={h.pos}
            className={`rx-heat-bar rx-${h.top}`}
            style={{ left: `${(h.pos / end) * 100}%`, height: `${20 + (h.count / most) * 80}%` }}
            title={t('{time}: {n} × {word}', { time: filmClock(h.pos), n: h.count, word: h.top })}
          />
        ))}
      </div>
      {now >= 0 && (
        <div className="rx-progress" aria-hidden="true">
          <span style={{ width: `${(now / end) * 100}%` }} />
        </div>
      )}
      {label && (
        <div className="rx-heat-legend">
          <span>{now >= 0 ? filmClock(now) : ''}</span>
          <span>{t('Bars: where the crew reacted')}</span>
          <span>{filmClock(end)}</span>
        </div>
      )}
    </div>
  )
}

type Float = Reaction & { key: number; left: number }
const FLOAT_MS = 2600
const MOST_FLOATS = 14

// ReactionLayer floats reactions up over the picture: a screening's as
// they're sent (and again as the film plays past a kept one), or a film's
// kept ones for someone watching it later. Drawn inside the player's stage.
export function ReactionLayer({ list, position, liveRef }: { list: Reaction[]; position: () => number; liveRef?: { current: ((r: Reaction) => void) | null } }) {
  const [floats, setFloats] = useState<Float[]>([])
  const shown = useRef(new Set<string>())
  const n = useRef(0)
  const listRef = useRef(list)
  const pos = useRef(position)
  useEffect(() => {
    listRef.current = list
    pos.current = position
  })
  useEffect(() => {
    const float = (r: Reaction) => {
      if (shown.current.has(r.id)) return
      shown.current.add(r.id)
      const key = ++n.current
      setFloats((f) => [...f, { ...r, key, left: 6 + Math.random() * 80 }].slice(-MOST_FLOATS))
      window.setTimeout(() => setFloats((f) => f.filter((x) => x.key !== key)), FLOAT_MS)
    }
    if (liveRef) liveRef.current = float
    let last = pos.current()
    const id = window.setInterval(() => {
      const now = pos.current()
      for (const r of dueReplays(listRef.current, last, now, shown.current)) float(r)
      // Gone back to before a reaction: it may float again.
      if (now < last - 2) for (const r of listRef.current) if (r.pos > now) shown.current.delete(r.id)
      last = now
    }, 250)
    return () => {
      window.clearInterval(id)
      if (liveRef) liveRef.current = null
    }
  }, [liveRef])
  if (floats.length === 0) return null
  return (
    <div className="rx-layer" aria-hidden="true">
      {floats.map((f) => (
        <span key={f.key} className={`rx-float rx-${f.word}`} style={{ left: `${f.left}%` }}>
          {f.word}
          {f.name && <small>{f.name}</small>}
        </span>
      ))}
    </div>
  )
}
