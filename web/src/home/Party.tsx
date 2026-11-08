// Usherr: the party at a showtime (partyLink.ts). The lobby before the film (a
// countdown, the pre-show playing in step, who's seated), the intermission
// (a countdown everyone sees, what people are up to, the host's resume), and
// the verdict at the credits (your stars and a line for your Diary, the
// crew's average, the most-reacted moment, your stub).
import { useId, useState } from 'react'
import { Link } from 'react-router-dom'
import Icon from '../components/Icon'
import { plural, t } from '../i18n'
import { clockTo, hue, initials, preshowNow, STATUS_LABEL, useParty, type Party, type PartyLink, type PartySeat, type PreshowItem } from './partyLink'
import { filmClock } from './reactionMath'
import { clockLabel } from './showtimes'
import { seatLabel, starsShort } from './stubPicture'
import './party.css'

const errText = (e: unknown) => (e instanceof Error ? e.message : String(e))
const SEATS_A_ROW = 8

function kindLabel(it: PreshowItem): string {
  if (it.kind === 'trailer') return t('Trailer')
  if (it.kind === 'clip') return t('Clip')
  return t('Card')
}

// What the pre-show is showing: "Trailer 2 of 3 · Dune", and what's next.
function preshowLine(items: PreshowItem[], index: number): { now: string; next: string } {
  const it = items[index]
  if (!it) return { now: '', next: '' }
  const same = items.filter((x) => x.kind === it.kind)
  const k = items.slice(0, index + 1).filter((x) => x.kind === it.kind).length
  const head = same.length > 1 ? t('{kind} {n} of {total}', { kind: kindLabel(it), n: k, total: same.length }) : kindLabel(it)
  const nx = items[index + 1]
  return { now: it.title ? `${head} · ${it.title}` : head, next: nx ? (nx.title ? t('Then: {title}', { title: nx.title }) : t('Then: {kind}', { kind: kindLabel(nx) })) : t('Then: the film') }
}

// The seat map: eight a row, Row A first (the host's seat is A1).
function SeatMap({ seats }: { seats: PartySeat[] }) {
  const placed = seats.filter((s) => s.seat >= 0)
  if (placed.length === 0) {
    return (
      <ul className="pty-waitlist">
        {seats.map((s, i) => (
          <li key={i} className={s.seated ? 'seated' : ''}>
            {s.name} {s.seated ? t('· seated') : t('· on the way')}
          </li>
        ))}
      </ul>
    )
  }
  const last = Math.max(...placed.map((s) => s.seat))
  const rows: (PartySeat | null)[][] = []
  for (let r = 0; r <= Math.floor(last / SEATS_A_ROW); r++) {
    const row: (PartySeat | null)[] = []
    for (let c = 0; c < SEATS_A_ROW; c++) row.push(placed.find((s) => s.seat === r * SEATS_A_ROW + c) ?? null)
    rows.push(row)
  }
  return (
    <div className="pty-seats" role="list" aria-label={t('Who’s seated')}>
      <div className="pty-screenline" aria-hidden="true" />
      {rows.map((row, r) => (
        <div className="pty-row" key={r}>
          <span className="pty-rowname" aria-hidden="true">
            {String.fromCharCode(65 + (r % 26))}
          </span>
          {row.map((s, c) => {
            const n = r * SEATS_A_ROW + c
            if (!s) return <span key={n} className="pty-seat empty" role="listitem" aria-label={t('{seat}: free', { seat: seatLabel(n) })} />
            const who = s.name || t('Someone from {house}', { house: s.house ?? '' })
            const tip = `${seatLabel(n)}: ${who}${s.house && s.name ? ` (${s.house})` : ''} · ${s.seated ? t('seated') : t('on the way')}`
            return (
              <span
                key={n}
                role="listitem"
                className={`pty-seat${s.seated ? ' seated' : ' coming'}${s.me ? ' me' : ''}${s.name ? '' : ' held'}`}
                style={{ ['--pty-hue' as string]: String(hue(s.name || s.house || '')) }}
                title={tip}
                aria-label={tip}
              >
                {s.name ? initials(s.name) : initials(s.house ?? '')}
              </span>
            )
          })}
        </div>
      ))}
      <p className="pty-key">
        <span>{t('Seated: filled')}</span>
        <span>{t('On the way: outline')}</span>
        <span>{t('Free: room for more')}</span>
      </p>
    </div>
  )
}

