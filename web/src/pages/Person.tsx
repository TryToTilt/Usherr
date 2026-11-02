import { useEffect, useMemo, useState } from 'react'
import { useParams } from 'react-router-dom'
import { api, type PersonCredit, type PersonPage } from '../api'
import AddDialog, { type AddTarget } from '../components/AddDialog'
import { DiscoverCards } from '../components/DiscoverRail'
import Icon from '../components/Icon'
import { useDocumentTitle } from '../documentTitle'
import { t } from '../i18n'
import { ageOf, arrangeCredits, bioIsLong, creditGenres, creditHaveFrom, creditSortFrom, CREDITS_FIRST, filterByRole, firstOf, longDate, roleFilterFrom, showRoleFilter, type CreditSort, type RoleFilter } from '../people'
import { useOwned } from '../useOwned'
import { useUrlState } from '../useUrlState'
import FollowButton from '../home/FollowButton' // Usherr
import './Person.css'

const FILTERS: { value: RoleFilter; label: () => string }[] = [
  { value: 'all', label: () => t('All') },
  { value: 'acting', label: () => t('Acting') },
  { value: 'directing', label: () => t('Directing') },
  { value: 'writing', label: () => t('Writing') },
]

// Usherr: how a filmography can be sorted.
const SORTS: { value: CreditSort; label: () => string }[] = [
  { value: 'popular', label: () => t('Most popular') },
  { value: 'newest', label: () => t('Newest first') },
  { value: 'oldest', label: () => t('Oldest first') },
  { value: 'rated', label: () => t('Highest rated') },
  { value: 'az', label: () => t('A to Z') },
]

