// Home fork: "Complete your library", after CinePlete. What's missing from the
// film series you have part of, from the directors and actors you collect,
// the acclaimed classics, and what your own films recommend most, as rows
// that scroll sideways; with scores for how complete things are.
import HelpLink from '../components/HelpLink'
import { useCallback, useEffect, useMemo, useState, type ReactNode } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { isAdmin, type DiscoverMovie } from '../api'
import { useAuth } from '../AuthContext'
import AddDialog, { type AddTarget } from '../components/AddDialog'
import { DiscoverCards } from '../components/DiscoverRail'
import Icon from '../components/Icon'
import { useToast } from '../components/Toast'
import { plural, t } from '../i18n'
import { useOwned } from '../useOwned'
import Shelves, { type Shelf } from './Shelves'
import './news.css'

interface GapFilm {
  tmdbId: number
  title: string
  year: number
  posterUrl?: string
  rating?: number
  voteCount?: number
  releaseDate?: string
  matches?: number
}

interface GapGroup {
  id: number
  name: string
  have: number
  total: number
  missing: GapFilm[]
}

interface Score {
  have: number
  total: number
  pct: number
}

interface GapsResult {
  builtAt: string
  owned: number
  scores: { franchises: Score; directors: Score; classics: Score; global: number }
  franchises: GapGroup[]
  directors: GapGroup[]
  actors: GapGroup[]
  classics: GapFilm[]
  suggestions: GapFilm[]
}

interface Ignore {
  kind: 'franchise' | 'director' | 'actor'
  id: number
  name: string
}

interface ScanStatus {
  running: boolean
  step: number
  steps: number
  label: string
  done: number
  of: number
}

type Tab = 'franchises' | 'directors' | 'actors' | 'classics' | 'suggestions' | 'ignored'
const TABS: Tab[] = ['franchises', 'directors', 'actors', 'classics', 'suggestions', 'ignored']

const asMovie = (f: GapFilm): DiscoverMovie => ({ tmdbId: f.tmdbId, title: f.title, year: f.year, posterUrl: f.posterUrl, rating: f.rating, voteCount: f.voteCount, releaseDate: f.releaseDate, mediaType: 'movie' })

async function call<T>(method: string, path: string, body?: unknown): Promise<T> {
  const r = await fetch(`/api/home${path}`, { method, credentials: 'include', headers: body ? { 'Content-Type': 'application/json' } : undefined, body: body ? JSON.stringify(body) : undefined })
  const out = await r.json().catch(() => ({}))
  if (!r.ok) throw new Error((out as { error?: string }).error || t('Something went wrong.'))
  return out as T
}

export function useGaps() {
  const [data, setData] = useState<{ result: GapsResult | null; ignored: Ignore[]; scan: ScanStatus } | null>(null)
  const load = useCallback(() => {
    call<{ result: GapsResult | null; ignored: Ignore[]; scan: ScanStatus }>('GET', '/gaps').then(setData).catch(() => undefined)
  }, [])
  useEffect(load, [load])
  // While a scan runs, look again every few seconds.
  useEffect(() => {
    if (!data?.scan.running) return
    const id = window.setTimeout(load, 2500)
    return () => window.clearTimeout(id)
  }, [data, load])
  return { data, setData, load }
}

function Ring({ pct, label, sub }: { pct: number; label: string; sub?: string }) {
  return (
    <div className="gap-score">
      <div className="gap-ring" style={{ ['--p' as string]: `${pct}` }}>
        <span>{pct}%</span>
      </div>
      <div>
        <strong>{label}</strong>
        {sub && <small className="hint">{sub}</small>}
      </div>
    </div>
  )
}

