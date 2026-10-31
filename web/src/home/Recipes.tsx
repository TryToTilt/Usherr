// Home fork: Discover recipes (after Reciperr). A recipe is a saved set of
// filters with a name; each one is its own Discover row that keeps itself up
// to date. Each person keeps their own and can share one with the household.
// "On my services" keeps to the streaming services you pay for.
import RateButton from './RateButton'
import RouletteButton from './RouletteButton'
import HelpLink from '../components/HelpLink'
import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { createPortal } from 'react-dom'
import { api } from '../api'
import type { AddTarget } from '../components/AddDialog'
import DiscoverRail from '../components/DiscoverRail'
import Icon from '../components/Icon'
import { useToast } from '../components/Toast'
import { sourceToQuery, type Source } from '../discoverSources'
import { t } from '../i18n'
import { useFocusTrap } from '../useFocusTrap'
import type { Owned } from '../useOwned'
import './news.css'

export type RecipeTab = 'movie' | 'tv' | 'anime'

export interface RecipeFilters {
  genres?: number[]
  allGenres?: boolean
  excludeGenres?: number[]
  yearFrom?: number
  yearTo?: number
  lastMonths?: number
  minRating?: number
  minVotes?: number
  maxVotes?: number // little known: at most this many votes
  runtimeMin?: number
  runtimeMax?: number
  language?: string // "!en": anything but English
  people?: { id: number; name: string }[]
  certs?: string[]
  keywords?: { id: number; name: string }[]
  myServices?: boolean
  anime?: boolean
  sort?: '' | 'popular' | 'rating' | 'newest' | 'votes' | 'revenue'
  minImdb?: number
  minTomatoes?: number
  minMetacritic?: number
  maxAge?: number
}

interface Recipe {
  id: number
  kind: RecipeTab
  name: string
  filters: RecipeFilters
  shared: boolean
  mine: boolean
  owner?: string
}

interface Provider {
  id: number
  name: string
  logo?: string
}

async function call<T>(method: string, path: string, body?: unknown): Promise<T> {
  const r = await fetch(`/api/home${path}`, { method, credentials: 'include', headers: body ? { 'Content-Type': 'application/json' } : undefined, body: body ? JSON.stringify(body) : undefined })
  const out = await r.json().catch(() => ({}))
  if (!r.ok) throw new Error((out as { error?: string }).error || t('Something went wrong.'))
  return out as T
}

export function recipeSource(tab: RecipeTab, name: string, filters: RecipeFilters): Source {
  return { kind: tab === 'movie' ? 'movie' : 'tv', list: 'recipe', name, filters: JSON.stringify(tab === 'anime' ? { ...filters, anime: true } : filters) }
}

const LANGUAGES: [string, string][] = [
  ['en', t('English')],
  ['ko', t('Korean')],
  ['ja', t('Japanese')],
  ['fr', t('French')],
  ['es', t('Spanish')],
  ['de', t('German')],
  ['it', t('Italian')],
  ['hi', t('Hindi')],
  ['zh', t('Chinese')],
  ['cn', t('Cantonese')],
  ['sv', t('Swedish')],
  ['da', t('Danish')],
  ['no', t('Norwegian')],
  ['pt', t('Portuguese')],
  ['tr', t('Turkish')],
  ['th', t('Thai')],
]

const CERTS = ['G', 'PG', 'PG-13', 'R']

