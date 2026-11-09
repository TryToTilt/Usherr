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
import { cancelShowtime, clockLabel, hostAnswer, inLabel, joinShowtime, linkedSummary, listShowtimes, moveShowtime, PLAY_ON_LABEL, shareShowtime, showtimeEnds, whenLabel, type PlayOn, type Showtime } from './showtimes'
import { rememberedQuality, streamApi, type StreamStart } from './streamApi'
import { api, type ImportCandidate } from '../api'
import { openShowtimeBooking } from './ShowtimePill' // Usherr: book from here
import { PartyChat } from './PartyChat' // Usherr: Community, the watch party chat
import { JoinedSetup } from './JoinedSetup' // Usherr: a friend's party, not linked yet
import { InviteLinked, LinkedScreenings } from './LinkedScreenings' // Usherr: linked screenings
import { ScreeningRoom } from './ScreeningRoom' // Usherr: linked screenings, step 3: the screening room
import { useRoomLink, type RoomLink } from './roomLink'
import { HostPartyButtons, PartyMoments } from './Party' // Usherr: the party: lobby, intermission, verdict
import { Link } from 'react-router-dom'
import './showtime.css'
import { broadcastOn } from '../neu/look' // Usherr Broadcast
import { BroadcastLobby, ComingUp, CrewVotes, NightHero } from './ShowtimeBroadcast' // Usherr Broadcast: Movie night's own layout

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
// (found ahead, during the pre-show, when it could be). Usherr: a linked
// screening's film follows its room, from where it's got to.
async function startFilm(s: Showtime, ahead?: Promise<StreamStart> | null, room?: { sync: RoomLink; startAt: number }) {
  if (s.libraryId) {
    playTitle('movie', s.libraryId, s.title, { noPreshow: true, party: s.party || room ? s.id : undefined, sync: room?.sync, startAt: room?.startAt }) // its pre-show has played; Usherr: its party's chat (a linked screening's too)
    return
  }
  const st = await (ahead ?? Promise.reject(new Error('none'))).catch(() => findStream(s))
  playStream(st, s.title, undefined, { about: { kind: 'movie', tmdbId: s.tmdbId }, noPreshow: true, party: s.party || room ? s.id : undefined, sync: room?.sync, startAt: room?.startAt }) // its pre-show has played; Usherr: its party's chat (a linked screening's too)
}

// LinkedParty: Usherr. A linked screening's party (the lobby, the
// intermission, the verdict), where its room says the film has got to.
function LinkedParty({ s, noLobby }: { s: Showtime; noLobby?: boolean }) { // Usherr Broadcast: noLobby, the lobby is its own screen there
  const { link, room } = useRoomLink(s.id)
  const at = link?.at() ?? null
  const state = at?.state
  const preshow = state === 'preshow' && room ? room.preshowSeconds + (at?.position ?? 0) : null
  return <PartyMoments showtimeId={s.id} title={s.title} film={noLobby || state === 'film' || state === 'paused'} preshow={preshow} preshowTotal={room?.preshowSeconds} pausedAt={state === 'paused' ? at?.position : undefined} />
}

// BookFilm: book a film from the Showtime page itself: search, pick, then
// the usual booking window.
function BookFilm() {
  const [q, setQ] = useState('')
  const [found, setFound] = useState<ImportCandidate[]>([])
  // What the last search said: which words it was for, and its error if it
  // failed (no TMDB key yet, say), so an empty list is never left unexplained.
  const [said, setSaid] = useState<{ q: string; error: string } | null>(null)
  useEffect(() => {
    if (q.trim().length < 2) return
    const id = window.setTimeout(() => {
      const words = q.trim()
      api
        .tmdbSearch('movie', words)
        .then((r) => {
          setFound(r.slice(0, 8))
          setSaid({ q: words, error: '' })
        })
        .catch((e: unknown) => {
          setFound([])
          setSaid({ q: words, error: e instanceof Error && e.message ? e.message : t('The film search didn’t answer. Try again in a moment.') })
        })
    }, 350)
    return () => window.clearTimeout(id)
  }, [q])
  const answered = q.trim().length >= 2 && said?.q === q.trim()
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
      {answered && said?.error && <p className="notice notice-warn">{said.error}</p>}
      {answered && !said?.error && found.length === 0 && <p className="hint">{t('No films found for “{q}”. Try fewer or different words.', { q: q.trim() })}</p>}
    </section>
  )
}

