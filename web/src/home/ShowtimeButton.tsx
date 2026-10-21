// Usherr: "Showtime" on a film — book it for a time (Showtimes at home): when,
// how many minutes of pre-show the Marquee runs first, and where the film
// starts. "Right after" another booking makes a double feature.
import { useEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { Link } from 'react-router-dom'
import Icon from '../components/Icon'
import { useToast } from '../components/Toast'
import { t } from '../i18n'
import { useFocusTrap } from '../useFocusTrap'
import { bookShowtime, listShowtimes, PLAY_ON_LABEL, showtimeEnds, whenLabel, type PlayOn, type Showtime } from './showtimes'
import './showtime.css'

export interface ShowtimeFilm {
  tmdbId: number
  title: string
  year?: number
  posterUrl?: string
  runtime?: number
}

// The usual evening start: 8 PM today, or the next quarter hour from 30
// minutes from now when that's later.
function defaultStart(): Date {
  const d = new Date()
  d.setHours(20, 0, 0, 0)
  const soon = Date.now() + 30 * 60_000
  if (d.getTime() < soon) {
    const q = 15 * 60_000
    return new Date(Math.ceil(soon / q) * q)
  }
  return d
}

// The value of a datetime-local input, in local time.
function localInput(d: Date): string {
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}T${p(d.getHours())}:${p(d.getMinutes())}`
}

const INTERMISSION = 10 * 60_000 // between a double feature's films

export function ShowtimeDialog({ film, onClose }: { film: ShowtimeFilm; onClose: () => void }) {
  const toast = useToast()
  const box = useRef<HTMLDivElement>(null)
  useFocusTrap(box)
  const [at, setAt] = useState(localInput(defaultStart()))
  const [after, setAfter] = useState('') // a booking's id: start right after it
  const [preshow, setPreshow] = useState(10)
  const [playOn, setPlayOn] = useState<PlayOn>('here')
  const [others, setOthers] = useState<Showtime[]>([])
  const [tv, setTv] = useState(false)
  const [busy, setBusy] = useState(false)
  useEffect(() => {
    listShowtimes()
      .then((d) => {
        setOthers(d.showtimes.filter((s) => s.state !== 'done' && showtimeEnds(s) > d.nowMs))
        setTv(d.tv)
      })
      .catch(() => undefined)
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && onClose()
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose])
  const prev = others.find((s) => String(s.id) === after)
  const startMs = prev ? showtimeEnds(prev) + INTERMISSION : new Date(at).getTime()

  const book = async () => {
    setBusy(true)
    try {
      await bookShowtime({ ...film, atMs: startMs, preshowMin: preshow, playOn })
      toast.success(
        playOn === 'here'
          ? t('{title} is booked for {when}. Leave the Showtime page open on the TV.', { title: film.title, when: whenLabel(startMs) })
          : t('{title} is booked for {when}.', { title: film.title, when: whenLabel(startMs) }),
      )
      onClose()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  return createPortal(
    <div className="modal-backdrop" onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      <div ref={box} tabIndex={-1} className="modal showtime-dialog" role="dialog" aria-modal="true" aria-label={t('Book a showtime')}>
        <div className="sht-head">
          {film.posterUrl ? <img src={film.posterUrl} alt="" /> : <Icon name="film" size={28} />}
          <div>
            <h2>{t('Book a showtime')}</h2>
            <p>{film.year ? `${film.title} (${film.year})` : film.title}</p>
          </div>
          <button className="icon-btn" onClick={onClose} aria-label={t('Close')}>
            <Icon name="x" size={18} />
          </button>
        </div>
        <div className="grid-form">
          {others.length > 0 && (
            <label>
              {t('Starts')}
              <select value={after} onChange={(e) => setAfter(e.target.value)}>
                <option value="">{t('At a time')}</option>
                {others.map((s) => (
                  <option key={s.id} value={String(s.id)}>
                    {t('Right after {title} (a double feature)', { title: s.title })}
                  </option>
                ))}
              </select>
            </label>
          )}
          {prev ? (
            <p className="hint">{t('Starts {when}, ten minutes after {title} ends.', { when: whenLabel(startMs), title: prev.title })}</p>
          ) : (
            <label>
              {t('Date and time')}
              <input type="datetime-local" value={at} onChange={(e) => setAt(e.target.value)} />
            </label>
          )}
          <label>
            {t('Pre-show on the Marquee first')}
            <select value={preshow} onChange={(e) => setPreshow(Number(e.target.value))}>
              <option value={0}>{t('None')}</option>
              {[5, 10, 15, 20, 30].map((n) => (
                <option key={n} value={n}>
                  {t('{n} minutes', { n })}
                </option>
              ))}
            </select>
          </label>
          <label>
            {t('The film plays')}
            <select value={playOn} onChange={(e) => setPlayOn(e.target.value as PlayOn)}>
              <option value="here">{PLAY_ON_LABEL.here()}</option>
              {tv && <option value="tv">{PLAY_ON_LABEL.tv()}</option>}
              <option value="none">{PLAY_ON_LABEL.none()}</option>
            </select>
          </label>
          <p className="hint">
            {playOn === 'here'
              ? t('Open the Showtime page in the browser on the TV and leave it there: it counts down and starts the film by itself.')
              : playOn === 'tv'
                ? t('At showtime the film opens on the TV (Play on TV).')
                : t('The Marquee counts down and runs the pre-show; start the film in Jellyfin or another app when it says so.')}{' '}
            <Link to="/showtime" onClick={onClose}>
              {t('The Showtime page')}
            </Link>
          </p>
        </div>
        <div className="sht-btns">
          <button className="btn-sm" onClick={onClose}>
            {t('Cancel')}
          </button>
          <button className="primary btn-with-icon" disabled={busy || !Number.isFinite(startMs)} onClick={() => void book()}>
            <Icon name="calendar" size={16} /> {t('Book it')}
          </button>
        </div>
      </div>
    </div>,
    document.body,
  )
}

// ShowtimeButton is the "Showtime" button on a film's page.
export default function ShowtimeButton({ film }: { film: ShowtimeFilm }) {
  const [open, setOpen] = useState(false)
  return (
    <>
      <button className="btn-with-icon" onClick={() => setOpen(true)} title={t('Book it for a time: the Marquee counts down and runs the pre-show, then the film starts.')}>
        <Icon name="clock" size={16} /> {t('Showtime')}
      </button>
      {open && <ShowtimeDialog film={film} onClose={() => setOpen(false)} />}
    </>
  )
}
