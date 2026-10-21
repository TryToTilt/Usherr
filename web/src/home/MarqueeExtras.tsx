// Usherr: what the Marquee shows besides films and lists (phase 3): picture
// and sound badges, the info panel, QR codes, tonight on Usherr TV, people in
// the spotlight, film trivia, your own slides, the pre-show (its built-in
// pieces are drawn here, so they are sharp on any screen) and theme music.
import { useCallback, useEffect, useRef, useState, type CSSProperties, type ReactNode } from 'react'
import { QRCode } from '../components/TwoStepCard'
import { locale, t } from '../i18n'
import { hours, type Data, type Feature, type MarqueeSettings, type MediaItem, type Playing, type Person, type PreShowPiece, type Spotlight } from './marqueeData'
import { Sign } from './marqueeParts'

// ---- Badges: what the picture and the sound are. ----
export function Badges({ p }: { p: Playing }) {
  const list = [p.resolution, p.hdr, p.audio, p.audio ? undefined : p.channels].filter(Boolean) as string[]
  if (!list.length) return null
  return (
    <div className="mqx-badges">
      {list.map((b) => (
        <span key={b} className={`mqx-badge b-${b.toLowerCase().replace(/[^a-z0-9]+/g, '')}`}>
          {b}
        </span>
      ))}
    </div>
  )
}

// ---- The info panel: the plot, who is in it, who made it. ----
export function InfoPanel({ overview, cast, director, meta, side }: { overview?: string; cast?: Person[]; director?: Person; meta?: string; side?: ReactNode }) {
  if (!overview && !cast?.length) return null
  return (
    <aside className={`mqx-info${side ? ' has-qr' : ''}`}>
      <div className="mqx-info-main">
        {meta && <p className="mqx-info-meta">{meta}</p>}
        {overview && <p className="mqx-info-plot">{overview}</p>}
        {cast && cast.length > 0 && (
          <ul className="mqx-cast">
            {cast.slice(0, 4).map((c) => (
              <li key={c.id}>
                {c.photo ? <img src={c.photo} alt="" /> : <span className="mqx-cast-ph">{c.name.slice(0, 1)}</span>}
                <strong>{c.name}</strong>
                {c.role && <small>{c.role}</small>}
              </li>
            ))}
          </ul>
        )}
        {director && <p className="mqx-info-meta">{t('Directed by {name}', { name: director.name })}</p>}
      </div>
      {side}
    </aside>
  )
}

// ---- QR codes: the trailer, or the title in Usherr. ----
const qrCache = new Map<string, string[]>()
export function QR({ text, label }: { text: string; label: string }) {
  const [rows, setRows] = useState<string[] | null>(() => qrCache.get(text) ?? null)
  useEffect(() => {
    if (qrCache.has(text)) {
      setRows(qrCache.get(text) ?? null)
      return
    }
    let gone = false
    fetch(`/api/home/marquee/qr?text=${encodeURIComponent(text)}`, { credentials: 'include' })
      .then((r) => (r.ok ? (r.json() as Promise<{ rows: string[] }>) : null))
      .then((d) => {
        if (!d || gone) return
        qrCache.set(text, d.rows)
        setRows(d.rows)
      })
      .catch(() => undefined)
    return () => {
      gone = true
    }
  }, [text])
  if (!rows) return null
  return (
    <div className="mqx-qr">
      <QRCode rows={rows} label={label} />
      <span>{label}</span>
    </div>
  )
}

export function qrFor(f: { tmdbId?: number; trailer?: string } | undefined, mode: MarqueeSettings['qr']): { text: string; label: string } | null {
  if (!f || mode === 'off') return null
  if (mode === 'trailer' && f.trailer) return { text: `https://www.youtube.com/watch?v=${f.trailer}`, label: t('Watch the trailer') }
  if (mode === 'usherr' && f.tmdbId) return { text: `${window.location.origin}/title/${f.tmdbId}`, label: t('Open in Usherr') }
  return null
}