export default function Complete() {
  const admin = isAdmin(useAuth().user)
  const toast = useToast()
  const owned = useOwned()
  const [adding, setAdding] = useState<AddTarget | null>(null)
  const [params, setParams] = useSearchParams()
  const tab: Tab = TABS.find((x) => x === params.get('tab')) ?? 'franchises'
  const setTab = (v: Tab) => setParams((p) => (p.set('tab', v), p), { replace: true })
  const [q, setQ] = useState('')
  const { data, setData, load } = useGaps()
  const res = data?.result ?? null
  const scan = data?.scan

  const ignore = async (kind: Ignore['kind'], g: GapGroup, undo = false) => {
    try {
      const r = await call<{ ignored: Ignore[] }>('POST', '/gaps/ignore', { kind, id: g.id, name: g.name, undo })
      if (undo) load()
      else
        setData((d) =>
          d && d.result
            ? {
                ...d,
                ignored: r.ignored,
                result: {
                  ...d.result,
                  franchises: kind === 'franchise' ? d.result.franchises.filter((x) => x.id !== g.id) : d.result.franchises,
                  directors: kind === 'director' ? d.result.directors.filter((x) => x.id !== g.id) : d.result.directors,
                  actors: kind === 'actor' ? d.result.actors.filter((x) => x.id !== g.id) : d.result.actors,
                },
              }
            : d,
        )
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    }
  }
  const rescan = async () => {
    try {
      await call('POST', '/gaps/scan')
      window.setTimeout(load, 800)
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    }
  }

  const needle = q.trim().toLowerCase()
  const matchGroup = (g: GapGroup) => !needle || g.name.toLowerCase().includes(needle) || g.missing.some((m) => m.title.toLowerCase().includes(needle))
  const matchFilm = (m: GapFilm) => !needle || m.title.toLowerCase().includes(needle)

  const groupShelves = (list: GapGroup[], kind: Ignore['kind']): Shelf<GapFilm>[] =>
    list.filter(matchGroup).map((g) => ({
      key: `${kind}:${g.id}`,
      title: g.name,
      hint: kind === 'franchise' ? t('you have {have} of {total}', { have: g.have, total: g.total }) : plural(g.have, 'you have {n} of theirs', 'you have {n} of theirs'),
      items: g.missing,
      extra: (
        <button className="btn-sm gap-ignore" onClick={() => void ignore(kind, g)} title={kind === 'franchise' ? t('Stop showing this series') : t('Stop showing this person')}>
          <Icon name="eye-off" size={13} /> {t('Ignore')}
        </button>
      ),
    }))

  const shelves = useMemo((): Shelf<GapFilm>[] => {
    if (!res) return []
    switch (tab) {
      case 'franchises':
        return groupShelves(res.franchises, 'franchise')
      case 'directors':
        return groupShelves(res.directors, 'director')
      case 'actors':
        return groupShelves(res.actors, 'actor')
      case 'classics': {
        const by = new Map<string, GapFilm[]>()
        for (const m of res.classics.filter(matchFilm)) {
          const d = m.year ? `${Math.floor(m.year / 10) * 10}` : '?'
          by.set(d, [...(by.get(d) ?? []), m])
        }
        return [...by.entries()].sort((a, b) => b[0].localeCompare(a[0])).map(([d, list]) => ({ key: `classics:${d}`, title: d === '?' ? t('Year unknown') : t('{decade}s', { decade: d }), items: list }))
      }
      case 'suggestions': {
        const list = res.suggestions.filter(matchFilm)
        const bands: [string, (n: number) => boolean][] = [
          [t('Recommended by 10 or more of your films'), (n) => n >= 10],
          [t('Recommended by 5 to 9 of your films'), (n) => n >= 5 && n < 10],
          [t('Recommended by 2 to 4 of your films'), (n) => n < 5],
        ]
        return bands.map(([title, test], i) => ({ key: `sugg:${i}`, title, items: list.filter((m) => test(m.matches ?? 0)) })).filter((s) => s.items.length > 0)
      }
      default:
        return []
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [res, tab, needle])

  const why = useMemo(() => {
    const out: Record<number, string> = {}
    for (const m of res?.suggestions ?? []) if (m.matches) out[m.tmdbId] = t('⚡ {n} of your films', { n: m.matches })
    return out
  }, [res])

  const counts: Record<Tab, number | undefined> = {
    franchises: res?.franchises.length,
    directors: res?.directors.length,
    actors: res?.actors.length,
    classics: res?.classics.length,
    suggestions: res?.suggestions.length,
    ignored: data?.ignored.length,
  }
  const tabName: Record<Tab, string> = { franchises: t('Film series'), directors: t('Directors'), actors: t('Actors'), classics: t('Classics'), suggestions: t('Suggestions'), ignored: t('Ignored') }

  let body: ReactNode
  if (!data) body = <div className="skeleton" style={{ height: 240 }} />
  else if (!res)
    body = (
      <div className="empty-state">
        <Icon name="film" size={44} />
        <p>{scan?.running ? t('The first look at your library is under way. It reads every film once, so it takes a few minutes.') : t('Your library hasn’t been checked yet.')}</p>
        {admin && !scan?.running && (
          <button className="primary btn-with-icon" onClick={() => void rescan()}>
            <Icon name="refresh" size={16} /> {t('Check my library now')}
          </button>
        )}
      </div>
    )
  else if (tab === 'ignored')
    body =
      data.ignored.length === 0 ? (
        <p className="hint">{t('Nothing ignored. Use Ignore on a series, director or actor you don’t want to see here.')}</p>
      ) : (
        <ul className="gap-ignored">
          {data.ignored.map((g) => (
            <li key={`${g.kind}:${g.id}`}>
              <span className="hint">{g.kind === 'franchise' ? t('Film series') : g.kind === 'director' ? t('Director') : t('Actor')}</span>
              <strong>{g.name}</strong>
              <button className="btn-sm" onClick={() => void ignore(g.kind, { id: g.id, name: g.name, have: 0, total: 0, missing: [] }, true)}>
                {t('Show again')}
              </button>
            </li>
          ))}
        </ul>
      )
  else
    body = (
      <Shelves
        key={tab}
        memory={`complete-${tab}`}
        shelves={shelves}
        itemKey={(m) => String(m.tmdbId)}
        render={(m) => <DiscoverCards items={[asMovie(m)]} kind="movie" owned={owned} onAdd={setAdding} why={tab === 'suggestions' ? why : undefined} />}
        empty={<p className="hint">{needle ? t('Nothing matches “{text}”.', { text: q.trim() }) : t('Nothing missing here.')}</p>}
      />
    )

  return (
    <div className="page-pad complete-page">
      <div className="complete-head">
        <div>
          <h1>
            {t('Complete your library')} <HelpLink topic="home-finding#complete-your-library" />
          </h1>
          <p className="hint">
            {res ? t('{n} films checked · {when}', { n: res.owned.toLocaleString(), when: new Date(res.builtAt).toLocaleString() }) : t('What’s missing from the series, directors and actors you collect.')}
          </p>
        </div>
        {admin && (
          <button className="btn-with-icon" disabled={scan?.running} onClick={() => void rescan()}>
            <Icon name="refresh" size={16} /> {scan?.running ? t('Checking…') : t('Check again')}
          </button>
        )}
      </div>
      {scan?.running && (
        <div className="gap-progress" role="status">
          <span>
            {t('Step {step} of {steps}', { step: scan.step, steps: scan.steps })} · {scan.label}
            {scan.of > 0 ? ` (${scan.done} / ${scan.of})` : ''}
          </span>
          <div className="bar">
            <span style={{ width: `${Math.round((((scan.step - 1) + (scan.of > 0 ? scan.done / scan.of : 0)) / scan.steps) * 100)}%` }} />
          </div>
        </div>
      )}
      {res && (
        <div className="gap-scores">
          <Ring pct={res.scores.global} label={t('Overall')} />
          <Ring pct={res.scores.franchises.pct} label={t('Film series')} sub={t('{have} of {total} films', { have: res.scores.franchises.have, total: res.scores.franchises.total })} />
          <Ring pct={res.scores.directors.pct} label={t('Directors')} sub={t('{have} of {total} films', { have: res.scores.directors.have, total: res.scores.directors.total })} />
          <Ring pct={res.scores.classics.pct} label={t('Classics')} sub={t('{have} of {total} films', { have: res.scores.classics.have, total: res.scores.classics.total })} />
        </div>
      )}
      {res && (
        <div className="toolbar lib-toolbar complete-bar">
          <div className="seg cx-subtabs complete-tabs" role="tablist" aria-label={t('Complete your library')}>
            {TABS.map((x) => (
              <button key={x} role="tab" aria-selected={tab === x} className={tab === x ? 'active' : ''} onClick={() => setTab(x)}>
                {tabName[x]} {counts[x] !== undefined ? <small>{counts[x]}</small> : null}
              </button>
            ))}
          </div>
          {tab !== 'ignored' && <input type="search" value={q} onChange={(e) => setQ(e.target.value)} placeholder={t('Find a series, person or film')} aria-label={t('Find a series, person or film')} />}
        </div>
      )}
      {body}
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

// The small card on Discover (and the Library) that leads here.
export function CompleteTeaser() {
  const { data } = useGaps()
  const res = data?.result
  return (
    <Link to="/discover/complete" className="complete-teaser">
      <Icon name="check" size={18} />
      <span>
        <strong>{t('Complete your library')}</strong>
        <small className="hint">
          {res
            ? t('{n}% complete · {f} series and {d} directors with films missing', { n: res.scores.global, f: res.franchises.length, d: res.directors.length })
            : t('What’s missing from the series, directors and actors you collect')}
        </small>
      </span>
      <Icon name="chevron-right" size={16} />
    </Link>
  )
}