// Ideas to start a recipe from.
const IDEAS: Record<RecipeTab, { name: string; filters: RecipeFilters }[]> = {
  movie: [
    { name: t('Acclaimed new releases'), filters: { lastMonths: 12, minRating: 7.3, minVotes: 300, sort: 'rating' } },
    { name: t('Family night'), filters: { genres: [10751], certs: ['G', 'PG'], minRating: 6.8, minVotes: 300, maxAge: 10 } },
    { name: t('Critics’ favourites'), filters: { minTomatoes: 90, minImdb: 7.5, minVotes: 1000, sort: 'rating' } },
    { name: t('Korean thrillers'), filters: { language: 'ko', genres: [53], runtimeMax: 120, minRating: 7, minVotes: 100 } },
    { name: t('Quick comedies'), filters: { genres: [35], runtimeMax: 95, minRating: 6.8, minVotes: 300 } },
    { name: t('Best on my services'), filters: { myServices: true, minRating: 7.5, minVotes: 1000, sort: 'rating' } },
    { name: t('Mind-bending sci-fi'), filters: { genres: [878, 9648], allGenres: true, minRating: 7, minVotes: 300 } },
  ],
  tv: [
    { name: t('Acclaimed new shows'), filters: { lastMonths: 18, minRating: 7.8, minVotes: 150, sort: 'rating' } },
    { name: t('Comfort sitcoms'), filters: { genres: [35], minRating: 7.5, minVotes: 500, sort: 'rating' } },
    { name: t('Crime dramas on my services'), filters: { genres: [80], myServices: true, minRating: 7.5, minVotes: 200 } },
    { name: t('Korean dramas'), filters: { language: 'ko', genres: [18], minRating: 7.8, minVotes: 100 } },
  ],
  anime: [
    { name: t('Top action anime'), filters: { genres: [10759], minRating: 8, minVotes: 300, sort: 'rating' } },
    { name: t('Recent hits'), filters: { lastMonths: 24, minRating: 7.8, minVotes: 100 } },
    { name: t('Anime on my services'), filters: { myServices: true, minRating: 7.5, minVotes: 100 } },
  ],
}

// Ready-made rows: built-in recipes everyone gets, each with Copy to mine
// (to change it) and Hide (per person). Keys are stable: hiding is kept by key.
const READY: Record<RecipeTab, { key: string; name: string; hint: string; filters: RecipeFilters }[]> = {
  movie: [
    { key: 'top', name: t('Top rated'), hint: t('the best-loved films of all time'), filters: { minRating: 7.5, minVotes: 5000, sort: 'rating' } },
    { key: 'box', name: t('Box office'), hint: t('the biggest earners of the last year'), filters: { lastMonths: 12, sort: 'revenue' } },
    { key: 'gems', name: t('Hidden gems'), hint: t('loved by the few who found them'), filters: { minRating: 7.3, minVotes: 100, maxVotes: 1500, sort: 'rating' } },
    { key: 'world', name: t('World cinema'), hint: t('acclaimed films not in English'), filters: { language: '!en', minRating: 7.3, minVotes: 1000 } },
  ],
  tv: [
    { key: 'top', name: t('Top rated'), hint: t('the best-loved shows of all time'), filters: { minRating: 8, minVotes: 1000, sort: 'rating', excludeGenres: [16] } },
    { key: 'gems', name: t('Hidden gems'), hint: t('loved by the few who found them'), filters: { minRating: 7.8, minVotes: 100, maxVotes: 500, sort: 'rating', excludeGenres: [16] } },
    { key: 'world', name: t('International shows'), hint: t('acclaimed shows not in English'), filters: { language: '!en', minRating: 7.8, minVotes: 300, excludeGenres: [16] } },
  ],
  anime: [
    { key: 'top', name: t('Top rated'), hint: t('the best-loved anime of all time'), filters: { minRating: 8.3, minVotes: 1000, sort: 'rating' } },
    { key: 'gems', name: t('Hidden gems'), hint: t('loved by the few who found them'), filters: { minRating: 7.8, minVotes: 100, maxVotes: 500, sort: 'rating' } },
  ],
}

// ---- the rows on Discover ----