// ---- Trivia, from TMDB's facts (no AI). ----
const money = (n: number) => (n >= 1e9 ? t('${n} billion', { n: (n / 1e9).toFixed(1) }) : t('${n} million', { n: Math.round(n / 1e6) }))
const listOf = (names: string[]) => {
  try {
    return new Intl.ListFormat(locale(), { type: 'conjunction' }).format(names)
  } catch {
    return names.join(', ')
  }
}

export function filmFacts(f: Feature): string[] {
  const out: string[] = []
  if (f.budget && f.budget >= 1e6 && f.revenue && f.revenue >= 1e6) out.push(t('{title} cost {budget} to make and took {revenue} at the box office.', { title: f.title, budget: money(f.budget), revenue: money(f.revenue) }))
  else if (f.budget && f.budget >= 1e6) out.push(t('{title} cost {budget} to make.', { title: f.title, budget: money(f.budget) }))
  if (f.director && f.directorFilms?.length) out.push(t('{director}, who directed {title}, also made {films}.', { director: f.director.name, title: f.title, films: listOf(f.directorFilms.map((x) => `“${x}”`)) }))
  const [a, b] = f.cast ?? []
  if (a?.role && b?.role) out.push(t('{a} plays {ra}, and {b} plays {rb}.', { a: a.name, ra: a.role, b: b.name, rb: b.role }))
  if (f.collection) out.push(t('{title} is part of {collection}.', { title: f.title, collection: f.collection }))
  if (f.originalTitle) out.push(t('Its original title is “{original}”.', { original: f.originalTitle }))
  if (f.studio) out.push(f.country ? t('{title} comes from {studio}, in {country}.', { title: f.title, studio: f.studio, country: f.country }) : t('{title} comes from {studio}.', { title: f.title, studio: f.studio }))
  if (f.releaseDate) {
    const d = new Date(`${f.releaseDate}T00:00:00`)
    const now = new Date()
    const years = now.getFullYear() - d.getFullYear()
    const thisYear = new Date(now.getFullYear(), d.getMonth(), d.getDate())
    if (years >= 1 && Math.abs(thisYear.getTime() - now.getTime()) < 7 * 86400000) out.push(t('{title} came out {n} years ago this week.', { title: f.title, n: years }))
  }
  return out
}

// trivia lists every film's facts, taking turns between the films.
export function trivia(features: Feature[]): { f: Feature; fact: string }[] {
  const each = features.map((f) => ({ f, facts: filmFacts(f) }))
  const out: { f: Feature; fact: string }[] = []
  for (let i = 0; i < 8; i++) for (const x of each) if (x.facts[i]) out.push({ f: x.f, fact: x.facts[i] })
  return out
}

const clock = (iso: string) => new Date(iso).toLocaleTimeString(locale(), { hour: 'numeric', minute: '2-digit' })

// ---- Tonight on Usherr TV: each channel's next hours, with times. ----
export function TonightView({ data, header }: { data: Data; header: boolean }) {
  const now = Date.now()
  return (
    <div className="lk lk-x lk-tonight">
      {header && <Sign lines={[t('Tonight on Usherr TV')]} />}
      <div className="lk-b-list">
        {(data.tonight ?? []).map((c) => (
          <div key={c.number} className="lk-b-row tv">
            <span className="lk-b-ch">{c.logo ? <img src={c.logo} alt="" /> : c.number}</span>
            <div className="lk-b-what">
              <strong>{c.name}</strong>
              <div className="mqx-times">
                {c.shows.map((s, i) => {
                  const on = new Date(s.start).getTime() <= now
                  return (
                    <span key={i} className={`mqx-time${on ? ' now' : ''}`}>
                      <b>{on ? t('Now') : clock(s.start)}</b>
                      <span>{s.title}</span>
                    </span>
                  )
                })}
              </div>
            </div>
          </div>
        ))}
      </div>
    </div>
  )
}

