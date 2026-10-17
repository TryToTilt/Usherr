// Home fork: one video player for the whole app. Any page calls
// playTitle(kind, id, title); <PlayerHost /> (in the app shell) shows it on
// top of everything, attached to <body> so it always opens where you are.
//
// Before playing, the player tells the server what this browser can decode.
// The server (with Jellyfin) answers "direct": play the file as it is, or
// "hls": a stream Jellyfin converts on the fly (for Dolby Vision, Atmos and
// other formats browsers can't decode), relayed by Mediarium.
import { InfoBanner, StaticBurst, SurfButtons, useChannels, useSurfKeys } from './TVSurf' // Usherr
import { useTVPrefs } from './tvPrefs' // Usherr
import { playCard, type CardInfo } from './tvCard' // Usherr
import { useCallback, useEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import Icon from '../components/Icon'
import WatchLinks from '../components/WatchLinks'
import { browserTrouble, streamApi, type StreamStart } from './streamApi'
import './player.css'
import { t } from '../i18n'

export type PlayKind = 'movie' | 'episode' | 'video'
// stream: a "Stream now" session (home_streaming.go); onKeep downloads it for later.
// channel: a Mediarium TV channel (home_tvchannels.go); startAt and onEnded
// let a program start part-way through and hand over to the next one; live
// is a live channel's HLS address (home_tvlive.go).
type Request = {
  kind: PlayKind | 'stream' | 'channel'
  id: number
  title: string
  stream?: StreamStart
  onKeep?: () => void
  startAt?: number
  onEnded?: () => void
  subtitle?: string
  live?: string
  // Usherr: between a channel's shows, an "Up next" card (drawn) or a filler video
  card?: CardInfo
  direct?: string
  // home fork, streamed titles: the next release in line, the title for
  // Stremio on the TV, and the media server's own page for it
  onNext?: () => Promise<void>
  tvTitle?: { kind: 'movie' | 'series'; tmdbId: number; season?: number; episode?: number }
  watchIn?: { tmdbId: number; kind: 'movie' | 'tv' }
}

export type StreamExtras = Pick<Request, 'onNext' | 'tvTitle' | 'watchIn'>

let show: ((r: Request | null) => void) | null = null

// playTitle opens the player on a movie, an episode or a video.
export function playTitle(kind: PlayKind, id: number, title: string) {
  show?.({ kind, id, title })
}

// playStream opens the player on a started stream. onKeep, when given, is
// the "Keep it" button: download this for later.
export function playStream(stream: StreamStart, title: string, onKeep?: () => void, extras?: StreamExtras) {
  show?.({ kind: 'stream', id: 0, title: title || stream.title || stream.release, stream, onKeep, ...extras })
}

// playChannel tunes in to a Mediarium TV channel: whatever is on, from the
// right second, then the next program, and so on.
export function playChannel(id: number, name: string) {
  show?.({ kind: 'channel', id, title: name })
}

export function PlayerHost() {
  const [req, setReq] = useState<Request | null>(null)
  useEffect(() => {
    show = setReq
    return () => {
      show = null
    }
  }, [])
  if (!req) return null
  if (req.kind === 'channel') return createPortal(<ChannelView channelId={req.id} name={req.title} onClose={() => setReq(null)} />, document.body)
  return createPortal(<PlayerView key={`${req.kind}-${req.id}-${req.stream?.id ?? ''}`} req={req} onClose={() => setReq(null)} />, document.body)
}

// ChannelView plays a channel: it asks what is on, plays that program from
// the right second, and when it ends asks again. A streamed episode plays
// through Stream now; when it can't be found (or ends before its slot does)
// the channel is "off air" until the next program.
function ChannelView({ channelId: firstId, name: firstName, onClose, inline = false, muted = false }: { channelId: number; name: string; onClose: () => void; inline?: boolean; muted?: boolean }) {
  // Usherr: surfing. The channel on can change (up, down, a number, Last).
  const [chan, setChan] = useState({ id: firstId, name: firstName })
  const channelId = chan.id
  const name = chan.name
  const before = useRef<{ id: number; name: string } | null>(null)
  const [tick, setTick] = useState(0)
  const channels = useChannels(tick)
  const [staticRun, setStaticRun] = useState(0)
  const [banner, setBanner] = useState(true)
  const bannerTimer = useRef(0)
  const flash = useCallback(() => {
    setBanner(true)
    window.clearTimeout(bannerTimer.current)
    bannerTimer.current = window.setTimeout(() => setBanner(false), 5000)
  }, [])
  useEffect(() => {
    flash()
    return () => window.clearTimeout(bannerTimer.current)
  }, [flash])
  const go = (c?: { id: number; name: string }) => {
    if (!c || c.id === chan.id) return
    before.current = chan
    setChan({ id: c.id, name: c.name })
    setStaticRun((n) => n + 1)
    setTick((n) => n + 1)
    flash()
  }
  const at = channels.findIndex((c) => c.id === chan.id)
  const step = (by: number) => channels.length > 0 && go(channels[(Math.max(at, 0) + by + channels.length) % channels.length])
  const surf = {
    up: () => step(1),
    down: () => step(-1),
    number: (n: number) => go(channels.find((c) => c.number === n)),
    last: () => go(before.current ?? undefined),
    info: () => {
      setTick((n) => n + 1)
      flash()
    },
  }
  useSurfKeys(inline ? { up: () => undefined, down: () => undefined, number: () => undefined, last: () => undefined, info: () => undefined } : surf)
  const prefs = useTVPrefs()
  const extras = inline ? null : (
    <>
      <StaticBurst run={staticRun} />
      <InfoBanner ch={channels.find((c) => c.id === chan.id)} show={banner} typed="" />
      <SurfButtons
        {...surf}
        startOver={(() => {
          const cur = channels.find((c) => c.id === chan.id)?.now
          if (!cur || (cur.kind !== 'episode' && cur.kind !== 'movie') || !cur.itemId) return undefined
          return () => {
            onClose()
            window.setTimeout(() => playTitle(cur.kind as PlayKind, cur.itemId!, cur.subtitle ? `${cur.title} · ${cur.subtitle}` : cur.title), 50)
          }
        })()}
      />
      {prefs.crt && <div className="tv-crt" aria-hidden="true" />}
    </>
  )
  const [now, setNow] = useState<{ key: string; req: Request } | null>(null)
  const [error, setError] = useState('')
  const [offAir, setOffAir] = useState('')
  const ended = useRef('')
  const timer = useRef(0)
  const tune = useCallback(() => {
    const asked = Date.now()
    window.clearTimeout(timer.current)
    fetch(`/api/tv/channels/${channelId}/now`, { credentials: 'include' })
      .then(async (r) => {
        const data = await r.json().catch(() => null)
        if (!r.ok) throw new Error(data?.error ?? t("Couldn't tune in (error {status}).", { status: r.status }))
        return data as {
          program?: { kind: 'episode' | 'movie' | 'live' | 'stream' | 'card' | 'filler'; itemId: number; title: string; subtitle?: string; start: string }
          upNext?: { title: string; subtitle?: string; start: string }
          filler?: string
          cut?: boolean
          offset?: number
          remaining?: number
          live?: string
          stream?: StreamStart
          offAir?: boolean
        }
      })
      .then((d) => {
        if (d.live) {
          setNow({
            key: `live-${channelId}`,
            req: { kind: 'channel', id: channelId, title: d.program ? `${name} · ${d.program.title}` : name, subtitle: d.program?.subtitle, live: d.live },
          })
          return
        }
        const p = d.program
        if (!p || p.kind === 'live') throw new Error(t('Nothing is on this channel right now.'))
        const key = `${p.kind}-${p.itemId}-${p.start}`
        // Off air: nothing to stream, or the program already played out.
        if (d.offAir || key === ended.current) {
          setNow(null)
          setOffAir(
            d.offAir
              ? t("{title} can't be streamed right now.", { title: `${p.title}${p.subtitle ? ' · ' + p.subtitle : ''}` })
              : t('{title} has finished.', { title: p.title }),
          )
          timer.current = window.setTimeout(tune, Math.max(2, d.remaining ?? 30) * 1000 + 500)
          return
        }
        setOffAir('')
        const onEnded = () => {
          ended.current = key
          window.setTimeout(tune, 500)
        }
        // Usherr: between shows, the "Up next" card or a filler video.
        if (p.kind === 'card') {
          setNow({ key, req: { kind: 'channel', id: channelId, title: `${name} · ${t('Up next')}`, subtitle: d.upNext?.title, card: { channel: name, next: d.upNext, seconds: d.remaining ?? 10 }, onEnded } })
          return
        }
        if (p.kind === 'filler' && d.filler) {
          setNow({ key, req: { kind: 'channel', id: channelId, title: `${name} · ${t('Break')}`, direct: d.filler, startAt: (d.offset ?? 0) + (Date.now() - asked) / 1000, onEnded } })
          return
        }
        // Usherr: a part of a show split by breaks ends before its file does.
        if (d.cut && d.remaining) timer.current = window.setTimeout(onEnded, d.remaining * 1000 + 300)
        // A converted stream already starts at the right moment.
        const startAt = d.stream && (d.stream as StreamStart & { converted?: boolean }).converted ? undefined : (d.offset ?? 0) + (Date.now() - asked) / 1000
        setNow({
          key,
          req: d.stream
            ? { kind: 'stream', id: 0, title: `${name} · ${p.title}`, subtitle: p.subtitle, stream: d.stream, startAt, onEnded }
            : { kind: p.kind as PlayKind, id: p.itemId, title: `${name} · ${p.title}`, subtitle: p.subtitle, startAt, onEnded },
        })
      })
      .catch((e: Error) => setError(e.message))
  }, [channelId, name])
  useEffect(() => {
    tune()
    return () => window.clearTimeout(timer.current)
  }, [tune])
  useEffect(() => {
    // A new channel starts clean.
    setError('')
    setOffAir('')
    ended.current = ''
  }, [channelId])
  if (inline && (error || offAir))
    return (
      <div className="hp-inline">
        <p className="hp-inline-msg">{error ? error : <><strong>{t('Off air.')}</strong> {offAir}</>}</p>
      </div>
    )
  if (error || offAir)
    return (
      <>
      {extras}
      <div className="hp-player" role="dialog" aria-modal="true" aria-label={name} onClick={onClose}>
        <div className="hp-box" onClick={(e) => e.stopPropagation()}>
          <div className="hp-head">
            <span className="hp-title">{name}</span>
            <button className="btn-sm" onClick={onClose} aria-label={t('Close the player')}>
              <Icon name="x" size={16} />
            </button>
          </div>
          {error ? (
            <p className="error-text hp-msg">{error}</p>
          ) : (
            <p className="hp-msg">
              <strong>{t('Off air.')}</strong> {offAir} {t('The channel comes back with the next program.')}
            </p>
          )}
        </div>
      </div>
      </>
    )
  if (!now) return inline ? <div className="hp-inline"><p className="hp-inline-msg">{t('Tuning in…')}</p></div> : extras
  // One player for the whole channel: each program loads into the same
  // video, so full screen (and a phone's permission to play) carry on.
  return (
    <>
      <PlayerView key={`channel-${channelId}`} req={now.req} onClose={onClose} inline={inline} muted={muted} />
      {extras}
    </>
  )
}

// ChannelPreview is a channel playing small and (unless unmuted) silent,
// inside a page: the TV guide's preview, as on a cable box.
export function ChannelPreview({ channelId, name, muted = true }: { channelId: number; name: string; muted?: boolean }) {
  return <ChannelView key={channelId} channelId={channelId} name={name} onClose={() => undefined} inline muted={muted} />
}

// What this browser can decode, measured on this device.
function browserCaps() {
  const ms = typeof MediaSource !== 'undefined' ? MediaSource : null
  const can = (type: string) => {
    try {
      return !!ms && ms.isTypeSupported(type)
    } catch {
      return false
    }
  }
  const v = document.createElement('video')
  const hdr = typeof window.matchMedia === 'function' && window.matchMedia('(dynamic-range: high)').matches
  return {
    hevc: can('video/mp4; codecs="hvc1.2.4.L153.B0"') || can('video/mp4; codecs="hev1.2.4.L153.B0"'),
    av1: can('video/mp4; codecs="av01.0.12M.10"'),
    vp9: can('video/webm; codecs="vp9"'),
    dv: can('video/mp4; codecs="dvh1.05.06"') || can('video/mp4; codecs="dvhe.05.06"'),
    hdr,
    ac3: can('audio/mp4; codecs="ac-3"'),
    eac3: can('audio/mp4; codecs="ec-3"'),
    opus: can('audio/mp4; codecs="opus"'),
    flac: can('audio/mp4; codecs="flac"'),
    mkv: v.canPlayType('video/x-matroska') !== '',
    nativeHls: v.canPlayType('application/vnd.apple.mpegurl') !== '',
  }
}

type Plan = { mode: 'direct' | 'hls'; url: string; session?: string; note?: string; reasons?: string[] }

async function prepare(req: Request, caps: ReturnType<typeof browserCaps>): Promise<Plan> {
  const res = await fetch('/api/play/prepare', {
    method: 'POST',
    credentials: 'include',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ kind: req.kind, id: req.id, caps }),
  })
  const data = await res.json().catch(() => null)
  if (!res.ok) throw new Error(data?.error ?? t("Couldn't start playback (error {status}).", { status: res.status }))
  return data as Plan
}

