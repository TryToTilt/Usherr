// Usherr Broadcast: Movie night in the Broadcast look (ShowtimePage.tsx
// shows these only then; Classic and Neu keep the page as it was). Tonight's
// showtime as a hero (Open the lobby has the focus, then Invite and Book
// another), the crew's vote as cards you OK to vote for, the showtimes coming
// up, and the lobby: a full screen with the seats (arrows and OK), the chat
// with quick replies, the reactions, and Start, Skip the pre-show, Use my
// phone as the remote and Leave.
import { PhoneHandoff } from '../components/PhoneHandoff' // Usherr Broadcast
import { useEffect, useRef, useState, type ReactNode } from 'react'
import { Link } from 'react-router-dom'
import Icon from '../components/Icon'
import { useToast } from '../components/Toast'
import { plural, t } from '../i18n'
import { crewsApi, crewTitle, posterOf, type Crew, type CrewVote } from './crewsApi'
import { clockTo, useParty, type PartySeat } from './partyLink'
import { usePartyChat } from './PartyChat'
import { subscribeParty } from './partyStream'
import { sendReaction } from './Reactions'
import { isWord, WORDS, type Word } from './reactionMath'
import { clockLabel, inLabel, PLAY_ON_LABEL, whenLabel, type Showtime } from './showtimes'
import './showtimeBroadcast.css'

const errText = (e: unknown) => (e instanceof Error ? e.message : String(e))
// "A3": a seat's row and number, as the seat map and the stub count them (eight a row).
const seatShort = (seat: number) => `${String.fromCharCode(65 + (Math.floor(seat / 8) % 26))}${(seat % 8) + 1}`

/** Moves the focus to the "Book a film" search on the page below. */
export function focusBooking() {
  const input = document.querySelector<HTMLInputElement>('.showtime-page .sht-book input')
  input?.scrollIntoView({ block: 'center' })
  input?.focus({ preventScroll: true })
}

// ---- the hero: tonight's showtime ----

export function NightHero({
  next,
  serverNow,
  due,
  ready,
  starting,
  error,
  onLobby,
  invite,
  extra,
}: {
  next: Showtime | undefined
  serverNow: number
  due: boolean
  ready: boolean
  starting: boolean
  error: string
  onLobby: () => void
  invite?: { label: string; run: () => void; expanded?: boolean }
  extra?: ReactNode
}) {
  if (!next) {
    return (
      <section className="bnt-hero empty">
        <div className="bnt-hero-text">
          <span className="bnt-where">{t('Movie night')}</span>
          <h1>{t('No showtime here yet')}</h1>
          <p>{t('Book a film for a time: the pre-show and the film start here on their own.')}</p>
          <div className="bnt-btns">
            <button type="button" className="bnt-btn pri" data-autofocus onClick={focusBooking}>
              <Icon name="plus" size={20} /> {t('Book a film')}
            </button>
          </div>
        </div>
      </section>
    )
  }
  const going = next.party?.going ?? []
  const facts = [
    due ? t('Starting now') : t('Starts {when}', { when: inLabel(next.atMs, serverNow) }),
    next.preshowMin > 0 ? t('pre-show at {time}', { time: clockLabel(next.atMs - next.preshowMin * 60_000) }) : '',
    going.length ? t('{names} going', { names: going.join(', ') }) : '',
    next.linked ? plural(next.linked.people, '{n} person coming', '{n} people coming') : '',
  ].filter(Boolean)
  return (
    <section className="bnt-hero" style={next.posterUrl ? { ['--bnt-pic' as string]: `url("${next.posterUrl}")` } : undefined}>
      <div className="bnt-hero-text">
        <span className="bnt-where">
          {t('Showtime')} · {whenLabel(next.atMs)} · {next.hosted ? t('Hosted by {host}', { host: next.hosted.hostName || next.hosted.host }) : PLAY_ON_LABEL[next.playOn]()}
        </span>
        <h1>{next.title}</h1>
        <p>{facts.join(' · ')}</p>
        <div className="bnt-btns">
          <button type="button" className="bnt-btn pri" data-autofocus data-walk="night-lobby" onClick={onLobby}>{/* Usherr Broadcast: the data-walk labels here are for Show me how */}
            <Icon name="play" size={20} /> {t('Open the lobby')}
          </button>
          {invite && (
            <button type="button" className="bnt-btn" aria-expanded={invite.expanded} onClick={invite.run}>
              {invite.label}
            </button>
          )}
          <button type="button" className="bnt-btn" onClick={focusBooking}>
            {t('Book another')}
          </button>
        </div>
        {ready && !due && !starting && <p className="bnt-armed">{t('Ready. Leave this page open: the film starts here by itself.')}</p>}
        {starting && <p className="bnt-armed">{next.libraryId ? t('Starting the film…') : t('Starting the film: finding its stream…')}</p>}
        {error && <p className="error-text">{error}</p>}
        {extra}
      </div>
    </section>
  )
}