export default function RecipeRails({ tab, owned, onAdd }: { tab: RecipeTab; owned: Owned; onAdd: (t: AddTarget) => void }) {
  const toast = useToast()
  const [recipes, setRecipes] = useState<Recipe[] | null>(null)
  const [services, setServices] = useState<number[] | null>(null)
  const [scoresOn, setScoresOn] = useState(false)
  const [editing, setEditing] = useState<Recipe | 'new' | null>(null)
  const [picking, setPicking] = useState(false)
  const [hidden, setHidden] = useState<string[] | null>(null)
  useEffect(() => {
    call<{ hidden: string[] }>('GET', '/ready-rows')
      .then((d) => setHidden(d.hidden))
      .catch(() => setHidden([]))
  }, [])
  const hide = (body: { key?: string; hidden?: boolean; showAll?: boolean }) =>
    call<{ hidden: string[] }>('PUT', '/ready-rows', body)
      .then((d) => setHidden(d.hidden))
      .catch((e: Error) => toast.error(e.message))
  const copyReady = async (name: string, filters: RecipeFilters) => {
    try {
      await call('POST', '/recipes', { kind: tab, name, filters })
      toast.success(t('Copied to your recipes.'))
      load()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    }
  }
  const load = useCallback(() => {
    call<Recipe[]>('GET', `/recipes?kind=${tab}`).then(setRecipes).catch(() => setRecipes([]))
    call<{ mine: number[]; scoresOn?: boolean }>('GET', '/providers')
      .then((p) => {
        setServices(p.mine ?? [])
        setScoresOn(!!p.scoresOn)
      })
      .catch(() => setServices([]))
  }, [tab])
  useEffect(load, [load])

  const remove = async (r: Recipe) => {
    if (!window.confirm(t('Remove the recipe “{name}”?', { name: r.name }))) return
    try {
      await call('DELETE', `/recipes/${r.id}`)
      load()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    }
  }
  const copy = async (r: Recipe) => {
    try {
      await call('POST', `/recipes/${r.id}/copy`)
      toast.success(t('Copied to your recipes.'))
      load()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    }
  }

  const servicesRail = services && services.length > 0 ? recipeSource(tab, tab === 'movie' ? t('On your services: movies') : tab === 'tv' ? t('On your services: shows') : t('On your services: anime'), { myServices: true, minVotes: 50 }) : null

  return (
    <>
      <div className="recipes-bar" data-walk="recipes-bar">
        <span className="recipes-title">
          <Icon name="list" size={16} /> {t('Your recipes')} <HelpLink topic="home-finding#your-recipes" />
        </span>
        <span className="hint recipes-hint">{t('saved filters, each its own row that keeps itself up to date')}</span>
        <RouletteButton />
        <RateButton />
        <button className="btn-sm" onClick={() => setPicking(true)}>
          <Icon name="tv" size={14} /> {services && services.length > 0 ? t('My services ({n})', { n: services.length }) : t('Choose my services')}
        </button>
        {(hidden ?? []).some((k) => k.startsWith(`${tab}:`)) && (
          <button className="btn-link" onClick={() => void hide({ showAll: true })}>
            {t('Show hidden rows')}
          </button>
        )}
        <button className="btn-sm primary" onClick={() => setEditing('new')}>
          <Icon name="plus" size={14} /> {t('New recipe')}
        </button>
      </div>
      {servicesRail && <DiscoverRail key={sourceToQuery(servicesRail)} title={servicesRail.name ?? ''} hint={t('most popular first')} source={servicesRail} kind={servicesRail.kind} owned={owned} onAdd={onAdd} rows={1} moreLabel={t('Browse more')} />}
      {(recipes ?? []).map((r) => {
        const src = recipeSource(tab, r.name, r.filters)
        return (
          <DiscoverRail
            key={`${r.id}:${sourceToQuery(src)}`}
            title={r.name}
            hint={r.mine ? (r.shared ? t('shared with the household') : undefined) : t('from {name}', { name: r.owner ?? '' })}
            source={src}
            kind={src.kind}
            owned={owned}
            onAdd={onAdd}
            rows={1}
            moreLabel={t('Browse more')}
            headExtra={
              r.mine ? (
                <span className="recipe-actions">
                  <button className="icon-btn" onClick={() => setEditing(r)} aria-label={t('Change the recipe {name}', { name: r.name })} title={t('Change')}>
                    <Icon name="edit" size={14} />
                  </button>
                  <button className="icon-btn" onClick={() => void remove(r)} aria-label={t('Remove the recipe {name}', { name: r.name })} title={t('Remove')}>
                    <Icon name="trash" size={14} />
                  </button>
                </span>
              ) : (
                <button className="btn-sm" onClick={() => void copy(r)}>
                  {t('Copy to mine')}
                </button>
              )
            }
          />
        )
      })}
      {hidden &&
        READY[tab]
          .filter((r) => !hidden.includes(`${tab}:${r.key}`))
          .map((r) => {
            const src = recipeSource(tab, r.name, r.filters)
            return (
              <DiscoverRail
                key={`ready-${r.key}:${sourceToQuery(src)}`}
                title={r.name}
                hint={r.hint}
                source={src}
                kind={src.kind}
                owned={owned}
                onAdd={onAdd}
                rows={1}
                moreLabel={t('Browse more')}
                headExtra={
                  <span className="recipe-actions">
                    <button className="btn-sm" onClick={() => void copyReady(r.name, r.filters)} title={t('Make your own copy to change')}>
                      {t('Copy to mine')}
                    </button>
                    <button className="icon-btn" onClick={() => void hide({ key: `${tab}:${r.key}`, hidden: true })} aria-label={t('Hide the row {name}', { name: r.name })} title={t('Hide')}>
                      <Icon name="eye-off" size={14} />
                    </button>
                  </span>
                }
              />
            )
          })}
      {editing && (
        <RecipeEditor
          tab={tab}
          recipe={editing === 'new' ? null : editing}
          hasServices={(services ?? []).length > 0}
          scoresOn={scoresOn}
          owned={owned}
          onAdd={onAdd}
          onClose={() => setEditing(null)}
          onSaved={() => {
            setEditing(null)
            load()
          }}
        />
      )}
      {picking && (
        <ServicesPicker
          onClose={() => setPicking(false)}
          onSaved={(ids) => {
            setServices(ids)
            setPicking(false)
          }}
        />
      )}
    </>
  )
}

