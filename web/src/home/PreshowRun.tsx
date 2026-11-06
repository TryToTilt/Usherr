// Usherr: the pre-show, full screen on this device, before the film: the
// cards Usherr draws (the Marquee's own, sized for the TV), clips from the
// pre-show folder and coming-soon trailers, one after another. Each step is
// told to the Marquee screens, which follow along. Skip goes to the next
// piece; Skip the pre-show (or Escape) goes straight to the film.
import { useCallback, useEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import Icon from '../components/Icon'
import { locale, t } from '../i18n'
import { Piece, QR } from './MarqueeExtras'
import { preshowApi, soonLine, type PreshowPlan, type PreshowStep } from './preshowApi'
import { browserCaps } from './browserCaps' // Usherr: what this browser plays
import './marquee.css'
import './marqueeLook.css'
import './marqueeExtras.css'
import './preshow.css'

function Timed({ seconds, onEnd, children }: { seconds: number; onEnd: () => void; children: React.ReactNode }) {
  const done = useRef(onEnd)
  useEffect(() => {
    done.current = onEnd
  })
  useEffect(() => {
    const id = window.setTimeout(() => done.current(), seconds * 1000)
    return () => window.clearTimeout(id)
  }, [seconds])
  return <>{children}</>
}

function Welcome({ plan }: { plan: PreshowPlan }) {
  const film = plan.film
  const at = film.atMs ? new Date(film.atMs).toLocaleTimeString(locale(), { hour: 'numeric', minute: '2-digit' }) : ''
  return (
    <div className="lk lk-x ps psr-welcome">
      <span className="ps-rays" aria-hidden="true" />
      {film.posterUrl && <img className="psr-welcome-poster" src={film.posterUrl} alt="" />}
      <div className="psr-welcome-text">
        <span className="ps-small">{t('Welcome to')}</span>
        <h1 className="ps-big">{plan.theater || t('the theater')}</h1>
        {film.title && <p className="ps-small psr-tonight">{at ? t('Tonight at {time}: {title}', { time: at, title: film.title }) : t('Tonight: {title}', { title: film.title })}</p>}
      </div>
    </div>
  )
}

function Rating({ plan }: { plan: PreshowPlan }) {
  return (
    <div className="lk lk-x ps psr-rating">
      <span className="ps-small">{t('The following feature is rated')}</span>
      <b className="psr-rated">{plan.film.rated}</b>
      {plan.film.title && <h1 className="ps-big psr-rating-title">{plan.film.title}</h1>}
    </div>
  )
}

// Before each coming-soon trailer: its poster, its name and when it comes.
function Soon({ step }: { step: PreshowStep }) {
  return (
    <div className="lk lk-x ps psr-welcome">
      <span className="ps-rays" aria-hidden="true" />
      {step.posterUrl && <img className="psr-welcome-poster" src={step.posterUrl} alt="" />}
      <div className="psr-welcome-text">
        <span className="ps-small">{t('Coming soon')}</span>
        <h1 className="ps-big">{step.title}</h1>
        <p className="ps-small psr-tonight">{soonLine(step.releaseDate, step.why, step.season)}</p>
      </div>
    </div>
  )
}

function Announce({ step }: { step: PreshowStep }) {
  return (
    <div className="lk lk-x ps psr-announce">
      <h1 className="ps-big">{step.title}</h1>
    </div>
  )
}

function QRCard({ step }: { step: PreshowStep }) {
  return (
    <div className="lk lk-x ps psr-welcome psr-qr">
      <QR text={step.title ?? ''} label="" />
      <div className="psr-welcome-text">
        <span className="ps-small">{t('On your phone')}</span>
        <h1 className="ps-big">{t('Scan for Usherr')}</h1>
        <p className="ps-small psr-tonight">{(step.title ?? '').replace(/^https?:\/\//, '')}</p>
      </div>
    </div>
  )
}

function Reel({ step }: { step: PreshowStep }) {
  return (
    <div className="lk lk-x ps psr-reel">
      <h1 className="ps-big">{t('Coming soon')}</h1>
      <ul>
        {(step.reel ?? []).map((it, i) => (
          <li key={i} style={{ animationDelay: `${0.3 + i * 0.35}s` }}>
            <img src={it.posterUrl} alt="" />
            <strong>{it.title}</strong>
            <span>{soonLine(it.releaseDate, it.why, it.season)}</span>
          </li>
        ))}
      </ul>
    </div>
  )
}

// Usherr: where this browser plays each clip and trailer (home_preshow_fit.go):
// the file itself, a trailer's 1080p copy, or a copy converted as it plays,
// with a converted one to fall back on.
type Fit = { url: string; fallback?: string }

// A screen under 4K (a phone, most laptops) gets 4K clips at 1080p: a phone
// often can't decode 4K at all.
const smallScreen = () => Math.max(window.screen.width, window.screen.height) * (window.devicePixelRatio || 1) < 3000

// A clip or trailer video. It starts with its sound; when the browser won't
// without a tap (phones), it says Tap to start instead of staying black. One
// that won't play is played converted, and only then left out.
function PreshowVideo({ fit, onEnd, from = 0 }: { fit: Fit; onEnd: () => void; from?: number }) {
  const ref = useRef<HTMLVideoElement>(null)
  const [url, setUrl] = useState(fit.url)
  const [tap, setTap] = useState(false)
  // Usherr: linked screenings, joined part-way through: from there.
  const skipTo = useRef(from)
  const onMeta = () => {
    const v = ref.current
    if (v && skipTo.current > 1 && Number.isFinite(v.duration) && skipTo.current < v.duration) v.currentTime = skipTo.current
    skipTo.current = 0
  }
  const start = useCallback(() => {
    const v = ref.current
    if (!v) return
    v.play().then(
      () => setTap(false),
      (e: unknown) => {
        if (e instanceof DOMException && e.name === 'NotAllowedError') setTap(true)
      },
    )
  }, [])
  useEffect(start, [url, start])
  const failed = () => {
    if (fit.fallback && url !== fit.fallback) setUrl(fit.fallback)
    else onEnd()
  }
  return (
    <>
      <video ref={ref} className="psr-clip" src={url} playsInline preload="auto" onLoadedMetadata={onMeta} onEnded={onEnd} onError={failed} />
      {tap && (
        <button type="button" className="psr-tap primary" onClick={start}>
          <Icon name="play" size={28} /> {t('Tap to start')}
        </button>
      )}
    </>
  )
}

// A coming-soon trailer kept by Usherr: a file, so it plays at once.
function TrailerFile({ step, fit, onEnd, from }: { step: PreshowStep; fit: Fit; onEnd: () => void; from?: number }) {
  return (
    <div className="psr-trailer">
      <PreshowVideo fit={fit} onEnd={onEnd} from={from} />
      <div className="psr-trailer-tag">
        <span>{t('Coming soon')}</span>
        <strong>{step.title}</strong>
        <span>{soonLine(step.releaseDate, step.why, step.season)}</span>
      </div>
    </div>
  )
}

// A coming-soon trailer from YouTube; it ends when the video does (or after
// its seconds, whichever is first).
function Trailer({ step, onEnd, from = 0 }: { step: PreshowStep; onEnd: () => void; from?: number }) {
  const frame = useRef<HTMLIFrameElement>(null)
  const done = useRef(onEnd)
  useEffect(() => {
    done.current = onEnd
  })
  // Captions off (cc_load_policy only asks; a trailer can force them on).
  const noCaptions = () => {
    const w = frame.current?.contentWindow
    w?.postMessage(JSON.stringify({ event: 'command', func: 'unloadModule', args: ['captions'] }), '*')
    w?.postMessage(JSON.stringify({ event: 'command', func: 'unloadModule', args: ['cc'] }), '*')
  }
  useEffect(() => {
    const id = window.setTimeout(() => done.current(), Math.max(10, step.seconds) * 1000)
    const listen = (e: MessageEvent) => {
      if (e.source !== frame.current?.contentWindow || typeof e.data !== 'string') return
      try {
        const d = JSON.parse(e.data) as { event?: string; info?: { playerState?: number } | number }
        const state = typeof d.info === 'object' ? d.info?.playerState : d.event === 'onStateChange' ? d.info : undefined
        if (state === 0) done.current()
        if (state === 1) noCaptions() // playing: some trailers switch their captions on regardless
      } catch {
        // not YouTube's
      }
    }
    window.addEventListener('message', listen)
    return () => {
      window.clearTimeout(id)
      window.removeEventListener('message', listen)
    }
  }, [step])
  const q = new URLSearchParams({ autoplay: '1', controls: '0', rel: '0', modestbranding: '1', playsinline: '1', iv_load_policy: '3', cc_load_policy: '0', enablejsapi: '1', origin: window.location.origin })
  if (from >= 1) q.set('start', String(Math.floor(from))) // Usherr: joined part-way through
  const date = step.releaseDate ? new Date(step.releaseDate + 'T12:00:00').toLocaleDateString(locale(), { month: 'long', day: 'numeric' }) : ''
  return (
    <div className="psr-trailer">
      <iframe
        ref={frame}
        referrerPolicy="strict-origin-when-cross-origin"
        src={`https://www.youtube-nocookie.com/embed/${encodeURIComponent(step.youtube ?? '')}?${q}`}
        title={step.title}
        allow="autoplay; encrypted-media"
        onLoad={() => {
          frame.current?.contentWindow?.postMessage(JSON.stringify({ event: 'listening' }), '*')
          noCaptions()
        }}
      />
      <div className="psr-trailer-tag">
        <span>{t('Coming soon')}</span>
        <strong>{step.title}</strong>
        {date && <span>{date}</span>}
      </div>
    </div>
  )
}

function Clip({ fit, onEnd, from }: { fit: Fit; onEnd: () => void; from?: number }) {
  return <PreshowVideo fit={fit} onEnd={onEnd} from={from} />
}

// slotAt: which piece is on s seconds into a pre-show, and how far into it
// (past the end: one past the last piece).
function slotAt(plan: PreshowPlan, s: number): { i: number; into: number } {
  let start = 0
  for (let i = 0; i < plan.steps.length; i++) {
    const len = plan.steps[i].seconds
    if (s < start + len) return { i, into: Math.max(0, s - start) }
    start += len
  }
  return { i: plan.steps.length, into: 0 }
}

// Usherr: linked screenings, step 3. With a clock (seconds into the pre-show,
// the same moment at every Usherr), the pre-show runs by it: each piece has
// its slot, a piece that ends early leaves the welcome card for the rest of
// it, one that runs long is cut, and the film starts when the clock says.
// Nobody skips; the host may start the film for everyone (lead).
export default function PreshowRun({ plan, onDone, clock, lead }: { plan: PreshowPlan; onDone: () => void; clock?: () => number | null; lead?: () => void }) {
  const timed = !!clock
  // i: the piece on; into (timed): how far into its slot it started
  const [{ i, into }, setAt] = useState(() => (clock ? slotAt(plan, clock() ?? 0) : { i: 0, into: 0 }))
  const setI = useCallback((f: (x: number) => number) => setAt((cur) => ({ i: f(cur.i), into: 0 })), [])
  const [held, setHeld] = useState(-1) // timed: the piece that ended before its slot did
  useEffect(() => {
    if (!clock) return
    const id = window.setInterval(() => {
      const s = clock()
      if (s === null) return
      const at = slotAt(plan, s)
      setAt((cur) => (cur.i === at.i ? cur : at))
    }, 250)
    return () => window.clearInterval(id)
  }, [clock, plan])
  const [chrome, setChrome] = useState(true)
  const [full, setFull] = useState(() => !!document.fullscreenElement)
  const finished = useRef(onDone)
  useEffect(() => {
    finished.current = onDone
  })
  const next = useCallback(() => (timed ? setHeld(i) : setI((x) => x + 1)), [timed, i, setI])
  const step = plan.steps[i]
  const slotLeft = timed && step ? Math.max(0.5, step.seconds - into) : (step?.seconds ?? 0)
  // Usherr: where this browser plays each clip and trailer, asked once
  // before the first one; after 8 seconds without an answer, the files as
  // they are (each still falls back to nothing worse than before).
  const [fits, setFits] = useState<Record<string, Fit> | null>(null)
  useEffect(() => {
    const urls = plan.steps.map((s) => s.url).filter((u): u is string => !!u)
    if (urls.length === 0) {
      setFits({})
      return
    }
    let live = true
    const timer = window.setTimeout(() => live && setFits((f) => f ?? {}), 8000)
    preshowApi
      .fit(browserCaps(), smallScreen(), urls)
      .then((r) => live && setFits(r.fits ?? {}))
      .catch(() => live && setFits((f) => f ?? {}))
    return () => {
      live = false
      window.clearTimeout(timer)
    }
  }, [plan])
  const fitOf = (s: PreshowStep): Fit => fits?.[s.url ?? ''] ?? { url: s.url ?? '' }
  useEffect(() => {
    if (!step) {
      preshowApi.now(null)
      finished.current()
      return
    }
    preshowApi.now(step, plan.film)
  }, [step, plan.film])
  // Leaving part-way: the Marquee goes back to its own.
  useEffect(() => () => preshowApi.now(null), [])
  const skipAll = useCallback(() => setI(() => plan.steps.length), [plan.steps.length, setI])
  useEffect(() => {
    const key = (e: KeyboardEvent) => {
      if (timed) return // a linked screening's pre-show runs by the clock
      if (e.key === 'Escape') skipAll()
      else if (e.key === 'ArrowRight') next()
    }
    window.addEventListener('keydown', key)
    return () => window.removeEventListener('keydown', key)
  }, [next, skipAll, timed])
  // Full screen, like a cinema: asked for at the start (a browser allows it
  // after a click, so a Showtime that starts by itself offers the button),
  // and kept for the film.
  const goFull = useCallback(() => void document.documentElement.requestFullscreen?.().catch(() => undefined), [])
  useEffect(() => {
    goFull()
    const on = () => setFull(!!document.fullscreenElement)
    document.addEventListener('fullscreenchange', on)
    return () => document.removeEventListener('fullscreenchange', on)
  }, [goFull])
  // The buttons show while the mouse moves, then fade.
  useEffect(() => {
    let id = window.setTimeout(() => setChrome(false), 3500)
    const wake = () => {
      setChrome(true)
      window.clearTimeout(id)
      id = window.setTimeout(() => setChrome(false), 3500)
    }
    window.addEventListener('pointermove', wake)
    window.addEventListener('pointerdown', wake)
    return () => {
      window.clearTimeout(id)
      window.removeEventListener('pointermove', wake)
      window.removeEventListener('pointerdown', wake)
    }
  }, [])
  if (!step) return null
  let body: React.ReactNode
  if (timed && held === i) body = <Welcome plan={plan} />
  else if (fits === null && step.url) body = <p className="psr-getting">{t('Getting the pre-show ready…')}</p>
  else if (step.kind === 'clip') body = <Clip key={i} fit={fitOf(step)} onEnd={next} from={into} />
  else if (step.kind === 'trailer') body = step.url ? <TrailerFile key={i} step={step} fit={fitOf(step)} onEnd={next} from={into} /> : <Trailer key={i} step={step} onEnd={next} from={into} />
  else if (step.card === 'welcome')
    body = (
      <Timed key={i} seconds={slotLeft} onEnd={next}>
        <Welcome plan={plan} />
      </Timed>
    )
  else if (step.card === 'rating')
    body = (
      <Timed key={i} seconds={slotLeft} onEnd={next}>
        <Rating plan={plan} />
      </Timed>
    )
  else if (step.card === 'soon' || step.card === 'announce' || step.card === 'qr' || step.card === 'reel') {
    const Card = { soon: Soon, announce: Announce, qr: QRCard, reel: Reel }[step.card]
    body = (
      <Timed key={i} seconds={slotLeft} onEnd={next}>
        <Card step={step} />
      </Timed>
    )
  } else body = <Piece key={i} piece={step.card ?? 'feature'} onEnd={next} />
  return createPortal(
    <div className={`mq psr${chrome ? '' : ' psr-quiet'}`} role="dialog" aria-modal="true" aria-label={t('The pre-show')}>
      <div className="psr-stage">{body}</div>
      <div className="psr-bar">
        <span className="psr-where">
          {t('The pre-show')} · {i + 1}/{plan.steps.length}
        </span>
        {!full && (
          <button type="button" className="btn-sm" onClick={goFull}>
            {t('Full screen')}
          </button>
        )}
        {timed ? (
          lead ? (
            <button type="button" className="btn-sm btn-with-icon primary" onClick={lead} title={t('Ends the pre-show here and at every linked Usherr')}>
              <Icon name="play" size={14} /> {t('Start the film now, for everyone')}
            </button>
          ) : (
            <span className="psr-where">{t('In step with the host')}</span>
          )
        ) : (
          <>
            <button type="button" className="btn-sm btn-with-icon" onClick={next}>
              <Icon name="skip-next" size={14} /> {t('Skip')}
            </button>
            <button type="button" className="btn-sm btn-with-icon primary" onClick={skipAll} autoFocus>
              <Icon name="play" size={14} /> {t('Skip the pre-show')}
            </button>
          </>
        )}
      </div>
    </div>,
    document.body,
  )
}