// JoinParty: Usherr, Community. A watch party's code, from its card in
// Discord, books the same film for the same moment here.
function JoinParty({ onJoined }: { onJoined: () => void }) {
  const toast = useToast()
  const [code, setCode] = useState('')
  const [playOn, setPlayOn] = useState<PlayOn>('here')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const join = async () => {
    setBusy(true)
    setError('')
    try {
      const d = await joinShowtime(code, playOn)
      toast.success(d.already ? t('{title} is already booked here for {when}.', { title: d.title, when: whenLabel(d.atMs) }) : t('{title} is booked for {when}, with the watch party.', { title: d.title, when: whenLabel(d.atMs) }))
      setCode('')
      onJoined()
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }
  return (
    <section className="card sht-join" data-walk="showtime-join">
      <h2>{t('Join a watch party')}</h2>
      <p className="hint">{t('Got a showtime code from a watch party in Discord? Paste it here: the same film is booked for the same moment, with a pre-show of the same length (your own clips and trailers).')}</p>
      <form
        className="sht-join-row"
        onSubmit={(e) => {
          e.preventDefault()
          void join()
        }}
      >
        <input value={code} onChange={(e) => setCode(e.target.value)} placeholder="HXK4-7QPM-2RD9C" aria-label={t('Showtime code')} spellCheck={false} autoComplete="off" />
        <select value={playOn} onChange={(e) => setPlayOn(e.target.value as PlayOn)} aria-label={t('The film plays')}>
          <option value="here">{PLAY_ON_LABEL.here()}</option>
          <option value="tv">{PLAY_ON_LABEL.tv()}</option>
          <option value="none">{PLAY_ON_LABEL.none()}</option>
        </select>
        <button className="primary btn-sm" type="submit" disabled={busy || code.trim().length < 13}>
          {t('Join')}
        </button>
      </form>
      {error && <p className="notice notice-warn">{error}</p>}
    </section>
  )
}

export default function ShowtimePage() {
  useDocumentTitle(t('Showtime'))
  const toast = useToast()
  const confirm = useConfirm()
  const admin = isAdmin(useAuth().user)
  const [list, setList] = useState<Showtime[] | null>(null)
  const [communityOn, setCommunityOn] = useState(false) // Usherr: showtimes can be shared to Discord
  const [linkOn, setLinkOn] = useState(false) // Usherr: linked Usherrs can be invited
  const [picking, setPicking] = useState<number | null>(null) // Usherr: the booking whose invite picker is open
  const [offset, setOffset] = useState(0) // the server's clock minus this one
  const [now, setNow] = useState(() => Date.now())
  const [ready, setReady] = useState(false) // pressed: allowed to start with sound
  const [started, setStarted] = useState<number | null>(null) // the film is on
  const [starting, setStarting] = useState<number | null>(null) // ... being started
  const [error, setError] = useState('')
  // Usherr Broadcast: the look's own layout, its full-screen lobby, and the pre-show skipped here
  const broadcast = broadcastOn()
  const [lobby, setLobby] = useState(false)
  const [skipPre, setSkipPre] = useState<number | null>(null)
  const load = useCallback(() => {
    listShowtimes()
      .then((d) => {
        setList(d.showtimes)
        setCommunityOn(!!d.community)
        setLinkOn(!!d.linkOn)
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

  // The next film to start here. Usherr: a linked screening (shared with
  // linked Usherrs, or a linked Usherr's) has its room here, wherever it was
  // booked to play.
  const next = (list ?? []).find((s) => (s.playOn === 'here' || s.linked || s.hosted) && s.state !== 'done' && showtimeEnds(s) > serverNow && s.id !== started)
  const room = !!next && !!(next.linked || next.hosted)
  const due = next && serverNow >= next.atMs
  // Usherr: the pre-show plays here first, timed to end at showtime. How
  // long it runs is asked once (without counting it as played).
  const [preSecs, setPreSecs] = useState<{ id: number; at: number; secs: number } | null>(null)
  const preTried = useRef('')
  useEffect(() => {
    if (!next || !ready || room || next.preshowMin <= 0 || due) return
    const key = `${next.id}-${next.atMs}`
    if (preSecs && `${preSecs.id}-${preSecs.at}` === key) return
    preshowApi
      .plan({ showtime: next.id, dry: true })
      .then((p) => setPreSecs({ id: next.id, at: next.atMs, secs: Math.min(p.seconds, next.preshowMin * 60) }))
      .catch(() => setPreSecs({ id: next.id, at: next.atMs, secs: 0 }))
  }, [next, ready, room, due, preSecs])
  const preStart = next && preSecs && preSecs.id === next.id && preSecs.at === next.atMs && preSecs.secs > 0 ? next.atMs - preSecs.secs * 1000 : null
  useEffect(() => {
    if (!next || !ready || room || preStart === null || due || serverNow < preStart || skipPre === next.id) return // Usherr Broadcast: skipPre
    const key = `${next.id}-${next.atMs}`
    if (preTried.current === key) return
    preTried.current = key
    void playWithPreshow({ showtime: next.id, budget: Math.max(30, Math.round((next.atMs - serverNow) / 1000)) }, () => undefined)
  }, [next, ready, room, preStart, due, serverNow, skipPre])
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
  // Usherr: linked screenings. The room starts the film, from where it's got to.
  const startSynced = useCallback((s: Showtime, sync: RoomLink, startAt: number) => {
    const a = ahead.current?.id === s.id && ahead.current.at === s.atMs ? ahead.current.p : null
    return startFilm(s, a, { sync, startAt }).catch((e: unknown) => {
      ahead.current = null
      throw e
    })
  }, [])
  const tried = useRef(0)
  useEffect(() => {
    if (!next || !due || !ready || room || tried.current === next.id) return
    // Started a while ago (the page was closed): not on its own.
    if (serverNow - next.atMs > 5 * 60_000) return
    tried.current = next.id
    go(next)
  }, [next, due, ready, room, serverNow, go])

  const move = async (s: Showtime, byMin: number) => {
    try {
      await moveShowtime(s.id, { atMs: s.atMs + byMin * 60_000, preshowMin: s.preshowMin, playOn: s.playOn })
      load()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    }
  }
  const share = async (s: Showtime) => {
    try {
      await shareShowtime(s.id)
      toast.success(t('Shared to Discord as a watch party.'))
      load()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    }
  }
  const answerHost = async (s: Showtime, a: 'going' | 'not') => {
    try {
      await hostAnswer(s.id, a)
      load()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    }
  }
  const cancel = async (s: Showtime) => {
    if (!(await confirm({ title: t('Cancel {title}?', { title: s.title }), body: s.party?.joined ? t('The Marquee stops counting down to it. Your friend’s watch party goes on.') : s.party ? t('The Marquee stops counting down to it, and the watch party in Discord is called off.') : t('The Marquee stops counting down to it.'), confirmLabel: t('Cancel it'), danger: true }))) return
    await cancelShowtime(s.id)
    load()
  }

  // Usherr Broadcast: what Invite does for the next showtime: linked Usherrs, else Discord.
  const canRun = !!next && (next.mine || admin) && next.state !== 'done' && !next.hosted
  const invite =
    next && canRun && linkOn && !next.party?.joined
      ? { label: t('Invite'), run: () => setPicking(picking === next.id ? null : next.id), expanded: picking === next.id }
      : next && canRun && communityOn && !next.party && next.state === 'booked'
        ? { label: t('Invite'), run: () => void share(next) }
        : undefined
  const preshowOn = !!next && !room && preStart !== null && !due && serverNow >= preStart

  return (
    <div className={`page showtime-page${broadcast ? ' sht-broadcast' : ''}`}>
      {broadcast && (
        <>
          <NightHero
            next={next}
            serverNow={serverNow}
            due={!!due}
            ready={ready && !room}
            starting={!!next && starting === next.id}
            error={error}
            onLobby={() => {
              setReady(true) // opening the lobby here makes this the screen the film plays on
              setLobby(true)
            }}
            invite={invite}
            extra={next && picking === next.id && canRun ? <InviteLinked showtime={next} onChanged={load} /> : null}
          />
          <CrewVotes />
          <ComingUp
            list={(list ?? []).filter((s) => s.id !== next?.id && s.state !== 'done' && showtimeEnds(s) > serverNow)}
            onOpen={(s) => {
              const row = document.getElementById(`sht-row-${s.id}`)
              row?.scrollIntoView({ block: 'center' })
              row?.querySelector<HTMLElement>('button, a[href]')?.focus({ preventScroll: true })
            }}
          />
          {lobby && next && (
            <BroadcastLobby
              s={next}
              serverNow={serverNow}
              due={!!due}
              preshowOn={preshowOn}
              canStart={(next.mine || admin) && started !== next.id && starting !== next.id}
              onStart={() => {
                setReady(true)
                setLobby(false)
                go(next)
              }}
              onSkipPre={
                !room && next.preshowMin > 0 && !due && skipPre !== next.id && !preshowOn
                  ? () => {
                      setSkipPre(next.id)
                      toast.info(t('The pre-show won’t play here. The film still starts on time.'))
                    }
                  : undefined
              }
              room={room ? <ScreeningRoom key={next.id} showtime={next} joined={ready} onJoin={() => setReady(true)} start={startSynced} /> : undefined}
              onClose={() => setLobby(false)}
            />
          )}
        </>
      )}
      {!broadcast && (
      <section className={`sht-screen${next ? '' : ' empty'}`}>
        {next && room ? (
          <>
            {next.posterUrl && <img className="sht-poster" src={next.posterUrl} alt="" />}
            <ScreeningRoom key={next.id} showtime={next} joined={ready} onJoin={() => setReady(true)} start={startSynced} />
          </>
        ) : next ? (
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
              {due && (next.mine || admin) && (
                <p className="sht-row-btns">
                  <HostPartyButtons showtimeId={next.id} />
                </p>
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
      )}

      {next && room && <LinkedParty key={next.id} s={next} noLobby={broadcast} />}
      {next && !room && (
        <PartyMoments
          key={next.id}
          showtimeId={next.id}
          title={next.title}
          film={broadcast || !!due} // Usherr Broadcast: the lobby is its own screen there
          preshow={preStart !== null && !due && serverNow >= preStart ? (serverNow - preStart) / 1000 : null}
          preshowTotal={preStart !== null ? (next.atMs - preStart) / 1000 : undefined}
        />
      )}
      {next && ((next.party && !next.party.waiting) || room) && !broadcast && <PartyChat showtimeId={next.id} />}
      {(() => {
        // Usherr: the first friend's party still to set up (any of them, not only the next on screen)
        const j = (list ?? []).find((s) => s.party?.joined && (s.party.waiting || s.party.needLink) && s.state !== 'done')
        return j?.party ? <JoinedSetup party={j.party} title={j.title} admin={admin} /> : null
      })()}

      <LinkedScreenings />
      {linkOn && (
        <p className="hint">
          {t('Your crews: a chat, a shared watchlist and a vote on the next film, between parties.')} <Link to="/crews">{t('Open Crews')}</Link>
        </p>
      )}
      <BookFilm />
      <JoinParty onJoined={load} />

      <section className="card sht-list" data-walk="showtime-list">
        <h2>{t('Showtimes')}</h2>
        {list && list.length === 0 && <p className="hint">{t('Nothing booked.')}</p>}
        <ul>
          {(list ?? []).map((s) => (
            <li key={s.id} id={broadcast ? `sht-row-${s.id}` : undefined} className={`sht-row st-${s.state}`}>
              {s.posterUrl ? <img src={s.posterUrl} alt="" /> : <span className="sht-row-ph" />}
              <div>
                <strong>{s.title}</strong>
                <span>
                  {whenLabel(s.atMs)} · {PLAY_ON_LABEL[s.playOn]()}
                  {s.preshowMin > 0 ? ` · ${t('{n} min pre-show', { n: s.preshowMin })}` : ''}
                  {s.state === 'playing' ? ` · ${t('playing')}` : s.state === 'preshow' ? ` · ${t('pre-show on')}` : s.state === 'done' ? ` · ${t('over')}` : ''}
                </span>
                {s.party?.waiting && <span className="sht-party">{t('A friend’s watch party · its chat isn’t linked here yet')}</span>}
                {s.party && !s.party.waiting && (
                  <span className="sht-party">
                    {s.party.joined && s.party.host ? `${t('Hosted by {host}', { host: s.party.host })} · ` : ''}
                    {(s.party.going ?? []).length > 0
                      ? t('Watch party in Discord · going: {names}', { names: (s.party.going ?? []).join(', ') })
                      : t('Watch party in Discord · nobody’s said I’m in yet')}{' '}
                    · <Link to={`/showtime/chat/${s.id}`}>{t('Chat')}</Link>
                  </span>
                )}
                {s.linked && <span className="sht-party">{linkedSummary(s)}</span>}
                {s.linked && s.state !== 'done' && (
                  <span className="sht-host-rsvp" role="group" aria-label={t('Are you coming?')}>
                    {t('You:')}{' '}
                    <button className={`btn-sm${s.linked.mine === 'going' ? ' primary' : ''}`} aria-pressed={s.linked.mine === 'going'} onClick={() => void answerHost(s, 'going')}>
                      {t('I’m in')}
                    </button>{' '}
                    <button className={`btn-sm${s.linked.mine === 'not' ? ' primary' : ''}`} aria-pressed={s.linked.mine === 'not'} onClick={() => void answerHost(s, 'not')}>
                      {t('Can’t make it')}
                    </button>
                  </span>
                )}
                {s.hosted && (
                  <span className="sht-party">
                    {s.hosted.hostName ? t('Hosted by {name} ({usherr})', { name: s.hosted.hostName, usherr: s.hosted.host }) : t('Hosted by {host}', { host: s.hosted.host })} · {t('the host sets the time; answer under From linked Usherrs')}
                  </span>
                )}
              </div>
              {(s.mine || admin) && s.state !== 'done' && !s.hosted && (
                <span className="sht-row-btns">
                  {linkOn && !s.party?.joined && (
                    <button className="btn-sm" data-walk="showtime-invite" aria-expanded={picking === s.id} onClick={() => setPicking(picking === s.id ? null : s.id)} title={t('Invite linked Usherrs')}>
                      {t('Invite')}
                    </button>
                  )}
                  {communityOn && !s.party && s.state === 'booked' && (
                    <button className="btn-sm" data-walk="showtime-share-later" onClick={() => void share(s)} title={t('Share it to Discord as a watch party')}>
                      {t('Share')}
                    </button>
                  )}
                  <button className="btn-sm" onClick={() => void move(s, -15)} aria-label={t('15 minutes earlier')} title={t('15 minutes earlier')}>
                    {t('−15 min')}
                  </button>
                  <button className="btn-sm" onClick={() => void move(s, 15)} aria-label={t('15 minutes later')} title={t('15 minutes later')}>
                    {t('+15 min')}
                  </button>
                  <button className="icon-btn" onClick={() => void cancel(s)} aria-label={t('Cancel {title}', { title: s.title })}>
                    <Icon name="x" size={15} />
                  </button>
                </span>
              )}
              {picking === s.id && (s.mine || admin) && s.state !== 'done' && !(broadcast && s.id === next?.id) && <InviteLinked showtime={s} onChanged={load} />}
            </li>
          ))}
        </ul>
      </section>
    </div>
  )
}