// ---- In the spotlight: a director or a star, with their films. ----
export function SpotlightView({ sp, header }: { sp: Spotlight; header: boolean }) {
  return (
    <div className="lk lk-x lk-spot">
      {header && <Sign lines={[t('In the spotlight')]} />}
      <div className="mqx-spot-body">
        <div className="mqx-spot-head">
          {sp.photo && <img className="mqx-spot-photo" src={sp.photo} alt="" />}
          <div>
            <span className="lk-label">{sp.known === 'Directing' ? t('Director') : t('Actor')}</span>
            <h2 className="lk-title">{sp.name}</h2>
            {sp.bio && <p className="mqx-spot-bio">{sp.bio}</p>}
          </div>
        </div>
        <ul className="mqx-spot-films">
          {sp.films.map((f) => (
            <li key={f.tmdbId}>
              <span className="mqx-spot-poster">
                <img src={f.posterUrl} alt="" />
                {f.have && <b>{t('On Usherr')}</b>}
              </span>
              <span>
                {f.title}
                {f.year ? ` (${f.year})` : ''}
              </span>
            </li>
          ))}
        </ul>
      </div>
    </div>
  )
}

// ---- Did you know? ----
export function TriviaView({ f, fact, header }: { f: Feature; fact: string; header: boolean }) {
  return (
    <div className="lk lk-x lk-trivia">
      {f.backdropUrl && <div className="mqx-trivia-bg" style={{ backgroundImage: `url(${f.backdropUrl})` }} aria-hidden="true" />}
      {header && <Sign lines={[t('Did you know?')]} />}
      <div className="mqx-trivia-body">
        {f.posterUrl && <img className="mqx-trivia-poster" src={f.posterUrl} alt="" />}
        <p className="mqx-fact">{fact}</p>
        <p className="lk-meta">{[f.title, f.year, hours(f.runtime)].filter(Boolean).join('  ·  ')}</p>
      </div>
    </div>
  )
}

// ---- Your slides and pre-show pieces. ----
export function MediaSlide({ m, sound, onEnd, header }: { m: MediaItem; sound: boolean; onEnd: () => void; header?: boolean }) {
  const done = useRef(onEnd)
  useEffect(() => {
    done.current = onEnd
  })
  useEffect(() => {
    // A video ends by itself; anything else after its seconds. A video that
    // doesn't play still moves on, after its seconds or ten minutes at most.
    const ms = m.kind === 'video' ? 10 * 60_000 : m.seconds * 1000
    const id = window.setTimeout(() => done.current(), ms)
    return () => window.clearTimeout(id)
  }, [m])
  switch (m.kind) {
    case 'image':
      return (
        <div className="lk lk-x lk-media">
          <img className="mqx-media-fill kb kb-0" style={{ '--kb-s': `${m.seconds + 4}s` } as CSSProperties} src={m.fileUrl} alt={m.title} />
          {header && m.title && <span className="mqx-media-title">{m.title}</span>}
        </div>
      )
    case 'video':
      return (
        <div className="lk lk-x lk-media">
          <video className="mqx-media-fill contain" src={m.fileUrl} autoPlay muted={!sound} playsInline onEnded={() => done.current()} onError={() => done.current()} />
        </div>
      )
    case 'web':
      return (
        <div className="lk lk-x lk-media">
          <iframe className="mqx-media-fill" src={m.url} title={m.title || m.url} sandbox="allow-scripts allow-same-origin" referrerPolicy="no-referrer" />
        </div>
      )
    default:
      return (
        <div className="lk lk-x lk-words">
          {header && m.title && <Sign lines={[m.title]} />}
          {m.body && <p className="mqx-words">{m.body}</p>}
        </div>
      )
  }
}

