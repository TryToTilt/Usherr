// Usherr: the Marquee, a lobby display for a tablet standing next to the TV
// (portrait, read from across the room). It rotates through scenes in the
// order set in Settings > Extras > Marquee: what's playing now, single films
// (new, in theaters, coming soon) with trailers, what arrived lately, what's
// in cinemas and the week ahead. Open /marquee full-screen. ?sound=1 plays
// trailers with sound, and ?only=new,trailer,... shows only those scenes.
import { useCallback, useEffect, useLayoutEffect, useRef, useState, type CSSProperties, type ReactNode } from 'react'
import { useSearchParams } from 'react-router-dom'
import { locale, t } from '../i18n'
import type { CalendarEntry } from '../api'
import { applyPalette, markSrc } from '../palette'
import './marquee.css'

interface Playing {
  title: string
  series?: string
  season?: number
  episode?: number
  kind: 'movie' | 'episode' | 'other'
  year?: number
  progress: number
  paused: boolean
  device?: string
  posterUrl?: string
  backdropUrl?: string
}
interface Feature {
  tmdbId: number
  title: string
  year?: number
  tagline?: string
  runtime?: number
  rating?: number
  rated?: string
  genres?: string[]
  posterUrl?: string
  backdropUrl?: string
  trailer?: string
  reason: 'new' | 'theaters' | 'coming'
  releaseDate?: string
}
interface Arrival {
  kind: string
  title: string
  year?: number
  posterUrl?: string
  at: string
}
export type SceneKind = 'playing' | 'new' | 'trailer' | 'theaters-film' | 'arrivals' | 'coming-film' | 'theaters' | 'coming'
export interface MarqueeSettings {
  scenes: { kind: SceneKind; on: boolean; seconds: number }[]
  showIcon: boolean
  showName: boolean
  showClock: boolean
  signMode: 'auto' | 'custom'
  signLine1: string
  signLine2: string
  textScale: number
  posterScale: number
  scrollSpeed: 'slow' | 'normal' | 'fast'
  trailerSound: boolean
  fromJellyfin: boolean
  fromStreamNZB: boolean
  profiles: string[]
  quietOn: boolean
  quietFrom: string
  quietTo: string
  quietMode: 'clock' | 'black'
  palette: string
}
interface Data {
  nowPlaying: Playing[]
  features: Feature[]
  arrivals: Arrival[]
  theaters: Feature[]
  coming: CalendarEntry[]
  theatre?: string
  settings: MarqueeSettings
}

type Scene = { kind: 'playing'; p: Playing } | { kind: 'feature'; f: Feature } | { kind: 'trailer'; f: Feature } | { kind: 'arrivals' } | { kind: 'theaters' } | { kind: 'coming' }

const REFRESH_MS = 2 * 60 * 1000
const SPEED = { slow: 18, normal: 28, fast: 44 } // pixels a second when a tall scene scrolls
const REASON: Partial<Record<SceneKind, Feature['reason']>> = { new: 'new', 'theaters-film': 'theaters', 'coming-film': 'coming' }
// ?only= also takes the name from before scenes could be ordered.
const OLD_NAMES: Record<string, SceneKind[]> = { feature: ['new', 'theaters-film', 'coming-film'] }

function heading(s: Scene): string {
  switch (s.kind) {
    case 'playing':
      return t('Now showing')
    case 'feature':
      return s.f.reason === 'new' ? t('New on the server') : s.f.reason === 'theaters' ? t('In theaters now') : t('Coming soon')
    case 'trailer':
      return t('Trailer')
    case 'arrivals':
      return t('Just added')
    case 'theaters':
      return t('In theaters now')
    case 'coming':
      return t('Coming up')
  }
}

const hours = (min?: number) => (min ? `${Math.floor(min / 60)}h ${min % 60}m` : '')

function dayLabel(date: string): string {
  const d = new Date(`${date}T00:00:00`)
  const today = new Date()
  today.setHours(0, 0, 0, 0)
  const diff = Math.round((d.getTime() - today.getTime()) / 86400000)
  if (diff === 0) return t('Today')
  if (diff === 1) return t('Tomorrow')
  return d.toLocaleDateString(locale(), { weekday: 'long', month: 'short', day: 'numeric' })
}

