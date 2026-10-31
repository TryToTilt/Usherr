// Usherr: Showtime from any page. The top bar's Showtime button counts down
// to the next booking (by the server's clock): its time when it's more than
// an hour off, then "Pre-show starts 12:34", "Show starting 07:48", "Now
// showing". Whatever it says, it opens the Showtime screen. And a window to
// book a film from anywhere (a card's "Book a showtime"), kept by the app.
import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import Icon from '../components/Icon'
import type { CardAction } from '../components/PosterCard'
import { t } from '../i18n'
import { ShowtimeDialog, type ShowtimeFilm } from './ShowtimeButton'
import { clockLabel, listShowtimes, showtimeEnds, whenLabel, type Showtime } from './showtimes'

const HOUR = 60 * 60_000

// 1:02:05, or 07:48 under an hour
export function countdown(ms: number): string {
  const s = Math.max(0, Math.ceil(ms / 1000))
  const h = Math.floor(s / 3600)
  const m = Math.floor((s % 3600) / 60)
  const ss = String(s % 60).padStart(2, '0')
  return h ? `${h}:${String(m).padStart(2, '0')}:${ss}` : `${String(m).padStart(2, '0')}:${ss}`
}

// Where the next booking stands, and what to say: the words and the time
// (a phone shows the time alone).
type Stage = { stage: 'none' | 'booked' | 'soon' | 'preshow' | 'on'; word: string; time: string; label: string }
export function showtimeStage(next: Showtime | undefined, now: number): Stage {
  const say = (stage: Stage['stage'], word: string, time = ''): Stage => ({ stage, word, time, label: time ? `${word} ${time}` : word })
  if (!next) return say('none', t('Showtime'))
  const pre = next.atMs - Math.max(next.preshowMin, 0) * 60_000
  if (now >= next.atMs) return say('on', t('Now showing'))
  if (now >= pre) return say('preshow', t('Show starting'), countdown(next.atMs - now))
  if (pre - now > HOUR) {
    const today = new Date(next.atMs).toDateString() === new Date(now).toDateString()
    return say('booked', t('Showtime'), today ? clockLabel(next.atMs) : whenLabel(next.atMs))
  }
  if (next.preshowMin <= 0) return say('soon', t('Show starting'), countdown(next.atMs - now))
  return say('soon', t('Pre-show starts'), countdown(pre - now))
}

// useNextShowtime: the next booking not over yet, and the server's clock.
export function useNextShowtime(): { next: Showtime | undefined; now: number } {
  const [list, setList] = useState<Showtime[]>([])
  const [offset, setOffset] = useState(0)
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    const load = () =>
      listShowtimes()
        .then((d) => {
          setList(d.showtimes)
          setOffset(d.nowMs - Date.now())
        })
        .catch(() => undefined)
    load()
    const a = window.setInterval(load, 30_000)
    const b = window.setInterval(() => setNow(Date.now()), 1000)
    const onBook = () => load()
    window.addEventListener('usherr-showtimes', onBook)
    return () => {
      window.clearInterval(a)
      window.clearInterval(b)
      window.removeEventListener('usherr-showtimes', onBook)
    }
  }, [])
  const serverNow = now + offset
  const next = list.filter((s) => s.state !== 'done' && showtimeEnds(s) > serverNow).sort((x, y) => x.atMs - y.atMs)[0]
  return { next, now: serverNow }
}

export function ShowtimePill() {
  const nav = useNavigate()
  const { next, now } = useNextShowtime()
  const { stage, word, time, label } = showtimeStage(next, now)
  const tip = next ? `${next.title} · ${whenLabel(next.atMs)}. ${t('Opens the Showtime screen.')}` : t('The Showtime screen: book a film for a time, with a pre-show first.')
  return (
    <button type="button" data-walk="showtime-pill" className={`icon-btn top-showtime st-${stage}`} onClick={() => nav('/showtime')} title={tip} aria-label={`${label}. ${tip}`}>
      <Icon name={stage === 'on' ? 'play' : 'clock'} size={18} />
      <span className="top-showtime-label">
        <span className="top-showtime-word">{word}</span>
        {time && <span className="top-showtime-time"> {time}</span>}
      </span>
    </button>
  )
}

// ShowtimeStrip: the Dashboard's line for the next booking, counting down
// (nothing when none is booked).
export function ShowtimeStrip() {
  const nav = useNavigate()
  const { next, now } = useNextShowtime()
  if (!next) return null
  const { label } = showtimeStage(next, now)
  return (
    <button type="button" className="sht-strip" onClick={() => nav('/showtime')} title={t('Opens the Showtime screen.')}>
      {next.posterUrl && <img src={next.posterUrl} alt="" />}
      <span className="sht-strip-main">
        {whenLabel(next.atMs)} · {next.title}
      </span>
      <strong>{label}</strong>
      <Icon name="chevron-right" size={16} />
    </button>
  )
}

// ---- booking from anywhere ----

let openBooking: ((f: ShowtimeFilm | null) => void) | null = null

// bookShowtime opens the booking window for a film, from any page.
export function openShowtimeBooking(film: ShowtimeFilm) {
  openBooking?.(film)
}

// showtimeAction: a film card's "Book a showtime".
export function showtimeAction(film: ShowtimeFilm): CardAction {
  return { icon: 'clock', label: t('Book a showtime'), onClick: () => openShowtimeBooking(film) }
}

export function ShowtimeBookingHost() {
  const [film, setFilm] = useState<ShowtimeFilm | null>(null)
  useEffect(() => {
    openBooking = setFilm
    return () => {
      openBooking = null
    }
  }, [])
  if (!film) return null
  return (
    <ShowtimeDialog
      film={film}
      onClose={() => {
        setFilm(null)
        window.dispatchEvent(new Event('usherr-showtimes'))
      }}
    />
  )
}
