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

// A coming-soon trailer kept by Usherr: a file, so it plays at once.
function TrailerFile({ step, onEnd }: { step: PreshowStep; onEnd: () => void }) {
  return (
    <div className="psr-trailer">
      <video className="psr-clip" src={step.url} autoPlay playsInline onEnded={onEnd} onError={onEnd} />
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
function Trailer({ step, onEnd }: { step: PreshowStep; onEnd: () => void }) {
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

function Clip({ step, onEnd }: { step: PreshowStep; onEnd: () => void }) {
  return <video className="psr-clip" src={step.url} autoPlay playsInline onEnded={onEnd} onError={onEnd} />
}

export default function PreshowRun({ plan, onDone }: { plan: PreshowPlan; onDone: () => void }) {
  const [i, setI] = useState(0)
  const [chrome, setChrome] = useState(true)
  const [full, setFull] = useState(() => !!document.fullscreenElement)
  const finished = useRef(onDone)
  useEffect(() => {
    finished.current = onDone
  })
  const next = useCallback(() => setI((x) => x + 1), [])
  const step = plan.steps[i]
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
  const skipAll = useCallback(() => setI(plan.steps.length), [plan.steps.length])
  useEffect(() => {
    const key = (e: KeyboardEvent) => {
      if (e.key === 'Escape') skipAll()
      else if (e.key === 'ArrowRight') next()
    }
    window.addEventListener('keydown', key)
    return () => window.removeEventListener('keydown', key)
  }, [next, skipAll])
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
  if (step.kind === 'clip') body = <Clip key={i} step={step} onEnd={next} />
  else if (step.kind === 'trailer') body = step.url ? <TrailerFile key={i} step={step} onEnd={next} /> : <Trailer key={i} step={step} onEnd={next} />
  else if (step.card === 'welcome')
    body = (
      <Timed key={i} seconds={step.seconds} onEnd={next}>
        <Welcome plan={plan} />
      </Timed>
    )
  else if (step.card === 'rating')
    body = (
      <Timed key={i} seconds={step.seconds} onEnd={next}>
        <Rating plan={plan} />
      </Timed>
    )
  else if (step.card === 'soon' || step.card === 'announce' || step.card === 'qr' || step.card === 'reel') {
    const Card = { soon: Soon, announce: Announce, qr: QRCard, reel: Reel }[step.card]
    body = (
      <Timed key={i} seconds={step.seconds} onEnd={next}>
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
        <button type="button" className="btn-sm btn-with-icon" onClick={next}>
          <Icon name="skip-next" size={14} /> {t('Skip')}
        </button>
        <button type="button" className="btn-sm btn-with-icon primary" onClick={skipAll} autoFocus>
          <Icon name="play" size={14} /> {t('Skip the pre-show')}
        </button>
      </div>
    </div>,
    document.body,
  )
}