function typeLabel(e: CalendarEntry): string {
  switch (e.kind) {
    case 'episode':
      return e.subtitle?.split(' · ')[0] ?? t('Episode')
    case 'album':
      return t('Album')
    case 'book':
      return t('Book')
    case 'comic':
      return t('Comic')
    default:
      return e.status === 'theaters' ? t('In theaters') : t('Movie')
  }
}

// Quiet hours: "23:30" to "14:00" runs over midnight.
function inQuiet(now: Date, from: string, to: string): boolean {
  const m = now.getHours() * 60 + now.getMinutes()
  const [fh, fm] = from.split(':').map(Number)
  const [th, tm] = to.split(':').map(Number)
  const a = fh * 60 + fm
  const b = th * 60 + tm
  return a <= b ? m >= a && m < b : m >= a || m < b
}

// FitLine keeps a line of the sign on one line, shrinking its type until it
// fits the sign's width.
function FitLine({ text }: { text: string }) {
  const ref = useRef<HTMLSpanElement>(null)
  useLayoutEffect(() => {
    const el = ref.current
    const box = el?.parentElement
    if (!el || !box) return
    const fit = () => {
      el.style.fontSize = ''
      const room = box.clientWidth - 2
      let k = 1
      for (let i = 0; i < 40 && el.scrollWidth > room && k > 0.2; i++) {
        k *= Math.max(0.6, Math.min(0.97, room / el.scrollWidth))
        el.style.fontSize = `calc(var(--mq-sign) * ${k.toFixed(3)})`
      }
    }
    fit()
    const ro = new ResizeObserver(fit)
    ro.observe(box)
    void document.fonts?.ready.then(fit)
    return () => ro.disconnect()
  }, [text])
  return (
    <span ref={ref} className="mq-line">
      {text}
    </span>
  )
}