// ---- the crew's vote ----

type OpenVote = { crew: Crew; v: CrewVote }

export function CrewVotes() {
  const toast = useToast()
  const [votes, setVotes] = useState<OpenVote[] | null>(null)
  const [busy, setBusy] = useState('')
  const load = () =>
    crewsApi
      .list()
      .then((l) =>
        l.on
          ? Promise.all(
              l.crews
                .filter((c) => c.joined)
                .slice(0, 4)
                .map((c) =>
                  crewsApi
                    .space(c.id)
                    .then((sp) => (sp.votes ?? []).filter((v) => v.state === 'open').map((v) => ({ crew: sp.crew, v })))
                    .catch(() => [] as OpenVote[]),
                ),
            )
          : [],
      )
      .then((x) => setVotes(x.flat().sort((a, b) => a.v.closes - b.v.closes)))
      .catch(() => setVotes([]))
  useEffect(() => {
    void load()
    const tm = window.setInterval(() => void load(), 60_000)
    return () => window.clearInterval(tm)
  }, [])
  if (!votes || votes.length === 0) return null
  const vote = (o: OpenVote, tmdb: number) => {
    const undo = o.v.myVote === tmdb
    setBusy(`${o.v.vid}-${tmdb}`)
    crewsApi
      .vote(o.crew.id, o.v.vid, undo ? 0 : tmdb)
      .then(() => {
        toast.success(undo ? t('Your vote is taken back.') : t('Your vote is in.'))
        return load()
      })
      .catch((e: unknown) => toast.error(errText(e)))
      .finally(() => setBusy(''))
  }
  return (
    <>
      {votes.slice(0, 2).map((o) => {
        const total = o.v.choices.reduce((n, c) => n + c.voters.length, 0)
        return (
          <section key={`${o.crew.id}-${o.v.vid}`} className="bnt-sec">
            <h2>
              {t('{crew} is picking the film for {when}', { crew: o.crew.name, when: whenLabel(o.v.at) })}
              <span className="bnt-note">{t('Voting closes {when} · OK to vote', { when: whenLabel(o.v.closes) })}</span>
            </h2>
            <div className="bnt-cards">
              {o.v.choices.map((c) => {
                const n = c.voters.length
                const mine = o.v.myVote === c.tmdb
                return (
                  <button key={c.tmdb} type="button" className="bnt-card bnt-vote" aria-pressed={mine} disabled={busy !== ''} onClick={() => vote(o, c.tmdb)} title={c.voters.map((s) => s.who).join(', ')}>
                    {c.poster ? <img src={posterOf(c.poster)} alt="" loading="lazy" /> : <span className="bnt-noposter" />}
                    <span className="bnt-vote-text">
                      <strong>{crewTitle(c)}</strong>
                      <span className="bnt-track">
                        <span style={{ width: `${total ? (n / total) * 100 : 0}%` }} />
                      </span>
                      <span className="bnt-dim">
                        {plural(n, '{n} vote', '{n} votes')}
                        {mine ? ` · ${t('yours')}` : ''}
                      </span>
                    </span>
                  </button>
                )
              })}
            </div>
            <Link className="bnt-more" to={`/crews/${o.crew.id}`}>
              {t('Open {crew}', { crew: o.crew.name })}
            </Link>
          </section>
        )
      })}
    </>
  )
}

// ---- coming up ----

export function ComingUp({ list, onOpen }: { list: Showtime[]; onOpen: (s: Showtime) => void }) {
  if (list.length === 0) return null
  return (
    <section className="bnt-sec">
      <h2>{t('Coming up')}</h2>
      <div className="bnt-cards">
        {list.slice(0, 3).map((s) => (
          <button key={s.id} type="button" className="bnt-card bnt-coming" onClick={() => onOpen(s)}>
            <span className="bnt-when">{whenLabel(s.atMs)}</span>
            <strong>{s.title}</strong>
          </button>
        ))}
      </div>
    </section>
  )
}

// ---- the lobby ----

// The quick replies: posted to the chat as they are.
const REPLIES = () => [t('On my way'), t('Got the snacks'), t('Ready when you are'), t('Can we wait a minute?'), t('This is going to be good')]

// Usherr Broadcast: what's typed goes to the phone (PhoneHandoff: a QR code
// and a one-time link that opens this page there, signed in).
function PhoneHint({ what, path, onClose }: { what: string; path: string; onClose: () => void }) {
  return <PhoneHandoff path={path} what={what} onClose={onClose} />
}