// Lobby: before the film. preshow is how far into the pre-show it is
// (seconds), when it's on.
export function Lobby({ link, party, now, preshow, preshowTotal }: { link: PartyLink; party: Party; now: number; preshow: number | null; preshowTotal?: number }) {
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const seats = party.lobby.seats
  const placed = seats.filter((s) => s.name)
  const seated = placed.filter((s) => s.seated).length
  const items = party.lobby.preshow
  const on = preshow !== null && preshow >= 0
  const pn = !on ? { index: -1, progress: 0 } : items.length ? preshowNow(items, preshow) : { index: -1, progress: preshowTotal ? Math.min(1, preshow / preshowTotal) : 0 }
  const line = on && pn.index >= 0 ? preshowLine(items, pn.index) : null
  const mySeat = seats.find((s) => s.me && s.seat >= 0)
  const sit = (v: boolean) => {
    setBusy(true)
    setError('')
    link
      .seat(v)
      .catch((e: unknown) => setError(errText(e)))
      .finally(() => setBusy(false))
  }
  return (
    <section className="card pty-lobby" aria-label={t('Lobby')}>
      <div className="pty-lobby-top">
        <div>
          <span className="pty-label">{t('Doors open · the film starts in')}</span>
          <div className="pty-big">{clockTo(party.atMs - now)}</div>
        </div>
        {on && (
          <div className="pty-preshow" role="status">
            <span className="pty-live">
              <span className="pty-dot" aria-hidden="true" /> {t('Pre-show, playing in step')}
            </span>
            {line && <span>{line.now}</span>}
            {line && <span className="hint">{line.next}</span>}
            <div className="pty-bar" role="progressbar" aria-label={t('Pre-show')} aria-valuemin={0} aria-valuemax={100} aria-valuenow={Math.round(pn.progress * 100)}>
              <div style={{ width: `${Math.round(pn.progress * 100)}%` }} />
            </div>
          </div>
        )}
      </div>
      <div className="pty-who">
        <h2>{t('Who’s seated')}</h2>
        <span className="hint">{placed.length ? plural(placed.length, '{seated} of {n} seated', '{seated} of {n} seated', { seated, n: placed.length }) : t('Nobody’s said I’m in yet')}</span>
        {party.lobby.canSit && (
          <button className={`btn-sm${party.lobby.seated ? '' : ' primary'}`} disabled={busy} onClick={() => sit(!party.lobby.seated)} aria-pressed={party.lobby.seated}>
            {party.lobby.seated ? t('Leave my seat') : t('Take my seat')}
          </button>
        )}
      </div>
      {mySeat && <p className="hint">{t('Your seat: {seat}. It’s the one on your stub.', { seat: seatLabel(mySeat.seat) })}</p>}
      <SeatMap seats={seats} />
      {party.role === 'guest' && seats.every((s) => s.seat < 0) && <p className="hint">{t('The host’s seat map shows here once it comes.')}</p>}
      {error && <p className="error-text">{error}</p>}
    </section>
  )
}

// Intermission: the countdown, what people are up to, and the host's
// buttons. pausedAt is where the film stopped, when known.
export function Intermission({ link, party, pausedAt, compact }: { link: PartyLink; party: Party; pausedAt?: number; compact?: boolean }) {
  const [error, setError] = useState('')
  const inter = party.intermission
  const left = (inter.untilMs ?? 0) - link.serverNow()
  const run = (p: Promise<unknown>) => {
    setError('')
    p.catch((e: unknown) => setError(errText(e)))
  }
  return (
    <section className={`pty-inter${compact ? ' compact' : ''}`} aria-label={t('Intermission')} role="region">
      <span className="pty-label">{t('Intermission')}</span>
      <div className="pty-big pty-inter-clock" role="timer">
        {left > 0 ? clockTo(left) : t('Any moment now')}
      </div>
      <p>
        {inter.untilMs ? t('Back at {time}.', { time: clockLabel(inter.untilMs) }) : ''}
        {pausedAt !== undefined ? ` ${t('Everyone’s paused at {pos}.', { pos: filmClock(pausedAt) })}` : ''}
      </p>
      {inter.statuses.length > 0 && (
        <ul className="pty-statuses">
          {inter.statuses.map((s, i) => (
            <li key={i}>
              {s.name}
              {s.house ? ` (${s.house})` : ''} · {(STATUS_LABEL[s.status] ?? (() => s.status))().toLowerCase()}
            </li>
          ))}
        </ul>
      )}
      <div className="pty-status-pick" role="group" aria-label={t('What are you up to?')}>
        {party.statusChoices.map((c) => (
          <button key={c} type="button" className={`btn-sm${inter.mine === c ? ' primary' : ''}`} aria-pressed={inter.mine === c} onClick={() => run(link.status(inter.mine === c ? '' : c))}>
            {(STATUS_LABEL[c] ?? (() => c))()}
          </button>
        ))}
      </div>
      {party.canHost && (
        <div className="pty-host-btns">
          <button type="button" className="btn-sm" onClick={() => run(link.intermission('more'))}>
            {t('+5 minutes')}
          </button>
          <button type="button" className="btn-sm primary btn-with-icon" onClick={() => run(link.intermission('end'))}>
            <Icon name="play" size={14} /> {t('Resume for everyone')}
          </button>
        </div>
      )}
      {error && <p className="error-text">{error}</p>}
    </section>
  )
}

