// Usherr: linked screenings, step 3. The screening room on the Showtime
// page: a linked screening's countdown, then "Join the screening" when it's
// time. Joined, the pre-show and the film play here in step with everyone
// (the host runs them; the player follows, see RoomFollow.tsx). Joining
// late comes in where it's got to.
import { useCallback, useEffect, useRef, useState } from 'react'
import Icon from '../components/Icon'
import { plural, t } from '../i18n'
import { endPreshow, playPreshowTimeline } from './Player'
import { preshowApi } from './preshowApi'
import { useRoomLink, type RoomLink } from './roomLink'
import { HostPartyButtons } from './Party' // Usherr: the party: intermission, roll credits
import { clockLabel, inLabel, type Showtime } from './showtimes'

const errText = (e: unknown) => (e instanceof Error ? e.message : String(e))

export function ScreeningRoom({ showtime: s, joined, onJoin, start }: { showtime: Showtime; joined: boolean; onJoin: () => void; start: (s: Showtime, sync: RoomLink, startAt: number) => Promise<void> }) {
  const { link, room } = useRoomLink(s.id)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const ran = useRef({ preshow: '', film: '' })
  const key = `${s.id}-${s.atMs}`
  const at = link?.at() ?? null
  const now = link?.serverNow() ?? Date.now()

  const goFilm = useCallback(() => {
    if (!link || ran.current.film === key) return
    ran.current.film = key
    endPreshow()
    setBusy(true)
    setError('')
    start(s, link, Math.max(0, link.at()?.position ?? 0))
      .catch((e: unknown) => {
        ran.current.film = ''
        setError(errText(e))
      })
      .finally(() => setBusy(false))
  }, [link, key, s, start])

  const goPreshow = useCallback(() => {
    if (!link || ran.current.preshow === key) return
    ran.current.preshow = key
    preshowApi
      .plan({ showtime: s.id })
      .then((plan) => {
        // Seconds into the pre-show, by the room's clock: the same moment at every Usherr.
        const clock = () => {
          const a = link.at()
          if (!a) return null
          if (a.state === 'waiting') return 0
          return a.state === 'preshow' ? plan.seconds + a.position : plan.seconds
        }
        const lead = link.room?.canControl ? () => void link.control('film', 0).catch((e: unknown) => setError(errText(e))) : undefined
        playPreshowTimeline(plan, clock, goFilm, lead)
      })
      .catch((e: unknown) => {
        ran.current.preshow = ''
        setError(errText(e))
      })
  }, [link, key, s.id, goFilm])

  // Joined: the pre-show when it's on, then the film.
  const state = at?.state
  useEffect(() => {
    if (!joined || !state) return
    if (state === 'preshow') goPreshow()
    else if (state === 'film' || state === 'paused') goFilm()
  }, [joined, state, goPreshow, goFilm])

  if (!link || !room) {
    return (
      <div className="sht-count">
        <span className="sht-label">{t('Linked screening')}</span>
        <h1>{s.title}</h1>
        {link?.error ? <p className="error-text">{link.error}</p> : <p className="hint">{t('Getting the screening room ready…')}</p>}
      </div>
    )
  }
  const host = room.hostName ? t('{name} ({usherr})', { name: room.hostName, usherr: room.host }) : room.host
  const open = now >= room.openMs
  const preStart = room.atMs - room.preshowSeconds * 1000
  const rejoin = () => {
    if (state === 'preshow') {
      ran.current.preshow = ''
      goPreshow()
    } else {
      ran.current.film = ''
      goFilm()
    }
  }
  return (
    <div className="sht-count">
      <span className="sht-label">{state === 'film' || state === 'paused' ? t('Feature presentation') : t('Linked screening · Showtime {time}', { time: clockLabel(room.atMs) })}</span>
      <h1>{s.title}</h1>
      <p className="sht-party">
        {room.role === 'guest'
          ? t('Hosted by {host}', { host })
          : room.canControl
            ? room.following > 0
              ? plural(room.following, 'You’re hosting · {n} linked Usherr invited', 'You’re hosting · {n} linked Usherrs invited')
              : t('You’re hosting')
            : t('Hosted here by {name}', { name: room.hostName || room.host })}
      </p>
      {(state === 'waiting' || state === 'preshow') && <p className="sht-in">{t('The film starts {when}', { when: inLabel(room.atMs, now) })}</p>}
      {!open && <p className="hint">{t('You can join the screening from {time}, a little before the pre-show.', { time: clockLabel(room.openMs) })}</p>}
      {open && !joined && state !== 'ended' && (
        <button className="primary btn-with-icon sht-ready" onClick={onJoin}>
          <Icon name="play" size={18} /> {t('Join the screening')}
        </button>
      )}
      {joined && state === 'waiting' && (
        <p className="sht-armed">
          {room.preshowSeconds > 0
            ? t('You’re in. Leave this page open: the pre-show starts here at {time}, in step with everyone.', { time: clockLabel(preStart) })
            : t('You’re in. Leave this page open: the film starts here at {time}, in step with everyone.', { time: clockLabel(room.atMs) })}
        </p>
      )}
      {joined && (state === 'preshow' || state === 'film' || state === 'paused') && (
        <button className="btn-with-icon sht-ready" onClick={rejoin} disabled={busy}>
          <Icon name="play" size={18} /> {t('Back to the screening')}
        </button>
      )}
      {state === 'paused' && <p className="sht-armed">{room.canControl ? t('Paused for everyone') : t('Host paused')}</p>}
      {room.canControl && (state === 'film' || state === 'paused') && (
        <p className="sht-row-btns">
          <HostPartyButtons showtimeId={s.id} />
        </p>
      )}
      {state === 'ended' && <p className="hint">{t('That’s the end of the screening. Thanks for coming!')}</p>}
      {busy && <p className="sht-armed">{t('Starting the film…')}</p>}
      {room.canControl && state !== 'ended' && (
        <label className="cx-check" title={t('Guests can ask to pause, and your Usherr pauses for everyone. Only you can play again.')}>
          <input type="checkbox" checked={room.anyonePause} onChange={(e) => void link.setAnyonePause(e.target.checked).catch((err: unknown) => setError(errText(err)))} /> {t('Anyone can pause')}
        </label>
      )}
      {room.relay !== 'connected' && (room.role === 'guest' || room.following > 0) && <p className="notice notice-warn">{t('Can’t reach the relay right now. The screening plays on here, and gets back in step when it’s back.')}</p>}
      {error && <p className="error-text">{error}</p>}
    </div>
  )
}