function stopSession(token?: string) {
  if (!token) return
  void fetch(`/api/play/hls/${token}`, { method: 'DELETE', credentials: 'include', keepalive: true }).catch(() => undefined)
}

function PlayerView({ req, onClose, inline = false, muted = false }: { req: Request; onClose: () => void; inline?: boolean; muted?: boolean }) {
  const videoRef = useRef<HTMLVideoElement>(null)
  const [plan, setPlan] = useState<Plan | null>(null)
  const [error, setError] = useState('')
  const [failed, setFailed] = useState(false)
  useEffect(() => {
    if (videoRef.current) videoRef.current.muted = muted
  }, [muted, plan])

  useEffect(() => {
    if (inline) return // part of a page: no keys, no locked scrolling
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
    }
    document.addEventListener('keydown', onKey)
    const overflow = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    return () => {
      document.removeEventListener('keydown', onKey)
      document.body.style.overflow = overflow
    }
  }, [onClose, inline])

  // Work out how to play, then start.
  useEffect(() => {
    let cancelled = false
    let session: string | undefined
    let hls: { destroy: () => void } | null = null
    const caps = browserCaps()
    // A channel's next program arrives in the same player: start clean.
    setPlan(null)
    setError('')
    setFailed(false)
    if (req.card) {
      // Usherr: a channel's "Up next" card, drawn into the video.
      setPlan({ mode: 'direct', url: '' })
      const video = videoRef.current
      const stop = video ? playCard(video, req.card, () => req.onEnded?.()) : () => undefined
      return () => {
        cancelled = true
        stop()
      }
    }
    if (req.direct) {
      // Usherr: a filler video from the channel's folder, played as it is.
      const url = req.direct
      setPlan({ mode: 'direct', url })
      const video = videoRef.current
      if (video) {
        if (req.startAt) {
          const at = req.startAt
          video.addEventListener('loadedmetadata', () => (video.currentTime = at), { once: true })
        }
        video.src = url
        void video.play().catch(() => undefined)
      }
      return () => {
        cancelled = true
      }
    }
    if (req.live) {
      // A live channel: its stream as it is, relayed by Mediarium.
      const src = req.live
      setPlan({ mode: 'hls', url: src, note: t('Live') })
      const video = videoRef.current
      void (async () => {
        if (!video) return
        // hls.js where it works (it copes best with ad breaks); the
        // browser's own HLS (iPhone, Safari) otherwise.
        const { default: Hls } = await import('hls.js')
        if (cancelled) return
        if (!Hls.isSupported()) {
          if (!caps.nativeHls) {
            setError(t("This browser can't play live channels."))
            return
          }
          video.src = src
        } else {
          const h = new Hls({ liveDurationInfinity: true })
          h.on(Hls.Events.ERROR, (_e, data) => {
            if (data.fatal) setFailed(true)
          })
          h.loadSource(src)
          h.attachMedia(video)
          hls = h
        }
        void video.play().catch(() => undefined)
      })()
      return () => {
        cancelled = true
        hls?.destroy()
      }
    }
    if (req.stream) {
      // A stream plays as it is, relayed by Mediarium.
      const st = req.stream
      setPlan({ mode: 'direct', url: st.videoUrl })
      const video = videoRef.current
      if (video) {
        if (req.startAt) {
          // A channel tuning in part-way through.
          const at = req.startAt
          video.addEventListener('loadedmetadata', () => (video.currentTime = at), { once: true })
        }
        video.src = st.videoUrl
        void video.play().catch(() => undefined)
      }
      return () => {
        cancelled = true
        streamApi.stop(st.id)
      }
    }
    prepare(req, caps as ReturnType<typeof browserCaps>)
      .then(async (p) => {
        if (cancelled) {
          stopSession(p.session)
          return
        }
        session = p.session
        setPlan(p)
        const video = videoRef.current
        if (!video) return
        if (p.mode === 'hls' && !caps.nativeHls) {
          const { default: Hls } = await import('hls.js')
          if (cancelled) return
          if (!Hls.isSupported()) {
            setError(t("This browser can't play converted streams."))
            return
          }
          const h = new Hls({
            startPosition: req.startAt ?? -1,
            xhrSetup: (xhr) => {
              xhr.withCredentials = true
            },
          })
          h.on(Hls.Events.ERROR, (_e, data) => {
            if (data.fatal) setFailed(true)
          })
          h.loadSource(p.url)
          h.attachMedia(video)
          hls = h
        } else {
          video.src = p.url
          if (req.startAt) {
            const at = req.startAt
            video.addEventListener('loadedmetadata', () => (video.currentTime = at), { once: true })
          }
        }
        void video.play().catch(() => undefined)
      })
      .catch((e: Error) => !cancelled && setError(e.message))
    return () => {
      cancelled = true
      hls?.destroy()
      stopSession(session)
    }
  }, [req])

  const note = req.live ? undefined : plan?.mode === 'hls' ? t('Converted by Jellyfin for this browser') : req.stream ? browserTrouble(req.stream) || plan?.note : plan?.note
  const [kept, setKept] = useState(false)
  const [copied, setCopied] = useState(false)
  const [tvName, setTvName] = useState('')
  const [nexting, setNexting] = useState(false)
  const [tvNote, setTvNote] = useState('')
  useEffect(() => {
    if (!req.stream) return
    streamApi
      .status()
      .then((st) => setTvName(st.tv?.name ?? ''))
      .catch(() => undefined)
  }, [req.stream])
  const playOnTV = () => {
    if (!req.stream) return
    setTvNote(t('Sending it to the TV…'))
    // A title Stremio knows opens there (its player handles 4K, Dolby Vision
    // and lossless sound best); otherwise the stream goes to VLC.
    ;(req.tvTitle ? streamApi.tvTitle(req.tvTitle) : streamApi.tvStream(req.stream.id))
      .then((r) => {
        videoRef.current?.pause()
        setTvNote(r.message)
      })
      .catch((e: Error) => setTvNote(e.message))
  }
  const copyHomeLink = () => {
    if (!req.stream) return
    streamApi
      .link(req.stream.id)
      .then((l) => navigator.clipboard?.writeText(l.homeUrl))
      .then(() => setCopied(true))
      .catch(() => undefined)
  }
  if (inline)
    return (
      <div className="hp-inline">
        {error || failed ? (
          <p className="hp-inline-msg">{error || t("This can't be previewed here. Open it to watch.")}</p>
        ) : (
          <video ref={videoRef} muted={muted} autoPlay playsInline onEnded={() => req.onEnded?.()} onError={() => plan?.mode === 'direct' && setFailed(true)} />
        )}
        {!plan && !error && <p className="hp-inline-msg">{t('Tuning in…')}</p>}
      </div>
    )
  return (
    <div className="hp-player" role="dialog" aria-modal="true" aria-label={t('Playing {title}', { title: req.title })} onClick={onClose}>
      <div className="hp-box" onClick={(e) => e.stopPropagation()}>
        <div className="hp-head">
          <span className="hp-title">
            {req.title}
            {req.subtitle && <span className="hp-sub"> · {req.subtitle}</span>}
          </span>
          <button className="btn-sm" onClick={onClose} aria-label={t('Close the player')}>
            <Icon name="x" size={16} />
          </button>
        </div>
        {error ? (
          <p className="error-text hp-msg">{error}</p>
        ) : failed ? (
          <p className="error-text hp-msg">
            {req.stream
              ? req.onNext
                ? t("This release couldn't be played in the browser. Try another release or a lower quality, watch it in Jellyfin (it converts), or play it on the TV.")
                : t("This stream couldn't be played in the browser. Copy the home link below and open it in VLC, or play it on the TV.")
              : req.live
                ? t("This channel isn't sending anything right now. Free channels come and go: try again later, or another channel.")
                : t("This video couldn't be played here. The file itself is fine: it plays in Jellyfin, VLC or mpv.")}
          </p>
        ) : (
          <video ref={videoRef} controls autoPlay playsInline onEnded={() => req.onEnded?.()} onError={() => plan?.mode === 'direct' && setFailed(true)} />
        )}
        {!plan && !error && <p className="hp-note">{t('Getting ready…')}</p>}
        {note && <p className="hp-note">{note}</p>}
        {tvNote && <p className="hp-note">{tvNote}</p>}
        {req.stream && (
          <div className="hp-actions">
            {req.stream.release && <span className="hp-release">{req.stream.release}</span>}
            {req.onNext && (
              <button
                className="btn-sm"
                disabled={nexting}
                onClick={() => {
                  setNexting(true)
                  setTvNote('')
                  req.onNext?.().catch((e: Error) => {
                    setTvNote(e.message)
                    setNexting(false)
                  })
                }}
                title={t('Plays the next release StreamNZB found instead of this one')}
              >
                <Icon name="refresh" size={14} /> {nexting ? t('Finding another…') : t('Try another release')}
              </button>
            )}
            {req.watchIn && <WatchLinks tmdbId={req.watchIn.tmdbId} kind={req.watchIn.kind} />}
            {tvName && (
              <button className="btn-sm" onClick={playOnTV} title={req.tvTitle ? t('Opens it in Stremio on the TV') : t('Plays this stream on the TV, in VLC')}>
                <Icon name="tv" size={14} /> {t('Play on {name}', { name: tvName })}
              </button>
            )}
            <button className="btn-sm" onClick={copyHomeLink} title={t('A link for VLC or another player on the home network')}>
              <Icon name="external" size={14} /> {copied ? t('Copied') : t('Copy home link')}
            </button>
            {req.onKeep && (
              <button
                className="btn-sm primary"
                disabled={kept}
                onClick={() => {
                  req.onKeep?.()
                  setKept(true)
                }}
              >
                <Icon name="download" size={14} /> {kept ? t('Downloading for later') : t('Keep it')}
              </button>
            )}
          </div>
        )}
      </div>
    </div>
  )
}