export default function Marquee() {
  const [params] = useSearchParams()
  const only = params.get('only')
  const [data, setData] = useState<Data | null>(null)
  const [scene, setScene] = useState<{ s: Scene; seconds: number; n: number } | null>(null)
  const [clock, setClock] = useState(() => new Date())
  const failures = useRef(0)
  const dataRef = useRef<Data | null>(null)
  const cursor = useRef({ step: -1, counts: {} as Record<string, number>, lastFeature: null as Feature | null, n: 0 })

  const load = useCallback(() => {
    fetch('/api/home/marquee', { credentials: 'include' })
      .then((r) => (r.ok ? (r.json() as Promise<Data>) : Promise.reject(new Error(String(r.status)))))
      .then((d) => {
        failures.current = 0
        dataRef.current = d
        setData(d)
      })
      .catch(() => {
        failures.current++
        // Signed out (or the server restarted with a new session): back to sign-in.
        if (failures.current >= 3) window.location.reload()
      })
  }, [])

  useEffect(() => {
    load()
    const id = window.setInterval(load, REFRESH_MS)
    return () => window.clearInterval(id)
  }, [load])

  // What's playing changes faster than the rest: asked every 30 seconds.
  useEffect(() => {
    const id = window.setInterval(() => {
      fetch('/api/home/marquee?only=playing', { credentials: 'include' })
        .then((r) => (r.ok ? (r.json() as Promise<Data>) : null))
        .then((d) => {
          if (!d) return
          setData((old) => {
            const next = old ? { ...old, nowPlaying: d.nowPlaying } : old
            dataRef.current = next
            return next
          })
        })
        .catch(() => undefined)
    }, 30_000)
    return () => window.clearInterval(id)
  }, [])

  useEffect(() => {
    const id = window.setInterval(() => setClock(new Date()), 20_000)
    return () => window.clearInterval(id)
  }, [])

  // Keep the screen on where the browser allows it (a kiosk app does this anyway).
  useEffect(() => {
    let lock: { release: () => Promise<void> } | null = null
    const nav = navigator as Navigator & { wakeLock?: { request: (k: 'screen') => Promise<{ release: () => Promise<void> }> } }
    nav.wakeLock
      ?.request('screen')
      .then((l) => (lock = l))
      .catch(() => undefined)
    return () => void lock?.release().catch(() => undefined)
  }, [])

  // The marquee's own colours, when one is set.
  const palette = data?.settings.palette
  useEffect(() => {
    if (palette) applyPalette(palette)
  }, [palette])

  // The next scene: the next one switched on, in the set order, that has
  // something to show; each takes the next film of its kind in turn.
  const advance = useCallback(() => {
    const d = dataRef.current
    if (!d) return
    let order = d.settings.scenes.filter((x) => x.on)
    if (only) {
      const want = new Set(only.split(',').flatMap((w) => OLD_NAMES[w.trim()] ?? [w.trim()]))
      order = d.settings.scenes.filter((x) => want.has(x.kind))
    }
    const c = cursor.current
    const take = <T,>(key: string, list: T[]): T | undefined => {
      if (!list.length) return undefined
      const i = c.counts[key] ?? 0
      c.counts[key] = i + 1
      return list[i % list.length]
    }
    for (let tries = 0; tries < order.length; tries++) {
      c.step = (c.step + 1) % order.length
      const slot = order[c.step]
      let s: Scene | undefined
      const reason = REASON[slot.kind]
      if (slot.kind === 'playing') {
        const p = take('playing', d.nowPlaying)
        if (p) s = { kind: 'playing', p }
      } else if (reason) {
        const f = take(
          slot.kind,
          d.features.filter((x) => x.reason === reason),
        )
        if (f) {
          c.lastFeature = f
          s = { kind: 'feature', f }
        }
      } else if (slot.kind === 'trailer') {
        const f = c.lastFeature?.trailer
          ? c.lastFeature
          : take(
              'trailer',
              d.features.filter((x) => x.trailer),
            )
        if (f?.trailer) {
          c.lastFeature = null
          s = { kind: 'trailer', f }
        }
      } else if (slot.kind === 'arrivals' && d.arrivals.length >= 2) s = { kind: 'arrivals' }
      else if (slot.kind === 'theaters' && d.theaters.length >= 2) s = { kind: 'theaters' }
      else if (slot.kind === 'coming' && d.coming.length) s = { kind: 'coming' }
      if (s) {
        c.n++
        setScene({ s, seconds: slot.seconds, n: c.n })
        return
      }
    }
    // Nothing to show right now: look again shortly.
    setScene(null)
    window.setTimeout(() => advance(), 15_000)
  }, [only])

  const started = useRef(false)
  useEffect(() => {
    if (data && !started.current) {
      started.current = true
      advance()
    }
  }, [data, advance])

  if (!data) return <div className="mq" />
  const set = data.settings
  const sound = set.trailerSound || params.get('sound') === '1'
  const quiet = set.quietOn && inQuiet(clock, set.quietFrom, set.quietTo)
  const time = clock.toLocaleTimeString(locale(), { hour: 'numeric', minute: '2-digit' })
  const vars = { '--mq-t': set.textScale / 100, '--mq-p': set.posterScale / 100 } as CSSProperties

  if (quiet) {
    return <div className={`mq mq-quiet${set.quietMode === 'black' ? ' black' : ''}`}>{set.quietMode === 'clock' && <span className="mq-quiet-clock">{time}</span>}</div>
  }
  const sign = set.signMode === 'custom' ? [set.signLine1, set.signLine2].filter(Boolean) : [scene ? heading(scene.s) : t('Now showing')]
  const top = set.showIcon || set.showName || set.showClock

  return (
    <div className="mq" style={vars} onDoubleClick={() => void document.documentElement.requestFullscreen?.().catch(() => undefined)}>
      {top && (
        <header className="mq-top">
          {set.showIcon && <img className="mq-mark" src={markSrc()} alt="" />}
          {set.showName && <span className="mq-name">Usherr</span>}
          {set.showClock && <span className="mq-clock">{time}</span>}
        </header>
      )}
      {sign.length > 0 && (
        <div className={`mq-marquee${top ? '' : ' alone'}`} aria-live="polite">
          <span className="mq-bulbs" aria-hidden="true" />
          <h1 className={sign.length > 1 ? 'two' : ''}>
            {sign.map((line, i) => (
              <FitLine key={i} text={line} />
            ))}
          </h1>
          <span className="mq-bulbs" aria-hidden="true" />
        </div>
      )}
      <main className="mq-stage">
        {scene ? (
          <SceneFrame key={scene.n} minMs={scene.seconds * 1000} still={scene.s.kind === 'trailer'} speed={SPEED[set.scrollSpeed] ?? SPEED.normal} onDone={advance}>
            <SceneView scene={scene.s} data={data} sound={sound} />
          </SceneFrame>
        ) : (
          <p className="mq-empty">{t('Nothing to show yet.')}</p>
        )}
      </main>
    </div>
  )
}

