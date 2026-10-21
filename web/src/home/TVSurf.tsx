// Usherr: watching a channel like a cable box. Channel up and down (keys,
// buttons or a swipe), typing a number, Last channel, a burst of static
// between channels, and the info banner: what's on, how long is left and
// what's next.
import { useEffect, useRef, useState } from 'react'
import Icon from '../components/Icon'
import { locale, t } from '../i18n'
import { tvPrefsNow } from './tvPrefs'

export interface SurfChannel {
  id: number
  number: number
  name: string
  logoUrl?: string
  enabled: boolean
  now?: { title: string; subtitle?: string; start: string; end: string; kind?: string; itemId?: number }
  next?: { title: string; subtitle?: string; start: string; end: string }
}

const clock = (iso: string) => new Date(iso).toLocaleTimeString(locale(), { hour: 'numeric', minute: '2-digit' })

// useChannels is the channel list with what's on now and next, read again on each tune.
export function useChannels(tick: number): SurfChannel[] {
  const [list, setList] = useState<SurfChannel[]>([])
  useEffect(() => {
    fetch('/api/tv/channels', { credentials: 'include' })
      .then((r) => (r.ok ? r.json() : []))
      .then((l: SurfChannel[]) => setList(l.filter((c) => c.enabled && !tvPrefsNow().hidden.includes(c.id))))
      .catch(() => undefined)
  }, [tick])
  return list
}

// useSurfKeys: ↑/↓ (and Page Up/Down) change channel, digits tune by
// number, Backspace or L goes back, I shows the banner. Swipes too.
export function useSurfKeys(on: { up: () => void; down: () => void; number: (n: number) => void; last: () => void; info: () => void }) {
  const digits = useRef('')
  const timer = useRef(0)
  const handlers = useRef(on)
  handlers.current = on
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const el = e.target as HTMLElement
      if (el && (el.tagName === 'INPUT' || el.tagName === 'TEXTAREA' || el.isContentEditable)) return
      const h = handlers.current
      if (e.key === 'ArrowUp' || e.key === 'PageUp' || e.key === 'ChannelUp') return void (e.preventDefault(), h.up())
      if (e.key === 'ArrowDown' || e.key === 'PageDown' || e.key === 'ChannelDown') return void (e.preventDefault(), h.down())
      if (e.key === 'Backspace' || e.key === 'l' || e.key === 'L') return void (e.preventDefault(), h.last())
      if (e.key === 'i' || e.key === 'I' || e.key === 'Info') return void h.info()
      if (/^[0-9]$/.test(e.key)) {
        digits.current += e.key
        window.clearTimeout(timer.current)
        timer.current = window.setTimeout(() => {
          const n = Number(digits.current)
          digits.current = ''
          if (n > 0) handlers.current.number(n)
        }, 1200)
      }
    }
    let y0 = 0
    let x0 = 0
    const start = (e: TouchEvent) => {
      y0 = e.touches[0]?.clientY ?? 0
      x0 = e.touches[0]?.clientX ?? 0
    }
    const end = (e: TouchEvent) => {
      const t0 = e.changedTouches[0]
      if (!t0) return
      const dy = t0.clientY - y0
      const dx = t0.clientX - x0
      if (Math.abs(dy) > 70 && Math.abs(dx) < 50) (dy < 0 ? handlers.current.up : handlers.current.down)()
    }
    document.addEventListener('keydown', onKey)
    document.addEventListener('touchstart', start, { passive: true })
    document.addEventListener('touchend', end, { passive: true })
    return () => {
      document.removeEventListener('keydown', onKey)
      document.removeEventListener('touchstart', start)
      document.removeEventListener('touchend', end)
      window.clearTimeout(timer.current)
    }
  }, [])
}

