// Usherr: the Marquee's looks besides its own (Marquee phase 2), each in the
// spirit of a project people love: Cinema and Poster wall and Screensaver
// (Posterrama), the Showtimes board (ScreenStage), Posterr (posterr) and the
// One-sheet (now-showing-ha). Our own code; the ideas are theirs.
import { useCallback, useEffect, useRef, useState, type CSSProperties, type ReactNode } from 'react'
import { locale, t } from '../i18n'
import { markSrc } from '../palette'
import { dayLabel, hours, typeLabel, type Data, type Feature, type MarqueeSettings, type MarqueeStyle, type Playing, type SceneKind } from './marqueeData'
import { Badges } from './MarqueeExtras'
import { Frame, Pic, Sign } from './marqueeParts'

// Item is one thing a look shows: a film, a show, what's playing.
export interface Item {
  key: string
  kind: SceneKind
  label: string
  title: string
  sub?: string
  tagline?: string
  meta?: string
  genres?: string[]
  posterUrl?: string
  backdropUrl?: string
  trailer?: string
  note?: string
  playing?: Playing
  feature?: Feature // the film, for the info panel, the QR code and its theme music
  themeKey?: string // "movie:{tmdbId}" or "tv:{seriesId}", for an arrival's theme music
}

export function sceneLabel(kind: SceneKind): string {
  switch (kind) {
    case 'playing':
      return t('Now playing')
    case 'new':
      return t('New on the server')
    case 'theaters-film':
    case 'theaters':
      return t('In theaters now')
    case 'coming-film':
    case 'coming':
      return t('Coming soon')
    case 'arrivals':
      return t('Just added')
    case 'trailer':
      return t('Trailer')
    case 'tvnow':
      return t('On Live TV')
    case 'tonight':
      return t('Tonight on Usherr TV')
    case 'spotlight':
      return t('In the spotlight')
    case 'trivia':
      return t('Did you know?')
    case 'slides':
      return t('Now showing')
    case 'preshow':
      return t('Pre-show')
  }
}

// Posterr's banner words.
function posterrLabel(kind: SceneKind): string {
  switch (kind) {
    case 'playing':
      return t('Now playing')
    case 'new':
    case 'arrivals':
      return t('On demand')
    case 'theaters-film':
    case 'theaters':
      return t('Now screening')
    case 'trailer':
      return t('Feature presentation')
    default:
      return t('Coming soon')
  }
}

const episodeLine = (p: Playing) => `S${String(p.season ?? 0).padStart(2, '0')}E${String(p.episode ?? 0).padStart(2, '0')}${p.title ? ` · ${p.title}` : ''}`

