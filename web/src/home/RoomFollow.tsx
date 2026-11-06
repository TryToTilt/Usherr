// Usherr: linked screenings, step 3. The player in a linked screening: it
// follows where the film should be (screeningRoom.ts), catching up with a
// slight change of speed when it's a little off and a seek when it's far
// off; the host's own play, pause and seek go to everyone. Below the film,
// the room's bar: the host's controls, or "Following the host".
import { useEffect, useRef, useState, type RefObject } from 'react'
import Icon from '../components/Icon'
import { plural, t } from '../i18n'
import { useRoomLink, type RoomLink } from './roomLink'
import { correct, driftWords, SEEK_AHEAD, type RoomState } from './syncMath'

export type Follow = { state: RoomState; drift: number; tap: boolean }

// useRoomFollow keeps a video in step with a room. shift is how far into the
// file a converted copy starts; reconvert asks for a converted copy again
// from a position (it can't seek).
export function useRoomFollow(videoRef: RefObject<HTMLVideoElement | null>, link: RoomLink | undefined, ready: boolean, shift: number, converted: boolean, reconvert: (at: number) => void): Follow | null {
  const [view, setView] = useState<Follow | null>(null)
  const redo = useRef(reconvert)
  useEffect(() => {
    redo.current = reconvert
  })
  useEffect(() => {
    const v = videoRef.current
    if (!v || !link || !ready) return
    const release = link.retain()
    // What the follower does itself isn't the viewer's doing.
    const quiet = { play: 0, pause: 0, seek: 0 }
    let lastSeek = 0
    let lastRedo = 0
    let tap = false
    let shown = ''
    const show = (f: Follow) => {
      const key = `${f.state}|${Math.round(f.drift * 10)}|${f.tap}`
      if (key === shown) return
      shown = key
      setView(f)
    }
    const play = () => {
      quiet.play = Date.now() + 1500
      v.play().then(
        () => (tap = false),
        (e: unknown) => {
          if (e instanceof DOMException && e.name === 'NotAllowedError') tap = true
        },
      )
    }
    const pause = () => {
      if (v.paused) return
      quiet.pause = Date.now() + 1000
      v.pause()
    }
    const seekTo = (pos: number) => {
      lastSeek = Date.now()
      if (converted) {
        // A copy converted here is made again from there, at most every 20 s.
        if (Date.now() - lastRedo < 20_000) return
        lastRedo = Date.now()
        redo.current(Math.max(0, pos + 2))
        return
      }
      quiet.seek = Date.now() + 2000
      v.currentTime = Math.max(0, pos - shift)
    }
    const tick = () => {
      const want = link.at()
      if (!want || v.readyState < 1) return
      const here = v.currentTime + shift
      if (want.state !== 'film') {
        v.playbackRate = 1
        pause()
        if (want.state === 'paused' && Math.abs(want.position - here) > 0.5 && Date.now() - lastSeek > 1500) seekTo(want.position)
        show({ state: want.state, drift: 0, tap })
        return
      }
      const drift = want.position - here
      const c = correct(drift)
      if (c.seek) {
        v.playbackRate = 1
        if (Date.now() - lastSeek > 3000) seekTo(want.position + SEEK_AHEAD)
      } else v.playbackRate = c.rate
      if (v.paused && !v.ended) play()
      show({ state: 'film', drift, tap })
    }
    // The viewer's own pause, play and seek: the host's go to everyone; a
    // guest's pause asks the host, when the host lets anyone pause. Anything
    // else is put back by the next tick.
    const act = (state: 'film' | 'paused') => {
      const r = link.room
      if (r?.canControl) void link.control(state, v.currentTime + shift).catch(() => undefined)
      else if (state === 'paused' && r?.canPause) void link.pause().catch(() => undefined)
    }
    const onPause = () => {
      if (Date.now() < quiet.pause || v.ended) return
      act('paused')
    }
    const onPlay = () => {
      if (Date.now() < quiet.play) return
      act('film')
    }
    const onSeeked = () => {
      if (Date.now() < quiet.seek || converted) return
      if (link.room?.canControl) act(v.paused ? 'paused' : 'film')
    }
    v.addEventListener('pause', onPause)
    v.addEventListener('play', onPlay)
    v.addEventListener('seeked', onSeeked)
    const id = window.setInterval(tick, 250)
    tick()
    return () => {
      window.clearInterval(id)
      v.removeEventListener('pause', onPause)
      v.removeEventListener('play', onPlay)
      v.removeEventListener('seeked', onSeeked)
      v.playbackRate = 1
      release()
    }
  }, [link, ready, shift, converted, videoRef])
  return link ? view : null
}

const errText = (e: unknown) => (e instanceof Error ? e.message : String(e))