// ---- a dialog ----

function Dialog({ title, onClose, children, wide }: { title: string; onClose: () => void; children: ReactNode; wide?: boolean }) {
  const box = useRef<HTMLDivElement>(null)
  useFocusTrap(box)
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && onClose()
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose])
  return createPortal(
    <div className="modal-backdrop" onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      <div ref={box} tabIndex={-1} className={`modal recipe-dialog${wide ? ' recipe-wide' : ''}`} role="dialog" aria-modal="true" aria-label={title}>
        <div className="recipe-dialog-head">
          <h2>{title}</h2>
          <button className="icon-btn" onClick={onClose} aria-label={t('Close')}>
            <Icon name="x" size={18} />
          </button>
        </div>
        {children}
      </div>
    </div>,
    document.body,
  )
}

// ---- the services you pay for ----

function ServicesPicker({ onClose, onSaved }: { onClose: () => void; onSaved: (ids: number[]) => void }) {
  const toast = useToast()
  const [list, setList] = useState<Provider[] | null>(null)
  const [mine, setMine] = useState<Set<number>>(new Set())
  const [q, setQ] = useState('')
  const [busy, setBusy] = useState(false)
  useEffect(() => {
    call<{ providers: Provider[]; mine: number[] }>('GET', '/providers')
      .then((p) => {
        setList(p.providers)
        setMine(new Set(p.mine))
      })
      .catch((e: Error) => toast.error(e.message))
  }, [toast])
  const shown = useMemo(() => {
    const n = q.trim().toLowerCase()
    const all = list ?? []
    return (n ? all.filter((p) => p.name.toLowerCase().includes(n)) : all.slice(0, 60)).sort((a, b) => Number(mine.has(b.id)) - Number(mine.has(a.id)))
  }, [list, q, mine])
  const save = async () => {
    setBusy(true)
    try {
      await call('PUT', '/my-services', { providers: [...mine] })
      onSaved([...mine])
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
      setBusy(false)
    }
  }
  return (
    <Dialog title={t('The services you pay for')} onClose={onClose} wide>
      <p className="hint">{t('Discover can keep to what you can watch now. Only you see your choice.')}</p>
      <input type="search" value={q} onChange={(e) => setQ(e.target.value)} placeholder={t('Find a service')} aria-label={t('Find a service')} />
      <div className="provider-grid">
        {list === null
          ? Array.from({ length: 12 }, (_, i) => <div key={i} className="skeleton provider-skel" />)
          : shown.map((p) => (
              <label key={p.id} className={`provider${mine.has(p.id) ? ' on' : ''}`}>
                <input
                  type="checkbox"
                  checked={mine.has(p.id)}
                  onChange={(e) =>
                    setMine((m) => {
                      const n = new Set(m)
                      if (e.target.checked) n.add(p.id)
                      else n.delete(p.id)
                      return n
                    })
                  }
                />
                {p.logo ? <img src={p.logo} alt="" loading="lazy" /> : <span className="provider-noimg" />}
                <span>{p.name}</span>
              </label>
            ))}
      </div>
      <div className="recipe-foot">
        <span className="hint">{t('{n} chosen', { n: mine.size })}</span>
        <button className="primary" disabled={busy} onClick={() => void save()}>
          {t('Save')}
        </button>
      </div>
    </Dialog>
  )
}

