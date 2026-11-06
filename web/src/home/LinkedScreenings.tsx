// Usherr: linked screenings, step 2 (internal/api/home_link_screenings.go).
// On the Showtime page: the screenings linked Usherrs invited this one to
// (I'm in / Can't make it / Maybe, and whether this Usherr has the film),
// and the host's "Invite linked Usherrs" picker under a booking.
import { useCallback, useEffect, useState } from 'react'
import { useToast } from '../components/Toast'
import { api, isRequested } from '../api'
import { plural, t } from '../i18n'
import { answerScreening, checkScreening, inviteLinked, linkedFor, listScreenings, whenLabel, type HasFilm, type LinkedGuest, type LinkedScreening, type ScreeningAnswer, type Showtime } from './showtimes'

const errText = (e: unknown) => (e instanceof Error ? e.message : String(e))

const ANSWER_LABEL: Record<ScreeningAnswer, () => string> = {
  going: () => t('I’m in'),
  not: () => t('Can’t make it'),
  maybe: () => t('Maybe'),
}

function answerText(answer: ScreeningAnswer | undefined, count: number | undefined): string {
  switch (answer) {
    case 'going':
      return plural(count ?? 0, 'Coming · {n} person', 'Coming · {n} people')
    case 'maybe':
      return plural(count ?? 0, 'Maybe · {n} person', 'Maybe · {n} people')
    case 'not':
      return t('Can’t make it')
  }
  return t('No answer yet')
}

const HAS_TEXT: Record<HasFilm, () => string> = {
  yes: () => t('has the film'),
  getting: () => t('is getting the film'),
  no: () => t('doesn’t have the film yet'),
}

// InviteLinked: the host picks which linked Usherrs see a booking, and sees
// what each one said.
export function InviteLinked({ showtime, onChanged }: { showtime: Showtime; onChanged: () => void }) {
  const toast = useToast()
  const [peers, setPeers] = useState<LinkedGuest[] | null>(null)
  const [picked, setPicked] = useState<Set<number>>(new Set())
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const show = useCallback((list: LinkedGuest[]) => {
    setPeers(list)
    setPicked(new Set(list.filter((p) => p.invited).map((p) => p.id)))
  }, [])
  useEffect(() => {
    linkedFor(showtime.id)
      .then((d) => show(d.peers))
      .catch((e: unknown) => setError(errText(e)))
  }, [showtime.id, show])
  const save = async () => {
    setBusy(true)
    setError('')
    try {
      const d = await inviteLinked(showtime.id, [...picked])
      show(d.peers)
      toast.success(picked.size === 0 ? t('{title} isn’t shared with linked Usherrs anymore.', { title: showtime.title }) : plural(picked.size, '{title} is shared with {n} linked Usherr.', '{title} is shared with {n} linked Usherrs.', { title: showtime.title }))
      onChanged()
    } catch (e) {
      setError(errText(e))
    } finally {
      setBusy(false)
    }
  }
  const changed = peers !== null && peers.some((p) => p.invited !== picked.has(p.id))
  return (
    <div className="sht-pick">
      <p className="hint">{t('Tick the Usherrs to invite. Each one sees this screening in its Showtime and can say who’s coming. The others don’t see it.')}</p>
      {peers === null && !error && <p className="hint">{t('Loading…')}</p>}
      {peers?.length === 0 && <p className="hint">{t('No linked Usherrs yet. Link one in Settings, Movie night, Linked Usherrs.')}</p>}
      {peers && peers.length > 0 && (
        <ul>
          {peers.map((p) => (
            <li key={p.id}>
              <label className="check-row">
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
                <span>{p.name}</span>
              </label>
              {p.invited && (
                <span className="sht-pick-state">
                  {answerText(p.answer, p.count)}
                  {p.has ? ` · ${HAS_TEXT[p.has]()}` : ''}
                  {p.differentCut ? ` · ${t('their copy may be a different cut')}` : ''}
                  {!p.seen ? ` · ${t('hasn’t received the latest yet')}` : ''}
                </span>
              )}
            </li>
          ))}
        </ul>
      )}
      {peers && peers.length > 0 && (
        <button className="primary btn-sm" onClick={() => void save()} disabled={busy || !changed}>
          {t('Save')}
        </button>
      )}
      {error && <p className="notice notice-warn">{error}</p>}
    </div>
  )
}