// itemsFor lists what a scene kind has to show, one item each.
export function itemsFor(kind: SceneKind, d: Data): Item[] {
  const label = sceneLabel(kind)
  const film = (reason: string) =>
    d.features
      .filter((f) => f.reason === reason)
      .map<Item>((f) => ({
        key: `${kind}-${f.tmdbId}`,
        kind,
        label,
        title: f.title,
        tagline: f.tagline,
        meta: [f.year, f.rated, hours(f.runtime), f.rating ? `★ ${f.rating}` : ''].filter(Boolean).join('  ·  '),
        genres: f.genres,
        posterUrl: f.posterUrl,
        backdropUrl: f.backdropUrl,
        note: reason === 'coming' && f.releaseDate ? t('Opens {date}', { date: dayLabel(f.releaseDate) }) : reason === 'theaters' ? d.theatre : undefined,
        feature: f,
      }))
  switch (kind) {
    case 'playing':
      return d.nowPlaying.map((p, i) => ({
        key: `playing-${i}-${p.title}`,
        kind,
        label,
        title: (p.kind === 'episode' ? p.series : p.title) || p.title,
        sub: p.kind === 'episode' ? episodeLine(p) : undefined,
        meta: [p.year, hours(p.runtime)].filter(Boolean).join('  ·  '),
        posterUrl: p.posterUrl,
        backdropUrl: p.backdropUrl,
        note: p.device,
        playing: p,
      }))
    case 'new':
      return film('new')
    case 'theaters-film':
      return film('theaters')
    case 'coming-film':
      return film('coming')
    case 'trailer':
      return d.features
        .filter((f) => f.trailer)
        .map((f) => ({ key: `trailer-${f.tmdbId}`, kind, label, title: f.title, meta: [f.year, hours(f.runtime)].filter(Boolean).join('  ·  '), posterUrl: f.posterUrl, backdropUrl: f.backdropUrl, trailer: f.trailer, feature: f }))
    case 'arrivals':
      return d.arrivals.map((a) => ({
        key: `arrivals-${a.kind}-${a.title}`,
        kind,
        label,
        title: a.title,
        meta: a.year ? String(a.year) : undefined,
        note: t('Added {date}', { date: new Date(a.at).toLocaleDateString(locale(), { weekday: 'short', month: 'short', day: 'numeric' }) }),
        posterUrl: a.posterUrl,
        themeKey: a.kind === 'movie' ? `movie:${a.tmdbId ?? 0}` : `tv:${a.seriesId ?? 0}`,
      }))
    case 'theaters':
      return d.theaters.map((f) => ({
        key: `theaters-${f.tmdbId}`,
        kind,
        label,
        title: f.title,
        tagline: f.tagline,
        meta: [f.year, f.rated, hours(f.runtime)].filter(Boolean).join('  ·  '),
        genres: f.genres,
        posterUrl: f.posterUrl,
        backdropUrl: f.backdropUrl,
        note: d.theatre,
        feature: f,
      }))
    case 'coming':
      return d.coming
        .filter((e) => e.posterUrl)
        .map((e) => ({ key: `coming-${e.kind}-${e.id}`, kind, label, title: e.title, meta: typeLabel(e), note: dayLabel(e.releaseDate), posterUrl: e.posterUrl }))
    default:
      return []
  }
}

// The pool for the poster wall: every picture the switched-on scenes have, once.
export function wallItems(kinds: SceneKind[], d: Data): Item[] {
  const seen = new Set<string>()
  const out: Item[] = []
  for (const k of kinds) {
    if (k === 'trailer') continue // the same films, under another name
    for (const it of itemsFor(k, d)) {
      if (!it.posterUrl || seen.has(it.posterUrl)) continue
      seen.add(it.posterUrl)
      out.push(it)
    }
  }
  return out
}

function minutesLeft(p: Playing): number | undefined {
  if (!p.runtime) return undefined
  return Math.max(0, Math.round(p.runtime * (1 - p.progress)))
}

function endsAt(p: Playing): string | undefined {
  const left = minutesLeft(p)
  if (left === undefined || p.paused) return undefined
  return new Date(Date.now() + left * 60_000).toLocaleTimeString(locale(), { hour: 'numeric', minute: '2-digit' })
}

const how = (p: Playing) => (p.playMethod === 'Transcode' ? 'transcode' : p.playMethod ? 'direct' : '')

function PlayBar({ p }: { p: Playing }) {
  return (
    <div className={`lk-bar ${how(p)}`} aria-label={t('Progress')}>
      <span style={{ width: `${Math.min(100, Math.round(p.progress * 100))}%` }} />
    </div>
  )
}

function Trailer({ it, sound }: { it: Item; sound: boolean }) {
  const q = new URLSearchParams({ autoplay: '1', mute: sound ? '0' : '1', controls: '0', rel: '0', modestbranding: '1', playsinline: '1', iv_load_policy: '3', origin: window.location.origin })
  return (
    <div className="lk-video">
      <iframe referrerPolicy="strict-origin-when-cross-origin" src={`https://www.youtube-nocookie.com/embed/${encodeURIComponent(it.trailer ?? '')}?${q}`} title={it.title} allow="autoplay; encrypted-media" />
    </div>
  )
}