// ---- making and changing a recipe ----

function useDebounced<T>(value: T, ms: number): T {
  const [v, setV] = useState(value)
  useEffect(() => {
    const id = window.setTimeout(() => setV(value), ms)
    return () => window.clearTimeout(id)
  }, [value, ms])
  return v
}

function NamedPicker({ label, path, value, onChange }: { label: string; path: 'people' | 'keywords'; value: { id: number; name: string }[]; onChange: (v: { id: number; name: string }[]) => void }) {
  const [q, setQ] = useState('')
  const [found, setFound] = useState<{ id: number; name: string; known?: string }[]>([])
  const dq = useDebounced(q, 300)
  useEffect(() => {
    if (dq.trim().length < 2) {
      setFound([])
      return
    }
    call<{ id: number; name: string; known?: string }[]>('GET', `/search/${path}?q=${encodeURIComponent(dq.trim())}`)
      .then(setFound)
      .catch(() => setFound([]))
  }, [dq, path])
  return (
    <div className="recipe-field">
      <span className="recipe-label">{label}</span>
      <div className="chip-row">
        {value.map((v) => (
          <button key={v.id} className="chip active" onClick={() => onChange(value.filter((x) => x.id !== v.id))} aria-label={t('Remove {name}', { name: v.name })}>
            {v.name} <Icon name="x" size={12} />
          </button>
        ))}
      </div>
      <input type="search" value={q} onChange={(e) => setQ(e.target.value)} placeholder={path === 'people' ? t('Type a name: Denis Villeneuve') : t('Type a word: heist, time travel')} aria-label={label} />
      {found.length > 0 && (
        <div className="chip-row recipe-found">
          {found
            .filter((f) => !value.some((v) => v.id === f.id))
            .map((f) => (
              <button
                key={f.id}
                className="chip"
                onClick={() => {
                  onChange([...value, { id: f.id, name: f.name }])
                  setQ('')
                  setFound([])
                }}
              >
                <Icon name="plus" size={12} /> {f.name}
                {f.known ? <small> · {f.known}</small> : null}
              </button>
            ))}
        </div>
      )}
    </div>
  )
}

function num(v: string): number | undefined {
  const n = Number(v)
  return v.trim() !== '' && Number.isFinite(n) && n > 0 ? n : undefined
}