// ---- The pre-show's built-in pieces. ----
export const PRESHOW_LABEL: Record<PreShowPiece, string> = {
  popcorn: t('Grab your popcorn'),
  silence: t('Please silence your phones'),
  coming: t('Coming attractions'),
  countdown: t('Countdown'),
  feature: t('Feature presentation'),
}
const PIECE_MS: Record<PreShowPiece, number> = { popcorn: 8000, silence: 7000, coming: 6500, countdown: 6000, feature: 7500 }

function Piece({ piece, onEnd }: { piece: PreShowPiece; onEnd: () => void }) {
  const done = useRef(onEnd)
  useEffect(() => {
    done.current = onEnd
  })
  useEffect(() => {
    const id = window.setTimeout(() => done.current(), PIECE_MS[piece])
    return () => window.clearTimeout(id)
  }, [piece])
  switch (piece) {
    case 'popcorn':
      return (
        <div className="lk lk-x ps ps-popcorn">
          <div className="ps-bucket" aria-hidden="true">
            {Array.from({ length: 14 }, (_, i) => (
              <i key={i} className="ps-kernel" style={{ '--i': i } as CSSProperties} />
            ))}
            <span className="ps-bucket-body">
              <b>{t('Popcorn')}</b>
            </span>
          </div>
          <h1 className="ps-big">{t('Grab your popcorn')}</h1>
          <p className="ps-small">{t('The show starts in a moment')}</p>
        </div>
      )
    case 'silence':
      return (
        <div className="lk lk-x ps ps-silence">
          <svg className="ps-phone" viewBox="0 0 120 120" aria-hidden="true">
            <rect x="38" y="14" width="44" height="80" rx="8" />
            <line x1="52" y1="84" x2="68" y2="84" />
            <path className="ps-wave w1" d="M92 40 q8 14 0 28" />
            <path className="ps-wave w2" d="M100 32 q14 22 0 44" />
            <path className="ps-wave w1" d="M28 40 q-8 14 0 28" />
            <path className="ps-wave w2" d="M20 32 q-14 22 0 44" />
            <line className="ps-slash" x1="16" y1="104" x2="104" y2="16" />
          </svg>
          <h1 className="ps-big">{t('Please silence your phones')}</h1>
          <p className="ps-small">{t('and enjoy the show')}</p>
        </div>
      )
    case 'coming':
      return (
        <div className="lk lk-x ps ps-coming">
          <span className="ps-beam b1" aria-hidden="true" />
          <span className="ps-beam b2" aria-hidden="true" />
          <h1 className="ps-big">{t('Coming attractions')}</h1>
          <span className="ps-curtain l" aria-hidden="true" />
          <span className="ps-curtain r" aria-hidden="true" />
        </div>
      )
    case 'countdown':
      return (
        <div className="lk lk-x ps ps-countdown">
          <div className="ps-leader" aria-hidden="true">
            <span className="ps-sweep" />
            <span className="ps-ring r1" />
            <span className="ps-ring r2" />
            <span className="ps-cross" />
            {[5, 4, 3, 2, 1].map((n, i) => (
              <b key={n} className="ps-num" style={{ '--i': i } as CSSProperties}>
                {n}
              </b>
            ))}
          </div>
        </div>
      )
    case 'feature':
      return (
        <div className="lk lk-x ps ps-feature">
          <span className="ps-rays" aria-hidden="true" />
          <div className="ps-feature-text">
            <span className="ps-small">{t('And now')}</span>
            <h1 className="ps-big">{t('Our feature presentation')}</h1>
          </div>
        </div>
      )
  }
}