export function BroadcastLobby({
  s,
  serverNow,
  due,
  preshowOn,
  canStart,
  onStart,
  onSkipPre,
  room,
  onClose,
}: {
  s: Showtime
  serverNow: number
  due: boolean
  preshowOn: boolean
  canStart: boolean
  onStart?: () => void
  onSkipPre?: () => void
  room?: ReactNode
  onClose: () => void
}) {
  const toast = useToast()
  const box = useRef<HTMLDivElement>(null)
  const { link, party } = useParty(s.id)
  const { a, msgs, say } = usePartyChat(s.id)
  const [burst, setBurst] = useState<{ word: Word; key: number } | null>(null)
  const [phone, setPhone] = useState<'' | 'chat' | 'remote'>('')
  const [text, setText] = useState('')
  const [busy, setBusy] = useState(false)
  const linkedRoom = !!(s.linked || s.hosted)

  // the focus starts in the lobby: your seat, else a free one, else Start
  useEffect(() => {
    const el = box.current?.querySelector<HTMLElement>('[data-autofocus]') ?? box.current?.querySelector<HTMLElement>('button')
    el?.focus({ preventScroll: true })
  }, [party !== null]) // eslint-disable-line react-hooks/exhaustive-deps

  // everyone's reactions float up here too (a linked screening's come over the link)
  const tm = useRef(0)
  const show = (word: Word) => {
    window.clearTimeout(tm.current)
    setBurst({ word, key: Date.now() })
    tm.current = window.setTimeout(() => setBurst(null), 1400)
  }
  useEffect(() => {
    const off = subscribeParty(s.id, (ev) => {
      if (ev.kind !== 'react') return
      const w = (ev.message as { word?: string } | null)?.word ?? ''
      if (isWord(w)) show(w)
    })
    return () => {
      off()
      window.clearTimeout(tm.current)
    }
  }, [s.id])
  const react = (w: Word) => {
    show(w)
    if (linkedRoom) sendReaction(s.id, w).catch((e: unknown) => toast.error(errText(e)))
  }

  const post = async (words: string) => {
    if (!words.trim()) return
    setBusy(true)
    try {
      await say(words.trim(), false)
      return true
    } catch (e) {
      toast.error(errText(e))
      return false
    } finally {
      setBusy(false)
    }
  }

  const sit = (v: boolean) => link?.seat(v).catch((e: unknown) => toast.error(errText(e)))
  const seats: PartySeat[] = party ? party.lobby.seats : []
  const placed = seats.filter((x) => x.seat >= 0).sort((x, y) => x.seat - y.seat)
  const people = placed.length ? placed : seats
  const free = party?.lobby.canSit && !party.lobby.seated ? Math.max(1, 6 - people.length) : 0
  const seated = seats.filter((x) => x.name && x.seated).length
  const atMs = party?.atMs ?? s.atMs
  const left = atMs - serverNow

  return (
    <div
      ref={box}
      className="blb"
      data-walk="lobby"
      role="dialog"
      aria-modal="true"
      aria-label={t('Lobby')}
      style={s.posterUrl ? { ['--bnt-pic' as string]: `url("${s.posterUrl}")` } : undefined}
      onKeyDown={(e) => {
        const el = e.target as HTMLElement
        if (e.key === 'Escape' && el.tagName !== 'INPUT') {
          e.preventDefault()
          onClose()
        }
      }}
    >
      <div className="blb-main">
        <span className="bnt-where">{t('Movie night · Lobby')}</span>
        <span className="blb-title">{s.title}</span>
        <span className="blb-line">
          {left > 0 ? (
            <>
              {t('Starts in')} <b>{clockTo(left)}</b>
            </>
          ) : (
            t('Feature presentation')
          )}
          {preshowOn ? ` · ${t('the pre-show is playing')}` : ''}
          {party ? ` · ${plural(seated, '{n} seated', '{n} seated')}` : ''}
        </span>

        <h2 className="blb-h" data-walk="lobby-seats">{t('Pick your seat')}</h2>
        {!party ? (
          <p className="bnt-dim">{link?.error || t('The seats show here in a moment…')}</p>
        ) : (
          <div className="blb-seats" role="group" aria-label={t('Seats')}>
            {people.map((x, i) => {
              const who = x.me ? t('You') : x.name || t('Someone from {house}', { house: x.house ?? '' })
              return (
                <button
                  key={`${x.seat}-${i}`}
                  type="button"
                  className={`blb-seat${x.me ? ' me' : ' taken'}`}
                  aria-pressed={!!x.me}
                  data-autofocus={x.me ? '' : undefined}
                  onClick={() => (x.me ? void sit(false) : toast.info(t('That seat’s taken.')))}
                  title={x.me ? t('OK to leave your seat') : who}
                >
                  <span className="blb-seat-n">{x.seat >= 0 ? seatShort(x.seat) : '•'}</span>
                  <span className="blb-seat-who">{who}</span>
                  <span className="blb-seat-state">{x.seated ? t('seated') : t('on the way')}</span>
                </button>
              )
            })}
            {Array.from({ length: free }, (_, i) => (
              <button key={`free-${i}`} type="button" className="blb-seat free" data-autofocus={i === 0 ? '' : undefined} onClick={() => void sit(true)} title={t('OK to sit here')}>
                <span className="blb-seat-n">+</span>
                <span className="blb-seat-who">{t('Free')}</span>
                <span className="blb-seat-state">{t('OK to sit')}</span>
              </button>
            ))}
          </div>
        )}
        <span className="blb-screen" aria-hidden="true" />
        <span className="blb-screen-label" aria-hidden="true">
          {t('SCREEN')}
        </span>

        {room && <div className="blb-room">{room}</div>}
        <div className="bnt-btns blb-acts">
          {!room && canStart && onStart && (
            <button type="button" className="bnt-btn pri" data-autofocus={party && !people.some((x) => x.me) && !free ? '' : undefined} onClick={onStart}>
              <Icon name="play" size={20} /> {due ? t('Start the film') : t('Start the film now')}
            </button>
          )}
          {!room && onSkipPre && (
            <button type="button" className="bnt-btn" onClick={onSkipPre}>
              {t('Skip the pre-show')}
            </button>
          )}
          <button type="button" className="bnt-btn" data-walk="lobby-phone" aria-expanded={phone === 'remote'} onClick={() => setPhone(phone === 'remote' ? '' : 'remote')}>
            {t('Use my phone as the remote')}
          </button>
          <button type="button" className="bnt-btn" onClick={onClose}>
            {t('Leave')}
          </button>
        </div>
        {phone === 'remote' && <PhoneHint what={t('Your phone as the remote')} path={`/showtime/chat/${s.id}`} onClose={() => setPhone('')} />}
      </div>

      <aside className="blb-side" data-walk="lobby-chat" aria-label={t('Chat')}>
        <span className="bnt-where">{t('Chat · everyone in the lobby')}</span>
        <div className="blb-msgs" role="log" aria-live="polite">
          {msgs.length === 0 && <p className="bnt-dim">{t('Nothing said yet. Say hello!')}</p>}
          {msgs.slice(-6).map((m) => (
            <div key={m.id} className="blb-msg">
              <span className="blb-who">{m.user.name}</span>
              <span className="blb-text">{m.spoiler ? t('(a spoiler: open the chat to see it)') : m.text}</span>
            </div>
          ))}
        </div>
        {a && !a.canPost && <p className="bnt-dim">{a.link ? t('Switch on Link with other Usherrs to chat here.') : t('To write here, link your Discord account under Linked services.')}</p>}
        {a?.canPost && (
          <>
            <span className="blb-label">{t('Quick replies')}</span>
            <div className="blb-chips" data-walk="lobby-replies">
              {REPLIES().map((q) => (
                <button key={q} type="button" className="blb-chip" disabled={busy} onClick={() => void post(q)}>
                  {q}
                </button>
              ))}
              <button type="button" className="blb-chip" aria-expanded={phone === 'chat'} onClick={() => setPhone(phone === 'chat' ? '' : 'chat')}>
                <Icon name="edit" size={16} /> {t('Write on my phone')}
              </button>
            </div>
            {phone === 'chat' && <PhoneHint what={t('Write in the chat')} path={`/showtime/chat/${s.id}`} onClose={() => setPhone('')} />}
            <form
              className="blb-say"
              onSubmit={(e) => {
                e.preventDefault()
                void post(text).then((ok) => ok && setText(''))
              }}
            >
              <input value={text} onChange={(e) => setText(e.target.value)} maxLength={a.link ? (a.maxText ?? 500) : 2000} placeholder={t('Say something…')} aria-label={t('Message')} enterKeyHint="send" />
            </form>
          </>
        )}
        <span className="blb-label">{t('React')}</span>
        <div className="blb-chips" data-walk="lobby-react">
          {WORDS.map((w) => (
            <button key={w} type="button" className="blb-chip blb-react" onClick={() => react(w)}>
              {w}
            </button>
          ))}
        </div>
      </aside>
      {burst && (
        <span key={burst.key} className="blb-burst" aria-hidden="true">
          {burst.word}
        </span>
      )}
    </div>
  )
}
