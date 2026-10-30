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
import { playStream, playTitle, playWithPreshow } from './Player'
import { preshowApi } from './preshowApi' // Usherr
import { cancelShowtime, clockLabel, inLabel, listShowtimes, moveShowtime, PLAY_ON_LABEL, showtimeEnds, whenLabel, type Showtime } from './showtimes'
import { rememberedQuality, streamApi, type StreamStart } from './streamApi'
import { api, type ImportCandidate } from '../api'
import { openShowtimeBooking } from './ShowtimePill' // Usherr: book from here
import './showtime.css'

// findStream starts the best stream for a showtime's film at this device's
// quality, trying the next release when one won't start (an indexer that
// doesn't hand over its file, a release missing on the servers).
async function findStream(s: Showtime): Promise<StreamStart> {
  if (s.streamedFileId) return watchStreamed(s)
  const status = await streamApi.status()
  if (!status.ready) throw new Error(t('{title} isn’t in your library and streaming isn’t set up.', { title: s.title }))
  const { options } = await streamApi.options({ kind: 'movie', tmdbId: s.tmdbId, quality: rememberedQuality(status.qualities ?? []) })
  if (!options.length) throw new Error(t('No stream found for {title}.', { title: s.title }))
  let last: unknown = null
  for (const o of options.slice(0, 4)) {
    try {
      return await streamApi.start({ option: o.id, title: s.title })
    } catch (e) {
      last = e
    }
  }
  throw last instanceof Error ? last : new Error(t('No stream for {title} would start.', { title: s.title }))
}

// watchStreamed starts the film's saved release in the streamed library
// (Streams), or the next in line when it won't start.
async function watchStreamed(s: Showtime): Promise<StreamStart> {
  const status = await streamApi.status().catch(() => null)
  const quality = rememberedQuality(status?.qualities ?? [])
  let last: unknown = null
  for (let i = 0; i < 4; i++) {
    const res = await fetch(`/api/streamed/files/${s.streamedFileId}/watch`, { method: 'POST', credentials: 'include', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ quality, next: i > 0 }) })
    const data = await res.json().catch(() => null)
    if (res.ok) return data as StreamStart
    last = new Error(data?.error ?? t("Couldn't start it (error {status}).", { status: res.status }))
  }
  throw last
}

// startFilm plays a showtime's film here: the library's file, or a stream
// (found ahead, during the pre-show, when it could be).
async function startFilm(s: Showtime, ahead?: Promise<StreamStart> | null) {
  if (s.libraryId) {
    playTitle('movie', s.libraryId, s.title, { noPreshow: true }) // its pre-show has played
    return
  }
  const st = await (ahead ?? Promise.reject(new Error('none'))).catch(() => findStream(s))
  playStream(st, s.title, undefined, { about: { kind: 'movie', tmdbId: s.tmdbId } })
}

