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
import { locale, t } from '../i18n'
import { TracksMenu, prefAudioLang, prefSubLang, pickSub, sameLang, type PlayTrack, type Tracks } from './playerTracks' // Usherr

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
  onNext?: (at?: number) => Promise<void>
  tvTitle?: { kind: 'movie' | 'series'; tmdbId: number; season?: number; episode?: number }
  watchIn?: { tmdbId: number; kind: 'movie' | 'tv' }
}

export type StreamExtras = Pick<Request, 'onNext' | 'tvTitle' | 'watchIn' | 'startAt'>

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
  const [staticRun, setStaticRun] = useState(inline ? 1 : 0) // the guide's player: a burst on each channel picked
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
  const [now, setNow] = useState<{ key: string; req: Request } | null>(null)
  const [error, setError] = useState('')
  const [offAir, setOffAir] = useState('')
  // Usherr: the channel's look for what's on: "vintage", "hd" or "" (the viewer's settings).
  const [look, setLook] = useState('')
  const crtOn = look === 'vintage' || (look !== 'hd' && prefs.crt)
  const staticForce = error || offAir || look === 'hd' ? false : look === 'vintage' ? true : undefined
  const crt = crtOn && <div className={`tv-crt${look === 'vintage' ? ' tv-old' : ''}`} aria-hidden="true" />
  const extras = inline ? null : (
    <>
      <StaticBurst run={staticRun} force={staticForce} />
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
      {crt}
    </>
  )
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
          look?: string
          preparing?: boolean
          retryIn?: number
        }
      })
      .then((d) => {
        setLook(d.look ?? '')
        // Usherr: the stream is still being found: the static stays, and it asks again shortly.
        if (d.preparing) {
          timer.current = window.setTimeout(tune, Math.max(2, d.retryIn ?? 3) * 1000)
          return
        }
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
    setLook('')
    setNow(null) // the last channel's picture doesn't play on under the static
    ended.current = ''
  }, [channelId])
  // Usherr: the guide's small player keeps its static and scan lines through tuning, off air and playing.
  if (inline)
    return (
      <div className="tv-inline">
        {error || offAir ? (
          <div className="hp-inline">
            <p className="hp-inline-msg">{error ? error : <><strong>{t('Off air.')}</strong> {offAir}</>}</p>
          </div>
        ) : now ? (
          <PlayerView key={`channel-${channelId}`} req={now.req} onClose={onClose} inline muted={muted} />
        ) : (
          <div className="hp-inline"><p className="hp-inline-msg">{t('Tuning in…')}</p></div>
        )}
        <StaticBurst run={staticRun} force={staticForce} />
        {crt}
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
  if (!now) return extras
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

type Plan = { mode: 'direct' | 'hls'; url: string; session?: string; note?: string; reasons?: string[]; converted?: boolean; audio?: number; shift?: number }
// Usherr: asking again: from where it got to, converted, with a sound track or a burned-in subtitle.
type Redo = { at: number; force?: boolean; audio?: number; burn?: number }

async function prepare(req: Request, caps: ReturnType<typeof browserCaps>, redo?: Redo): Promise<Plan> {
  const res = await fetch('/api/play/prepare', {
    method: 'POST',
    credentials: 'include',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      kind: req.kind,
      id: req.id,
      caps,
      force: !!redo?.force,
      at: redo ? redo.at : (req.startAt ?? 0),
      audio: redo?.audio,
      burn: redo?.burn,
      lang: prefAudioLang() || locale() || 'en',
    }),
  })
  const data = await res.json().catch(() => null)
  if (!res.ok) throw new Error(data?.error ?? t("Couldn't start playback (error {status}).", { status: res.status }))
  return data as Plan
}

function stopSession(token?: string) {
  if (!token) return
  void fetch(`/api/play/hls/${token}`, { method: 'DELETE', credentials: 'include', keepalive: true }).catch(() => undefined)
}