function Star({ fill, size }: { fill: 0 | 1 | 2; size: number }) {
  const id = useId()
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" aria-hidden="true">
      <defs>
        <linearGradient id={id}>
          <stop offset="50%" stopColor="currentColor" />
          <stop offset="50%" stopColor="transparent" />
        </linearGradient>
      </defs>
      <path d="M12 2.8l2.8 5.9 6.4.8-4.7 4.4 1.2 6.3L12 17.1l-5.7 3.1 1.2-6.3L2.8 9.5l6.4-.8z" fill={fill === 2 ? 'currentColor' : fill === 1 ? `url(#${id})` : 'none'} stroke="currentColor" strokeWidth="1.5" strokeLinejoin="round" />
    </svg>
  )
}

// Verdict: at the credits. Your stars (half stars too: tap a star twice) and
// a line go to your Diary; the crew's card shows the average.
export function Verdict({ link, party, title, compact }: { link: PartyLink; party: Party; title: string; compact?: boolean }) {
  const v = party.verdict
  const [stars, setStars] = useState(v.mine?.stars ?? 0)
  const [line, setLine] = useState(v.mine?.line ?? '')
  const [share, setShare] = useState(v.mine?.share ?? false)
  const [review, setReview] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [notes, setNotes] = useState<string[]>([])
  const [sent, setSent] = useState(!!v.mine)
  const pick = (i: number) => {
    setSent(false)
    setStars((cur) => (cur === i * 2 ? i * 2 - 1 : i * 2))
  }
  const send = () => {
    setBusy(true)
    setError('')
    link
      .verdict({ stars, line: line.trim(), share, review: review && v.canReview })
      .then((n) => {
        setNotes(n)
        setSent(true)
      })
      .catch((e: unknown) => setError(errText(e)))
      .finally(() => setBusy(false))
  }
  const top = v.top
  return (
    <div className={`pty-verdict${compact ? ' compact' : ''}`}>
      <section className="card pty-mine" aria-label={t('Your verdict')}>
        <span className="pty-label">{t('The credits are rolling')}</span>
        <h2>{t('Your verdict on {title}', { title })}</h2>
        <div className="pty-stars" role="group" aria-label={t('Your stars')}>
          {[1, 2, 3, 4, 5].map((i) => (
            <button key={i} type="button" onClick={() => pick(i)} aria-label={plural(i, '{n} star', '{n} stars')} title={t('Tap again for half a star')} aria-pressed={stars >= i * 2 - 1}>
              <Star fill={stars >= i * 2 ? 2 : stars === i * 2 - 1 ? 1 : 0} size={compact ? 28 : 34} />
            </button>
          ))}
          {stars > 0 && <span className="pty-stars-said">{t('{stars} out of 5', { stars: starsShort(stars).replace('★', '') })}</span>}
        </div>
        <label className="pty-line">
          {t('A few words for your Diary (optional)')}
          <input value={line} maxLength={140} onChange={(e) => (setLine(e.target.value), setSent(false))} placeholder={t('One line is plenty')} />
        </label>
        <label className="cx-check">
          <input type="checkbox" checked={share} onChange={(e) => (setShare(e.target.checked), setSent(false))} /> {t('Show my name and stars to the crew')}
        </label>
        {v.canReview && (
          <label className="cx-check">
            <input type="checkbox" checked={review} onChange={(e) => setReview(e.target.checked)} /> {t('Post it as my review too')}
          </label>
        )}
        <p className="hint">{t('Your stars go to your own Diary. The crew sees the average, and names only for people who share.')}</p>
        <div className="pty-mine-btns">
          <button type="button" className="primary" disabled={busy || stars < 1 || sent} onClick={send}>
            {sent ? t('Saved') : v.mine ? t('Save my verdict again') : t('Save my verdict')}
          </button>
          {v.stub && (
            <Link className="btn btn-with-icon" to="/you/stubs">
              <Icon name="bookmark" size={16} /> {t('Collect your stub')}
            </Link>
          )}
        </div>
        {notes.map((n) => (
          <p key={n} className="notice notice-warn">
            {n}
          </p>
        ))}
        {error && <p className="error-text">{error}</p>}
      </section>
      <section className="pty-crew" aria-label={t('Crew verdict')}>
        <span className="pty-label">{t('Crew verdict')}</span>
        {v.count > 0 ? (
          <p className="pty-avg">
            <span className="pty-big">{v.average.toFixed(1)}</span> <span>{plural(v.count, 'out of 5 · {n} voted', 'out of 5 · {n} voted')}</span>
          </p>
        ) : (
          <p className="hint">{t('Nobody’s voted yet.')}</p>
        )}
        {v.shared.length > 0 && (
          <ul className="pty-shared">
            {v.shared.map((s, i) => (
              <li key={i}>
                <span className="pty-av" style={{ ['--pty-hue' as string]: String(hue(s.name)) }} aria-hidden="true">
                  {initials(s.name)}
                </span>
                <span>
                  {s.name}
                  {s.house ? <span className="hint"> · {s.house}</span> : null}
                </span>
                <span className="pty-sh-stars">{starsShort(s.stars)}</span>
              </li>
            ))}
          </ul>
        )}
        {top && top.total > 0 && (
          <p className="pty-top">
            {t('Most-reacted moment: {time}, {word} ×{n}', { time: filmClock(top.peak), word: top.top, n: top.words[top.top] ?? top.total })}
          </p>
        )}
      </section>
    </div>
  )
}