// SceneFrame shows a scene for its time. When the scene is taller than the
// screen it reads it out slowly: a pause at the top, a slow glide to the
// bottom, a pause, back up, and only then the next scene.
const HOLD_TOP_MS = 4000
const HOLD_BOTTOM_MS = 3000
function SceneFrame({ minMs, still, speed, onDone, children }: { minMs: number; still?: boolean; speed: number; onDone: () => void; children: ReactNode }) {
  const box = useRef<HTMLDivElement>(null)
  useEffect(() => {
    const el = box.current
    let raf = 0
    const timers: number[] = []
    const later = (ms: number, f: () => void) => timers.push(window.setTimeout(f, ms))
    const glide = (from: number, to: number, ms: number, then: () => void) => {
      const start = performance.now()
      const step = (now: number) => {
        const k = Math.min(1, (now - start) / ms)
        const ease = k < 0.5 ? 2 * k * k : 1 - Math.pow(-2 * k + 2, 2) / 2
        if (el) el.scrollTop = from + (to - from) * ease
        if (k < 1) raf = requestAnimationFrame(step)
        else then()
      }
      raf = requestAnimationFrame(step)
    }
    const begun = performance.now()
    const finish = () => later(Math.max(0, minMs - (performance.now() - begun)), onDone)
    later(HOLD_TOP_MS, () => {
      // Measured now, once the posters have loaded.
      const over = el ? el.scrollHeight - el.clientHeight : 0
      if (still || !el || over < 8) {
        finish()
        return
      }
      const down = (over / speed) * 1000
      glide(0, over, down, () => later(HOLD_BOTTOM_MS, () => glide(over, 0, down / 1.5, () => later(1500, finish))))
    })
    return () => {
      cancelAnimationFrame(raf)
      timers.forEach((t) => window.clearTimeout(t))
    }
  }, [minMs, still, speed, onDone])
  return (
    <div ref={box} className="mq-scroll">
      {children}
    </div>
  )
}