function TrailerStep({ f, sound, seconds, onEnd }: { f: Feature; sound: boolean; seconds: number; onEnd: () => void }) {
  const done = useRef(onEnd)
  useEffect(() => {
    done.current = onEnd
  })
  useEffect(() => {
    const id = window.setTimeout(() => done.current(), seconds * 1000)
    return () => window.clearTimeout(id)
  }, [seconds])
  const q = new URLSearchParams({ autoplay: '1', mute: sound ? '0' : '1', controls: '0', rel: '0', modestbranding: '1', playsinline: '1', iv_load_policy: '3', origin: window.location.origin })
  return (
    <div className="lk lk-x ps ps-trailer">
      <div className="lk-video">
        <iframe referrerPolicy="strict-origin-when-cross-origin" src={`https://www.youtube-nocookie.com/embed/${encodeURIComponent(f.trailer ?? '')}?${q}`} title={f.title} allow="autoplay; encrypted-media" />
      </div>
      <p className="lk-meta">{f.title}</p>
    </div>
  )
}

type Step = { kind: 'piece'; piece: PreShowPiece } | { kind: 'media'; m: MediaItem } | { kind: 'trailer'; f: Feature }

// preShowSteps puts the pre-show together: popcorn and phones, your own
// pieces, coming attractions and trailers, the countdown, the feature.
export function preShowSteps(data: Data, set: MarqueeSettings): Step[] {
  const has = (p: PreShowPiece) => set.preShowBuiltins.includes(p)
  const steps: Step[] = []
  for (const p of ['popcorn', 'silence'] as const) if (has(p)) steps.push({ kind: 'piece', piece: p })
  for (const m of data.media ?? []) if (m.place === 'preshow') steps.push({ kind: 'media', m })
  if (has('coming')) steps.push({ kind: 'piece', piece: 'coming' })
  for (const f of data.features.filter((x) => x.trailer && x.reason !== 'new').slice(0, set.preShowTrailers)) steps.push({ kind: 'trailer', f })
  for (const p of ['countdown', 'feature'] as const) if (has(p)) steps.push({ kind: 'piece', piece: p })
  return steps
}

export function PreShow({ data, set, sound, trailerSeconds, onDone }: { data: Data; set: MarqueeSettings; sound: boolean; trailerSeconds: number; onDone: () => void }) {
  const [steps] = useState(() => preShowSteps(data, set))
  const [i, setI] = useState(0)
  const next = useCallback(() => setI((x) => x + 1), [])
  const finished = useRef(onDone)
  useEffect(() => {
    finished.current = onDone
  })
  useEffect(() => {
    if (i >= steps.length) finished.current()
  }, [i, steps.length])
  const step = steps[i]
  if (!step) return <div className="lk lk-x ps" />
  switch (step.kind) {
    case 'piece':
      return <Piece key={i} piece={step.piece} onEnd={next} />
    case 'media':
      return <MediaSlide key={i} m={step.m} sound={sound} onEnd={next} />
    default:
      return <TrailerStep key={i} f={step.f} sound={sound} seconds={Math.min(trailerSeconds, 150)} onEnd={next} />
  }
}

// ---- Theme music, faded in and out. ----
export function ThemeMusic({ url, volume }: { url: string; volume: number }) {
  const audio = useRef<HTMLAudioElement>(null)
  useEffect(() => {
    const a = audio.current
    if (!a || !url) return
    const target = Math.max(0.05, Math.min(1, volume / 100))
    a.src = url
    a.volume = 0
    void a.play().catch(() => undefined)
    let v = 0
    const up = window.setInterval(() => {
      v = Math.min(target, v + target / 20)
      a.volume = v
      if (v >= target) window.clearInterval(up)
    }, 75)
    return () => {
      window.clearInterval(up)
      const down = window.setInterval(() => {
        a.volume = Math.max(0, a.volume - target / 15)
        if (a.volume <= 0.01) {
          window.clearInterval(down)
          a.pause()
        }
      }, 60)
    }
  }, [url, volume])
  return <audio ref={audio} loop preload="none" />
}

// Whether the pre-show has anything to show.
export function preShowHasSomething(data: Data, set: MarqueeSettings): boolean {
  return preShowSteps(data, set).length > 0
}