// PartyMoments shows the moment the party is at: the intermission while
// it's on, the verdict at the credits, else the lobby (before the film).
// film: the film is on (no lobby). preshow: seconds into the pre-show.
export function PartyMoments({ showtimeId, title, film, preshow, preshowTotal, pausedAt }: { showtimeId: number; title: string; film: boolean; preshow: number | null; preshowTotal?: number; pausedAt?: number }) {
  const { link, party } = useParty(showtimeId)
  if (!link || !party) return null
  if (party.intermission.on) return <Intermission link={link} party={party} pausedAt={pausedAt} />
  if (party.verdict.open) return <Verdict key={party.showtimeId} link={link} party={party} title={title} />
  if (film) return null
  return <Lobby link={link} party={party} now={link.serverNow()} preshow={preshow} preshowTotal={preshowTotal} />
}

// PartyStage is the party over the film in the player: the intermission
// screen, or the verdict at the credits.
export function PartyStage({ showtimeId, title, ended, position }: { showtimeId: number; title: string; ended: boolean; position: () => number }) {
  const { link, party } = useParty(showtimeId)
  const [closed, setClosed] = useState(false)
  if (!link || !party) return null
  if (party.intermission.on)
    return (
      <div className="pty-stage">
        <Intermission link={link} party={party} pausedAt={position()} compact />
      </div>
    )
  if ((ended || party.verdict.open) && !closed)
    return (
      <div className="pty-stage pty-stage-verdict">
        <button type="button" className="icon-btn pty-stage-x" onClick={() => setClosed(true)} aria-label={t('Close')}>
          <Icon name="x" size={16} />
        </button>
        {party.verdict.open ? <Verdict key={party.showtimeId} link={link} party={party} title={title} compact /> : <p className="hint">{t('The verdict opens in a moment…')}</p>}
      </div>
    )
  return null
}

// HostPartyButtons: the host's Intermission and Roll credits, by the
// player's controls and on the phone.
export function HostPartyButtons({ showtimeId, className }: { showtimeId: number; className?: string }) {
  const { link, party } = useParty(showtimeId)
  const [error, setError] = useState('')
  if (!link || !party?.canHost || party.verdict.open) return null
  const run = (p: Promise<unknown>) => {
    setError('')
    p.catch((e: unknown) => setError(errText(e)))
  }
  return (
    <>
      {party.role === 'host' && !party.intermission.on && (
        <button type="button" className={className ?? 'btn-sm'} onClick={() => run(link.intermission('start'))} title={t('Pause for everyone, with a 10-minute countdown')}>
          {t('Intermission')}
        </button>
      )}
      <button type="button" className={className ?? 'btn-sm'} onClick={() => run(link.credits())} title={t('End the film for everyone and open the verdict')}>
        {t('Roll credits')}
      </button>
      {error && <span className="hp-room-warn">{error}</span>}
    </>
  )
}