function SceneView({ scene, data, sound }: { scene: Scene; data: Data; sound: boolean }) {
  switch (scene.kind) {
    case 'playing': {
      const p = scene.p
      const title = p.kind === 'episode' ? p.series : p.title
      const sub = p.kind === 'episode' ? `S${String(p.season ?? 0).padStart(2, '0')}E${String(p.episode ?? 0).padStart(2, '0')}${p.title ? ` · ${p.title}` : ''}` : p.year ? String(p.year) : ''
      return (
        <div className="mq-scene mq-feature">
          {p.posterUrl ? <img className="mq-poster" src={p.posterUrl} alt="" /> : <div className="mq-poster mq-blank">{title}</div>}
          <h2 className="mq-title">{title}</h2>
          {sub && <p className="mq-meta">{sub}</p>}
          <div className="mq-progress" aria-label={t('Progress')}>
            <span style={{ width: `${Math.min(100, Math.round(p.progress * 100))}%` }} />
          </div>
          <p className="mq-meta mq-dim">
            {p.paused ? t('Paused') : t('Playing')}
            {p.device ? ` · ${p.device}` : ''}
          </p>
        </div>
      )
    }
    case 'feature': {
      const f = scene.f
      const meta = [f.year, f.rated, hours(f.runtime), f.rating ? `★ ${f.rating}` : ''].filter(Boolean).join('  ·  ')
      return (
        <div className="mq-scene mq-feature">
          {f.backdropUrl && <div className="mq-backdrop" style={{ backgroundImage: `url(${f.backdropUrl})` }} aria-hidden="true" />}
          <img className="mq-poster" src={f.posterUrl} alt="" />
          <h2 className="mq-title">{f.title}</h2>
          {f.tagline && <p className="mq-tagline">“{f.tagline}”</p>}
          <p className="mq-meta">{meta}</p>
          {f.genres && f.genres.length > 0 && <p className="mq-meta mq-dim">{f.genres.slice(0, 3).join(' · ')}</p>}
          {f.reason === 'coming' && f.releaseDate && <p className="mq-meta mq-gold">{t('Opens {date}', { date: dayLabel(f.releaseDate) })}</p>}
          {f.reason === 'theaters' && data.theatre && <p className="mq-meta mq-gold">{data.theatre}</p>}
        </div>
      )
    }
    case 'trailer': {
      const f = scene.f
      const q = new URLSearchParams({ autoplay: '1', mute: sound ? '0' : '1', controls: '0', rel: '0', modestbranding: '1', playsinline: '1', iv_load_policy: '3', origin: window.location.origin })
      return (
        <div className="mq-scene mq-trailer">
          <div className="mq-video">
            <iframe
              referrerPolicy="strict-origin-when-cross-origin"
              src={`https://www.youtube-nocookie.com/embed/${encodeURIComponent(f.trailer!)}?${q}`}
              title={f.title}
              allow="autoplay; encrypted-media"
            />
          </div>
          <img className="mq-poster mq-small" src={f.posterUrl} alt="" />
          <h2 className="mq-title">{f.title}</h2>
          <p className="mq-meta">{[f.year, hours(f.runtime)].filter(Boolean).join('  ·  ')}</p>
        </div>
      )
    }
    case 'arrivals':
      return (
        <div className="mq-scene">
          <ul className="mq-grid">
            {data.arrivals.slice(0, 8).map((a) => (
              <li key={a.kind + a.title}>
                <img src={a.posterUrl} alt="" />
                <strong>{a.title}</strong>
                <span>{new Date(a.at).toLocaleDateString(locale(), { weekday: 'short', month: 'short', day: 'numeric' })}</span>
              </li>
            ))}
          </ul>
        </div>
      )
    case 'theaters':
      return (
        <div className="mq-scene">
          {data.theatre && <p className="mq-meta mq-gold mq-center">{data.theatre}</p>}
          <ul className="mq-grid">
            {data.theaters.slice(0, 8).map((f) => (
              <li key={f.tmdbId}>
                <img src={f.posterUrl} alt="" />
                <strong>{f.title}</strong>
              </li>
            ))}
          </ul>
        </div>
      )
    case 'coming': {
      const days = new Map<string, CalendarEntry[]>()
      const today = new Date()
      const todayIso = `${today.getFullYear()}-${String(today.getMonth() + 1).padStart(2, '0')}-${String(today.getDate()).padStart(2, '0')}`
      for (const e of data.coming) if (e.releaseDate >= todayIso) days.set(e.releaseDate, [...(days.get(e.releaseDate) ?? []), e])
      return (
        <div className="mq-scene mq-coming">
          {[...days.entries()].slice(0, 7).map(([day, list]) => (
            <section key={day}>
              <h3>{dayLabel(day)}</h3>
              <ul>
                {list.slice(0, 8).map((e) => (
                  <li key={`${e.kind}-${e.id}`}>
                    {e.posterUrl ? <img className="mq-slim" src={e.posterUrl} alt="" loading="lazy" /> : <span className="mq-slim mq-slim-blank" aria-hidden="true" />}
                    <span className="mq-what">{e.title}</span>
                    <span className="mq-type">{typeLabel(e)}</span>
                  </li>
                ))}
                {list.length > 8 && <li className="mq-dim">{t('+{n} more', { n: list.length - 8 })}</li>}
              </ul>
            </section>
          ))}
        </div>
      )
    }
  }
}
