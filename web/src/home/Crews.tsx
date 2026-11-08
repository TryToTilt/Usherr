// Usherr: Crews (/crews), the crew space between parties
// (internal/api/home_crews_http.go). A crew is a group of linked Usherrs and
// the people in each who joined it: an ongoing chat, a shared watchlist with
// "Have it?" for each house, a vote on the next film (booked in Showtime for
// the crew), film clubs, and, for people who turn it on themselves, what the
// crew is watching.
import { useCallback, useEffect, useMemo, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import Switch from '../components/Switch'
import { useToast } from '../components/Toast'
import { useConfirm } from '../components/ConfirmProvider'
import { api, type ImportCandidate } from '../api'
import { useDocumentTitle } from '../documentTitle'
import { plural, t } from '../i18n'
import { preshowApi } from './preshowApi'
import { whenLabel } from './showtimes'
import { addToStack } from './stack'
import { crewFilmIn, crewsApi, crewTitle, posterOf, wrappedFeatured, type ActivityLine, type ChatLine, type Club, type Crew, type CrewFilm, type CrewList, type CrewSpace, type CrewVote, type HouseHas, type ListEntry } from './crewsApi'
import CrewWrapped from './CrewWrapped'
import './crews.css'

const errText = (e: unknown) => (e instanceof Error ? e.message : String(e))

// Each house wears a colour, by its number in the crew.
const TONES = ['var(--accent)', 'var(--warning)', 'var(--c-movie)', 'var(--c-tv)', 'var(--c-music)', 'var(--c-book)', 'var(--info)', 'var(--success)']
const toneOf = (h: number) => TONES[(h - 1) % TONES.length]
const initial = (name: string) => (name.trim()[0] ?? '?').toUpperCase()

type Tab = 'chat' | 'list' | 'vote' | 'clubs'

// haveText says who can play a film, in a few words.
function haveText(have: HouseHas[], crew: Crew): string {
  const joined = crew.houses.filter((h) => h.state === 'joined')
  const yes = new Set(have.filter((x) => x.has === 'yes').map((x) => x.h))
  const getting = new Set(have.filter((x) => x.has === 'getting').map((x) => x.h))
  const missing = joined.filter((h) => !yes.has(h.h))
  if (missing.length === 0) return t('Everyone has it')
  if (missing.length === joined.length && getting.size === 0) return t('Nobody has it yet')
  const me = missing.find((h) => h.me)
  if (me && missing.length === 1) return getting.has(me.h) ? t('You’re getting it') : t('You don’t have it yet')
  const names = missing.map((h) => (h.me ? t('you') : h.name)).join(', ')
  return t('Not at {houses} yet', { houses: names })
}

// FilmSearch: find a film by name (TMDB), and do something with it.
function FilmSearch({ label, onPick }: { label: string; onPick: (f: ImportCandidate) => void }) {
  const [q, setQ] = useState('')
  const [found, setFound] = useState<ImportCandidate[]>([])
  const [said, setSaid] = useState<{ q: string; error: string } | null>(null)
  useEffect(() => {
    if (q.trim().length < 2) return
    const id = window.setTimeout(() => {
      const words = q.trim()
      api
        .tmdbSearch('movie', words)
        .then((r) => {
          setFound(r.slice(0, 6))
          setSaid({ q: words, error: '' })
        })
        .catch((e: unknown) => {
          setFound([])
          setSaid({ q: words, error: errText(e) || t('The film search didn’t answer. Try again in a moment.') })
        })
    }, 350)
    return () => window.clearTimeout(id)
  }, [q])
  const answered = q.trim().length >= 2 && said?.q === q.trim()
  return (
    <div className="crw-search">
      <input type="search" value={q} onChange={(e) => setQ(e.target.value)} placeholder={label} aria-label={label} />
      {q.trim().length >= 2 && found.length > 0 && (
        <ul className="crw-found">
          {found.map((f) => (
            <li key={f.tmdbId}>
              <button
                type="button"
                onClick={() => {
                  onPick(f)
                  setQ('')
                  setFound([])
                }}
              >
                {f.posterUrl ? <img src={f.posterUrl} alt="" loading="lazy" /> : <span className="crw-noposter" />}
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
    </div>
  )
}

const fromCandidate = (f: ImportCandidate) => ({ tmdbId: f.tmdbId, title: f.title, year: f.year, poster: f.posterUrl })

function Poster({ film, small }: { film: CrewFilm; small?: boolean }) {
  const src = posterOf(film.poster)
  return src ? <img className={small ? 'crw-poster sm' : 'crw-poster'} src={src} alt="" loading="lazy" /> : <span className={small ? 'crw-poster sm' : 'crw-poster'} aria-hidden="true" />
}

// MakeCrew: name a crew and pick the linked Usherrs in it.
function MakeCrew({ peers, onMade }: { peers: CrewList['peers']; onMade: (c: Crew) => void }) {
  const [name, setName] = useState('')
  const [picked, setPicked] = useState<Set<number>>(new Set())
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  if (peers.length === 0) {
    return <p className="hint">{t('A crew is made of linked Usherrs. Ask an administrator to link this Usherr with a friend’s first (Settings, Linked Usherrs).')}</p>
  }
  const make = async () => {
    setBusy(true)
    setError('')
    try {
      onMade(await crewsApi.make(name, [...picked]))
      setName('')
      setPicked(new Set())
    } catch (e) {
      setError(errText(e))
    } finally {
      setBusy(false)
    }
  }
  return (
    <form
      className="crw-make"
      onSubmit={(e) => {
        e.preventDefault()
        void make()
      }}
    >
      <label>
        {t('Crew name')}
        <input value={name} onChange={(e) => setName(e.target.value)} maxLength={60} placeholder={t('The Friday Crew')} />
      </label>
      <fieldset>
        <legend>{t('Linked Usherrs in it')}</legend>
        {peers.map((p) => (
          <label key={p.id} className="crw-check">
            <input
              type="checkbox"
              checked={picked.has(p.id)}
              onChange={(e) => {
                const next = new Set(picked)
                if (e.target.checked) next.add(p.id)
                else next.delete(p.id)
                setPicked(next)
              }}
            />
            {p.name}
          </label>
        ))}
      </fieldset>
      <p className="hint">{t('Each one is sent an invite. In each house, only the people who join the crew themselves are in it.')}</p>
      <button className="primary btn-sm" type="submit" disabled={busy || !name.trim() || picked.size === 0}>
        {t('Make the crew')}
      </button>
      {error && <p className="notice notice-warn">{error}</p>}
    </form>
  )
}

// Members: the people in a crew, a round badge each, by house.
function Members({ crew }: { crew: Crew }) {
  const faces = crew.houses.flatMap((h) =>
    (h.people.length > 0 ? h.people : [h.name]).map((p, i) => ({ key: `${h.h}-${i}`, h, who: p })),
  )
  const online = crew.houses.filter((h) => h.online).length
  return (
    <div className="crw-members">
      <div className="crw-faces">
        {faces.slice(0, 10).map((f) => (
          <span key={f.key} className="crw-face" style={{ ['--tone' as string]: toneOf(f.h.h) }} title={`${f.who} · ${f.h.name}${f.h.state === 'invited' ? ` · ${t('invited')}` : ''}`}>
            {initial(f.who)}
            <span className={f.h.online ? 'crw-dot on' : 'crw-dot'} />
          </span>
        ))}
      </div>
      <span className="crw-dim">
        {plural(crew.houses.length, '{n} Usherr', '{n} Usherrs')} · {t('{n} online', { n: online })}
      </span>
    </div>
  )
}

// nextUp is what the crew has coming: an open vote, a booked winner, or a
// club night.
function nextUp(space: CrewSpace, now: number): string {
  const open = space.votes?.find((v) => v.state === 'open' && v.closes > now)
  if (open) return t('Vote open until {when}', { when: whenLabel(open.closes) })
  const won = space.votes?.find((v) => v.winner && v.at > now)
  if (won) {
    const f = won.choices.find((c) => c.tmdb === won.winner)
    if (f) return `${f.title} · ${whenLabel(won.at)}`
  }
  const club = [...(space.clubs ?? [])].sort((a, b) => a.next - b.next)[0]
  if (club?.next) return `${club.name} · ${whenLabel(club.next)}`
  return ''
}

// ---- Chat ----

function ChatTab({ space, onChanged }: { space: CrewSpace; onChanged: () => void }) {
  const toast = useToast()
  const [draft, setDraft] = useState('')
  const [busy, setBusy] = useState(false)
  const crew = space.crew
  const send = async () => {
    const text = draft.trim()
    if (!text) return
    setBusy(true)
    try {
      await crewsApi.chat(crew.id, text)
      setDraft('')
      onChanged()
    } catch (e) {
      toast.error(errText(e))
    } finally {
      setBusy(false)
    }
  }
  const lines = space.chat ?? []
  return (
    <div className="crw-split">
      <section className="crw-chat card">
        <ol className="crw-lines" aria-live="polite">
          {lines.length === 0 && <li className="hint">{t('Nothing said yet. Say hi to the crew.')}</li>}
          {lines.map((m) => (
            <ChatItem key={m.id} m={m} crew={crew} />
          ))}
        </ol>
        <form
          className="crw-say"
          onSubmit={(e) => {
            e.preventDefault()
            void send()
          }}
        >
          <input value={draft} onChange={(e) => setDraft(e.target.value)} maxLength={500} placeholder={t('Message {crew}', { crew: crew.name })} aria-label={t('Message the crew')} />
          <button className="primary btn-sm" type="submit" disabled={busy || !draft.trim()}>
            {t('Send')}
          </button>
        </form>
      </section>
      <Watching space={space} onChanged={onChanged} />
    </div>
  )
}

function ChatItem({ m, crew }: { m: ChatLine; crew: Crew }) {
  const toast = useToast()
  const mine = m.h === crew.myHid
  const forMe = m.card?.to === crew.myHid
  return (
    <li className="crw-line">
      <span className="crw-face sm" style={{ ['--tone' as string]: toneOf(m.h) }} aria-hidden="true">
        {initial(m.who)}
      </span>
      <div className="crw-line-body">
        <div className="crw-line-head">
          <strong>{m.who}</strong> <span className="crw-dim">{m.house && !mine ? `${m.house} · ` : ''}{whenLabel(m.at)}</span>
        </div>
        {m.text && <p className="crw-text">{m.text}</p>}
        {m.card && (
          <div className="crw-card">
            <Poster film={m.card} small />
            <div className="crw-card-main">
              <strong>{crewTitle(m.card)}</strong>
              <span className="crw-accent">{forMe ? t('Sent to you') : m.card.toWho ? t('Sent to {who}', { who: m.card.toWho }) : t('Sent to the crew')}</span>
              {m.card.note && <span className="crw-dim">{m.card.note}</span>}
            </div>
            <button
              type="button"
              className="btn-sm"
              onClick={() =>
                void addToStack({ kind: 'movie', tmdbId: m.card!.tmdb, title: m.card!.title, year: m.card!.year, posterUrl: posterOf(m.card!.poster) || undefined })
                  .then(() => toast.success(t('{title} is on your list.', { title: m.card!.title })))
                  .catch((e: unknown) => toast.error(errText(e)))
              }
            >
              {t('Add to my list')}
            </button>
          </div>
        )}
      </div>
    </li>
  )
}

const ACT: Record<ActivityLine['kind'], (l: ActivityLine) => string> = {
  finished: (l) => t('finished {title}', { title: l.part ? `${l.title} ${l.part}` : l.title }),
  watched: (l) => t('watched {title}', { title: l.part ? `${l.title} ${l.part}` : l.title }),
  rated: (l) => t('gave {title} {n} stars', { title: l.title, n: (l.stars ?? 0) / 2 }),
}

// Watching: what the crew is watching (only people who turned sharing on).
function Watching({ space, onChanged }: { space: CrewSpace; onChanged: () => void }) {
  const toast = useToast()
  const lines = space.activity ?? []
  return (
    <aside className="crw-side">
      <h3>{t('What the crew is watching')}</h3>
      <p className="crw-dim">{t('Only people who turned sharing on. Nothing NSFW, ever.')}</p>
      <Switch
        checked={space.sharing}
        label={t('Share what I watch with my crews')}
        onChange={(next) =>
          void crewsApi
            .sharing(next)
            .then(() => {
              toast.success(next ? t('What you watch from now on is shared with your crews.') : t('What you watch isn’t shared anymore.'))
              onChanged()
            })
            .catch((e: unknown) => toast.error(errText(e)))
        }
      />
      {lines.length === 0 && <p className="hint">{t('Nothing shared yet.')}</p>}
      {lines.map((l, i) => (
        <div key={i} className="crw-act" title={whenLabel(l.at)}>
          <strong style={{ color: toneOf(l.h) }}>{l.who}</strong> {ACT[l.kind](l)}
        </div>
      ))}
    </aside>
  )
}

// ---- Shared watchlist ----

function ListTab({ space, onChanged }: { space: CrewSpace; onChanged: () => void }) {
  const toast = useToast()
  const crew = space.crew
  const list = space.list ?? []
  const friends = crew.houses.filter((h) => !h.me && h.state === 'joined').flatMap((h) => (h.people.length > 0 ? h.people : [h.name]).map((who) => ({ h: h.h, who, house: h.name })))
  const [sendTo, setSendTo] = useState(friends.length > 0 ? `${friends[0].h}|${friends[0].who}` : '')
  const [sent, setSent] = useState<Set<string>>(new Set())
  const add = (f: ImportCandidate) =>
    void crewsApi
      .addFilm(crew.id, fromCandidate(f))
      .then(() => {
        toast.success(t('{title} is on the shared watchlist.', { title: f.title }))
        onChanged()
      })
      .catch((e: unknown) => toast.error(errText(e)))
  const send = (e: ListEntry) => {
    const [h, who] = sendTo.split('|')
    void crewsApi
      .send(crew.id, crewFilmIn(e), Number(h), who)
      .then(() => {
        setSent(new Set(sent).add(`${e.tmdb}|${sendTo}`))
        toast.success(t('{title} is sent to {who}.', { title: e.title, who }))
        onChanged()
      })
      .catch((err: unknown) => toast.error(errText(err)))
  }
  return (
    <div className="crw-list-tab">
      <div className="crw-head2">
        <h2>{t('Shared watchlist')}</h2>
        <span className="crw-dim">{t('Anyone in the crew can add. Picks for the vote come from here.')}</span>
      </div>
      <div className="crw-tools">
        <FilmSearch label={t('Add a film')} onPick={add} />
        {friends.length > 0 && (
          <label className="crw-sendto">
            {t('Send films to')}
            <select value={sendTo} onChange={(e) => setSendTo(e.target.value)}>
              {friends.map((f) => (
                <option key={`${f.h}|${f.who}`} value={`${f.h}|${f.who}`}>
                  {f.who} · {f.house}
                </option>
              ))}
            </select>
          </label>
        )}
      </div>
      {list.length === 0 && <p className="hint">{t('The list is empty. Add a film the crew should see.')}</p>}
      <div className="crw-grid">
        {list.map((e) => {
          const wasSent = sent.has(`${e.tmdb}|${sendTo}`)
          return (
            <div key={e.tmdb} className="crw-entry card">
              <Poster film={e} />
              <div className="crw-entry-main">
                <strong>{crewTitle(e)}</strong>
                <span className="crw-dim">{t('Added by {who}', { who: e.by })}</span>
                <span className={e.can === 'all' ? 'crw-accent' : 'crw-warn'}>{haveText(e.have, crew)}</span>
                <div className="crw-entry-tools">
                  {friends.length > 0 && (
                    <button type="button" className={wasSent ? 'btn-sm' : 'primary btn-sm'} disabled={wasSent} onClick={() => send(e)}>
                      {wasSent ? t('Sent to {who}', { who: sendTo.split('|')[1] }) : t('Send to {who}', { who: sendTo.split('|')[1] })}
                    </button>
                  )}
                  <button
                    type="button"
                    className="btn-sm"
                    onClick={() =>
                      void addToStack({ kind: 'movie', tmdbId: e.tmdb, title: e.title, year: e.year, posterUrl: posterOf(e.poster) || undefined })
                        .then(() => toast.success(t('{title} is on your list.', { title: e.title })))
                        .catch((err: unknown) => toast.error(errText(err)))
                    }
                  >
                    {t('Add to my list')}
                  </button>
                  {e.mine && (
                    <button type="button" className="btn-sm link-btn" onClick={() => void crewsApi.removeFilm(crew.id, e.tmdb).then(onChanged).catch((err: unknown) => toast.error(errText(err)))}>
                      {t('Take off')}
                    </button>
                  )}
                </div>
              </div>
            </div>
          )
        })}
      </div>
    </div>
  )
}

// ---- Next film vote ----

// localInput is a time for a datetime-local field.
function localInput(ms: number): string {
  const d = new Date(ms)
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}T${p(d.getHours())}:${p(d.getMinutes())}`
}

function VoteTab({ space, now, onChanged }: { space: CrewSpace; now: number; onChanged: () => void }) {
  const toast = useToast()
  const crew = space.crew
  const votes = space.votes ?? []
  const [starting, setStarting] = useState(false)
  const cast = (v: CrewVote, tmdb: number) =>
    void crewsApi
      .vote(crew.id, v.vid, v.myVote === tmdb ? 0 : tmdb)
      .then(onChanged)
      .catch((e: unknown) => toast.error(errText(e)))
  return (
    <div className="crw-split">
      <div className="crw-votes">
        {votes.length === 0 && <p className="hint">{t('No vote yet. Start one: pick films from the shared watchlist and when it plays.')}</p>}
        {votes.map((v) => {
          const total = v.choices.reduce((n, c) => n + c.voters.length, 0)
          const open = v.state === 'open' && v.closes > now
          const won = v.choices.find((c) => c.tmdb === v.winner)
          return (
            <section key={v.vid} className="crw-vote">
              <div className="crw-head2">
                <h2>{open ? t('What do we watch {when}?', { when: whenLabel(v.at) }) : won ? t('The crew picked {title}', { title: won.title }) : t('Nobody voted')}</h2>
                <span className="crw-dim">
                  {open ? t('Voting closes {when} · one vote each', { when: whenLabel(v.closes) }) : won ? t('Plays {when}, hosted by {host}', { when: whenLabel(v.at), host: v.host }) : t('Closed {when}', { when: whenLabel(v.closes) })}
                </span>
              </div>
              {!open && won && (
                <p className="crw-note">
                  {v.showtimeId ? (
                    <>
                      {t('It’s booked in your Showtime.')} <Link to="/showtime">{t('Open Showtime')}</Link>
                    </>
                  ) : (
                    t('{host} books it in Showtime for the crew, with the pre-show they picked.', { host: v.host })
                  )}
                </p>
              )}
              {v.choices.map((c) => {
                const mine = v.myVote === c.tmdb
                const n = c.voters.length
                return (
                  <div key={c.tmdb} className={mine ? 'crw-choice mine' : c.tmdb === v.winner ? 'crw-choice won' : 'crw-choice'}>
                    <Poster film={c} />
                    <div className="crw-choice-main">
                      <div>
                        <strong className="crw-title">{c.title}</strong> <span className="crw-dim">{c.year ?? ''}</span>
                      </div>
                      <div className={c.can === 'all' ? 'crw-accent' : 'crw-warn'}>
                        {c.can !== 'all' && <span className="crw-flag">{c.can === 'none' ? t('Nobody can play it yet') : t('Not everyone can play it')}</span>} {haveText(c.have, crew)}
                      </div>
                      <div className="crw-bar-row">
                        <div className="crw-bar">
                          <div style={{ width: `${total ? Math.round((n / total) * 100) : 0}%` }} />
                        </div>
                        <span className="crw-count" title={c.voters.map((s) => s.who).join(', ')}>
                          {plural(n, '{n} vote', '{n} votes')}
                        </span>
                      </div>
                    </div>
                    {open && (
                      <button type="button" className={mine ? 'primary' : ''} onClick={() => cast(v, c.tmdb)} aria-pressed={mine}>
                        {mine ? t('Your vote') : t('Vote')}
                      </button>
                    )}
                  </div>
                )
              })}
            </section>
          )
        })}
      </div>
      <aside className="crw-side">
        <div className="crw-panel">
          <h3>{t('How the ballot is made')}</h3>
          <p>{t('Each pick comes from the shared watchlist. Every house says whether it has the film, so a film not everyone can play is marked.')}</p>
        </div>
        {starting ? (
          <StartVote
            space={space}
            onDone={() => {
              setStarting(false)
              onChanged()
            }}
            onCancel={() => setStarting(false)}
          />
        ) : (
          <button type="button" className="crw-dashed" onClick={() => setStarting(true)} disabled={(space.list ?? []).length < 2}>
            {(space.list ?? []).length < 2 ? t('Add two films to the shared watchlist to start a vote') : t('Start a vote')}
          </button>
        )}
        <div className="crw-panel crw-gold">{t('When voting closes, the winner is booked in Showtime for the crew, with the pre-show the host picked. Whoever starts the vote hosts.')}</div>
      </aside>
    </div>
  )
}

function StartVote({ space, onDone, onCancel }: { space: CrewSpace; onDone: () => void; onCancel: () => void }) {
  const toast = useToast()
  const list = space.list ?? []
  const [picked, setPicked] = useState<Set<number>>(() => new Set(list.filter((e) => e.can === 'all').slice(0, 4).map((e) => e.tmdb)))
  const [closes, setCloses] = useState(() => localInput(Date.now() + 24 * 3600_000))
  const [at, setAt] = useState(() => localInput(Date.now() + 48 * 3600_000))
  const [pre, setPre] = useState(10)
  const [themes, setThemes] = useState<{ id: number; name: string }[]>([])
  const [theme, setTheme] = useState(0)
  const [busy, setBusy] = useState(false)
  useEffect(() => {
    preshowApi
      .themeNames()
      .then((d) => setThemes(d.themes))
      .catch(() => undefined)
  }, [])
  const start = async () => {
    setBusy(true)
    try {
      await crewsApi.openVote(space.crew.id, { films: [...picked], closesMs: new Date(closes).getTime(), atMs: new Date(at).getTime(), preshowMin: pre, preshowTheme: theme })
      toast.success(t('The vote is open.'))
      onDone()
    } catch (e) {
      toast.error(errText(e))
    } finally {
      setBusy(false)
    }
  }
  return (
    <form
      className="crw-panel crw-form"
      onSubmit={(e) => {
        e.preventDefault()
        void start()
      }}
    >
      <h3>{t('Start a vote')}</h3>
      <fieldset>
        <legend>{t('Films on the ballot (two to eight)')}</legend>
        {list.map((e) => (
          <label key={e.tmdb} className="crw-check">
            <input
              type="checkbox"
              checked={picked.has(e.tmdb)}
              onChange={(ev) => {
                const next = new Set(picked)
                if (ev.target.checked) next.add(e.tmdb)
                else next.delete(e.tmdb)
                setPicked(next)
              }}
            />
            {crewTitle(e)}
            {e.can !== 'all' && <span className="crw-warn"> · {t('not everyone has it')}</span>}
          </label>
        ))}
      </fieldset>
      <label>
        {t('Voting closes')}
        <input type="datetime-local" value={closes} onChange={(e) => setCloses(e.target.value)} />
      </label>
      <label>
        {t('The film starts')}
        <input type="datetime-local" value={at} onChange={(e) => setAt(e.target.value)} />
      </label>
      <label>
        {t('Pre-show (minutes)')}
        <input type="number" min={0} max={60} value={pre} onChange={(e) => setPre(Math.max(0, Math.min(60, Number(e.target.value) || 0)))} />
      </label>
      {pre > 0 && themes.length > 0 && (
        <label>
          {t('Pre-show theme')}
          <select value={theme} onChange={(e) => setTheme(Number(e.target.value))}>
            <option value={0}>{t('The schedule’s')}</option>
            {themes.map((th) => (
              <option key={th.id} value={th.id}>
                {th.name}
              </option>
            ))}
          </select>
        </label>
      )}
      <div className="crw-row">
        <button className="primary btn-sm" type="submit" disabled={busy || picked.size < 2 || picked.size > 8}>
          {t('Open the vote')}
        </button>
        <button type="button" className="btn-sm" onClick={onCancel}>
          {t('Cancel')}
        </button>
      </div>
    </form>
  )
}

// ---- Film clubs ----

const weekdayName = (d: number) => new Date(2026, 10, 1 + d).toLocaleDateString(undefined, { weekday: 'long' }) // Nov 1 2026 is a Sunday
const minuteLabel = (m: number) => new Date(2026, 0, 1, Math.floor(m / 60), m % 60).toLocaleTimeString(undefined, { hour: 'numeric', minute: '2-digit' })

function clubWhen(c: Club): string {
  return c.every === 2 ? t('Every other {day} · {time}', { day: weekdayName(c.weekday), time: minuteLabel(c.minute) }) : t('Every {day} · {time}', { day: weekdayName(c.weekday), time: minuteLabel(c.minute) })
}

function ClubsTab({ space, onChanged }: { space: CrewSpace; onChanged: () => void }) {
  const toast = useToast()
  const confirm = useConfirm()
  const crew = space.crew
  const clubs = space.clubs ?? []
  const [starting, setStarting] = useState(false)
  const [picking, setPicking] = useState<string | null>(null)
  const act = (p: Promise<unknown>, done?: string) =>
    void p
      .then(() => {
        if (done) toast.success(done)
        onChanged()
      })
      .catch((e: unknown) => toast.error(errText(e)))
  const coming = (c: Club) => (space.votes ?? []).find((v) => v.club === c.kid && v.at > Date.now())
  return (
    <div className="crw-clubs">
      {clubs.map((c, i) => {
        const v = coming(c)
        const won = v?.choices.find((x) => x.tmdb === v.winner)
        const rule = c.mode === 'vote' ? t('The crew votes') : c.picker ? t('Who picks rotates: {who} next', { who: c.myTurn ? t('you') : c.picker.who }) : t('Who picks rotates')
        let next = c.mode === 'vote' ? t('The crew’s vote') : c.pick ? crewTitle(c.pick) : c.picker ? t('{who}’s pick', { who: c.myTurn ? t('your') : c.picker.who }) : t('Nobody’s in yet')
        if (v) next = won ? `${won.title} · ${whenLabel(v.at)}` : t('Vote open until {when}', { when: whenLabel(v.closes) })
        const tone = TONES[(i + 2) % TONES.length]
        return (
          <div key={c.kid} className="crw-club card" style={{ ['--tone' as string]: tone }}>
            <span className="crw-club-bar" />
            <h3>{c.name}</h3>
            <div>{clubWhen(c)}</div>
            <div className="crw-dim">{rule}</div>
            <div>
              {t('Next up:')} <strong>{next}</strong>
            </div>
            {c.next > 0 && <div className="crw-dim">{t('Next night: {when}', { when: whenLabel(c.next) })}</div>}
            <div className="crw-streak">{c.streak > 0 ? plural(c.streak, '{n} night in a row', '{n} nights in a row') : c.nights > 0 ? plural(c.nights, '{n} night so far', '{n} nights so far') : t('No nights yet')}</div>
            {c.myTurn && c.mode === 'pick' && picking === c.kid && (
              <FilmSearch
                label={t('Pick the next film')}
                onPick={(f) => {
                  setPicking(null)
                  act(crewsApi.club(crew.id, c.kid, 'pick', fromCandidate(f)), t('Your pick is in.'))
                }}
              />
            )}
            {c.myTurn && c.mode === 'pick' && picking !== c.kid && (
              <button type="button" className="btn-sm" onClick={() => setPicking(c.kid)}>
                {c.pick ? t('Change my pick') : t('Pick the next film')}
              </button>
            )}
            <button type="button" className={c.in ? 'btn-sm' : 'primary btn-sm'} onClick={() => act(crewsApi.club(crew.id, c.kid, c.in ? 'leave' : 'join'))}>
              {c.in ? t('You’re in · Leave') : t('Join')}
            </button>
            {c.byH === crew.myHid && c.by === space.me && (
              <button
                type="button"
                className="btn-sm link-btn"
                onClick={() =>
                  void confirm({ title: t('End {club}?', { club: c.name }), body: t('No more nights are arranged. Its nights so far are kept.'), confirmLabel: t('End the club') }).then((ok) => ok && act(crewsApi.endClub(crew.id, c.kid)))
                }
              >
                {t('End the club')}
              </button>
            )}
          </div>
        )
      })}
      {starting ? (
        <StartClub
          crewId={crew.id}
          onDone={() => {
            setStarting(false)
            onChanged()
          }}
          onCancel={() => setStarting(false)}
        />
      ) : (
        <button type="button" className="crw-dashed crw-club-new" onClick={() => setStarting(true)}>
          {t('Start a club: a night, how often, and who picks')}
        </button>
      )}
    </div>
  )
}

function StartClub({ crewId, onDone, onCancel }: { crewId: number; onDone: () => void; onCancel: () => void }) {
  const toast = useToast()
  const [name, setName] = useState('')
  const [day, setDay] = useState(5)
  const [time, setTime] = useState('21:00')
  const [every, setEvery] = useState(1)
  const [mode, setMode] = useState<'pick' | 'vote'>('pick')
  const [pre, setPre] = useState(10)
  const [busy, setBusy] = useState(false)
  const start = async () => {
    const [h, m] = time.split(':').map(Number)
    setBusy(true)
    try {
      await crewsApi.addClub(crewId, { name, weekday: day, minute: (h || 0) * 60 + (m || 0), every, mode, preshowMin: pre })
      toast.success(t('{club} is started.', { club: name.trim() }))
      onDone()
    } catch (e) {
      toast.error(errText(e))
    } finally {
      setBusy(false)
    }
  }
  return (
    <form
      className="crw-club card crw-form"
      onSubmit={(e) => {
        e.preventDefault()
        void start()
      }}
    >
      <h3>{t('Start a club')}</h3>
      <label>
        {t('Name')}
        <input value={name} onChange={(e) => setName(e.target.value)} maxLength={60} placeholder={t('Friday Horror Night')} />
      </label>
      <div className="crw-row">
        <label>
          {t('Night')}
          <select value={day} onChange={(e) => setDay(Number(e.target.value))}>
            {[0, 1, 2, 3, 4, 5, 6].map((d) => (
              <option key={d} value={d}>
                {weekdayName(d)}
              </option>
            ))}
          </select>
        </label>
        <label>
          {t('Time')}
          <input type="time" value={time} onChange={(e) => setTime(e.target.value)} />
        </label>
      </div>
      <label>
        {t('How often')}
        <select value={every} onChange={(e) => setEvery(Number(e.target.value))}>
          <option value={1}>{t('Every week')}</option>
          <option value={2}>{t('Every other week')}</option>
        </select>
      </label>
      <label>
        {t('Who picks')}
        <select value={mode} onChange={(e) => setMode(e.target.value as 'pick' | 'vote')}>
          <option value="pick">{t('Take turns: whose turn it is picks')}</option>
          <option value="vote">{t('The crew votes')}</option>
        </select>
      </label>
      <label>
        {t('Pre-show (minutes)')}
        <input type="number" min={0} max={60} value={pre} onChange={(e) => setPre(Math.max(0, Math.min(60, Number(e.target.value) || 0)))} />
      </label>
      <p className="hint">{t('Two days before each night, Usherr books the pick, or opens the vote from the shared watchlist.')}</p>
      <div className="crw-row">
        <button className="primary btn-sm" type="submit" disabled={busy || !name.trim()}>
          {t('Start the club')}
        </button>
        <button type="button" className="btn-sm" onClick={onCancel}>
          {t('Cancel')}
        </button>
      </div>
    </form>
  )
}

// ---- the crew space ----

function CrewSpaceView({ id, onLeft }: { id: number; onLeft: () => void }) {
  const toast = useToast()
  const confirm = useConfirm()
  const [space, setSpace] = useState<CrewSpace | null>(null)
  const [error, setError] = useState('')
  const [tab, setTab] = useState<Tab>('chat')
  const [now, setNow] = useState(() => Date.now())
  const [wrapped, setWrapped] = useState<number | null>(null) // the Wrapped year open, or null
  const load = useCallback(() => {
    crewsApi
      .space(id)
      .then((d) => {
        setNow(Date.now())
        setSpace(d)
        setError('')
      })
      .catch((e: unknown) => setError(errText(e)))
  }, [id])
  useEffect(() => {
    load()
    const timer = window.setInterval(() => {
      if (document.visibilityState === 'visible') load()
    }, 8000)
    return () => window.clearInterval(timer)
  }, [load])
  if (error) return <p className="notice notice-warn">{error}</p>
  if (!space) return <p className="hint">{t('Loading…')}</p>
  const crew = space.crew
  const tabs: { id: Tab; label: string }[] = [
    { id: 'chat', label: t('Chat') },
    { id: 'list', label: t('Shared watchlist') },
    { id: 'vote', label: t('Next film vote') },
    { id: 'clubs', label: t('Film clubs') },
  ]
  const join = () =>
    void crewsApi
      .join(crew.id)
      .then(() => {
        toast.success(t('You’re in {crew}.', { crew: crew.name }))
        load()
      })
      .catch((e: unknown) => toast.error(errText(e)))
  const leave = async () => {
    if (!(await confirm({ title: t('Leave {crew}?', { crew: crew.name }), body: t('You won’t see its chat, list or votes. You can join again later.'), confirmLabel: t('Leave') }))) return
    try {
      await crewsApi.leave(crew.id)
      onLeft()
    } catch (e) {
      toast.error(errText(e))
    }
  }
  const end = async () => {
    if (!(await confirm({ title: t('End {crew}?', { crew: crew.name }), body: t('It ends for every Usherr in it: its chat, list, votes and clubs go.'), confirmLabel: t('End the crew') }))) return
    try {
      await crewsApi.end(crew.id)
      onLeft()
    } catch (e) {
      toast.error(errText(e))
    }
  }
  const next = nextUp(space, now)
  const featured = wrappedFeatured(new Date(now))
  return (
    <section className="crw-space">
      <header className="crw-header">
        <div>
          <div className="crw-dim">{crew.mine ? t('Crew run by this Usherr') : t('Crew run by {house}', { house: crew.houses[0]?.name ?? '' })}</div>
          <h1>{crew.name}</h1>
        </div>
        <Members crew={crew} />
        {next && (
          <div className="crw-next">
            {t('Next:')} <strong>{next}</strong>
          </div>
        )}
        {crew.joined && (
          <div className="crw-wrapped-open">
            {featured > 0 ? (
              <button type="button" className="btn-sm" onClick={() => setWrapped(featured)} aria-expanded={wrapped !== null}>
                {t('{year} Wrapped', { year: featured })}
              </button>
            ) : (
              <button type="button" className="btn-sm link-btn" onClick={() => setWrapped(new Date(now).getFullYear() - 1)} aria-expanded={wrapped !== null}>
                {t('Past Wrapped')}
              </button>
            )}
          </div>
        )}
      </header>
      {crew.joined && wrapped !== null && <CrewWrapped key={wrapped} crewId={crew.id} year={wrapped} onClose={() => setWrapped(null)} />}
      {!space.on && <p className="notice notice-warn">{t('Linking with other Usherrs is switched off here, so the crew can’t hear from this Usherr right now.')}</p>}
      {!crew.joined ? (
        <div className="card crw-joinme">
          <p>{crew.state === 'invited' ? t('{house} invites you to {crew}.', { house: crew.houses[0]?.name ?? '', crew: crew.name }) : t('You’re not in {crew} yet.', { crew: crew.name })}</p>
          <p className="hint">{t('Joining is just for you: the others in this house decide for themselves. The crew sees your first name.')}</p>
          <div className="crw-row">
            <button type="button" className="primary btn-sm" onClick={join}>
              {t('Join the crew')}
            </button>
            {crew.state === 'invited' && (
              <button type="button" className="btn-sm" onClick={() => void crewsApi.decline(crew.id).then(onLeft).catch((e: unknown) => toast.error(errText(e)))}>
                {t('No thanks')}
              </button>
            )}
          </div>
        </div>
      ) : (
        <>
          <nav className="crw-tabs" role="tablist" aria-label={t('Crew sections')}>
            {tabs.map((tb) => (
              <button key={tb.id} type="button" role="tab" aria-selected={tab === tb.id} className={tab === tb.id ? 'active' : ''} onClick={() => setTab(tb.id)}>
                {tb.label}
              </button>
            ))}
          </nav>
          <div className="crw-body">
            {tab === 'chat' && <ChatTab space={space} onChanged={load} />}
            {tab === 'list' && <ListTab space={space} onChanged={load} />}
            {tab === 'vote' && <VoteTab space={space} now={now} onChanged={load} />}
            {tab === 'clubs' && <ClubsTab space={space} onChanged={load} />}
          </div>
          <div className="crw-row crw-foot">
            <button type="button" className="btn-sm link-btn" onClick={() => void leave()}>
              {t('Leave the crew')}
            </button>
            {crew.canEdit && (
              <button type="button" className="btn-sm link-btn" onClick={() => void end()}>
                {t('End the crew')}
              </button>
            )}
          </div>
        </>
      )}
    </section>
  )
}

export default function Crews() {
  useDocumentTitle(t('Crews'))
  const params = useParams()
  const nav = useNavigate()
  const [data, setData] = useState<CrewList | null>(null)
  const [error, setError] = useState('')
  const [making, setMaking] = useState(false)
  const load = useCallback(() => {
    crewsApi
      .list()
      .then(setData)
      .catch((e: unknown) => setError(errText(e)))
  }, [])
  useEffect(load, [load])
  const crews = useMemo(() => data?.crews ?? [], [data])
  const wanted = Number(params.id) || 0
  const current = crews.find((c) => c.id === wanted) ?? crews.find((c) => c.joined) ?? crews[0]
  if (error) return <div className="page crews-page"><p className="notice notice-warn">{error}</p></div>
  if (!data) return <div className="page crews-page"><p className="hint">{t('Loading…')}</p></div>
  return (
    <div className="page crews-page">
      <div className="crw-top">
        <span className="crw-dim">
          <Link to="/showtime">{t('Movie night')}</Link> · {t('Crews')}
        </span>
        <div className="crw-picker">
          {crews.map((c) => (
            <button key={c.id} type="button" className={c.id === current?.id ? 'active' : ''} onClick={() => nav(`/crews/${c.id}`)}>
              {c.name}
              {c.state === 'invited' && <span className="crw-badge">{t('Invite')}</span>}
            </button>
          ))}
          <button type="button" className="crw-new" onClick={() => setMaking(!making)} aria-expanded={making}>
            {t('Make a crew')}
          </button>
        </div>
      </div>
      {!data.on && <p className="notice notice-warn">{t('Crews need Link with other Usherrs switched on (Settings, Linked Usherrs).')}</p>}
      {making && (
        <section className="card">
          <h2>{t('Make a crew')}</h2>
          <MakeCrew
            peers={data.peers}
            onMade={(c) => {
              setMaking(false)
              load()
              nav(`/crews/${c.id}`)
            }}
          />
        </section>
      )}
      {current ? (
        <CrewSpaceView
          key={current.id}
          id={current.id}
          onLeft={() => {
            load()
            nav('/crews')
          }}
        />
      ) : (
        !making && (
          <section className="card crw-empty">
            <h2>{t('No crews yet')}</h2>
            <p>{t('A crew is your movie-night group across linked Usherrs: a chat that keeps going between parties, a shared watchlist, a vote on the next film and film clubs.')}</p>
            <button type="button" className="primary btn-sm" onClick={() => setMaking(true)}>
              {t('Make a crew')}
            </button>
          </section>
        )
      )}
    </div>
  )
}
