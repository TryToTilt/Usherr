// Usherr: on the crew screen, a showtime starting soon says so, and when its
// pre-show (or the film) starts everyone here is moved to Showtime, ready to
// watch. Once per showtime: coming back to the crew doesn't move you again.
import { useCallback, useEffect, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { t } from '../i18n'
import { useToast } from '../components/Toast'
import { handoffFor, type Handoff } from './handoffMath'
import { clockLabel, inLabel, listShowtimes, type Showtime } from './showtimes'

const SKIP_KEY = 'usherr.showtime.handoff'

function skipped(): Set<number> {
  try {
    return new Set((JSON.parse(sessionStorage.getItem(SKIP_KEY) ?? '[]') as number[]).filter((n) => typeof n === 'number'))
  } catch {
    return new Set()
  }
}
function skip(id: number) {
  try {
    sessionStorage.setItem(SKIP_KEY, JSON.stringify([...skipped(), id].slice(-20)))
  } catch {
    // private window: only not remembered
  }
}

/** The state Showtime reads: open the lobby, ready to start. */
export interface ShowtimeArrival {
  showtimeReady: number
}

export default function ShowtimeHandoff() {
  const nav = useNavigate()
  const toast = useToast()
  const [list, setList] = useState<Showtime[]>([])
  const [offset, setOffset] = useState(0)
  const [now, setNow] = useState(() => Date.now())
  const [, setSkips] = useState(0)
  const moved = useRef(0)
  const load = useCallback(() => {
    listShowtimes()
      .then((d) => {
        setList(d.showtimes)
        setOffset(d.nowMs - Date.now())
      })
      .catch(() => undefined)
  }, [])
  useEffect(() => {
    load()
    const poll = window.setInterval(load, 20_000)
    const tick = window.setInterval(() => setNow(Date.now()), 1000)
    return () => {
      window.clearInterval(poll)
      window.clearInterval(tick)
    }
  }, [load])
  const serverNow = now + offset
  const h: Handoff | null = handoffFor(list, serverNow, skipped())
  const go = useCallback(
    (s: Showtime) => {
      if (moved.current === s.id) return
      moved.current = s.id
      skip(s.id)
      toast.info(s.preshowMin > 0 && serverNow < s.atMs ? t('{title}: the pre-show is starting.', { title: s.title }) : t('{title} is starting.', { title: s.title }))
      const arrival: ShowtimeArrival = { showtimeReady: s.id }
      nav('/showtime', { state: arrival })
    },
    [nav, toast, serverNow],
  )
  useEffect(() => {
    if (h?.go) go(h.s)
  }, [h, go])
  if (!h || h.go) return null
  return (
    <div className="crw-panel crw-gold crw-handoff" role="status">
      <strong>{t('{title} at {time}', { title: h.s.title, time: clockLabel(h.s.atMs) })}</strong>
      <span>
        {h.s.preshowMin > 0
          ? t('The pre-show starts {when}. You’ll be taken there when it does.', { when: inLabel(h.goAt, serverNow) })
          : t('It starts {when}. You’ll be taken there when it does.', { when: inLabel(h.goAt, serverNow) })}
      </span>
      <span className="crw-handoff-tools">
        <button type="button" className="primary btn-sm" onClick={() => go(h.s)}>
          {t('Go now')}
        </button>
        <button
          type="button"
          className="btn-sm link-btn"
          onClick={() => {
            skip(h.s.id)
            setSkips((n) => n + 1)
          }}
        >
          {t('Stay here')}
        </button>
      </span>
    </div>
  )
}