// LinkedScreenings: what linked Usherrs invited this one to.
export function LinkedScreenings() {
  const toast = useToast()
  const [list, setList] = useState<LinkedScreening[] | null>(null)
  const [busy, setBusy] = useState<number | null>(null)
  const load = useCallback(() => {
    listScreenings()
      .then((d) => setList(d.screenings))
      .catch(() => setList([]))
  }, [])
  useEffect(() => {
    load()
    const id = window.setInterval(load, 30_000)
    return () => window.clearInterval(id)
  }, [load])
  if (!list || list.length === 0) return null

  const answer = async (s: LinkedScreening, a: ScreeningAnswer) => {
    setBusy(s.id)
    try {
      const d = await answerScreening(s.id, a)
      setList(d.screenings)
    } catch (e) {
      toast.error(errText(e))
      load()
    } finally {
      setBusy(null)
    }
  }
  const getIt = async (s: LinkedScreening) => {
    setBusy(s.id)
    try {
      const res = await api.addMovie(s.tmdbId, { searchNow: true })
      toast.success(isRequested(res) ? t('Asked for {title}. An administrator will look at it under Activity > Requests.', { title: s.title }) : t('Getting {title}: Usherr is looking for it now.', { title: s.title }))
      const d = await checkScreening(s.id)
      setList(d.screenings)
    } catch (e) {
      toast.error(errText(e))
    } finally {
      setBusy(null)
    }
  }

  return (
    <section className="card sht-list sht-linked">
      <h2>{t('From linked Usherrs')}</h2>
      <ul>
        {list.map((s) => {
          const coming = s.roster.filter((g, i) => g.answer === 'going' && !(s.rosterHost && i === 0))
          const hostHome = s.rosterHost ? s.roster[0] : undefined // step 3: the host's own household
          const others = s.people.filter((p) => p.answer)
          return (
            <li key={s.id} className="sht-row">
              {s.posterUrl ? <img src={s.posterUrl} alt="" /> : <span className="sht-row-ph" />}
              <div>
                <strong>
                  {s.title}
                  {s.year ? ` (${s.year})` : ''}
                </strong>
                <span>
                  {whenLabel(s.atMs)}
                  {s.preshowMin > 0 ? ` · ${t('{n} min pre-show', { n: s.preshowMin })}` : ''}
                  {s.edition ? ` · ${s.edition}` : ''}
                </span>
                <span className="sht-party">
                  {s.hostName ? t('Hosted by {name} ({usherr})', { name: s.hostName, usherr: s.host }) : t('Hosted by {host}', { host: s.host })}
                  {hostHome?.answer === 'going' && hostHome.count ? ` · ${plural(hostHome.count, '{n} person at the host’s', '{n} people at the host’s')}` : ''}
                  {coming.length > 0 ? ` · ${t('coming: {names}', { names: coming.map((g) => (g.count && g.count > 1 ? `${g.name} (${g.count})` : g.name)).join(', ') })}` : ''}
                </span>
                {others.length > 0 && <span>{t('Here: {names}', { names: others.map((p) => `${p.name} (${ANSWER_LABEL[p.answer]()})`).join(', ') })}</span>}
                <span>
                  {s.has === 'yes' && (s.myRuntime ? t('You have it ({n} min).', { n: s.myRuntime }) : t('You have it.'))}
                  {s.has === 'getting' && t('It’s on its way here.')}
                  {s.has === 'no' && t('You don’t have it yet.')}
                  {s.has === 'no' && (
                    <>
                      {' '}
                      <button className="btn-sm" onClick={() => void getIt(s)} disabled={busy === s.id}>
                        {t('Get it before showtime')}
                      </button>
                    </>
                  )}
                </span>
                {s.differentCut && <span className="sht-warn">{t('Your copy may be a different cut, so it may not stay in step with the host’s.')}</span>}
                {s.showtimeId ? <span>{t('Booked in your Showtime: the Marquee counts down to it.')}</span> : null}
              </div>
              <span className="sht-row-btns" role="group" aria-label={t('Are you coming?')}>
                {(['going', 'not', 'maybe'] as ScreeningAnswer[]).map((a) => (
                  <button key={a} className={`btn-sm${s.mine === a ? ' primary' : ''}`} aria-pressed={s.mine === a} onClick={() => void answer(s, a)} disabled={busy === s.id}>
                    {ANSWER_LABEL[a]()}
                  </button>
                ))}
              </span>
            </li>
          )
        })}
      </ul>
    </section>
  )
}
