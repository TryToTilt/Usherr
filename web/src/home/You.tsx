// Home fork: You. One page for your own side of Mediarium: your taste in
// films and shows (what you lean towards and away from), For you, everything
// you've voted on, who you follow, your recipes and services, and whether
// the household may see your taste. Seeing theirs takes sharing yours.
import { lazy, Suspense, useEffect, useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import AddDialog, { type AddTarget } from '../components/AddDialog'
import HelpLink from '../components/HelpLink'
import Icon from '../components/Icon'
import Switch from '../components/Switch'
import { plural, t } from '../i18n'
import { useOwned } from '../useOwned'
import { ForYouRail } from './DiscoverExtras'
import AutoStream from './AutoStream'
import RateButton from './RateButton'
import type { TasteTag } from './useTaste'
import { setVote, type VoteKind } from './useVotes'
import './news.css'
const AddonCard = lazy(() => import('./ExtrasSettings').then((m) => ({ default: m.AddonCard }))) // Usherr: the Stremio and Nuvio link

const Diary = lazy(() => import('./Diary'))
const Wrapped = lazy(() => import('./Wrapped'))

type YouTab = 'taste' | 'diary' | 'wrapped'
const TABS: { key: YouTab; to: string; icon: 'heart' | 'calendar' | 'sparkles'; label: () => string }[] = [
  { key: 'taste', to: '/you', icon: 'heart', label: () => t('Taste and votes') },
  { key: 'diary', to: '/you/diary', icon: 'calendar', label: () => t('Diary') },
  { key: 'wrapped', to: '/you/wrapped', icon: 'sparkles', label: () => t('Wrapped') },
]

interface Summary {
  ready: boolean
  liked: number
  disliked: number
  watched: number
  likes: TasteTag[]
  dislikes: TasteTag[]
}
interface YouData {
  name: string
  share: boolean
  movie: Summary
  tv: Summary
  votes: Record<string, { liked?: number; disliked?: number }>
  following: number
  recipes: number
  services: number
}
interface Vote {
  kind: VoteKind
  ref: string
  vote: number
  title?: string
  year?: number
  image?: string
  to?: string
}
interface Person {
  name: string
  inCommon: number
  both: string[]
  likes: TasteTag[]
}

async function getJSON<T>(path: string, init?: RequestInit): Promise<T | null> {
  try {
    const r = await fetch(path, { credentials: 'include', ...init })
    return r.ok ? ((await r.json()) as T) : null
  } catch {
    return null
  }
}

const KINDS: { key: VoteKind | ''; label: () => string }[] = [
  { key: '', label: () => t('Everything') },
  { key: 'movie', label: () => t('Films') },
  { key: 'tv', label: () => t('Shows') },
  { key: 'artist', label: () => t('Artists') },
  { key: 'album', label: () => t('Albums') },
  { key: 'book', label: () => t('Books') },
  { key: 'comic', label: () => t('Comics') },
]

// The traits of a taste, in groups a person reads easily.
function TasteGroups({ tags }: { tags: TasteTag[] }) {
  const groups: { label: string; list: string[] }[] = [
    { label: t('People'), list: tags.filter((x) => x.type === 'maker' || x.type === 'cast').map((x) => x.name) },
    { label: t('Subjects'), list: tags.filter((x) => x.type === 'keyword').map((x) => x.name) },
    { label: t('Genres'), list: tags.filter((x) => x.type === 'genre').map((x) => x.name) },
  ].filter((g) => g.list.length > 0)
  return (
    <>
      {groups.map((g) => (
        <div key={g.label} className="you-group">
          <span className="room-label">{g.label}</span>
          <div className="chip-row">
            {g.list.map((n) => (
              <span key={n} className="chip">
                {n}
              </span>
            ))}
          </div>
        </div>
      ))}
    </>
  )
}

function TasteCard({ title, s }: { title: string; s: Summary }) {
  return (
    <section className="card you-card">
      <h2>{title}</h2>
      <p className="hint">{t('From {liked} liked, {disliked} not for you and {watched} watched.', { liked: s.liked, disliked: s.disliked, watched: s.watched })}</p>
      {s.ready ? (
        <>
          <h3>{t('You lean towards')}</h3>
          <TasteGroups tags={s.likes} />
          {s.dislikes.length > 0 && (
            <>
              <h3>{t('Not so much')}</h3>
              <TasteGroups tags={s.dislikes} />
            </>
          )}
        </>
      ) : (
        <p>{t('Not enough to go on yet. Like a few titles, rate 20, or watch a few in Jellyfin.')}</p>
      )}
    </section>
  )
}

export default function You({ tab = 'taste' }: { tab?: YouTab }) {
  const owned = useOwned()
  const [adding, setAdding] = useState<AddTarget | null>(null)
  const [data, setData] = useState<YouData | null>(null)
  const [votes, setVotes] = useState<Vote[] | null>(null)
  const [kind, setKind] = useState<VoteKind | ''>('')
  const [which, setWhich] = useState<0 | 1 | -1>(0)
  const [household, setHousehold] = useState<Person[]>([])
  const [error, setError] = useState('')

  useEffect(() => {
    void getJSON<YouData>('/api/home/you').then((d) => (d ? setData(d) : setError(t('Your page couldn’t be loaded.'))))
    void getJSON<{ votes: Vote[] }>('/api/home/you/votes').then((d) => setVotes(d?.votes ?? []))
  }, [])
  useEffect(() => {
    if (!data?.share) {
      setHousehold([])
      return
    }
    void getJSON<{ people: Person[] }>('/api/home/you/household').then((d) => setHousehold(d?.people ?? []))
  }, [data?.share])

  const share = async (on: boolean) => {
    if (!data) return
    setData({ ...data, share: on })
    const d = await getJSON<{ share: boolean }>('/api/home/you/share', { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ share: on }) })
    if (!d) setData({ ...data, share: !on })
  }

  const shown = useMemo(() => (votes ?? []).filter((v) => (!kind || v.kind === kind) && (!which || v.vote === which)), [votes, kind, which])
  const unvote = (v: Vote) => {
    setVote(v.kind, v.ref, 0)
    setVotes((list) => (list ?? []).filter((x) => !(x.kind === v.kind && x.ref === v.ref)))
  }

  const liked = Object.values(data?.votes ?? {}).reduce((n, v) => n + (v.liked ?? 0), 0)
  const disliked = Object.values(data?.votes ?? {}).reduce((n, v) => n + (v.disliked ?? 0), 0)

  return (
    <div className="page-pad you-page">
      <h1 className="sr-only">{t('You')}</h1>
      {error && <p className="error-text">{error}</p>}
      {data && (
        <>
          <section className="card you-head">
            <div className="you-avatar" aria-hidden="true">
              {(data.name || '?').slice(0, 1).toUpperCase()}
            </div>
            <div className="you-name">
              <h2>{data.name}</h2>
              <p className="hint">
                {plural(liked, 'One title liked', '{n} titles liked')} · {plural(disliked, 'one not for you', '{n} not for you')} ·{' '}
                {plural(data.following, 'following one person', 'following {n} people')}
              </p>
            </div>
            <div className="you-actions">
              <RateButton />
              <HelpLink topic="home-finding#you" />
            </div>
          </section>
          <nav className="room-row you-tabs" aria-label={t('Your page')}>
            {TABS.map((x) => (
              <Link key={x.key} to={x.to} className={`room-chip${tab === x.key ? ' on' : ''}`} aria-current={tab === x.key ? 'page' : undefined}>
                <Icon name={x.icon} size={14} /> {x.label()}
              </Link>
            ))}
          </nav>
          <Suspense fallback={null}>
            {tab === 'diary' && <Diary />}
            {tab === 'wrapped' && <Wrapped />}
          </Suspense>
          {tab === 'taste' && (
            <>
              <div className="you-grid">
                <TasteCard title={t('Your taste in films')} s={data.movie} />
                <TasteCard title={t('Your taste in shows')} s={data.tv} />
              </div>

              <ForYouRail kind="movie" owned={owned} onAdd={setAdding} invite={false} />
              <ForYouRail kind="tv" owned={owned} onAdd={setAdding} invite={false} />
              <AutoStream />
              {/* Usherr: each person's own add-on link for Stremio and Nuvio (members can't open Extras) */}
              <Suspense fallback={null}>
                <AddonCard />
              </Suspense>

              <section className="card you-card">
                <h2>{t('Your votes')}</h2>
                <div className="room-row" role="group" aria-label={t('Kind')}>
                  {KINDS.map((k) => (
                    <button key={k.key || 'all'} type="button" className={`room-chip${kind === k.key ? ' on' : ''}`} aria-pressed={kind === k.key} onClick={() => setKind(k.key)}>
                      {k.label()}
                    </button>
                  ))}
                  <span className="room-sep" />
                  <button type="button" className={`room-chip${which === 1 ? ' on' : ''}`} aria-pressed={which === 1} onClick={() => setWhich(which === 1 ? 0 : 1)}>
                    <Icon name="thumb-up" size={13} /> {t('Liked')}
                  </button>
                  <button type="button" className={`room-chip${which === -1 ? ' on' : ''}`} aria-pressed={which === -1} onClick={() => setWhich(which === -1 ? 0 : -1)}>
                    <Icon name="thumb-down" size={13} /> {t('Not for me')}
                  </button>
                </div>
                {votes === null ? (
                  <p className="hint">{t('Loading…')}</p>
                ) : shown.length === 0 ? (
                  <p className="hint">
                    {votes.length === 0 ? t('Nothing yet. Thumbs up and down are on every title’s page, on Discover’s posters and in the Library.') : t('Nothing here with these choices.')}
                  </p>
                ) : (
                  <ul className="you-votes">
                    {shown.map((v) => {
                      const face = (
                        <>
                          {v.image ? (
                            <img src={v.image} alt="" loading="lazy" />
                          ) : (
                            <span className="you-noimage">
                              <Icon name={v.kind === 'tv' ? 'tv' : v.kind === 'movie' ? 'film' : v.kind === 'book' ? 'book' : 'star'} size={22} />
                            </span>
                          )}
                          <span className="you-vote-title">{v.title || v.ref}</span>
                          {v.year ? <span className="hint">{v.year}</span> : null}
                        </>
                      )
                      return (
                        <li key={`${v.kind}:${v.ref}`}>
                          {v.to ? <Link to={v.to}>{face}</Link> : <span>{face}</span>}
                          <button
                            type="button"
                            className={`you-vote-mark ${v.vote > 0 ? 'up' : 'down'}`}
                            onClick={() => unvote(v)}
                            title={v.vote > 0 ? t('You like this (tap to undo)') : t('Not for you (tap to undo)')}
                            aria-label={v.vote > 0 ? t('You like this (tap to undo)') : t('Not for you (tap to undo)')}
                          >
                            <Icon name={v.vote > 0 ? 'thumb-up' : 'thumb-down'} size={13} />
                          </button>
                        </li>
                      )
                    })}
                  </ul>
                )}
              </section>

              <div className="you-grid you-links">
                <Link to="/following" className="card you-link">
                  <Icon name="user" size={18} />
                  <span>
                    <strong>{t('Following')}</strong>
                    <span className="hint">{plural(data.following, 'One person', '{n} people')}</span>
                  </span>
                </Link>
                <Link to="/discover" className="card you-link">
                  <Icon name="list" size={18} />
                  <span>
                    <strong>{t('Your recipes')}</strong>
                    <span className="hint">{plural(data.recipes, 'One recipe', '{n} recipes')}</span>
                  </span>
                </Link>
                <Link to="/discover" className="card you-link">
                  <Icon name="tv" size={18} />
                  <span>
                    <strong>{t('My services')}</strong>
                    <span className="hint">{data.services ? plural(data.services, 'One service', '{n} services') : t('None chosen')}</span>
                  </span>
                </Link>
              </div>

              <section className="card you-card">
                <h2>{t('The household')}</h2>
                <span data-walk="you-share">
                  <Switch checked={data.share} onChange={(v) => void share(v)} label={t('Share my taste with the household')} />
                </span>
                <p className="hint">{t('Others who share theirs see what you lean towards and how much taste you have in common. Your votes stay yours. To see theirs, share yours.')}</p>
                {data.share &&
                  (household.length === 0 ? (
                    <p className="hint">{t('Nobody else shares their taste yet.')}</p>
                  ) : (
                    <ul className="you-household">
                      {household.map((p) => (
                        <li key={p.name}>
                          <div className="you-person-head">
                            <strong>{p.name}</strong>
                            <span className="taste-match good">{t('{n}% in common', { n: p.inCommon })}</span>
                          </div>
                          {p.both.length > 0 && <p>{t('You both like {list}', { list: p.both.join(', ') })}</p>}
                          {p.likes.length > 0 && (
                            <p className="hint">
                              {t('{name} leans towards {list}', {
                                name: p.name,
                                list: p.likes
                                  .map((l) => l.name)
                                  .slice(0, 6)
                                  .join(', '),
                              })}
                            </p>
                          )}
                        </li>
                      ))}
                    </ul>
                  ))}
              </section>
            </>
          )}
        </>
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