function RecipeEditor({ tab, recipe, hasServices, scoresOn, owned, onAdd, onClose, onSaved }: { tab: RecipeTab; recipe: Recipe | null; hasServices: boolean; scoresOn: boolean; owned: Owned; onAdd: (t: AddTarget) => void; onClose: () => void; onSaved: () => void }) {
  const toast = useToast()
  const [name, setName] = useState(recipe?.name ?? '')
  const [f, setF] = useState<RecipeFilters>(recipe?.filters ?? {})
  const [shared, setShared] = useState(recipe?.shared ?? false)
  const [genres, setGenres] = useState<{ id: number; name: string }[]>([])
  const [busy, setBusy] = useState(false)
  const movie = tab === 'movie'
  useEffect(() => {
    api
      .discoverGenres(movie ? 'movie' : 'tv')
      .then(setGenres)
      .catch(() => setGenres([]))
  }, [movie])
  const patch = (p: Partial<RecipeFilters>) => setF((x) => ({ ...x, ...p }))
  const toggle = (key: 'genres' | 'excludeGenres' | 'certs', v: number | string) =>
    setF((x) => {
      const list = (x[key] ?? []) as (number | string)[]
      const next = list.includes(v) ? list.filter((y) => y !== v) : [...list, v]
      return { ...x, [key]: next.length ? next : undefined }
    })
  const preview = useDebounced(f, 500)
  const src = recipeSource(tab, name || t('Preview'), preview)
  const recent = (f.lastMonths ?? 0) > 0

  const save = async () => {
    setBusy(true)
    try {
      const body = { kind: tab, name: name.trim(), filters: f, shared }
      if (recipe) await call('PUT', `/recipes/${recipe.id}`, body)
      else await call('POST', '/recipes', body)
      toast.success(recipe ? t('Recipe saved.') : t('Recipe added. It’s a row on Discover now.'))
      onSaved()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
      setBusy(false)
    }
  }

  return (
    <Dialog title={recipe ? t('Change the recipe') : t('New recipe')} onClose={onClose} wide>
      {!recipe && (
        <div className="recipe-field">
          <span className="recipe-label">{t('Start from an idea')}</span>
          <div className="chip-row">
            {IDEAS[tab].map((idea) => (
              <button
                key={idea.name}
                className="chip"
                onClick={() => {
                  setName(idea.name)
                  setF(idea.filters)
                }}
              >
                {idea.name}
              </button>
            ))}
          </div>
        </div>
      )}
      <label className="recipe-field">
        <span className="recipe-label">{t('Name')}</span>
        <input value={name} maxLength={60} onChange={(e) => setName(e.target.value)} placeholder={t('Acclaimed new releases')} />
      </label>

      <div className="recipe-field">
        <span className="recipe-label">
          {t('Genres')}{' '}
          {(f.genres?.length ?? 0) > 1 && (
            <label className="check-inline">
              <input type="checkbox" checked={!!f.allGenres} onChange={(e) => patch({ allGenres: e.target.checked || undefined })} /> {t('all of them, not any')}
            </label>
          )}
        </span>
        <div className="chip-row">
          {genres.map((g) => (
            <button key={g.id} className={`chip${f.genres?.includes(g.id) ? ' active' : ''}${f.excludeGenres?.includes(g.id) ? ' chip-no' : ''}`} onClick={() => toggle('genres', g.id)} onContextMenu={(e) => (e.preventDefault(), toggle('excludeGenres', g.id))} title={t('Tap to include; right-click or long-press to leave out')}>
              {f.excludeGenres?.includes(g.id) ? '− ' : ''}
              {g.name}
            </button>
          ))}
        </div>
      </div>

      <div className="recipe-grid">
        <label className="recipe-field">
          <span className="recipe-label">{t('Released')}</span>
          <select
            value={recent ? `m${f.lastMonths}` : f.yearFrom || f.yearTo ? 'years' : ''}
            onChange={(e) => {
              const v = e.target.value
              if (v.startsWith('m')) patch({ lastMonths: Number(v.slice(1)), yearFrom: undefined, yearTo: undefined })
              else if (v === 'years') patch({ lastMonths: undefined, yearFrom: f.yearFrom ?? 2000 })
              else patch({ lastMonths: undefined, yearFrom: undefined, yearTo: undefined })
            }}
          >
            <option value="">{t('Any time')}</option>
            <option value="m3">{t('In the last 3 months')}</option>
            <option value="m12">{t('In the last year')}</option>
            <option value="m24">{t('In the last 2 years')}</option>
            <option value="years">{t('Between years…')}</option>
          </select>
          {!recent && (f.yearFrom || f.yearTo) ? (
            <span className="recipe-pair">
              <input type="number" min={1900} max={2100} value={f.yearFrom ?? ''} onChange={(e) => patch({ yearFrom: num(e.target.value) })} aria-label={t('From year')} placeholder={t('From')} />
              <input type="number" min={1900} max={2100} value={f.yearTo ?? ''} onChange={(e) => patch({ yearTo: num(e.target.value) })} aria-label={t('To year')} placeholder={t('To')} />
            </span>
          ) : null}
        </label>
        <label className="recipe-field">
          <span className="recipe-label">{t('Rated at least')}</span>
          <select value={f.minRating ?? ''} onChange={(e) => patch({ minRating: num(e.target.value) })}>
            <option value="">{t('Any rating')}</option>
            {[6, 6.5, 7, 7.5, 8, 8.5].map((r) => (
              <option key={r} value={r}>
                {t('{n} out of 10', { n: r })}
              </option>
            ))}
          </select>
        </label>
        <label className="recipe-field">
          <span className="recipe-label">{t('With at least')}</span>
          <select value={f.minVotes ?? ''} onChange={(e) => patch({ minVotes: num(e.target.value) })}>
            <option value="">{t('Any number of votes')}</option>
            {[100, 300, 1000, 5000, 20000].map((v) => (
              <option key={v} value={v}>
                {t('{n} votes', { n: v.toLocaleString() })}
              </option>
            ))}
          </select>
        </label>
        <label className="recipe-field">
          <span className="recipe-label">{t('Little known')}</span>
          <select value={f.maxVotes ?? ''} onChange={(e) => patch({ maxVotes: num(e.target.value) })}>
            <option value="">{t('Any number of votes')}</option>
            {[500, 1500, 5000].map((v) => (
              <option key={v} value={v}>
                {t('At most {n} votes', { n: v.toLocaleString() })}
              </option>
            ))}
          </select>
        </label>
        <label className="recipe-field">
          <span className="recipe-label">{movie ? t('Runtime') : t('Episode length')}</span>
          <select value={f.runtimeMax ?? ''} onChange={(e) => patch({ runtimeMax: num(e.target.value) })}>
            <option value="">{t('Any length')}</option>
            {(movie ? [90, 100, 120, 150] : [25, 45, 60]).map((m) => (
              <option key={m} value={m}>
                {t('Up to {n} minutes', { n: m })}
              </option>
            ))}
          </select>
        </label>
        {tab !== 'anime' && (
          <label className="recipe-field">
            <span className="recipe-label">{t('Original language')}</span>
            <select value={f.language ?? ''} onChange={(e) => patch({ language: e.target.value || undefined })}>
              <option value="">{t('Any language')}</option>
              <option value="!en">{t('Not English')}</option>
              {LANGUAGES.map(([code, label]) => (
                <option key={code} value={code}>
                  {label}
                </option>
              ))}
            </select>
          </label>
        )}
        <label className="recipe-field">
          <span className="recipe-label">{t('Order')}</span>
          <select value={f.sort ?? ''} onChange={(e) => patch({ sort: (e.target.value || undefined) as RecipeFilters['sort'] })}>
            <option value="">{t('Most popular')}</option>
            <option value="rating">{t('Best rated')}</option>
            <option value="newest">{t('Newest')}</option>
            <option value="votes">{t('Most voted')}</option>
            {movie && <option value="revenue">{t('Biggest box office')}</option>}
          </select>
        </label>
      </div>

      {scoresOn && (
        <div className="recipe-grid">
          <label className="recipe-field">
            <span className="recipe-label">{t('Rotten Tomatoes at least')}</span>
            <select value={f.minTomatoes ?? ''} onChange={(e) => patch({ minTomatoes: num(e.target.value) })}>
              <option value="">{t('Any')}</option>
              {[60, 75, 85, 90, 95].map((v) => (
                <option key={v} value={v}>
                  {v}%
                </option>
              ))}
            </select>
          </label>
          <label className="recipe-field">
            <span className="recipe-label">{t('IMDb at least')}</span>
            <select value={f.minImdb ?? ''} onChange={(e) => patch({ minImdb: num(e.target.value) })}>
              <option value="">{t('Any')}</option>
              {[6.5, 7, 7.5, 8, 8.5].map((v) => (
                <option key={v} value={v}>
                  {v}
                </option>
              ))}
            </select>
          </label>
          <label className="recipe-field">
            <span className="recipe-label">{t('Metacritic at least')}</span>
            <select value={f.minMetacritic ?? ''} onChange={(e) => patch({ minMetacritic: num(e.target.value) })}>
              <option value="">{t('Any')}</option>
              {[60, 70, 80, 90].map((v) => (
                <option key={v} value={v}>
                  {v}
                </option>
              ))}
            </select>
          </label>
          <label className="recipe-field">
            <span className="recipe-label">{t('Common Sense age, at most')}</span>
            <select value={f.maxAge ?? ''} onChange={(e) => patch({ maxAge: num(e.target.value) })}>
              <option value="">{t('Any age')}</option>
              {[5, 8, 10, 12, 13, 15].map((v) => (
                <option key={v} value={v}>
                  {t('age {n}+', { n: v })}
                </option>
              ))}
            </select>
          </label>
        </div>
      )}
      {movie && (
        <div className="recipe-field">
          <span className="recipe-label">{t('Age rating (US)')}</span>
          <div className="chip-row">
            {CERTS.map((c) => (
              <button key={c} className={`chip${f.certs?.includes(c) ? ' active' : ''}`} onClick={() => toggle('certs', c)}>
                {c}
              </button>
            ))}
          </div>
        </div>
      )}
      {movie && <NamedPicker label={t('With these people (cast or crew)')} path="people" value={f.people ?? []} onChange={(v) => patch({ people: v.length ? v : undefined })} />}
      <NamedPicker label={t('About')} path="keywords" value={f.keywords ?? []} onChange={(v) => patch({ keywords: v.length ? v : undefined })} />

      <div className="recipe-checks">
        <label className="check-inline">
          <input type="checkbox" checked={!!f.myServices} onChange={(e) => patch({ myServices: e.target.checked || undefined })} disabled={!hasServices && !f.myServices} /> {t('Only on my services')}
          {!hasServices && <small className="hint"> {t('(choose your services first)')}</small>}
        </label>
        <label className="check-inline">
          <input type="checkbox" checked={shared} onChange={(e) => setShared(e.target.checked)} /> {t('Share with the household')}
        </label>
      </div>

      <div className="recipe-preview">
        <DiscoverRail key={sourceToQuery(src)} title={t('Preview')} source={src} kind={src.kind} owned={owned} onAdd={onAdd} rows={1} scroll />
      </div>

      <div className="recipe-foot">
        <button onClick={onClose}>{t('Cancel')}</button>
        <button className="primary" disabled={busy || !name.trim()} onClick={() => void save()}>
          {recipe ? t('Save') : t('Add recipe')}
        </button>
      </div>
    </Dialog>
  )
}

