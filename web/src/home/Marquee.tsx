// Usherr: the Marquee, a lobby display for a tablet standing next to the TV
// (portrait, read from across the room). It rotates through scenes in the
// order set on the Marquee's page: what's playing now, single films
// (new, in theaters, coming soon) with trailers, what arrived lately, what's
// in cinemas and the week ahead. Open /marquee full-screen. ?sound=1 plays
// trailers with sound, ?only=new,trailer,... shows only those scenes, and
// ?screen=NAME gives the screen a fixed id. The Marquee remote steers it.
import { useCallback, useEffect, useMemo, useRef, useState, type CSSProperties, type ReactNode } from 'react'
import { useSearchParams } from 'react-router-dom'
import { locale, t } from '../i18n'
import type { CalendarEntry } from '../api'
import { dayLabel, hours, inQuiet, typeLabel, type Data, type Feature, type MediaItem, type Playing, type SceneKind, type Spotlight } from './marqueeData'
import { applyPalette, markSrc } from '../palette'
import { Board, ItemLook, itemsFor, Wall, wallItems, type Item } from './MarqueeLooks'
import { FitLine, SceneFrame, Stage } from './marqueeParts'
import { Badges, FormatBadges, InfoPanel, MediaSlide, PreShow, PreshowFollow, preShowHasSomething, QR, qrFor, SpotlightView, ThemeMusic, TonightView, trivia, TriviaView, type FollowStep } from './MarqueeExtras'
import { GameNow, hasScores, hasTonight, ScoresView, SportsCountdown, SportsTicker, SportsTonightView } from './MarqueeSports' // Usherr: sports
import { clockLabel, inLabel, type Showtime } from './showtimes'
import './showtime.css'
import './marqueeFonts'
import './marquee.css'
import './marqueeLook.css'
import './marqueeExtras.css'

// A scene of the Usherr look, one item of another single-poster look, or the
// whole of the wall or the board.
type Scene =
  | { kind: 'playing'; p: Playing }
  | { kind: 'feature'; f: Feature }
  | { kind: 'trailer'; f: Feature }
  | { kind: 'arrivals' }
  | { kind: 'theaters' }
  | { kind: 'coming' }
  | { kind: 'tvnow' }
  | { kind: 'item'; it: Item }
  | { kind: 'wall' }
  | { kind: 'board' }
  | { kind: 'tonight' }
  | { kind: 'spotlight'; sp: Spotlight }
  | { kind: 'trivia'; f: Feature; fact: string }
  | { kind: 'slide'; m: MediaItem }
  | { kind: 'preshow' }
  | { kind: 'scores' } // Usherr: sports
  | { kind: 'sportsTonight' }

// The scenes every look shows the same way (with its own colours and fonts).
const EXTRA = new Set(['tonight', 'spotlight', 'trivia', 'slide', 'preshow', 'scores', 'sportsTonight'])

const SINGLE = new Set(['cinema', 'screensaver', 'posterr', 'onesheet'])
const NUDGE_MS = 3 * 60 * 1000 // burn-in protection: a few pixels every three minutes
const BOARD_MS = 40_000 // the board, read through before it starts again

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
    case 'tvnow':
      return t('On Live TV')
    case 'item':
      return s.it.label
    case 'wall':
    case 'board':
      return t('Now showing')
    case 'tonight':
      return t('Tonight on Usherr TV')
    case 'spotlight':
      return t('In the spotlight')
    case 'trivia':
      return t('Did you know?')
    case 'slide':
      return s.m.title || t('Now showing')
    case 'preshow':
      return t('Now showing')
    case 'scores':
      return t('Scoreboard')
    case 'sportsTonight':
      return t('Tonight’s games')
  }
}

// Each browser showing the Marquee is a screen with an id it keeps (or the
// one in ?screen=), so the remote can name it, give it its own settings and
// steer it.
const SCREEN_KEY = 'usherr-marquee-screen'
function screenId(fromUrl: string | null): string {
  const clean = (v: string | null | undefined) => {
    const id = (v ?? '').trim().toLowerCase()
    return /^[a-z0-9-]{6,40}$/.test(id) ? id : ''
  }
  if (clean(fromUrl)) return clean(fromUrl)
  try {
    const kept = clean(localStorage.getItem(SCREEN_KEY))
    if (kept) return kept
    const made = `mq-${Math.random().toString(36).slice(2, 10)}${Date.now().toString(36)}`
    localStorage.setItem(SCREEN_KEY, made)
    return made
  } catch {
    return ''
  }
}