// A person's page: who they are, then the movies and shows they worked on,
// the most popular first, as the same cards Discover uses. Usherr: a row of
// menus finds, narrows and sorts a long filmography.
export default function Person() {
  const { id } = useParams<{ id: string }>()
  const personId = Number(id)
  const owned = useOwned()
  const [person, setPerson] = useState<PersonPage | null>(null)
  const [error, setError] = useState('')
  const [adding, setAdding] = useState<AddTarget | null>(null)
  const [bioOpen, setBioOpen] = useState(false)
  const [photoBroken, setPhotoBroken] = useState(false)
  const [more, setMore] = useState({ movies: CREDITS_FIRST, shows: CREDITS_FIRST })
  // Usherr: the filmography's filters and order, kept in the address so Back returns to them.
  const [ui, patchUi] = useUrlState({ role: 'all', sort: 'popular', genre: '', q: '', have: 'all', show: 'all' })
  useDocumentTitle(person?.name)

  useEffect(() => {
    setPerson(null)
    setError('')
    setBioOpen(false)
    setPhotoBroken(false)
    setMore({ movies: CREDITS_FIRST, shows: CREDITS_FIRST })
    // Going from one person to another: the answer for the first must not land on the second.
    let current = true
    api
      .tmdbPerson(personId)
      .then((p) => current && setPerson(p))
      .catch((e) => current && setError(e instanceof Error ? e.message : String(e)))
    return () => {
      current = false
    }
  }, [personId])

  const role = roleFilterFrom(ui.role, person?.workKinds ?? [])
  const view = { sort: creditSortFrom(ui.sort), genre: ui.genre, query: ui.q, have: creditHaveFrom(ui.have) }
  const has = (kind: 'movie' | 'tv') => (id: number) => (kind === 'movie' ? owned.movies : owned.shows).has(id)
  // A few hundred titles at most: worked out on each draw.
  const movies = ui.show === 'tv' ? [] : arrangeCredits(filterByRole(person?.movies ?? [], role), view, has('movie'))
  const shows = ui.show === 'movie' ? [] : arrangeCredits(filterByRole(person?.shows ?? [], role), view, has('tv'))
  const genres = useMemo(() => creditGenres([...(person?.movies ?? []), ...(person?.shows ?? [])]), [person])
  // Any change starts the lists from the top again.
  const change = (c: Partial<typeof ui>) => {
    patchUi(c)
    setMore({ movies: CREDITS_FIRST, shows: CREDITS_FIRST })
  }

  if (error) return <p className="error-text">{error}</p>
  if (!person) return <div className="skeleton" style={{ height: 320, borderRadius: 22 }} />

  const age = ageOf(person.birthday, person.deathday)
  const born = person.birthday ? (age !== undefined && !person.deathday ? t('{date} (age {age})', { date: longDate(person.birthday), age }) : longDate(person.birthday)) : ''
  const died = person.deathday ? (age !== undefined ? t('{date} (age {age})', { date: longDate(person.deathday), age }) : longDate(person.deathday)) : ''
  const facts = [
    born ? { label: t('Born'), value: born } : null,
    died ? { label: t('Died'), value: died } : null,
    person.placeOfBirth ? { label: t('Place of birth'), value: person.placeOfBirth } : null,
  ].filter(Boolean) as { label: string; value: string }[]
  const long = bioIsLong(person.biography)
  const nothing = person.movies.length === 0 && person.shows.length === 0
  const both = person.movies.length > 0 && person.shows.length > 0
  const narrowed = ui.q !== '' || ui.genre !== '' || ui.have !== 'all' || ui.show !== 'all' || role !== 'all'
  const found = movies.length + shows.length

  return (
    <div className="person-page">
      <section className="person-head">
        <div className="person-photo">
          {person.photoUrl && !photoBroken ? <img src={person.photoUrl} alt="" onError={() => setPhotoBroken(true)} /> : <span aria-hidden="true">{person.name.charAt(0).toUpperCase()}</span>}
        </div>
        <div className="person-info">
          <h1>{person.name}</h1>
          {facts.length > 0 && (
            <dl className="title-facts">
              {facts.map((f) => (
                <div key={f.label}>
                  <dt>{f.label}</dt>
                  <dd>{f.value}</dd>
                </div>
              ))}
            </dl>
          )}
          {person.biography && (
            <div>
              <p className={`person-bio${long && !bioOpen ? ' folded' : ''}`}>{person.biography}</p>
              {long && (
                <button type="button" className="btn-sm" aria-expanded={bioOpen} onClick={() => setBioOpen(!bioOpen)}>
                  {bioOpen ? t('Show less') : t('Show more')}
                </button>
              )}
            </div>
          )}
          <div className="title-links">
            <FollowButton id={person.id} name={person.name} image={person.photoUrl} known={person.knownFor} /> {/* Usherr */}
            {person.imdbId && /^nm\d+$/.test(person.imdbId) && (
              <a className="link-pill" href={`https://www.imdb.com/name/${person.imdbId}/`} target="_blank" rel="noreferrer">
                <Icon name="external" size={13} /> IMDb
              </a>
            )}
            <a className="link-pill" href={`https://www.themoviedb.org/person/${person.id}`} target="_blank" rel="noreferrer">
              <Icon name="external" size={13} /> TMDB
            </a>
          </div>
        </div>
      </section>

      {!nothing && (
        <div className="person-tools" role="group" aria-label={t('Sort and filter their work')}>
          <label className="person-find">
            <Icon name="search" size={15} />
            <input type="search" value={ui.q} onChange={(e) => change({ q: e.target.value })} placeholder={t('Find a title')} aria-label={t('Find a title')} />
          </label>
          {showRoleFilter(person.workKinds) && (
            <select value={role} onChange={(e) => change({ role: e.target.value })} aria-label={t('Kind of work')}>
              {FILTERS.filter((f) => f.value === 'all' || person.workKinds.includes(f.value)).map((f) => (
                <option key={f.value} value={f.value}>
                  {f.value === 'all' ? t('Every role') : f.label()}
                </option>
              ))}
            </select>
          )}
          {both && (
            <select value={ui.show} onChange={(e) => change({ show: e.target.value })} aria-label={t('Movies or TV shows')}>
              <option value="all">{t('Movies and shows')}</option>
              <option value="movie">{t('Movies')}</option>
              <option value="tv">{t('TV shows')}</option>
            </select>
          )}
          {genres.length > 1 && (
            <select value={ui.genre} onChange={(e) => change({ genre: e.target.value })} aria-label={t('Genre')}>
              <option value="">{t('Every genre')}</option>
              {genres.map((g) => (
                <option key={g} value={g}>
                  {g}
                </option>
              ))}
            </select>
          )}
          <select value={ui.have} onChange={(e) => change({ have: e.target.value })} aria-label={t('In your library')}>
            <option value="all">{t('Have it or not')}</option>
            <option value="have">{t('In your library')}</option>
            <option value="missing">{t('Not in your library')}</option>
          </select>
          <select value={view.sort} onChange={(e) => change({ sort: e.target.value })} aria-label={t('Sort by')}>
            {SORTS.map((s) => (
              <option key={s.value} value={s.value}>
                {s.label()}
              </option>
            ))}
          </select>
          {narrowed && (
            <>
              <span className="person-count">{t('{n} found', { n: found })}</span>
              <button type="button" className="btn-sm" onClick={() => change({ q: '', genre: '', have: 'all', show: 'all', role: 'all' })}>
                {t('Clear')}
              </button>
            </>
          )}
        </div>
      )}
      {!nothing && narrowed && found === 0 && <p className="person-empty">{t('Nothing of {name}’s matches. Try another genre, or Clear.', { name: person.name })}</p>}

      {nothing && <p className="person-empty">{t('TMDB lists no movies or shows for {name}.', { name: person.name })}</p>}
      <Credits title={t('Movies')} kind="movie" items={movies} shown={more.movies} onMore={() => setMore((m) => ({ ...m, movies: m.movies + CREDITS_FIRST }))} owned={owned} onAdd={setAdding} />
      <Credits title={t('TV shows')} kind="tv" items={shows} shown={more.shows} onMore={() => setMore((m) => ({ ...m, shows: m.shows + CREDITS_FIRST }))} owned={owned} onAdd={setAdding} />

      {adding && (
        <AddDialog
          target={adding}
          onClose={() => setAdding(null)}
          onAdded={() => {
            // Stay here so you can keep browsing: the card switches to "Open" by itself.
            setAdding(null)
            owned.reload()
          }}
        />
      )}
    </div>
  )
}

function Credits({ title, kind, items, shown, onMore, owned, onAdd }: { title: string; kind: 'movie' | 'tv'; items: PersonCredit[]; shown: number; onMore: () => void; owned: ReturnType<typeof useOwned>; onAdd: (t: AddTarget) => void }) {
  if (items.length === 0) return null
  const { visible, hidden } = firstOf(items, shown)
  return (
    <section className="discover-rail person-credits">
      <div className="rail-head">
        <h2>{title}</h2>
      </div>
      <div className="poster-grid">
        <DiscoverCards items={visible} kind={kind} owned={owned} onAdd={onAdd} />
      </div>
      {hidden > 0 && (
        <button type="button" className="btn-sm person-more" onClick={onMore}>
          {t('Show more')}
        </button>
      )}
    </section>
  )
}