// Details under a poster, as much as the footer setting asks for.
function Details({ it, footer, badges }: { it: Item; footer: MarqueeSettings['footer']; badges: boolean }) {
  if (footer === 'none') return it.playing ? <PlayBar p={it.playing} /> : null
  const line = footer === 'tagline' ? it.tagline || it.title : it.title
  return (
    <>
      <h2 className={footer === 'tagline' && it.tagline ? 'lk-tag' : 'lk-title'}>{footer === 'tagline' && it.tagline ? `“${it.tagline}”` : line}</h2>
      {footer === 'details' && (
        <>
          {it.sub && <p className="lk-meta">{it.sub}</p>}
          {it.meta && <p className="lk-meta">{it.meta}</p>}
          {it.tagline && <p className="lk-tag">“{it.tagline}”</p>}
          {it.note && <p className="lk-meta lk-note">{it.note}</p>}
        </>
      )}
      {it.playing && <PlayBar p={it.playing} />}
      {it.playing && badges && <Badges p={it.playing} />}
    </>
  )
}

// ItemLook shows one item in one of the single-poster looks.
export function ItemLook({ style, it, set, n, seconds, sign, time, portrait, sound }: { style: MarqueeStyle; it: Item; set: MarqueeSettings; n: number; seconds: number; sign: string[] | null; time: string; portrait: boolean; sound: boolean }) {
  const kb = set.kenBurns ? n : undefined
  const video = it.trailer && it.kind === 'trailer' ? <Trailer it={it} sound={sound} /> : null
  const pic = video ?? <Pic src={it.posterUrl} framing={set.framing} kb={kb} seconds={seconds} blank={it.title} />
  const lines = sign ?? [style === 'posterr' ? posterrLabel(it.kind) : it.label]

  switch (style) {
    case 'posterr': {
      const p = it.playing
      const left = p ? minutesLeft(p) : undefined
      return (
        <div className="lk lk-posterr">
          <div className="lk-p-banner">
            <Sign lines={lines} />
          </div>
          <div className="lk-p-poster lk-fit">{pic}</div>
          {(set.footer !== 'none' || p) && (
            <div className="lk-p-foot">
              {set.footer !== 'none' && <h2 className="lk-title">{set.footer === 'tagline' && it.tagline ? it.tagline : it.title}</h2>}
              {p ? (
                <>
                  <PlayBar p={p} />
                  {set.badges === 'on' && <Badges p={p} />}
                  <div className="lk-p-how">
                    {p.playMethod && <b className={how(p)}>{p.playMethod === 'Transcode' ? t('Converting') : t('Direct play')}</b>}
                    {p.paused && <span>{t('Paused')}</span>}
                    {p.device && <span>{p.device}</span>}
                    {left !== undefined && <span>{t('{n} min left', { n: left })}</span>}
                  </div>
                </>
              ) : (
                set.footer === 'details' && (it.meta || it.note) && <p className="lk-meta">{[it.meta, it.note].filter(Boolean).join('  ·  ')}</p>
              )}
            </div>
          )}
        </div>
      )
    }
    case 'onesheet':
      return (
        <div className="lk lk-one">
          <Sign lines={lines} />
          <div className="lk-o-stage lk-fit">{video ?? <Frame kind={set.frame}>{pic}</Frame>}</div>
          <div className="lk-o-info">
            <Details it={it} footer={set.footer} badges={set.badges === 'on'} />
          </div>
        </div>
      )
    case 'screensaver': {
      // A portrait screen shows the poster across it; a wide one the backdrop,
      // with the poster in the corner.
      const bg = portrait ? it.posterUrl || it.backdropUrl : it.backdropUrl || it.posterUrl
      const card = !video && bg !== it.posterUrl
      return (
        <div className="lk lk-saver">
          <div className="lk-s-bg">{!video && bg && <img className={kb !== undefined ? `kb kb-${kb % 4}` : undefined} style={{ '--kb-s': `${seconds + 4}s` } as CSSProperties} src={bg} alt="" />}</div>
          {video && <div className="lk-s-video">{video}</div>}
          <div className="lk-s-shade" />
          <div className="lk-s-top">
            {set.showIcon && <img className="mq-mark" src={markSrc()} alt="" />}
            {set.showClock && <span className="lk-clock">{time}</span>}
          </div>
          <div className="lk-s-card">
            {card && <Pic src={it.posterUrl} framing="fill" />}
            <div>
              {sign ? <span className="lk-label">{sign.join(' · ')}</span> : <span className="lk-label">{it.label}</span>}
              <Details it={it} footer={set.footer === 'none' ? 'title' : set.footer} badges={set.badges === 'on'} />
            </div>
          </div>
        </div>
      )
    }
    default:
      return (
        <div className={`lk lk-cinema${set.footer === 'none' && !it.playing ? ' no-foot' : ''}`}>
          {set.ambilight && it.posterUrl && <div className="lk-ambi" style={{ backgroundImage: `url(${it.posterUrl})` }} aria-hidden="true" />}
          <div className="lk-c-head">
            <Sign lines={lines} />
          </div>
          <div className="lk-c-poster lk-fit">{pic}</div>
          <div className="lk-c-foot">
            <Details it={it} footer={set.footer} badges={set.badges === 'on'} />
          </div>
        </div>
      )
  }
}