// "Hosted by Ryan (Robin's Usherr)": who runs the film.
function hostLabel(host: string, hostName?: string): string {
  return hostName ? t('{name} ({usherr})', { name: hostName, usherr: host }) : host
}

// Over the film, when it's paused for everyone.
export function RoomPaused({ link, follow }: { link: RoomLink; follow: Follow | null }) {
  const { room } = useRoomLink(link.id)
  if (!room || !follow) return null
  if (follow.state === 'paused') return <div className="hp-room-paused" role="status">{room.canControl ? t('Paused for everyone') : t('Host paused')}</div>
  if (follow.state === 'ended') return <div className="hp-room-paused" role="status">{t('That’s the end of the screening. Thanks for coming!')}</div>
  if (follow.tap)
    return (
      <button type="button" className="hp-room-paused primary" onClick={() => void link.load()}>
        <Icon name="play" size={18} /> {t('Tap to join in')}
      </button>
    )
  return null
}

// RoomBar: under the film. The host runs it for everyone; everyone else
// follows, with the drift shown quietly.
export function RoomBar({ link, follow, position }: { link: RoomLink; follow: Follow | null; position: () => number }) {
  const { room } = useRoomLink(link.id)
  const [error, setError] = useState('')
  const [asked, setAsked] = useState(false)
  if (!room) return null
  const state = follow?.state ?? link.at()?.state
  const run = (p: Promise<unknown>) => {
    setError('')
    p.catch((e: unknown) => setError(errText(e)))
  }
  const relayOut = room.relay !== 'connected' && (room.role === 'guest' || room.following > 0)
  if (room.canControl) {
    const playing = state === 'film'
    const go = (by: number) => run(link.control(playing ? 'film' : 'paused', Math.max(0, position() + by)))
    return (
      <div className="hp-actions hp-room" role="group" aria-label={t('The screening')}>
        <span className="hp-room-note">{room.following > 0 ? plural(room.following, 'You’re running the film for everyone · {n} linked Usherr follows', 'You’re running the film for everyone · {n} linked Usherrs follow') : t('You’re running the film')}</span>
        <button type="button" className="btn-sm" onClick={() => go(-10)} aria-label={t('Back 10 seconds, for everyone')} title={t('Back 10 seconds, for everyone')}>
          {t('−10 s')}
        </button>
        <button type="button" className="btn-sm primary btn-with-icon" onClick={() => run(link.control(playing ? 'paused' : 'film', position()))}>
          <Icon name={playing ? 'pause' : 'play'} size={14} /> {playing ? t('Pause for everyone') : t('Play for everyone')}
        </button>
        <button type="button" className="btn-sm" onClick={() => go(10)} aria-label={t('Forward 10 seconds, for everyone')} title={t('Forward 10 seconds, for everyone')}>
          {t('+10 s')}
        </button>
        <label className="cx-check hp-room-opt" title={t('Guests can ask to pause, and your Usherr pauses for everyone. Only you can play again.')}>
          <input type="checkbox" checked={room.anyonePause} onChange={(e) => run(link.setAnyonePause(e.target.checked))} /> {t('Anyone can pause')}
        </label>
        {relayOut && <span className="hp-room-warn">{t('The relay is out of reach: the film plays on here, and everyone is back in step when it returns.')}</span>}
        {error && <span className="hp-room-warn">{error}</span>}
      </div>
    )
  }
  const d = driftWords(follow?.drift ?? 0)
  const drift = follow?.state !== 'film' ? '' : d.key === 'in' ? t('in step') : d.key === 'behind' ? t('{secs} s behind, catching up', { secs: d.secs }) : t('{secs} s ahead, easing back', { secs: d.secs })
  return (
    <div className="hp-actions hp-room" role="group" aria-label={t('The screening')}>
      <span className="hp-room-note">
        {t('Following the host, {host}', { host: hostLabel(room.host, room.hostName) })}
        {drift && <span className="hp-room-drift"> · {drift}</span>}
      </span>
      {room.canPause && state === 'film' && (
        <button
          type="button"
          className="btn-sm btn-with-icon"
          disabled={asked}
          onClick={() => {
            setAsked(true)
            window.setTimeout(() => setAsked(false), 5000)
            run(link.pause())
          }}
        >
          <Icon name="pause" size={14} /> {room.role === 'guest' ? (asked ? t('Asked') : t('Ask to pause')) : t('Pause for everyone')}
        </button>
      )}
      {relayOut && <span className="hp-room-warn">{t('The link to the host is out for now: the film plays on here, and catches up when it’s back.')}</span>}
      {error && <span className="hp-room-warn">{error}</span>}
    </div>
  )
}