// ---- where a title streams (title pages) ----

export function StreamingOn({ kind, tmdbId }: { kind: 'movie' | 'tv'; tmdbId: number }) {
  const [data, setData] = useState<{ providers: Provider[]; mine: number[]; link?: string } | null>(null)
  useEffect(() => {
    call<{ providers: Provider[]; mine: number[]; link?: string }>('GET', `/watch/${kind}/${tmdbId}`)
      .then(setData)
      .catch(() => setData(null))
  }, [kind, tmdbId])
  if (!data || data.providers.length === 0) return null
  const mine = new Set(data.mine)
  const list = [...data.providers].sort((a, b) => Number(mine.has(b.id)) - Number(mine.has(a.id)))
  return (
    <div className="streaming-on">
      <span className="hint">{t('Streaming on')}</span>
      {list.slice(0, 8).map((p) => (
        <span key={p.id} className={`streaming-chip${mine.has(p.id) ? ' mine' : ''}`} title={mine.has(p.id) ? t('{name}: one of your services', { name: p.name }) : p.name}>
          {p.logo ? <img src={p.logo} alt="" /> : null}
          {p.name}
          {mine.has(p.id) && <Icon name="check" size={12} />}
        </span>
      ))}
      <small className="hint">{t('from JustWatch')}</small>
    </div>
  )
}