// Wall: a grid of posters filling the screen; every few seconds one tile turns
// to another film. The hero layout shows one film big above the grid.
export function Wall({ items, set, paused, bump, sign, time, onShow }: { items: Item[]; set: MarqueeSettings; paused: boolean; bump: number; sign: string[] | null; time: string; onShow: (it: Item | undefined) => void }) {
  const grid = useRef<HTMLDivElement>(null)
  const [size, setSize] = useState({ w: 0, h: 0 })
  useEffect(() => {
    const el = grid.current
    if (!el) return
    const ro = new ResizeObserver(() => setSize({ w: el.clientWidth, h: el.clientHeight }))
    ro.observe(el)
    return () => ro.disconnect()
  }, [])
  const cols = set.wallColumns
  const rows = size.w > 0 ? Math.max(1, Math.round(size.h / ((size.w / cols) * 1.5))) : 0
  const count = cols * rows
  const len = items.length
  const [tiles, setTiles] = useState<{ cur: number; old?: number; v: number }[]>([])
  const [hero, setHero] = useState(0)
  const next = useRef(0)
  const swaps = useRef(0)
  useEffect(() => {
    if (!len) return
    setTiles(Array.from({ length: count }, (_, i) => ({ cur: (i + 1) % len, v: 0 })))
    next.current = count + 1
  }, [count, len])

  const swap = useCallback(() => {
    if (!len) return
    swaps.current++
    if (set.wallLayout === 'hero' && swaps.current % 3 === 0) setHero((h) => (h + 1) % len)
    setTiles((list) => {
      if (!list.length) return list
      const showing = new Set(list.map((x) => x.cur))
      let pick = next.current % len
      for (let i = 0; i < len && showing.has(pick) && len > list.length; i++) pick = (pick + 1) % len
      next.current = pick + 1
      const j = Math.floor(Math.random() * list.length)
      return list.map((x, i) => (i === j ? { cur: pick, old: x.cur, v: x.v + 1 } : x))
    })
  }, [len, set.wallLayout])

  useEffect(() => {
    if (paused) return
    const id = window.setInterval(swap, set.wallSwap * 1000)
    return () => window.clearInterval(id)
  }, [swap, paused, set.wallSwap])
  useEffect(() => {
    if (bump) swap()
  }, [bump, swap])

  const heroItem = set.wallLayout === 'hero' && len ? items[hero % len] : undefined
  useEffect(() => onShow(heroItem), [heroItem, onShow])

  return (
    <div className={`lk lk-wall mqx-${set.transition === 'random' ? 'flip' : set.transition}`}>
      {sign && (
        <div className="lk-w-head">
          <Sign lines={sign} />
        </div>
      )}
      {heroItem && (
        <div className="lk-w-hero">
          {heroItem.backdropUrl ? <div className="lk-h-bg" style={{ backgroundImage: `url(${heroItem.backdropUrl})` }} /> : <div className="lk-ambi" style={{ backgroundImage: `url(${heroItem.posterUrl})` }} />}
          <Pic src={heroItem.posterUrl} framing="fill" />
          <div>
            <span className="lk-label">{heroItem.label}</span>
            <h2 className="lk-title">{heroItem.title}</h2>
            {heroItem.meta && <p className="lk-meta">{heroItem.meta}</p>}
            {heroItem.note && <p className="lk-meta lk-note">{heroItem.note}</p>}
          </div>
        </div>
      )}
      <div ref={grid} className="lk-w-grid" style={{ gridTemplateColumns: `repeat(${cols}, minmax(0, 1fr))`, gridTemplateRows: rows ? `repeat(${rows}, minmax(0, 1fr))` : undefined }}>
        {len === 0 && <p className="lk-b-empty">{t('Nothing to show yet.')}</p>}
        {len > 0 &&
          tiles.map((x, i) => (
            <div key={i} className="lk-tile">
              {x.old !== undefined && (
                <div key={`o${x.v}`} className="old">
                  <img src={items[x.old % len]?.posterUrl} alt="" />
                </div>
              )}
              <div key={`n${x.v}`} className={x.v ? 'new' : undefined}>
                <img src={items[x.cur % len]?.posterUrl} alt="" />
              </div>
            </div>
          ))}
      </div>
      {!sign && set.showClock && <span className="lk-w-clock lk-clock">{time}</span>}
    </div>
  )
}