// What a scene shows, in a line and a picture, for the remote's preview.
function summary(s: Scene, d: Data): { title: string; posterUrl?: string } {
  switch (s.kind) {
    case 'playing':
      return { title: s.p.game?.title ?? s.p.series ?? s.p.title, posterUrl: s.p.posterUrl }
    case 'feature':
    case 'trailer':
      return { title: s.f.title, posterUrl: s.f.posterUrl }
    case 'arrivals':
      return { title: heading(s), posterUrl: d.arrivals[0]?.posterUrl }
    case 'theaters':
      return { title: heading(s), posterUrl: d.theaters[0]?.posterUrl }
    case 'coming':
      return { title: heading(s), posterUrl: d.coming[0]?.posterUrl }
    case 'tvnow':
    case 'wall':
    case 'board':
      return { title: heading(s) }
    case 'item':
      return { title: s.it.title, posterUrl: s.it.posterUrl }
    case 'tonight':
    case 'preshow':
    case 'scores':
    case 'sportsTonight':
      return { title: s.kind === 'preshow' ? t('The pre-show') : heading(s) }
    case 'spotlight':
      return { title: s.sp.name, posterUrl: s.sp.photo }
    case 'trivia':
      return { title: s.f.title, posterUrl: s.f.posterUrl }
    case 'slide':
      return { title: s.m.title || t('Your slide'), posterUrl: s.m.kind === 'image' ? s.m.fileUrl : undefined }
  }
}

// The film behind a scene, for the info panel, the QR code and theme music.
function filmOf(s: Scene): Feature | undefined {
  if (s.kind === 'feature' || s.kind === 'trailer') return s.f
  if (s.kind === 'item') return s.it.feature
  return undefined
}

function playingOf(s: Scene): Playing | undefined {
  if (s.kind === 'playing') return s.p
  if (s.kind === 'item') return s.it.playing
  return undefined
}

type Shown = { s: Scene; seconds: number; n: number; kind: SceneKind }
interface Remote {
  paused: boolean // stays on this scene
  pinned: boolean // plays this scene again and again (a scroll, a trailer)
  dim: boolean
  blank: boolean
  info: boolean // the info panel, from the remote
}

