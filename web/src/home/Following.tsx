// Home fork: Upcoming → Following. The people you follow (actors, directors,
// writers): find someone to follow, what they all have coming and have just
// put out, and a row per person (their new work first, then what they're
// best known for).
import HelpLink from '../components/HelpLink'
import { useEffect, useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import type { DiscoverMovie } from '../api'
import AddDialog, { type AddTarget } from '../components/AddDialog'
import { DiscoverCards } from '../components/DiscoverRail'
import Icon from '../components/Icon'
import { plural, t } from '../i18n'
import { useOwned } from '../useOwned'
import { follow, followWork, unfollow, whoFor, type Followed, type FollowWork } from './follows'
import Shelves, { type Shelf } from './Shelves'
import './news.css'

interface Item {
  kind: 'movie' | 'tv'
  m: DiscoverMovie
  all?: boolean // on a row from everyone, so say who it's from
}

function useDebounced<T>(value: T, ms: number) {
  const [v, setV] = useState(value)
  useEffect(() => {
    const id = window.setTimeout(() => setV(value), ms)
    return () => window.clearTimeout(id)
  }, [value, ms])
  return v
}

const today = () => new Date().toISOString().slice(0, 10)
const byDate = (a: Item, b: Item) => (a.m.releaseDate ?? '').localeCompare(b.m.releaseDate ?? '')

export default function Following() {
  const owned = useOwned()
  const [adding, setAdding] = useState<AddTarget | null>(null)
  const [work, setWork] = useState<FollowWork | null>(null)
  const [error, setError] = useState('')
  const [q, setQ] = useState('')
  const [found, setFound] = useState<Followed[]>([])
  const dq = useDebounced(q, 300)

  const load = () =>
    followWork()
      .then(setWork)
      .catch((e: Error) => setError(e.message))
  useEffect(() => {
    void load()
  }, [])
  useEffect(() => {
    if (dq.trim().length < 2) {
      setFound([])
      return
    }
    fetch(`/api/home/search/people?q=${encodeURIComponent(dq.trim())}`, { credentials: 'include' })
      .then((r) => (r.ok ? r.json() : []))
      .then((list: Partial<Followed>[]) => setFound(list.map((p) => ({ id: p.id ?? 0, name: p.name ?? '', image: p.image ?? '', known: p.known ?? '' }))))
      .catch(() => setFound([]))
  }, [dq])

  const people = work?.people ?? []
  const following = new Set(people.map((p) => p.id))
  const change = (p: Promise<unknown>) => {
    setError('')
    p.then(load).catch((e: Error) => setError(e.message))
  }

  const shelves = useMemo<Shelf<Item>[]>(() => {
    if (!work) return []
    const now = today()
    const all: Item[] = [...work.movies.map((m) => ({ kind: 'movie' as const, m, all: true })), ...work.shows.map((m) => ({ kind: 'tv' as const, m, all: true }))]
    const coming = all.filter((x) => (x.m.releaseDate ?? '') > now).sort(byDate)
    const out = all.filter((x) => (x.m.releaseDate ?? '') <= now).sort((a, b) => byDate(b, a))
    const list: Shelf<Item>[] = []
    if (coming.length) list.push({ key: 'coming', title: t('Coming soon'), hint: t('from everyone you follow'), items: coming })
    if (out.length) list.push({ key: 'new', title: t('Out in the last year'), hint: t('from everyone you follow'), items: out })
    for (const p of work.people) {
      const items: Item[] = [...p.movies.map((m) => ({ kind: 'movie' as const, m })), ...p.shows.map((m) => ({ kind: 'tv' as const, m }))]
      if (!items.length) continue
      list.push({
        key: `p${p.id}`,
        title: p.name,
        hint: p.fresh > 0 ? plural(p.fresh, '{n} new or coming', '{n} new or coming') : t('best known first'),
        items,
        extra: (
          <Link to={`/person/${p.id}`} className="btn-link">
            {t('Their page')}
          </Link>
        ),
      })
    }
    return list
  }, [work])

  const whoMovie = whoFor(work, 'movie')
  const whoTV = whoFor(work, 'tv')

  return (
    <div className="page-pad following-page">
      <div className="following-find">
        <HelpLink topic="home-finding#people-and-following" className="following-help" />
        <input type="search" value={q} onChange={(e) => setQ(e.target.value)} placeholder={t('Follow someone: type a name')} aria-label={t('Follow someone')} />
        {found.length > 0 && (
          <div className="chip-row">
            {found.map((p) =>
              following.has(p.id) ? (
                <span key={p.id} className="chip active">
                  <Icon name="check" size={12} /> {p.name}
                </span>
              ) : (
                <button
                  key={p.id}
                  className="chip"
                  onClick={() => {
                    change(follow(p))
                    setQ('')
                    setFound([])
                  }}
                >
                  <Icon name="plus" size={12} /> {p.name}
                  {p.known ? <small> · {p.known}</small> : null}
                </button>
              ),
            )}
          </div>
        )}
      </div>
      {error && <p className="error-text">{error}</p>}
      {people.length > 0 && (
        <div className="chip-row following-people">
          {people.map((p) => (
            <span key={p.id} className="chip following-chip">
              {p.image ? <img src={p.image} alt="" /> : <Icon name="user" size={14} />}
              <Link to={`/person/${p.id}`}>{p.name}</Link>
              <button className="btn-link" onClick={() => change(unfollow(p.id))} aria-label={t('Unfollow {name}', { name: p.name })} title={t('Unfollow')}>
                <Icon name="x" size={12} />
              </button>
            </span>
          ))}
        </div>
      )}
      {!work && !error ? (
        <div className="skeleton" style={{ height: 260 }} />
      ) : (
        <Shelves
          memory="following"
          shelves={shelves}
          itemKey={(x) => `${x.kind}:${x.m.tmdbId}`}
          render={(x) => <DiscoverCards items={[x.m]} kind={x.kind} owned={owned} onAdd={setAdding} why={!x.all ? undefined : x.kind === 'movie' ? whoMovie : whoTV} />}
          empty={<p className="hint">{t('Follow actors, directors and writers to see their new and coming films and shows here. Search above, or tap Follow on anyone’s page.')}</p>}
        />
      )}
      {adding && (
        <AddDialog
          target={adding}
          onClose={() => setAdding(null)}
          onAdded={() => {
            setAdding(null)
            owned.reload()
          }}
        />
      )}
    </div>
  )
}