// BookFilm: book a film from the Showtime page itself: search, pick, then
// the usual booking window.
function BookFilm() {
  const [q, setQ] = useState('')
  const [found, setFound] = useState<ImportCandidate[]>([])
  useEffect(() => {
    if (q.trim().length < 2) return
    const id = window.setTimeout(() => {
      api
        .tmdbSearch('movie', q.trim())
        .then((r) => setFound(r.slice(0, 8)))
        .catch(() => setFound([]))
    }, 350)
    return () => window.clearTimeout(id)
  }, [q])
  return (
    <section className="card sht-book">
      <h2>{t('Book a film')}</h2>
      <input type="search" value={q} onChange={(e) => setQ(e.target.value)} placeholder={t('Search for a film')} aria-label={t('Search for a film')} title={t('Any film: from your library, Streams, or one to stream')} />
      {q.trim().length >= 2 && found.length > 0 && (
        <ul className="sht-found">
          {found.map((f) => (
            <li key={f.tmdbId}>
              <button type="button" onClick={() => openShowtimeBooking({ tmdbId: f.tmdbId, title: f.title, year: f.year, posterUrl: f.posterUrl })} title={t('Book a showtime')}>
                {f.posterUrl ? <img src={f.posterUrl} alt="" loading="lazy" /> : <span className="sht-noposter" />}
                <span>
                  {f.title}
                  {f.year ? ` (${f.year})` : ''}
                </span>
              </button>
            </li>
          ))}
        </ul>
      )}
    </section>
  )
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
  const [started, setStarted] = useState<number | null>(null) // the film is on
  const [starting, setStarting] = useState<number | null>(null) // ... being started
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
  // Usherr: the pre-show plays here first, timed to end at showtime. How
  // long it runs is asked once (without counting it as played).
  const [preSecs, setPreSecs] = useState<{ id: number; at: number; secs: number } | null>(null)
  const preTried = useRef('')
  useEffect(() => {
    if (!next || !ready || next.preshowMin <= 0 || due) return
    const key = `${next.id}-${next.atMs}`
    if (preSecs && `${preSecs.id}-${preSecs.at}` === key) return
    preshowApi
      .plan({ showtime: next.id, dry: true })
      .then((p) => setPreSecs({ id: next.id, at: next.atMs, secs: Math.min(p.seconds, next.preshowMin * 60) }))
      .catch(() => setPreSecs({ id: next.id, at: next.atMs, secs: 0 }))
  }, [next, ready, due, preSecs])
  const preStart = next && preSecs && preSecs.id === next.id && preSecs.at === next.atMs && preSecs.secs > 0 ? next.atMs - preSecs.secs * 1000 : null
  useEffect(() => {
    if (!next || !ready || preStart === null || due || serverNow < preStart) return
    const key = `${next.id}-${next.atMs}`
    if (preTried.current === key) return
    preTried.current = key
    void playWithPreshow({ showtime: next.id, budget: Math.max(30, Math.round((next.atMs - serverNow) / 1000)) }, () => undefined)
  }, [next, ready, preStart, due, serverNow])
  // A film that isn't in the library: its stream is found while the pre-show
  // runs (from five minutes before at the latest), so it's ready at showtime.
  const ahead = useRef<{ id: number; at: number; p: Promise<StreamStart> } | null>(null)
  useEffect(() => {
    if (!next || !ready || next.libraryId || serverNow < next.atMs - Math.max(next.preshowMin, 5) * 60_000) return
    if (ahead.current?.id === next.id && ahead.current.at === next.atMs) return
    const p = findStream(next)
    p.catch(() => undefined) // tried again at showtime
    ahead.current = { id: next.id, at: next.atMs, p }
  }, [next, ready, serverNow])
  const go = useCallback(
    (s: Showtime) => {
      setStarting(s.id)
      setError('')
      const a = ahead.current?.id === s.id && ahead.current.at === s.atMs ? ahead.current.p : null
      startFilm(s, a)
        .then(() => setStarted(s.id))
        .catch((e: Error) => {
          ahead.current = null
          setError(e.message)
        })
        .finally(() => setStarting(null))
    },
    [],
  )
  const tried = useRef(0)
  useEffect(() => {
    if (!next || !due || !ready || tried.current === next.id) return
    // Started a while ago (the page was closed): not on its own.
    if (serverNow - next.atMs > 5 * 60_000) return
    tried.current = next.id
    go(next)
  }, [next, due, ready, serverNow, go])

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
              {next.preshowMin > 0 && !due && (
                <p className="hint">
                  {ready && preStart
                    ? t('The pre-show starts here at {time}, and the Marquee follows it.', { time: clockLabel(preStart) })
                    : t('The pre-show starts on the Marquee {n} minutes before.', { n: next.preshowMin })}
                </p>
              )}
              {!ready ? (
                <button className="primary btn-with-icon sht-ready" onClick={() => setReady(true)}>
                  <Icon name="play" size={18} /> {due ? t('Start the film') : t('Ready: start it on time')}
                </button>
              ) : (
                !due && <p className="sht-armed">{t('Ready. Leave this page open: the film starts by itself.')}</p>
              )}
              {starting === next.id && <p className="sht-armed">{next.libraryId ? t('Starting the film…') : t('Starting the film: finding its stream…')}</p>}
              {due && ready && started !== next.id && starting !== next.id && (
                <button className="primary btn-with-icon sht-ready" onClick={() => go(next)}>
                  <Icon name="play" size={18} /> {error ? t('Try again') : t('Start the film')}
                </button>
              )}
              {error && <p className="error-text">{error}</p>}
            </div>
          </>
        ) : (
          <div className="sht-count">
            <Icon name="clock" size={40} />
            <h1>{t('No showtime here yet')}</h1>
            <p className="hint">{t('Book one below, or with Showtime on a film’s page or card, choosing “On the Showtime screen”. Then leave this page open in the browser on the TV.')}</p>
          </div>
        )}
      </section>

      <BookFilm />

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