// Static: snow (and, when chosen, its hiss) on a channel change, until the
// new channel's picture is actually moving. force: true always, false never
// (or stop now), undefined as the viewer set it.
export function StaticBurst({ run, force }: { run: number; force?: boolean }) {
  const canvas = useRef<HTMLCanvasElement>(null)
  const [on, setOn] = useState(false)
  const active = useRef(0)
  const stop = useRef<(() => void) | null>(null)
  useEffect(() => () => stop.current?.(), [])
  useEffect(() => {
    if (run === 0) return
    if (!(force ?? tvPrefsNow().staticOn)) {
      stop.current?.()
      return
    }
    if (active.current === run) return // already showing (or shown) for this change
    stop.current?.()
    active.current = run
    setOn(true)
    const c = canvas.current
    const ctx = c?.getContext('2d')
    let frame = 0
    const draw = () => {
      if (!c || !ctx) return
      const img = ctx.createImageData(c.width, c.height)
      for (let i = 0; i < img.data.length; i += 4) {
        const v = Math.random() * 255
        img.data[i] = img.data[i + 1] = img.data[i + 2] = v
        img.data[i + 3] = 255
      }
      ctx.putImageData(img, 0, 0)
      frame = requestAnimationFrame(draw)
    }
    draw()
    let audio: AudioContext | null = null
    if (tvPrefsNow().staticSound) {
      try {
        audio = new AudioContext()
        const buf = audio.createBuffer(1, audio.sampleRate * 0.5, audio.sampleRate)
        const d = buf.getChannelData(0)
        for (let i = 0; i < d.length; i++) d[i] = (Math.random() * 2 - 1) * 0.15
        const src = audio.createBufferSource()
        src.buffer = buf
        src.loop = true
        src.connect(audio.destination)
        src.start()
      } catch {
        audio = null
      }
    }
    // The picture is moving when its time goes on between two looks; a
    // black, buffering or paused video doesn't count.
    const t0 = Date.now()
    let last = -1
    const video = () => {
      const p = c?.parentElement
      if (!p) return null
      return (p.classList.contains('tv-inline') ? p.querySelector('video') : p.querySelector(':scope > .hp-player video')) as HTMLVideoElement | null
    }
    const watch = window.setInterval(() => {
      const v = video()
      const waited = Date.now() - t0
      if (v && !v.paused && v.readyState >= 3 && v.videoWidth > 0) {
        if (last >= 0 && v.currentTime > last + 0.03 && waited >= 450) stop.current?.()
        last = v.currentTime
      } else last = -1
      if (c?.parentElement?.querySelector('[data-tv-error]') || (!v && waited > 8000) || waited > 20000) stop.current?.()
    }, 120)
    stop.current = () => {
      stop.current = null
      cancelAnimationFrame(frame)
      window.clearInterval(watch)
      void audio?.close().catch(() => undefined)
      setOn(false)
    }
  }, [run, force])
  return <canvas ref={canvas} width={160} height={90} className={`tv-static${on ? ' on' : ''}`} aria-hidden="true" />
}

// The info banner along the bottom: channel, what's on, time left, next.
export function InfoBanner({ ch, show, typed }: { ch?: SurfChannel; show: boolean; typed: string }) {
  if (typed) return <div className="tv-banner on tv-banner-typed">{typed}</div>
  if (!ch) return null
  const now = ch.now
  const left = now ? Math.max(0, Math.round((new Date(now.end).getTime() - Date.now()) / 60000)) : 0
  const progress = now ? Math.min(1, (Date.now() - new Date(now.start).getTime()) / (new Date(now.end).getTime() - new Date(now.start).getTime())) : 0
  return (
    <div className={`tv-banner${show ? ' on' : ''}`} aria-live="polite">
      <div className="tv-banner-ch">
        {ch.logoUrl ? <img src={ch.logoUrl} alt="" /> : null}
        <span className="tv-banner-num">{ch.number}</span>
        <span className="tv-banner-name">{ch.name}</span>
      </div>
      <div className="tv-banner-now">
        {now ? (
          <>
            <strong>{now.title}</strong>
            {now.subtitle && <span className="tv-banner-sub">{now.subtitle}</span>}
            <span className="tv-banner-time">
              {clock(now.start)}–{clock(now.end)} · {t('{n} min left', { n: left })}
            </span>
            <span className="tv-banner-bar">
              <span style={{ width: `${progress * 100}%` }} />
            </span>
          </>
        ) : (
          <strong>{t('Up next')}</strong>
        )}
      </div>
      {ch.next && (
        <div className="tv-banner-next">
          <small>{t('Next')}</small>
          <span>
            {clock(ch.next.start)} {ch.next.title}
          </span>
        </div>
      )}
    </div>
  )
}

// The buttons for those without a keyboard: up, down, last, info.
export function SurfButtons({ up, down, last, info, startOver }: { up: () => void; down: () => void; last: () => void; info: () => void; startOver?: () => void }) {
  return (
    <div className="tv-surf" onClick={(e) => e.stopPropagation()}>
      {startOver && (
        <button type="button" className="icon-btn" onClick={startOver} aria-label={t('Start over')} title={t('Start over: play this from the beginning')}>
          <Icon name="refresh" size={16} />
        </button>
      )}
      <button type="button" className="icon-btn" onClick={up} aria-label={t('Channel up')} title={t('Channel up (↑)')}>
        <Icon name="chevron-up" size={18} />
      </button>
      <button type="button" className="icon-btn" onClick={down} aria-label={t('Channel down')} title={t('Channel down (↓)')}>
        <Icon name="chevron-down" size={18} />
      </button>
      <button type="button" className="btn-sm" onClick={last} title={t('The channel before (Backspace)')}>
        {t('Last')}
      </button>
      <button type="button" className="icon-btn" onClick={info} aria-label={t('What’s on')} title={t('What’s on (I)')}>
        <Icon name="info" size={16} />
      </button>
    </div>
  )
}
