// Usherr: the Showtime page (/showtime). Left open in the browser on the TV,
// it counts down to the next film booked to play "on the Showtime screen"
// and starts it at showtime: the library's file, or a stream. Below, every
// booking, to move or cancel.
import { useCallback, useEffect, useRef, useState } from 'react'
import Icon from '../components/Icon'
import { useToast } from '../components/Toast'
import { useConfirm } from '../components/ConfirmProvider'
import { useAuth } from '../AuthContext'
import { isAdmin } from '../api'
import { useDocumentTitle } from '../documentTitle'
import { t } from '../i18n'
import { playStream, playTitle } from './Player'
import { cancelShowtime, clockLabel, inLabel, listShowtimes, moveShowtime, PLAY_ON_LABEL, showtimeEnds, whenLabel, type Showtime } from './showtimes'
import { rememberedQuality, streamApi } from './streamApi'
import './showtime.css'

// startFilm plays a showtime's film here: the library's file, or the best
// stream at this device's quality.
async function startFilm(s: Showtime) {
  if (s.libraryId) {
    playTitle('movie', s.libraryId, s.title)
    return
  }
  const status = await streamApi.status()
  if (!status.ready) throw new Error(t('{title} isn’t in your library and streaming isn’t set up.', { title: s.title }))
  const { options } = await streamApi.options({ kind: 'movie', tmdbId: s.tmdbId, quality: rememberedQuality(status.qualities ?? []) })
  if (!options.length) throw new Error(t('No stream found for {title}.', { title: s.title }))
  const st = await streamApi.start({ option: options[0].id, title: s.title })
  playStream(st, s.title, undefined, { about: { kind: 'movie', tmdbId: s.tmdbId } })
}

export default function ShowtimePage() {
  useDocumentTitle(t('Showtime'))
  const toast = useToast()
  const confirm = useConfirm()
  const admin = isAdmin(useAuth().user)
  const [list, setList] = useState<Showtime[] | null>(null)
  const [offset, setOffset] = useState(0) // the server's clock minus this one
  const [now, setNow] = useState(() => Date.now())
  const [ready, setReady] = useState(false) // pressed: allowed to start with sound
  const [started, setStarted] = useState<number | null>(null)
  const [error, setError] = useState('')
  const load = useCallback(() => {
    listShowtimes()
      .then((d) => {
        setList(d.showtimes)
        setOffset(d.nowMs - Date.now())
      })
      .catch((e: Error) => setError(e.message))
  }, [])
  useEffect(() => {
    load()
    const id = window.setInterval(load, 30_000)
    return () => window.clearInterval(id)
  }, [load])
  useEffect(() => {
    const id = window.setInterval(() => setNow(Date.now()), 1000)
    return () => window.clearInterval(id)
  }, [])
  const serverNow = now + offset

  // The next film to start here.
  const next = (list ?? []).find((s) => s.playOn === 'here' && s.state !== 'done' && showtimeEnds(s) > serverNow && s.id !== started)
  const due = next && serverNow >= next.atMs
  const tried = useRef(0)
  useEffect(() => {
    if (!next || !due || !ready || tried.current === next.id) return
    // Started a while ago (the page was closed): not on its own.
    if (serverNow - next.atMs > 5 * 60_000) return
    tried.current = next.id
    setStarted(next.id)
    startFilm(next).catch((e: Error) => {
      setError(e.message)
      setStarted(null)
    })
  }, [next, due, ready, serverNow])

  const move = async (s: Showtime, byMin: number) => {
    try {
      await moveShowtime(s.id, { atMs: s.atMs + byMin * 60_000, preshowMin: s.preshowMin, playOn: s.playOn })
      load()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    }
  }
  const cancel = async (s: Showtime) => {
    if (!(await confirm({ title: t('Cancel {title}?', { title: s.title }), body: t('The Marquee stops counting down to it.'), confirmLabel: t('Cancel it'), danger: true }))) return
    await cancelShowtime(s.id)
    load()
  }

  return (
    <div className="page showtime-page">
      <section className={`sht-screen${next ? '' : ' empty'}`}>
        {next ? (
          <>
            {next.posterUrl && <img className="sht-poster" src={next.posterUrl} alt="" />}
            <div className="sht-count">
              <span className="sht-label">{due ? t('Feature presentation') : t('Showtime {time}', { time: clockLabel(next.atMs) })}</span>
              <h1>{next.title}</h1>
              {!due && <p className="sht-in">{t('Starts {when}', { when: inLabel(next.atMs, serverNow) })}</p>}
              {next.preshowMin > 0 && !due && <p className="hint">{t('The pre-show starts on the Marquee {n} minutes before.', { n: next.preshowMin })}</p>}
              {!ready ? (
                <button className="primary btn-with-icon sht-ready" onClick={() => setReady(true)}>
                  <Icon name="play" size={18} /> {due ? t('Start the film') : t('Ready: start it on time')}
                </button>
              ) : (
                !due && <p className="sht-armed">{t('Ready. Leave this page open: the film starts by itself.')}</p>
              )}
              {due && ready && started !== next.id && (
                <button className="primary btn-with-icon sht-ready" onClick={() => void startFilm(next).catch((e: Error) => setError(e.message))}>
                  <Icon name="play" size={18} /> {t('Start the film')}
                </button>
              )}
              {error && <p className="error-text">{error}</p>}
            </div>
          </>
        ) : (
          <div className="sht-count">
            <Icon name="clock" size={40} />
            <h1>{t('No showtime here yet')}</h1>
            <p className="hint">{t('Book one with Showtime on a film’s page, choosing “On the Showtime screen”. Then leave this page open in the browser on the TV.')}</p>
          </div>
        )}
      </section>

      <section className="card sht-list">
        <h2>{t('Showtimes')}</h2>
        {list && list.length === 0 && <p className="hint">{t('Nothing booked.')}</p>}
        <ul>
          {(list ?? []).map((s) => (
            <li key={s.id} className={`sht-row st-${s.state}`}>
              {s.posterUrl ? <img src={s.posterUrl} alt="" /> : <span className="sht-row-ph" />}
              <div>
                <strong>{s.title}</strong>
                <span>
                  {whenLabel(s.atMs)} · {PLAY_ON_LABEL[s.playOn]()}
                  {s.preshowMin > 0 ? ` · ${t('{n} min pre-show', { n: s.preshowMin })}` : ''}
                  {s.state === 'playing' ? ` · ${t('playing')}` : s.state === 'preshow' ? ` · ${t('pre-show on')}` : s.state === 'done' ? ` · ${t('over')}` : ''}
                </span>
              </div>
              {(s.mine || admin) && s.state !== 'done' && (
                <span className="sht-row-btns">
                  <button className="btn-sm" onClick={() => void move(s, -15)} aria-label={t('15 minutes earlier')} title={t('15 minutes earlier')}>
                    −15
                  </button>
                  <button className="btn-sm" onClick={() => void move(s, 15)} aria-label={t('15 minutes later')} title={t('15 minutes later')}>
                    +15
                  </button>
                  <button className="icon-btn" onClick={() => void cancel(s)} aria-label={t('Cancel {title}', { title: s.title })}>
                    <Icon name="x" size={15} />
                  </button>
                </span>
              )}
            </li>
          ))}
        </ul>
      </section>
    </div>
  )
}