// Usherr: how late this device's speakers play what the page sends them
// (spatial sound such as Dolby Atmos or DTS:X, a receiver, Bluetooth), as the
// browser measures it. A video's own sound is matched to its picture by the
// browser; a sound played beside the picture is matched with this.
let speakers: AudioContext | null = null
function speakerDelay(): number {
  try {
    speakers ??= new AudioContext()
    if (speakers.state === 'suspended') void speakers.resume().catch(() => undefined)
    return (speakers.outputLatency || 0) + (speakers.baseLatency || 0)
  } catch {
    return 0
  }
}

// The sound sync set in the player, in milliseconds (more is later), kept on
// this device: what the measured delay leaves over.
const SYNC_KEY = 'usherr.player.soundSync'
function savedSync(): number {
  try {
    return Math.max(-2000, Math.min(2000, Number(localStorage.getItem(SYNC_KEY)) || 0))
  } catch {
    return 0
  }
}

function PlayerView({ req, onClose, inline = false, muted = false }: { req: Request; onClose: () => void; inline?: boolean; muted?: boolean }) {
  const videoRef = useRef<HTMLVideoElement>(null)
  const [plan, setPlan] = useState<Plan | null>(null)
  const [error, setError] = useState('')
  const [failed, setFailed] = useState(false)
  // Usherr: played as it is but no sound (or no picture): ask again for a converted copy, from where it got to.
  const [redo, setRedo] = useState<Redo | null>(null)
  const retried = useRef(false)
  // Usherr's own engine: when a release breaks part-way, the next one in line plays from there.
  const autoNexts = useRef(0)
  // Usherr: the title's sound tracks and subtitles, and the subtitle showing.
  const [tracks, setTracks] = useState<Tracks | null>(null)
  const [sub, setSub] = useState<PlayTrack | null>(null)
  useEffect(() => {
    retried.current = false
    autoNexts.current = 0
    setRedo(null)
    setTracks(null)
    setSub(null)
  }, [req])
  const titled = !!req.id && (req.kind === 'movie' || req.kind === 'episode' || req.kind === 'video') && !req.direct && !req.stream && !req.live && !req.card
  // where the picture is, in the file's own time
  const fileTime = () => (videoRef.current?.currentTime ?? 0) + (plan?.converted ? (plan.shift ?? 0) : 0)
  const failOver = () => {
    if (req.stream?.engine !== 'usherr' || !req.stream.canNext || !req.onNext || autoNexts.current >= 2) return false
    autoNexts.current++
    setTvNote(t('This release stopped. Trying another…'))
    req.onNext(fileTime()).catch((e: Error) => {
      setTvNote(e.message)
      setFailed(true)
    })
    return true
  }
  const convertInstead = () => {
    if (retried.current || plan?.mode !== 'direct' || plan.converted || !titled) return false
    retried.current = true
    setRedo({ ...redo, at: fileTime() || (req.startAt ?? 0), force: true })
    return true
  }
  useEffect(() => {
    if (!titled || !plan || inline) return
    let gone = false
    fetch(`/api/play/tracks/${req.kind}/${req.id}`, { credentials: 'include' })
      .then((r) => (r.ok ? r.json() : null))
      .then((d: Tracks | null) => {
        if (gone || !d) return
        setTracks(d)
        // the subtitles this person reads, when they read any
        setSub((cur) => cur ?? pickSub(d, prefSubLang()))
      })
      .catch(() => undefined)
    return () => {
      gone = true
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [plan?.url, req])
  const subUrl = sub && sub.kind === 'text' && plan ? `/api/play/subs/${req.kind}/${req.id}?key=${encodeURIComponent(sub.key ?? '')}&shift=${plan.converted ? (plan.shift ?? 0) : 0}` : ''
  // Subtitles inside a big file arrive as it's read through: what's there so
  // far shows at once, then again every 20 seconds until all of it is.
  const [subSrc, setSubSrc] = useState('')
  useEffect(() => {
    setSubSrc('')
    if (!subUrl) return
    let gone = false
    let made = ''
    let tm = 0
    const load = () =>
      fetch(subUrl, { credentials: 'include' })
        .then(async (r) => {
          if (!r.ok || gone) return
          const text = await r.text()
          if (gone) return
          const url = URL.createObjectURL(new Blob([text], { type: 'text/vtt' }))
          if (made) URL.revokeObjectURL(made)
          made = url
          setSubSrc(url)
          if (r.headers.get('X-Subs-Complete') === '0') tm = window.setTimeout(load, 20000)
        })
        .catch(() => undefined)
    void load()
    return () => {
      gone = true
      window.clearTimeout(tm)
      if (made) URL.revokeObjectURL(made)
    }
  }, [subUrl])
  useEffect(() => {
    // the newest subtitle track shows, the others don't
    const v = videoRef.current
    if (!v) return
    const tm = window.setTimeout(() => {
      const list = v.textTracks
      for (let i = 0; i < list.length; i++) list[i].mode = subSrc && i === list.length - 1 ? 'showing' : 'disabled'
    }, 50)
    return () => window.clearTimeout(tm)
  }, [subSrc])
  useEffect(() => {
    const v = videoRef.current
    if (!v || plan?.mode !== 'direct' || plan.converted || !req.id || req.direct || req.stream || req.live || req.card) return
    // A browser playing a sound it can't decode stays silent: no audio bytes decoded.
    const tm = window.setInterval(() => {
      if (v.paused || v.currentTime < 3) return
      window.clearInterval(tm)
      const x = v as HTMLVideoElement & { webkitAudioDecodedByteCount?: number; mozHasAudio?: boolean }
      const silent = x.webkitAudioDecodedByteCount === 0 || x.mozHasAudio === false
      if (silent) convertInstead()
    }, 1000)
    return () => window.clearInterval(tm)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [plan, req])
  useEffect(() => {
    if (videoRef.current) videoRef.current.muted = muted
  }, [muted, plan])
  // Usherr: a stream's sound on its own, beside the picture, converted by
  // Usherr and kept in step with it: when the browser plays the stream
  // silently (Dolby, Atmos, TrueHD or DTS in an MKV), and when another
  // language than the file's first is picked (a browser only ever plays an
  // MKV's first sound track). The picture stays as it is, in 4K, and seeks as
  // usual.
  const [silent, setSilent] = useState(false)
  const [streamTrack, setStreamTrack] = useState<number | undefined>(undefined)
  const [sync, setSync] = useState(savedSync)
  const syncRef = useRef(sync)
  useEffect(() => {
    syncRef.current = sync
    try {
      localStorage.setItem(SYNC_KEY, String(sync))
    } catch {
      // kept for this visit only
    }
  }, [sync])
  const streamFirst = tracks?.audio[0]?.index
  const sideSound = !!req.stream && (silent || (streamTrack !== undefined && streamTrack !== streamFirst))
  // the picture's own sound, once it's been taken through Web Audio to turn it off
  const ownSound = useRef<{ ctx: AudioContext; node: MediaElementAudioSourceNode } | null>(null)
  useEffect(() => {
    // A stream's languages, and the one this person listens in when the
    // file's first is another (a MULTi release).
    const st = req.stream
    setStreamTrack(undefined)
    if (!st || inline || !plan || plan.url.includes('/browser')) return
    let gone = false
    fetch(`/api/stream/${st.id}/tracks`, { credentials: 'include' })
      .then((r) => (r.ok ? r.json() : null))
      .then((d: Tracks | null) => {
        if (gone || !d) return
        setTracks(d)
        const want = prefAudioLang() || locale() || 'en'
        const first = d.audio[0]
        if (first?.lang && !sameLang(first.lang, want)) {
          const mine = d.audio.find((x) => sameLang(x.lang, want))
          if (mine) setStreamTrack(mine.index)
        }
      })
      .catch(() => undefined)
    return () => {
      gone = true
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [plan?.url, req])
  useEffect(() => {
    setSilent(false)
    const v = videoRef.current
    if (!v || !req.stream || muted || plan?.mode !== 'direct' || plan.converted || plan.url.includes('/browser')) return
    let heard = 0
    const tm = window.setInterval(() => {
      if (v.paused || v.readyState < 3) return
      if (++heard < 3) return // three seconds of playing first
      window.clearInterval(tm)
      const x = v as HTMLVideoElement & { webkitAudioDecodedByteCount?: number; mozHasAudio?: boolean }
      if (x.webkitAudioDecodedByteCount === 0 || x.mozHasAudio === false) setSilent(true)
    }, 1000)
    return () => window.clearInterval(tm)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [plan?.url, req])
  useEffect(() => {
    const v = videoRef.current
    const st = req.stream
    if (!sideSound || !v || !st) return
    const a = new Audio()
    const lang = prefAudioLang() || locale() || 'en'
    const pick = streamTrack !== undefined ? `&track=${streamTrack}` : ''
    // The picture's own sound is turned off while this plays (it's heard
    // when it's another language than the one picked).
    let own = ownSound.current
    if (!own && !silent) {
      try {
        const ctx = new AudioContext()
        own = { ctx, node: ctx.createMediaElementSource(v) } // not connected: no sound
        ownSound.current = own
      } catch {
        own = null
      }
    }
    own?.node.disconnect()
    let base = 0 // where the sound started, in the video's time
    let holding = false // the picture waits while the sound starts
    let fails = 0
    let wait = 0
    let gone = false
    // What's heard now is the sound from the speakers' delay ago; it should
    // be the picture's moment, moved by the sync set in the player.
    const target = () => v.currentTime - syncRef.current / 1000
    const drift = () => base + a.currentTime - speakerDelay() - target()
    const release = () => {
      if (!holding) return
      holding = false
      void v.play().catch(() => undefined)
    }
    const start = () => {
      holding = !v.paused
      if (holding) v.pause()
      base = Math.max(0, target() + speakerDelay())
      a.src = `/api/stream/${st.id}/sound?at=${base.toFixed(2)}&lang=${encodeURIComponent(lang)}${pick}`
      a.playbackRate = v.playbackRate
      void a.play().catch(() => undefined)
      // The picture never waits more than a few seconds for its sound.
      window.clearTimeout(wait)
      wait = window.setTimeout(release, 8000)
    }
    const level = () => {
      a.volume = v.volume
      a.muted = v.muted
    }
    const onPlaying = () => {
      fails = 0
      release()
    }
    const onError = () => {
      if (gone) return
      release()
      // a blip: ask again from where the picture is, a few times
      if (++fails <= 3) window.setTimeout(() => !gone && start(), 2000)
    }
    const onPause = () => {
      if (!holding) a.pause()
    }
    const onPlay = () => {
      if (holding) return
      if (Math.abs(drift()) > 1) start()
      else void a.play().catch(() => undefined)
    }
    const onSeeking = () => a.pause()
    const onSeeked = () => start()
    const onWaiting = () => {
      if (!holding) a.pause()
    }
    const onVideoPlaying = () => {
      if (!holding && a.paused && a.src) void a.play().catch(() => undefined)
    }
    a.addEventListener('playing', onPlaying)
    a.addEventListener('error', onError)
    v.addEventListener('pause', onPause)
    v.addEventListener('play', onPlay)
    v.addEventListener('seeking', onSeeking)
    v.addEventListener('seeked', onSeeked)
    v.addEventListener('waiting', onWaiting)
    v.addEventListener('playing', onVideoPlaying)
    v.addEventListener('volumechange', level)
    level()
    start()
    // In step: small differences are taken up by playing the sound a little
    // faster or slower; a big one starts it again from where the picture is.
    const tm = window.setInterval(() => {
      if (holding || v.paused || a.paused) return
      const d = drift()
      if (Math.abs(d) > 1.5) start()
      else a.playbackRate = v.playbackRate * (Math.abs(d) < 0.03 ? 1 : 1 - Math.max(-0.05, Math.min(0.05, d * 0.5)))
    }, 1000)
    return () => {
      gone = true
      window.clearInterval(tm)
      window.clearTimeout(wait)
      a.removeEventListener('playing', onPlaying)
      a.removeEventListener('error', onError)
      v.removeEventListener('pause', onPause)
      v.removeEventListener('play', onPlay)
      v.removeEventListener('seeking', onSeeking)
      v.removeEventListener('seeked', onSeeked)
      v.removeEventListener('waiting', onWaiting)
      v.removeEventListener('playing', onVideoPlaying)
      v.removeEventListener('volumechange', level)
      a.pause()
      a.removeAttribute('src')
      a.load()
      release()
      if (own) {
        // the picture's own sound back
        own.node.connect(own.ctx.destination)
        void own.ctx.resume().catch(() => undefined)
      }
    }
  }, [sideSound, silent, streamTrack, req])

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
    prepare(req, caps as ReturnType<typeof browserCaps>, redo ?? undefined)
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
          if (req.startAt && !p.converted) {
            // a copy converted here already starts at the right moment
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
  }, [req, redo])

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
          <p className="hp-inline-msg" data-tv-error="">{error || t("This can't be previewed here. Open it to watch.")}</p>
        ) : (
          <video ref={videoRef} muted={muted} autoPlay playsInline onEnded={() => req.onEnded?.()} onError={() => plan?.mode === 'direct' && !convertInstead() && !failOver() && setFailed(true)} />
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
          {sideSound && (
            <div className="hp-sync" role="group" aria-label={t('Sound sync')}>
              <button type="button" className="btn-sm" onClick={() => setSync((s) => Math.max(-2000, s - 50))} aria-label={t('Sound earlier')} title={t('Sound earlier')}>
                −
              </button>
              <button type="button" className="btn-sm hp-sync-val" onClick={() => setSync(0)} title={t('Sound sync (tap to put it back)')}>
                {sync === 0 ? t('Sync') : t('{secs} s', { secs: `${sync > 0 ? '+' : '−'}${(Math.abs(sync) / 1000).toFixed(2)}` })}
              </button>
              <button type="button" className="btn-sm" onClick={() => setSync((s) => Math.min(2000, s + 50))} aria-label={t('Sound later')} title={t('Sound later')}>
                +
              </button>
            </div>
          )}
          {tracks && (
            <TracksMenu
              tracks={tracks}
              audio={req.stream ? (streamTrack ?? streamFirst) : plan?.audio}
              burn={redo?.burn}
              sub={sub}
              onAudio={(tr) => (req.stream ? setStreamTrack(tr.index) : setRedo({ ...redo, at: fileTime(), audio: tr.index }))}
              onSub={(tr) => {
                if (tr?.kind === 'image') {
                  setSub(null)
                  setRedo({ ...redo, at: fileTime(), audio: plan?.audio, burn: tr.index })
                  return
                }
                setSub(tr)
                if (redo?.burn !== undefined) setRedo({ ...redo, at: fileTime(), audio: plan?.audio, burn: undefined })
              }}
            />
          )}
          <button className="btn-sm" onClick={onClose} aria-label={t('Close the player')}>
            <Icon name="x" size={16} />
          </button>
        </div>
        {error ? (
          <p className="error-text hp-msg" data-tv-error="">{error}</p>
        ) : failed ? (
          <p className="error-text hp-msg" data-tv-error="">
            {req.stream
              ? req.onNext
                ? t("This release couldn't be played in the browser. Try another release or a lower quality, watch it in Jellyfin (it converts), or play it on the TV.")
                : t("This stream couldn't be played in the browser. Copy the home link below and open it in VLC, or play it on the TV.")
              : req.live
                ? t("This channel isn't sending anything right now. Free channels come and go: try again later, or another channel.")
                : t("This video couldn't be played here. The file itself is fine: it plays in Jellyfin, VLC or mpv.")}
          </p>
        ) : (
          <video ref={videoRef} controls autoPlay playsInline onEnded={() => req.onEnded?.()} onError={() => plan?.mode === 'direct' && !convertInstead() && !failOver() && setFailed(true)}>
            {subSrc && <track key={subSrc} kind="subtitles" src={subSrc} srcLang={sub?.lang} label={sub?.lang ?? t('Subtitles')} default />}
          </video>
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
                  req.onNext?.(fileTime()).catch((e: Error) => {
                    setTvNote(e.message)
                    setNexting(false)
                  })
                }}
                title={t('Plays the next release found instead of this one')}
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