const clockOf = (iso?: string) => (iso ? new Date(iso).toLocaleTimeString(locale(), { hour: 'numeric', minute: '2-digit' }) : '')

// Board: a cinema's showtimes board. What's on now, Live TV with what's next,
// what's in theaters, new arrivals and the week ahead, as rows. The list
// scrolls itself when it is longer than the screen (wrap).
export function Board({ data, kinds, sign, time, showClock, wrap }: { data: Data; kinds: SceneKind[]; sign: string[]; time: string; showClock: boolean; wrap: (list: ReactNode) => ReactNode }) {
  const on = (...k: SceneKind[]) => k.some((x) => kinds.includes(x))
  const today = new Date()
  const todayIso = `${today.getFullYear()}-${String(today.getMonth() + 1).padStart(2, '0')}-${String(today.getDate()).padStart(2, '0')}`
  const coming = data.coming.filter((e) => e.releaseDate >= todayIso).slice(0, 14)
  const sections: ReactNode[] = []

  if (on('playing') && data.nowPlaying.length) {
    sections.push(
      <section key="playing" className="lk-b-sec">
        <h2>{t('Now playing')}</h2>
        {data.nowPlaying.map((p, i) => {
          const ends = endsAt(p)
          return (
            <div key={i} className="lk-b-row">
              {p.posterUrl ? <img src={p.posterUrl} alt="" /> : <span className="lk-b-ph" />}
              <div className="lk-b-what">
                <strong>{(p.kind === 'episode' ? p.series : p.title) || p.title}</strong>
                <small>{[p.kind === 'episode' ? episodeLine(p) : p.year, p.device].filter(Boolean).join('  ·  ')}</small>
                <PlayBar p={p} />
                <div className="lk-b-times">
                  <span className="lk-chip now">{p.paused ? t('Paused') : t('Now')}</span>
                  {ends && <span className="lk-chip">{t('Ends {time}', { time: ends })}</span>}
                </div>
              </div>
            </div>
          )
        })}
      </section>,
    )
  }
  if ((on('tvnow') || on('tonight')) && (data.tonight?.length ?? 0) > 0) {
    const now = Date.now()
    sections.push(
      <section key="tv" className="lk-b-sec">
        <h2>{t('Tonight on Usherr TV')}</h2>
        {(data.tonight ?? []).slice(0, 12).map((c) => (
          <div key={c.number} className="lk-b-row tv">
            <span className="lk-b-ch">{c.logo ? <img src={c.logo} alt="" /> : c.number}</span>
            <div className="lk-b-what">
              <strong>{c.shows[0]?.title ?? c.name}</strong>
              <small>{c.name}</small>
              <div className="lk-b-times">
                {c.shows.slice(0, 5).map((sh, i) => {
                  const live = new Date(sh.start).getTime() <= now
                  return (
                    <span key={i} className={`lk-chip${live ? ' now' : ''}`} title={sh.title}>
                      {live ? t('Now') : clockOf(sh.start)}
                    </span>
                  )
                })}
              </div>
            </div>
          </div>
        ))}
      </section>,
    )
  } else if (on('tvnow') && (data.tvNow?.length ?? 0) > 0) {
    sections.push(
      <section key="tv" className="lk-b-sec">
        <h2>{t('On Live TV')}</h2>
        {(data.tvNow ?? []).slice(0, 12).map((c) => (
          <div key={c.number} className="lk-b-row tv">
            <span className="lk-b-ch">{c.logo ? <img src={c.logo} alt="" /> : c.number}</span>
            <div className="lk-b-what">
              <strong>{c.title || c.next}</strong>
              <small>{[c.name, c.subtitle].filter(Boolean).join('  ·  ')}</small>
              <div className="lk-b-times">
                {c.title && <span className="lk-chip now">{t('Now')}</span>}
                {c.next && c.nextAt && <span className="lk-chip">{`${clockOf(c.nextAt)}  ${c.next}`}</span>}
              </div>
            </div>
          </div>
        ))}
      </section>,
    )
  }
  if (on('theaters', 'theaters-film') && data.theaters.length) {
    sections.push(
      <section key="theaters" className="lk-b-sec">
        <h2>{t('In theaters now')}</h2>
        {data.theaters.slice(0, 10).map((f) => (
          <div key={f.tmdbId} className="lk-b-row">
            {f.posterUrl ? <img src={f.posterUrl} alt="" /> : <span className="lk-b-ph" />}
            <div className="lk-b-what">
              <strong>{f.title}</strong>
              <small>{[f.rated, hours(f.runtime), f.genres?.slice(0, 2).join(', ')].filter(Boolean).join('  ·  ')}</small>
              {data.theatre && (
                <div className="lk-b-times">
                  <span className="lk-chip">{data.theatre}</span>
                </div>
              )}
            </div>
          </div>
        ))}
      </section>,
    )
  }
  if (on('arrivals', 'new') && data.arrivals.length) {
    sections.push(
      <section key="new" className="lk-b-sec">
        <h2>{t('Just added')}</h2>
        {data.arrivals.slice(0, 8).map((a) => (
          <div key={a.kind + a.title} className="lk-b-row">
            {a.posterUrl ? <img src={a.posterUrl} alt="" /> : <span className="lk-b-ph" />}
            <div className="lk-b-what">
              <strong>{a.title}</strong>
              {a.year && <small>{a.year}</small>}
              <div className="lk-b-times">
                <span className="lk-chip">{new Date(a.at).toLocaleDateString(locale(), { weekday: 'short', month: 'short', day: 'numeric' })}</span>
              </div>
            </div>
          </div>
        ))}
      </section>,
    )
  }
  if (on('coming', 'coming-film') && coming.length) {
    sections.push(
      <section key="coming" className="lk-b-sec">
        <h2>{t('Coming soon')}</h2>
        {coming.map((e) => (
          <div key={`${e.kind}-${e.id}`} className="lk-b-row">
            {e.posterUrl ? <img src={e.posterUrl} alt="" loading="lazy" /> : <span className="lk-b-ph" />}
            <div className="lk-b-what">
              <strong>{e.title}</strong>
              <small>{typeLabel(e)}</small>
              <div className="lk-b-times">
                <span className="lk-chip">{dayLabel(e.releaseDate)}</span>
              </div>
            </div>
          </div>
        ))}
      </section>,
    )
  }

  return (
    <div className="lk lk-board">
      <header className="lk-b-head">
        <Sign lines={sign} />
        {showClock && <span className="lk-clock">{time}</span>}
      </header>
      <div className="lk-b-body">{wrap(sections.length ? <div className="lk-b-list">{sections}</div> : <p className="lk-b-empty">{t('Nothing to show yet.')}</p>)}</div>
    </div>
  )
}