export default function Marquee() {
  const [params] = useSearchParams()
  const only = params.get('only')
  const [screen] = useState(() => screenId(params.get('screen')))
  const [data, setData] = useState<Data | null>(null)
  const [scene, setScene] = useState<Shown | null>(null)
  const [clock, setClock] = useState(() => new Date())
  const [remote, setRemote] = useState<Remote>({ paused: false, pinned: false, dim: false, blank: false, info: false })
  // Usherr: the pre-show playing on the TV, which this screen follows.
  const [follow, setFollow] = useState<FollowStep | null>(null)
  useEffect(() => {
    if (!follow) return
    const id = window.setTimeout(() => setFollow(null), 6 * 60_000) // the TV went quiet
    return () => window.clearTimeout(id)
  }, [follow])
  const remoteRef = useRef(remote)
  const waiting = useRef(false) // a paused scene's time is up
  const shown = useRef<Shown[]>([]) // the scenes shown, for "previous"
  const failures = useRef(0)
  const dataRef = useRef<Data | null>(null)
  const cursor = useRef({ step: -1, counts: {} as Record<string, number>, lastFeature: null as Feature | null, n: 0 })
  const current = useRef(0) // the scene on screen (its n)
  const [bump, setBump] = useState(0) // the wall's "next"
  const [wallHero, setWallHero] = useState<Item | undefined>(undefined)
  const [nudge, setNudge] = useState({ x: 0, y: 0 })
  const startPreShow = useRef<(thenPlaying: boolean) => void>(() => undefined)
  const afterPreShow = useRef(false) // show what's playing once the pre-show ends

  useEffect(() => {
    remoteRef.current = remote
  }, [remote])

  const load = useCallback(() => {
    const who = screen ? `?screen=${screen}&w=${Math.round(window.innerWidth * devicePixelRatio)}&h=${Math.round(window.innerHeight * devicePixelRatio)}` : ''
    fetch(`/api/home/marquee${who}`, { credentials: 'include' })
      .then((r) => (r.ok ? (r.json() as Promise<Data>) : Promise.reject(new Error(String(r.status)))))
      .then((d) => {
        failures.current = 0
        // Usherr: Usherr was updated (or restarted): the screen takes the new code.
        if (d.build && dataRef.current?.build && d.build !== dataRef.current.build) {
          window.location.reload()
          return
        }
        dataRef.current = d
        setData(d)
      })
      .catch(() => {
        failures.current++
        // Signed out (or the server restarted with a new session): back to sign-in.
        if (failures.current >= 3) window.location.reload()
      })
  }, [screen])

  useEffect(() => {
    load()
    const id = window.setInterval(load, REFRESH_MS)
    return () => window.clearInterval(id)
  }, [load])

  // Usherr: the scores change every minute, while the screen shows sports.
  useEffect(() => {
    const id = window.setInterval(() => {
      if (!dataRef.current?.sports) return
      fetch(`/api/home/marquee?only=sports${screen ? `&screen=${screen}` : ''}`, { credentials: 'include' })
        .then((r) => (r.ok ? (r.json() as Promise<Data>) : null))
        .then((d) => {
          if (!d) return
          setData((old) => {
            const next = old ? { ...old, sports: d.sports } : old
            dataRef.current = next
            return next
          })
        })
        .catch(() => undefined)
    }, 60_000)
    return () => window.clearInterval(id)
  }, [screen])

  // What's playing changes faster than the rest: asked every 30 seconds.
  useEffect(() => {
    const id = window.setInterval(() => {
      fetch(`/api/home/marquee?only=playing${screen ? `&screen=${screen}` : ''}`, { credentials: 'include' })
        .then((r) => (r.ok ? (r.json() as Promise<Data>) : null))
        .then((d) => {
          if (!d) return
          const before = new Set((dataRef.current?.nowPlaying ?? []).map((p) => `${p.device}|${p.title}`))
          const started = d.nowPlaying.some((p) => !before.has(`${p.device}|${p.title}`))
          setData((old) => {
            const next = old ? { ...old, nowPlaying: d.nowPlaying } : old
            dataRef.current = next
            return next
          })
          if (started && dataRef.current?.settings.preShowOnStart) startPreShow.current(true)
        })
        .catch(() => undefined)
    }, 30_000)
    return () => window.clearInterval(id)
  }, [screen])

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

  const show = useCallback((next: Shown) => {
    shown.current = [...shown.current.slice(-29), next]
    current.current = next.n
    setScene(next)
  }, [])

  // pick finds the next thing for a scene to show, or nothing.
  const pick = useCallback((kind: SceneKind): Scene | undefined => {
    const d = dataRef.current
    if (!d) return undefined
    const c = cursor.current
    const take = <T,>(key: string, list: T[]): T | undefined => {
      if (!list.length) return undefined
      const i = c.counts[key] ?? 0
      c.counts[key] = i + 1
      return list[i % list.length]
    }
    // The scenes every look shows alike.
    switch (kind) {
      case 'tonight':
        return (d.tonight ?? []).some((c) => c.shows.length) ? { kind: 'tonight' } : undefined
      case 'spotlight': {
        const sp = take('spotlight', d.spotlights ?? [])
        return sp && { kind: 'spotlight', sp }
      }
      case 'trivia': {
        const x = take('trivia', trivia(d.features))
        return x && { kind: 'trivia', f: x.f, fact: x.fact }
      }
      case 'slides': {
        const m = take(
          'slides',
          (d.media ?? []).filter((x) => x.place === 'slide'),
        )
        return m && { kind: 'slide', m }
      }
      case 'preshow':
        return preShowHasSomething(d, d.settings) ? { kind: 'preshow' } : undefined
      case 'scores': // Usherr: sports
        return hasScores(d) ? { kind: 'scores' } : undefined
      case 'sports-tonight':
        return hasTonight(d) ? { kind: 'sportsTonight' } : undefined
    }
    // The other single-poster looks show one film, show or channel at a time.
    if (SINGLE.has(d.settings.style)) {
      const it = take(kind, itemsFor(kind, d))
      return it && { kind: 'item', it }
    }
    const reason = REASON[kind]
    if (kind === 'playing') {
      const p = take('playing', d.nowPlaying)
      return p && { kind: 'playing', p }
    }
    if (reason) {
      const f = take(
        kind,
        d.features.filter((x) => x.reason === reason),
      )
      if (f) c.lastFeature = f
      return f && { kind: 'feature', f }
    }
    if (kind === 'trailer') {
      const f = c.lastFeature?.trailer
        ? c.lastFeature
        : take(
            'trailer',
            d.features.filter((x) => x.trailer),
          )
      if (!f?.trailer) return undefined
      c.lastFeature = null
      return { kind: 'trailer', f }
    }
    if (kind === 'arrivals' && d.arrivals.length >= 2) return { kind: 'arrivals' }
    if (kind === 'theaters' && d.theaters.length >= 2) return { kind: 'theaters' }
    if (kind === 'coming' && d.coming.length) return { kind: 'coming' }
    if (kind === 'tvnow' && (d.tvNow?.length ?? 0) > 0) return { kind: 'tvnow' }
    return undefined
  }, [])

  // The next scene: the next one switched on, in the set order, that has
  // something to show; each takes the next film of its kind in turn.
  const advance = useCallback(() => {
    const d = dataRef.current
    if (!d) return
    const c = cursor.current
    // The wall and the board are one scene each.
    if (d.settings.style === 'wall' || d.settings.style === 'board') {
      c.n++
      show({ s: { kind: d.settings.style }, seconds: BOARD_MS / 1000, n: c.n, kind: 'playing' })
      return
    }
    let order = d.settings.scenes.filter((x) => x.on)
    if (only) {
      const want = new Set(only.split(',').flatMap((w) => OLD_NAMES[w.trim()] ?? [w.trim()]))
      order = d.settings.scenes.filter((x) => want.has(x.kind))
    }
    // While something plays, only that, when asked.
    if (d.settings.playingFirst && d.nowPlaying.length) {
      const playing = d.settings.scenes.find((x) => x.kind === 'playing')
      order = [{ kind: 'playing', on: true, seconds: playing?.seconds ?? 20 }]
    }
    for (let tries = 0; tries < order.length; tries++) {
      c.step = (c.step + 1) % order.length
      const slot = order[c.step]
      const s = pick(slot.kind)
      if (s) {
        c.n++
        show({ s, seconds: slot.seconds, n: c.n, kind: slot.kind })
        return
      }
    }
    // Nothing to show right now: look again shortly.
    setScene(null)
    window.setTimeout(() => advance(), 15_000)
  }, [only, pick, show])

  // A scene's time is up: the next one, unless the remote holds this one.
  const done = useCallback((n: number) => {
    if (n !== current.current) return // a scene leaving with a transition
    const r = remoteRef.current
    if (r.paused) {
      waiting.current = true
      return
    }
    if (r.pinned) {
      const again = ++cursor.current.n
      current.current = again
      setScene((sc) => sc && { ...sc, n: again })
      return
    }
    // After a pre-show that something starting began: what's playing.
    if (afterPreShow.current) {
      afterPreShow.current = false
      const s = pick('playing')
      if (s) {
        const seconds = dataRef.current?.settings.scenes.find((x) => x.kind === 'playing')?.seconds ?? 20
        show({ s, seconds, n: ++cursor.current.n, kind: 'playing' })
        return
      }
    }
    advance()
  }, [advance, pick, show])

  useEffect(() => {
    startPreShow.current = (thenPlaying: boolean) => {
      const d = dataRef.current
      if (!d || d.settings.style === 'wall' || d.settings.style === 'board' || !preShowHasSomething(d, d.settings)) return
      afterPreShow.current = thenPlaying
      waiting.current = false
      show({ s: { kind: 'preshow' }, seconds: 0, n: ++cursor.current.n, kind: 'preshow' })
    }
  }, [show])

  // The remote's buttons.
  const command = useCallback(
    (cmd: string, arg?: string) => {
      const on = arg === 'on'
      const c = cursor.current
      switch (cmd) {
        case 'next':
          waiting.current = false
          if (dataRef.current?.settings.style === 'wall') setBump((b) => b + 1)
          else advance()
          break
        case 'prev': {
          if (dataRef.current?.settings.style === 'wall') {
            setBump((b) => b + 1)
            break
          }
          const before = shown.current.slice(0, -1)
          const back = before[before.length - 1]
          if (!back) break
          waiting.current = false
          shown.current = before.slice(0, -1)
          c.n++
          show({ ...back, n: c.n })
          break
        }
        case 'scene': {
          const kind = arg as SceneKind
          const s = pick(kind)
          if (!s) break
          const seconds = dataRef.current?.settings.scenes.find((x) => x.kind === kind)?.seconds ?? 20
          waiting.current = false
          c.n++
          show({ s, seconds, n: c.n, kind })
          break
        }
        case 'pause':
          setRemote((r) => ({ ...r, paused: on }))
          if (!on && waiting.current) {
            waiting.current = false
            advance()
          }
          break
        case 'pin':
          setRemote((r) => ({ ...r, pinned: on }))
          break
        case 'dim':
          setRemote((r) => ({ ...r, dim: on }))
          break
        case 'blank':
          setRemote((r) => ({ ...r, blank: on }))
          break
        case 'reload':
          window.location.reload()
          break
        case 'info':
          setRemote((r) => ({ ...r, info: on }))
          break
        case 'preshow':
          startPreShow.current(false)
          break
        case 'follow': {
          let f: Omit<FollowStep, 'at'> & { end?: boolean }
          try {
            f = JSON.parse(arg ?? '{}') as typeof f
          } catch {
            break
          }
          setFollow(f.end ? null : { ...f, at: Date.now() })
          break
        }
      }
    },
    [advance, pick, show],
  )

  // The live stream: new settings and the remote's buttons arrive at once.
  useEffect(() => {
    if (!screen || typeof EventSource === 'undefined') return
    const es = new EventSource(`/api/home/marquee/events?screen=${screen}`)
    es.onmessage = (m) => {
      let ev: { type?: string; cmd?: string; arg?: string }
      try {
        ev = JSON.parse(m.data as string) as typeof ev
      } catch {
        return
      }
      if (ev.type === 'settings') load()
      else if (ev.type === 'command' && ev.cmd) command(ev.cmd, ev.arg)
    }
    return () => es.close()
  }, [screen, load, command])

  const started = useRef(false)
  useEffect(() => {
    if (data && !started.current) {
      started.current = true
      advance()
    }
  }, [data, advance])

  // A new look (from the settings or the remote) starts from its first scene.
  const style = data?.settings.style
  const lastStyle = useRef(style)
  useEffect(() => {
    if (!started.current || style === lastStyle.current) return
    lastStyle.current = style
    shown.current = []
    advance()
  }, [style, advance])

  // Burn-in protection: everything moves a few pixels now and then.
  const nudgeOn = !!data?.settings.nudge
  useEffect(() => {
    if (!nudgeOn) return
    const id = window.setInterval(() => setNudge({ x: Math.round(Math.random() * 12 - 6), y: Math.round(Math.random() * 12 - 6) }), NUDGE_MS)
    return () => {
      window.clearInterval(id)
      setNudge({ x: 0, y: 0 })
    }
  }, [nudgeOn])

  const kinds = useMemo(() => (data ? data.settings.scenes.filter((x) => x.on).map((x) => x.kind) : []), [data])
  const pool = useMemo(() => (data && data.settings.style === 'wall' ? wallItems(kinds, data) : []), [data, kinds])

  // Tell the remote what's on, for its preview.
  const quiet = !!data && data.settings.quietOn && inQuiet(clock, data.settings.quietFrom, data.settings.quietTo)
  useEffect(() => {
    const d = dataRef.current
    if (!screen || !d) return
    const what = scene?.s.kind === 'wall' && wallHero ? { title: wallHero.title, posterUrl: wallHero.posterUrl } : scene ? summary(scene.s, d) : { title: '' }
    fetch(`/api/home/marquee/showing?screen=${screen}`, {
      method: 'POST',
      credentials: 'include',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ scene: scene?.s.kind === 'wall' || scene?.s.kind === 'board' ? '' : (scene?.kind ?? ''), ...what, ...remote, quiet }),
    }).catch(() => undefined)
  }, [screen, scene, remote, quiet, wallHero])

  if (!data) return <div className="mq" />
  const set = data.settings
  const sound = set.trailerSound || params.get('sound') === '1'
  const time = clock.toLocaleTimeString(locale(), { hour: 'numeric', minute: '2-digit' })
  const vars = {
    '--mq-t': set.textScale / 100,
    '--mq-p': set.posterScale / 100,
    '--mq-bulb': set.bulbSize / 100,
    ...(set.accent ? { '--mq-accent': set.accent } : {}),
    ...(set.background ? { '--mq-bg': set.background } : {}),
  } as CSSProperties
  const look = `th-${set.theme} f-${set.font} c-${set.corners} st-${set.style}`

  if (remote.blank) return <div className={`mq ${look} mq-quiet black`} />
  if (quiet) {
    return (
      <div className={`mq ${look} mq-quiet${set.quietMode === 'black' ? ' black' : ''}`} style={vars}>
        {set.quietMode === 'clock' && <span className="mq-quiet-clock">{time}</span>}
      </div>
    )
  }
  const custom = set.signMode === 'custom' ? [set.signLine1, set.signLine2].filter(Boolean) : null
  const speed = SPEED[set.scrollSpeed] ?? SPEED.normal
  // Dimmed by the remote, or at night.
  const night = set.nightDimOn && inQuiet(clock, set.nightFrom, set.nightTo)
  const shade = Math.max(remote.dim ? 0.65 : 0, night ? 1 - set.nightLevel / 100 : 0)

  // Scenes every look shows alike, with its colours and fonts.
  const extra = (sc: Shown, header: boolean): ReactNode => {
    const s = sc.s
    const end = () => done(sc.n)
    switch (s.kind) {
      case 'tonight':
        return (
          <SceneFrame n={sc.n} minMs={sc.seconds * 1000} speed={speed} onDone={done}>
            <TonightView data={data} header={header} />
          </SceneFrame>
        )
      case 'spotlight':
        return (
          <SceneFrame n={sc.n} minMs={sc.seconds * 1000} still speed={speed} onDone={done}>
            <SpotlightView sp={s.sp} header={header} />
          </SceneFrame>
        )
      case 'trivia':
        return (
          <SceneFrame n={sc.n} minMs={sc.seconds * 1000} still speed={speed} onDone={done}>
            <TriviaView f={s.f} fact={s.fact} header={header} />
          </SceneFrame>
        )
      case 'slide':
        return <MediaSlide m={s.m} sound={sound} onEnd={end} header={header} />
      case 'preshow':
        return <PreShow data={data} set={set} sound={sound} trailerSeconds={set.scenes.find((x) => x.kind === 'trailer')?.seconds ?? 75} onDone={end} />
      case 'scores':
        return (
          <SceneFrame n={sc.n} minMs={sc.seconds * 1000} speed={speed} onDone={done}>
            <ScoresView data={data} header={header} />
          </SceneFrame>
        )
      case 'sportsTonight':
        return (
          <SceneFrame n={sc.n} minMs={sc.seconds * 1000} speed={speed} onDone={done}>
            <SportsTonightView data={data} header={header} />
          </SceneFrame>
        )
      default:
        return null
    }
  }
  const isExtra = !!scene && EXTRA.has(scene.s.kind)
  const preshow = scene?.s.kind === 'preshow'
  const ticker = !preshow && !!set.sportsTicker && (data.sports?.games.length ?? 0) > 0 // Usherr: sports

  // Over the scene: the info panel and a QR code, for a film or what's playing.
  const film = scene ? filmOf(scene.s) : undefined
  const playing = scene ? playingOf(scene.s) : undefined
  const infoOn = !preshow && (remote.info || set.infoPanel === 'always' || (set.infoPanel === 'held' && (remote.paused || remote.pinned || !!playing?.paused)))
  const qr = !preshow ? qrFor(film, set.qr) : null
  const info =
    infoOn && (film || playing) ? (
      <InfoPanel
        overview={film?.overview ?? playing?.overview}
        cast={film?.cast ?? playing?.cast}
        director={film?.director ?? playing?.director}
        meta={[film?.genres?.slice(0, 3).join(', ') ?? playing?.genres?.slice(0, 3).join(', '), film?.rated ?? playing?.rated].filter(Boolean).join('  ·  ')}
        side={qr ? <QR text={qr.text} label={qr.label} /> : undefined}
      />
    ) : null
  const overlays = info ?? (qr ? <QR text={qr.text} label={qr.label} /> : null)

  // Theme music for a film or a show of the library, when it has some.
  let theme = ''
  if (set.themeMusic && scene && !preshow && !sound) {
    if (film?.themeUrl) theme = film.themeUrl
    else if (scene.s.kind === 'item' && scene.s.it.themeKey) theme = data.themes?.[scene.s.it.themeKey] ?? ''
  }

  let body: ReactNode
  if (set.style === 'wall') {
    body = <Wall items={pool} set={set} paused={remote.paused} bump={bump} sign={custom && custom.length ? custom : null} time={time} onShow={setWallHero} />
  } else if (set.style === 'board') {
    body = (
      <Board
        data={data}
        kinds={kinds}
        sign={custom ?? [t('Now showing')]}
        time={time}
        showClock={set.showClock}
        wrap={(list) =>
          scene ? (
            <SceneFrame key={scene.n} n={scene.n} minMs={BOARD_MS} speed={speed} onDone={done}>
              {list}
            </SceneFrame>
          ) : (
            list
          )
        }
      />
    )
  } else if (set.style !== 'usherr') {
    body = (
      <main className="mq-stage">
        {scene && isExtra ? (
          <Stage k={scene.n} type={set.transition} ms={set.transitionMs}>
            {extra(scene, true)}
          </Stage>
        ) : scene?.s.kind === 'item' ? (
          <Stage k={scene.n} type={set.transition} ms={set.transitionMs}>
            <SceneFrame n={scene.n} minMs={scene.seconds * 1000} still speed={speed} onDone={done}>
              <ItemLook style={set.style} it={scene.s.it} set={set} n={scene.n} seconds={scene.seconds} sign={custom} time={time} portrait={window.innerHeight >= window.innerWidth} sound={sound} />
            </SceneFrame>
          </Stage>
        ) : (
          <p className="mq-empty">{t('Nothing to show yet.')}</p>
        )}
        {overlays}
      </main>
    )
  } else if (preshow && scene) {
    body = (
      <main className="mq-stage">
        <Stage k={scene.n} type={set.transition} ms={set.transitionMs}>
          {extra(scene, false)}
        </Stage>
      </main>
    )
  } else {
    const sign = custom ?? [scene ? heading(scene.s) : t('Now showing')]
    const top = set.showIcon || set.showName || set.showClock
    body = (
      <>
        {top && (
          <header className="mq-top">
            {set.showIcon && <img className="mq-mark" src={markSrc()} alt="" />}
            {set.showName && <span className="mq-name">Usherr</span>}
            {set.showClock && <span className="mq-clock">{time}</span>}
          </header>
        )}
        {sign.length > 0 && (
          <div className={`mq-marquee fr-${set.frame}${top ? '' : ' alone'}`} aria-live="polite">
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
          {scene && isExtra ? (
            <Stage k={scene.n} type={set.transition} ms={set.transitionMs}>
              {extra(scene, false)}
            </Stage>
          ) : scene ? (
            <Stage k={scene.n} type={set.transition} ms={set.transitionMs}>
              <SceneFrame n={scene.n} minMs={scene.seconds * 1000} still={scene.s.kind === 'trailer'} speed={speed} onDone={done}>
                <SceneView scene={scene.s} data={data} sound={sound} badges={set.badges === 'on'} />
              </SceneFrame>
            </Stage>
          ) : (
            <p className="mq-empty">{t('Nothing to show yet.')}</p>
          )}
          {overlays}
        </main>
      </>
    )
  }

  return (
    <div className={`mq ${look}${ticker ? ' has-ticker' : ''}`} style={vars} onDoubleClick={() => void document.documentElement.requestFullscreen?.().catch(() => undefined)}>
      <div className="mq-nudger" style={nudge.x || nudge.y ? { transform: `translate(${nudge.x}px, ${nudge.y}px)` } : undefined}>
        {follow ? <PreshowFollow f={follow} /> : body}
      </div>
      {!preshow && !follow && <ShowtimeBand list={data.showtimes} now={clock.getTime()} />}
      {!preshow && set.sportsCountdown && <SportsCountdown data={data} now={clock.getTime()} />}
      {ticker && <SportsTicker data={data} />}
      {set.vintage && <div className="mq-fx mq-vintage" aria-hidden="true" />}
      {set.grain && <div className="mq-fx mq-grain" aria-hidden="true" />}
      <div className="mq-fx mq-shade" style={{ opacity: shade }} aria-hidden="true" />
      {theme && !follow && <ThemeMusic url={theme} volume={set.themeVolume} />}
    </div>
  )
}

// ShowtimeBand counts down to the next film booked for tonight (Showtimes at
// home), over whatever the screen shows: "Showtime 8:00 PM · Dune · in 12 min".
function ShowtimeBand({ list, now }: { list?: Showtime[]; now: number }) {
  const next = (list ?? []).find((s) => s.state === 'booked' && s.atMs > now - 60_000)
  if (!next) return null
  const soon = next.atMs - now <= 10 * 60_000
  return (
    <div className={`mq-showtime-band${soon ? ' soon' : ''}`} role="status">
      <b>{t('Showtime {time}', { time: clockLabel(next.atMs) })}</b>
      <span>{next.title}</span>
      <span>{inLabel(next.atMs, now)}</span>
    </div>
  )
}

function SceneView({ scene, data, sound, badges }: { scene: Scene; data: Data; sound: boolean; badges: boolean }) {
  switch (scene.kind) {
    case 'playing': {
      const p = scene.p
      if (p.game) return <GameNow p={p} g={p.game} /> // Usherr: a game, as a matchup card
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
          {badges && <Badges p={p} />}
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
          {badges && <FormatBadges formats={f.formats} />}
          {f.reason === 'coming' && f.releaseDate && <p className="mq-meta mq-gold">{t('Opens {date}', { date: dayLabel(f.releaseDate) })}</p>}
          {f.reason === 'theaters' && data.theatre && <p className="mq-meta mq-gold">{data.theatre}</p>}
        </div>
      )
    }
    case 'trailer': {
      const f = scene.f
      const q = new URLSearchParams({ autoplay: '1', mute: sound ? '0' : '1', controls: '0', rel: '0', modestbranding: '1', playsinline: '1', iv_load_policy: '3', cc_load_policy: '0', origin: window.location.origin })
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
    case 'tvnow':
      // Usherr: what the Live TV channels have on.
      return (
        <div className="mq-scene mq-tvnow">
          <ul>
            {(data.tvNow ?? []).slice(0, 8).map((c) => (
              <li key={c.number}>
                <span className="mq-tv-ch">{c.logo ? <img src={c.logo} alt="" /> : <b>{c.number}</b>}</span>
                <span className="mq-tv-what">
                  <strong>{c.title || c.next}</strong>
                  {c.subtitle && <small>{c.subtitle}</small>}
                  {c.title && (
                    <span className="mq-tv-bar">
                      <span style={{ width: `${Math.min(100, c.progress * 100)}%` }} />
                    </span>
                  )}
                </span>
                <span className="mq-type">{c.name}</span>
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
    default:
      return null
  }
}
