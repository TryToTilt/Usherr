// Home fork: the Comics page. Library (series), Add (search ComicVine),
// Settings (administrators), and a page per series with its issues.
import VoteButtons from './VoteButtons'
import Marks from '../bookshelf/Marks'
import { marksApi, pageOfMark, type ReadingMark } from '../bookshelf/marksApi'
import { KeepButton, useKept } from '../bookshelf/Keep'
import { keptId } from '../bookshelf/offline'
import { saveComicPlace } from '../bookshelf/progress'
import HelpLink from '../components/HelpLink'
import { lazy, Suspense, useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { ComicCatchUp } from './CatchUp'
import LibraryTabs from './LibraryTabs' // library hook (home fork)
import { PullDrawer } from '../components/CompactBar' // Usherr
import { createPortal } from 'react-dom'
import { Link, useNavigate, useParams } from 'react-router-dom'
import Icon from '../components/Icon'
import { can, isAdmin } from '../api'
import { useAuth } from '../AuthContext'
import { comicsApi, type ComicArcForYou, type ComicIssue, type ComicPages, type ComicRelease, type ComicSearchLog, type ComicSeries, type ComicSettings } from './comicsApi'
import ComicReviewList from './ComicReview' // review list (home fork)
import ComicWeekView from './ComicWeek' // the week's new comics (home fork)
import { ComicArcList } from './ComicArcs' // story arcs (home fork)
import ComicTools from './ComicTools' // library tools (home fork)
import './comics.css'
import type { ComicFlipHandle } from './ComicFlip'
import ComicGuided, { comicGuidedSource, type ComicGuidedHandle } from './ComicGuided' // guided view (home fork)
import ComicWebtoon, { type ComicWebtoonHandle } from './ComicWebtoon' // Usherr
import ComicReadingTab, { ComicContinueRow } from './ComicReadingViews' // Usherr
import ComicFixMatch from './ComicFixMatch' // Usherr
import { isLayout, keyTurn, spreadEnd, spreadOf, stepSpread, swipeTurn, tapTurn, type ReadDirection, type ReaderLayout } from './comicReading' // Usherr
import { flipSpeeds, type FlipSpeed } from './comicFlipSpeed'
import Shelves from './Shelves' // home fork
import { groupShelves, letterShelves, RowsByPick, ShelfControls, useShelfView } from './shelfKit' // home fork
import { locale, plural, t } from '../i18n'
import { PosterSizeSlider, usePosterSize } from './posterSize' // Usherr

// The page-turn view loads only when a comic is opened.
const ComicFlip = lazy(() => import('./ComicFlip'))

// Marvel's ".NOW" issues: ComicVine puts ".NOW" in front of the title of
// what was printed as "#15.NOW"; show the title and that number instead.
function issueTitle(i: ComicIssue) {
  const m = /^\s*\.now\b[\s:,-]*/i.exec(i.title ?? '')
  if (!m) return i.title
  const n = parseInt(i.number, 10)
  return (
    <>
      {i.title.slice(m[0].length)} {n > 1 && <span className="hint">{t('(printed as #{n}.NOW)', { n: n - 1 })}</span>}
    </>
  )
}

function size(b?: number) {
  if (!b) return ''
  return b >= 1e9 ? `${(b / 1e9).toFixed(1)} GB` : `${Math.round(b / 1e6)} MB`
}

function day(d: string) {
  if (!d) return ''
  const dt = new Date(d + 'T12:00:00')
  return isNaN(dt.getTime()) ? d : dt.toLocaleDateString(locale(), { year: 'numeric', month: 'short', day: 'numeric' })
}

// useComicsOn: whether the Comics entry belongs in the menu. Administrators
// always see it (to switch the module on); others only while it is on.
export { useComicsOn } from './libraryKinds'

export default function Comics() {
  const { user } = useAuth()
  const admin = isAdmin(user)
  const [tab, setTab] = useState<'library' | 'reading' | 'week' | 'arcs' | 'add' | 'review' | 'settings'>(() => {
    const asked = new URLSearchParams(window.location.search).get('tab')
    return asked === 'arcs' || asked === 'add' || asked === 'reading' ? asked : 'library'
  })
  const [reviewCount, setReviewCount] = useState(0)
  const [libText, setLibText] = useState('') // the library's search (home fork: in the top card)
  useEffect(() => {
    if (!admin) return
    comicsApi
      .reviews()
      .then((l) => setReviewCount(new Set(l.map((r) => r.issueId)).size))
      .catch(() => undefined)
  }, [admin])
  const [settings, setSettings] = useState<ComicSettings | null>(null)
  const [enabled, setEnabled] = useState<boolean | null>(null)

  useEffect(() => {
    if (admin) {
      comicsApi
        .settings()
        .then((s) => {
          setSettings(s)
          setEnabled(s.enabled)
          if (!s.enabled || !s.keySet) setTab('settings')
        })
        .catch(() => setEnabled(false))
    } else {
      comicsApi
        .enabled()
        .then((r) => setEnabled(r.enabled))
        .catch(() => setEnabled(false))
    }
  }, [admin])

  if (enabled === null) return <div className="page-pad"><div className="skeleton" style={{ height: 120 }} /></div>
  if (!enabled && !admin) return <div className="page-pad"><p className="hint">{t("Comics aren't switched on.")}</p></div>

  return (
    <div className="page-pad cx-page">
      {/* Usherr: search and AI are in the top bar */}
      {/* home fork: one card like the Library's other tabs: kinds, Comics' own tabs, and the search */}
      <PullDrawer className="cx-pull">{/* Usherr: pulls down from under the top bar, like the Library's */}
      <div className="toolbar lib-toolbar cx-libbar">
        <LibraryTabs current="comics" />
        <HelpLink topic="home-watching#comics" />
        <div className="seg cx-subtabs" role="tablist" aria-label={t('Comics')}>
          <button role="tab" data-walk="cx-tab-library" aria-selected={tab === 'library'} className={tab === 'library' ? 'active' : ''} onClick={() => setTab('library')} disabled={!enabled}>
            {t('Library')}
          </button>
          <button role="tab" data-walk="cx-tab-reading" aria-selected={tab === 'reading'} className={tab === 'reading' ? 'active' : ''} onClick={() => setTab('reading')} disabled={!enabled}>
            {t('Reading')}
          </button>
          <button role="tab" data-walk="cx-tab-week" aria-selected={tab === 'week'} className={tab === 'week' ? 'active' : ''} onClick={() => setTab('week')} disabled={!enabled || (admin && !settings?.keySet)}>
            {t('This week')}
          </button>
          <button role="tab" data-walk="cx-tab-arcs" aria-selected={tab === 'arcs'} className={tab === 'arcs' ? 'active' : ''} onClick={() => setTab('arcs')} disabled={!enabled || (admin && !settings?.keySet)}>
            {t('Story arcs')}
          </button>
          {(admin || can(user, 'comics')) && ( // home fork: or the comics switch
            <button role="tab" data-walk="cx-tab-add" aria-selected={tab === 'add'} className={tab === 'add' ? 'active' : ''} onClick={() => setTab('add')} disabled={!enabled || (admin && !settings?.keySet)}>
              {t('Add')}
            </button>
          )}
          {admin && (
            <button role="tab" data-walk="cx-tab-review" aria-selected={tab === 'review'} className={tab === 'review' ? 'active' : ''} onClick={() => setTab('review')} disabled={!enabled}>
              {t('Review')}{reviewCount > 0 ? ` (${reviewCount})` : ''}
            </button>
          )}
          {admin && (
            <button role="tab" aria-selected={tab === 'settings'} className={tab === 'settings' ? 'active' : ''} onClick={() => setTab('settings')} aria-label={t('Comics settings')}>
              <Icon name="sliders" size={15} />
            </button>
          )}
        </div>
        {tab === 'library' && enabled && <input type="search" data-walk="cx-lib-search" value={libText} onChange={(e) => setLibText(e.target.value)} placeholder={t('Find a series or publisher')} aria-label={t('Find a series')} />}
      </div>
      </PullDrawer>
      {tab === 'library' && enabled && <ComicLibrary text={libText} onAdd={admin || can(user, 'comics') ? () => setTab('add') : undefined} />}
      {tab === 'reading' && enabled && <ComicReadingTab />}
      {tab === 'add' && (admin || can(user, 'comics')) && <ComicAdd />}
      {tab === 'week' && enabled && <ComicWeekView admin={admin} />}
      {tab === 'arcs' && enabled && <ComicArcList admin={admin} />}
      {tab === 'review' && admin && <ComicReviewList onCount={setReviewCount} />}
      {tab === 'settings' && admin && settings && (
        <ComicSettingsTab
          settings={settings}
          onSaved={(s) => {
            setSettings(s)
            setEnabled(s.enabled)
          }}
        />
      )}
      {tab === 'settings' && admin && enabled && <ComicTools />}
    </div>
  )
}

// ---- library ----

// home fork: the story arcs you're into, from the issues you have and have read
// (read in this browser counts three times), highlighted above the shelves.
function ArcsForYou() {
  const admin = isAdmin(useAuth().user)
  const navigate = useNavigate()
  const [arcs, setArcs] = useState<ComicArcForYou[] | null>(null)
  const [busy, setBusy] = useState(0)
  useEffect(() => {
    comicsApi.arcsForYou().then(setArcs).catch(() => setArcs([]))
  }, [])
  const ranked = useMemo(() => {
    const done = (id: number) => {
      try {
        return localStorage.getItem(`comic-done-${id}`) === '1'
      } catch {
        return false
      }
    }
    return (arcs ?? [])
      .map((a) => ({ a, read: a.issues.filter((i) => done(i.issueId)).length }))
      .map((x) => ({ ...x, score: x.a.have + 3 * x.read + (x.a.series > 1 ? 1 : 0) }))
      .filter((x) => x.a.have >= 2 || x.read > 0)
      .sort((p, q) => q.score - p.score || p.a.name.localeCompare(q.a.name))
      .slice(0, 20)
  }, [arcs])
  if (ranked.length === 0) return null
  const open = async (a: ComicArcForYou) => {
    if (a.libraryId) return navigate(`/comics/arc/${a.libraryId}`)
    setBusy(a.cvId)
    try {
      const added = await comicsApi.addArc(a.cvId, false)
      navigate(`/comics/arc/${added.id}`)
    } catch {
      setBusy(0)
    }
  }
  const card = ({ a, read }: { a: ComicArcForYou; read: number }) => (
    <button className="cx-card cx-arc-pick" disabled={!a.libraryId && (!admin || busy !== 0)} onClick={() => void open(a)} title={!a.libraryId && admin ? t('Adds it to Story arcs and opens it') : undefined}>
      {a.imageUrl ? <img src={a.imageUrl} alt="" loading="lazy" /> : <div className="cx-noimg"><Icon name="book" size={28} /></div>}
      <strong>{a.name}</strong>
      <span className="cx-count">
        {t('{n} of yours', { n: a.have })}
        {read > 0 ? ` · ${t('{n} read', { n: read })}` : ''}
      </span>
      {busy === a.cvId && <span className="hint">{t('Adding…')}</span>}
    </button>
  )
  return (
    <div className="shelf-featured">
      <Shelves
        shelves={[{ key: 'arcs-for-you', title: t('Story arcs for you'), hint: t('from the issues you have and read'), items: ranked }]}
        render={card}
        itemKey={(x) => String(x.a.cvId)}
        rowClass="comic-shelf"
        memory="comics"
      />
    </div>
  )
}

function ComicLibrary({ onAdd, text }: { onAdd?: () => void; text: string }) {
  const [list, setList] = useState<ComicSeries[] | null>(null)
  const [error, setError] = useState('')
  useEffect(() => {
    comicsApi.list().then(setList).catch((e: Error) => setError(e.message))
  }, [])
  const shown = useMemo(() => {
    const q = text.trim().toLowerCase()
    return (list ?? []).filter((s) => !q || s.name.toLowerCase().includes(q) || s.publisher.toLowerCase().includes(q))
  }, [list, text])
  // home fork: shelves, rows that scroll sideways, by publisher, decade…
  const shelfView = useShelfView<'publisher' | 'decade' | 'az' | 'wanted'>('comics', ['publisher', 'decade', 'az', 'wanted'], 'publisher')
  const posterSize = usePosterSize('comics', 150)
  const shelves = useMemo(() => {
    if (!shelfView.on) return []
    const byName = (a: ComicSeries, b: ComicSeries) => a.name.localeCompare(b.name) || a.startYear - b.startYear
    switch (shelfView.by) {
      case 'decade':
        return groupShelves(shown, (s) => [s.startYear ? t('{decade}s', { decade: String(Math.floor(s.startYear / 10) * 10) }) : ''], { prefix: 'decade', byName: true, rest: t('Year unknown'), order: byName }).reverse()
      case 'az':
        return letterShelves(shown, (s) => s.name)
      case 'wanted': {
        const wanted = shown.filter((s) => s.wanted > 0).sort((a, b) => b.wanted - a.wanted || byName(a, b))
        const behind = shown.filter((s) => s.wanted === 0 && s.issueCount > s.have).sort(byName)
        const done = shown.filter((s) => s.wanted === 0 && s.issueCount > 0 && s.issueCount <= s.have).sort(byName)
        return [
          { key: 'wanted', title: t('Issues wanted'), items: wanted },
          { key: 'gaps', title: t('Not complete'), items: behind },
          { key: 'done', title: t('Complete'), items: done },
        ].filter((r) => r.items.length > 0)
      }
      default:
        return groupShelves(shown, (s) => [s.publisher], { prefix: 'pub', rest: t('Publisher unknown'), order: byName })
    }
  }, [shown, shelfView.on, shelfView.by])
  const card = (s: ComicSeries) => (
    <Link to={`/comics/${s.id}`} className="cx-card">
      {s.imageUrl ? <img src={s.imageUrl} alt="" loading="lazy" /> : <div className="cx-noimg"><Icon name="book" size={28} /></div>}
      <strong>{s.name}</strong>
      <span className="hint">
        {s.startYear || ''} {s.publisher ? `· ${s.publisher}` : ''}
      </span>
      <span className="cx-count">
        {t('{have} of {total} issues', { have: s.have, total: s.issueCount || '?' })}{s.wanted > 0 ? ` · ${t('{n} wanted', { n: s.wanted })}` : ''}
      </span>
    </Link>
  )
  if (error) return <p className="error-text">{error}</p>
  if (!list) return <div className="skeleton" style={{ height: 200 }} />
  if (list.length === 0)
    return (
      <div className="cx-empty">
        <p className="hint">{t('No comics yet.')}</p>
        {onAdd && (
          <button className="btn-sm primary" onClick={onAdd}>
            <Icon name="plus" size={14} /> {t('Add a series')}
          </button>
        )}
      </div>
    )
  return (
    <div style={posterSize.style}>
      <div className="cx-toolbar">
        <span className="hint">
          {t('{shown} of {total} series', { shown: shown.length, total: list.length })}
        </span>
        <span className="spacer" />
        <ShelfControls view={shelfView} />
        <PosterSizeSlider value={posterSize} />
      </div>
      {!text.trim() && <ComicContinueRow />}
      {shelfView.on && !text.trim() && <ArcsForYou />}
      {shelfView.on ? (
        <Shelves key={shelfView.by} memory="comics" shelves={shelves} render={card} itemKey={(s) => String(s.id)} rowClass="comic-shelf" bar={<RowsByPick value={shelfView.by} onChange={shelfView.setBy} options={[
            { value: 'publisher', label: t('Publisher') },
            { value: 'decade', label: t('Decade') },
            { value: 'az', label: t('A–Z') },
            { value: 'wanted', label: t('Wanted') },
          ]} />} />
      ) : (
        <ul className="cx-grid">
          {shown.map((s) => (
            <li key={s.id}>{card(s)}</li>
          ))}
        </ul>
      )}
    </div>
  )
}

// ---- add ----

function ComicAdd() {
  const nav = useNavigate()
  // ?q= starts a search (the news on the Dashboard links here)
  const [q, setQ] = useState(() => new URLSearchParams(window.location.search).get('q') ?? '')
  const [found, setFound] = useState<ComicSeries[] | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [want, setWant] = useState<'all' | 'future' | 'none'>('future')
  useEffect(() => {
    const term = q.trim()
    if (term.length < 2) {
      setFound(null)
      return
    }
    const timer = window.setTimeout(() => {
      setBusy(true)
      setError('')
      comicsApi
        .lookup(term)
        .then(setFound)
        .catch((e: Error) => setError(e.message))
        .finally(() => setBusy(false))
    }, 450)
    return () => window.clearTimeout(timer)
  }, [q])
  const add = (s: ComicSeries) => {
    setBusy(true)
    setError('')
    comicsApi
      .add(s.cvId, want)
      .then((r) => nav(`/comics/${r.series.id}`))
      .catch((e: Error) => setError(e.message))
      .finally(() => setBusy(false))
  }
  return (
    <section className="cx-add">
      <input type="search" autoFocus value={q} onChange={(e) => setQ(e.target.value)} placeholder={t('Search ComicVine for a series, e.g. Saga or Amazing Spider-Man')} aria-label={t('Search ComicVine')} />
      <div className="cx-radios" role="radiogroup" aria-label={t('Which issues to get')}>
        <label>
          <input type="radio" checked={want === 'future'} onChange={() => setWant('future')} /> {t('New issues as they come out')}
        </label>
        <label>
          <input type="radio" checked={want === 'all'} onChange={() => setWant('all')} /> {t('Every issue (searches now)')}
        </label>
        <label>
          <input type="radio" checked={want === 'none'} onChange={() => setWant('none')} /> {t('None yet (pick issues later)')}
        </label>
      </div>
      {busy && <p className="hint">{t('Searching…')}</p>}
      {error && <p className="error-text">{error}</p>}
      {found && found.length === 0 && !busy && <p className="hint">{t('ComicVine has nothing by that name.')}</p>}
      <ul className="cx-results">
        {(found ?? []).map((s) => (
          <li key={s.cvId}>
            {s.imageUrl ? <img src={s.imageUrl} alt="" loading="lazy" /> : <div className="cx-noimg small"><Icon name="book" size={20} /></div>}
            <div className="cx-result-main">
              <strong>
                {s.name} {s.startYear ? `(${s.startYear})` : ''}
              </strong>
              <span className="hint">
                {s.publisher} · {plural(s.issueCount, '{n} issue', '{n} issues')}
              </span>
              {s.description && <p className="cx-desc">{s.description}</p>}
            </div>
            {s.inLibrary ? (
              <Link className="btn-sm" to={`/comics/${s.inLibrary}`}>
                {t('Open')}
              </Link>
            ) : (
              <button className="btn-sm primary" disabled={busy} onClick={() => add(s)}>
                <Icon name="plus" size={14} /> {t('Add')}
              </button>
            )}
          </li>
        ))}
      </ul>
    </section>
  )
}

// ---- settings ----

function ComicSettingsTab({ settings, onSaved }: { settings: ComicSettings; onSaved: (s: ComicSettings) => void }) {
  const [path, setPath] = useState(settings.path)
  const [key, setKey] = useState('')
  const [busy, setBusy] = useState(false)
  const [msg, setMsg] = useState('')
  const [error, setError] = useState('')
  const save = (body: Parameters<typeof comicsApi.saveSettings>[0], done: string) => {
    setBusy(true)
    setError('')
    setMsg('')
    comicsApi
      .saveSettings(body)
      .then((s) => {
        onSaved(s)
        setKey('')
        setMsg(done)
      })
      .catch((e: Error) => setError(e.message))
      .finally(() => setBusy(false))
  }
  return (
    <section className="card cx-settings">
      <h2>{t('Comics')}</h2>
      <p className="hint">{t("Series and issue details come from ComicVine. Issues are searched in your indexers' comics category and saved as Series/Volume 01 (Year)/Series (Year) Volume 01 Issue 001.cbz.")}</p>
      <label className="cx-field">
        <span>{t('Comics folder (inside the container)')}</span>
        <input value={path} onChange={(e) => setPath(e.target.value)} placeholder="/data/comics" />
      </label>
      <button className="btn-sm" disabled={busy} onClick={() => save({ path }, t('Folder saved.'))}>
        {t('Save folder')}
      </button>
      <label className="cx-field">
        <span>{settings.keySet ? t('ComicVine API key (saved — paste a new one to replace it)') : t('ComicVine API key (comicvine.gamespot.com/api, while logged in)')}</span>
        <input type="password" autoComplete="off" value={key} onChange={(e) => setKey(e.target.value)} placeholder={settings.keySet ? '••••••••' : t('Paste your API key')} />
      </label>
      <div className="cx-actions">
        <button className="btn-sm" disabled={busy || !key.trim()} onClick={() => save({ key: key.trim() }, t('Key checked with ComicVine and saved.'))}>
          {t('Check and save key')}
        </button>
        {settings.keySet && (
          <button className="btn-sm" disabled={busy} onClick={() => save({ removeKey: true }, t('Key removed.'))}>
            {t('Remove key')}
          </button>
        )}
      </div>
      <div className="cx-switch">
        <strong>{settings.enabled ? t('Comics are on') : t('Comics are off')}</strong>
        <button className={settings.enabled ? 'btn-sm' : 'btn-sm primary'} disabled={busy} onClick={() => save({ enabled: !settings.enabled }, settings.enabled ? t('Switched off.') : t('Switched on.'))}>
          {settings.enabled ? t('Switch off') : t('Switch on')}
        </button>
      </div>
      {msg && <p className="hint">{msg}</p>}
      {error && <p className="error-text">{error}</p>}
    </section>
  )
}

// ---- one series ----

export function ComicSeriesPage() {
  const { id } = useParams()
  const sid = Number(id)
  const nav = useNavigate()
  const { user } = useAuth()
  const admin = isAdmin(user)
  const [data, setData] = useState<{ series: ComicSeries; issues: ComicIssue[] } | null>(null)
  const [error, setError] = useState('')
  const [note, setNote] = useState('')
  const [picked, setPicked] = useState<Set<number>>(new Set())
  const [releasesFor, setReleasesFor] = useState<ComicIssue | null>(null)
  const [reading, setReading] = useState<ComicIssue | null>(null)
  const [readFrom, setReadFrom] = useState(0) // a bookmark's page, or 0 for where it was left
  const [marksSeen, setMarksSeen] = useState(0) // bumped when the reader closes, to list new bookmarks
  const [fixing, setFixing] = useState(false) // fixing the ComicVine match (Usherr)
  const [kept] = useKept()
  const keptIds = new Set(kept.map((k) => k.id))

  const load = useCallback(() => {
    comicsApi.get(sid).then(setData).catch((e: Error) => setError(e.message))
  }, [sid])
  const closeReader = useCallback(() => {
    setReading(null)
    setReadFrom(0)
    setMarksSeen((n) => n + 1)
    load() // what's read now
  }, [load])
  useEffect(() => {
    load()
    const timer = window.setInterval(load, 15000)
    return () => window.clearInterval(timer)
  }, [load])

  if (error && !data) return <div className="page-pad"><p className="error-text">{error}</p></div>
  if (!data) return <div className="page-pad"><div className="skeleton" style={{ height: 200 }} /></div>
  const { series, issues } = data
  const act = (p: Promise<unknown>, done: string) =>
    p
      .then(() => {
        setNote(done)
        setError('')
        load()
      })
      .catch((e: Error) => setError(e.message))
  const toggle = (iid: number) =>
    setPicked((s) => {
      const n = new Set(s)
      if (n.has(iid)) n.delete(iid)
      else n.add(iid)
      return n
    })
  const ids = [...picked]
  // Usherr: read or unread, for this person.
  const mark = (issue: ComicIssue, scope: 'issue' | 'upto' | 'series', read: boolean, done: string) =>
    act(comicsApi.markRead(issue.id, scope, read).then((r) => noteReadHere(r.issueIds, read)), done)
  const unreadLeft = issues.some((i) => !i.read)
  const wantIssue = (i: ComicIssue) =>
    act(comicsApi.wantRead(series.id, i.id, !i.wantRead), i.wantRead ? t('Taken off Want to Read.') : t('Added to Want to Read.'))

  return (
    <div className="page-pad cx-page">
      <Link to="/comics" className="hint cx-back">
        ← {t('Comics')}
      </Link>
      <header className="cx-series-head">
        {series.imageUrl && <img src={series.imageUrl} alt="" />}
        <div>
          <h1>
            {series.name} {series.startYear ? <span className="hint">({series.startYear})</span> : null}
          </h1>
          <p className="hint">
            {series.publisher} · {t('{have} of {total} issues', { have: series.have, total: issues.length })}{series.wanted ? ` · ${t('{n} wanted', { n: series.wanted })}` : ''}
          </p>
          {series.description && <p className="cx-desc">{series.description}</p>}
          <div className="cx-actions">
            <VoteButtons kind="comic" refId={series.cvId} meta={{ title: series.name, year: series.startYear, image: series.imageUrl }} />
            <ComicCatchUp seriesId={series.id} issues={issues} />
            <button
              className={`btn-sm${series.wantRead ? ' primary' : ''}`}
              data-walk="cx-want-read"
              aria-pressed={!!series.wantRead}
              onClick={() => act(comicsApi.wantRead(series.id, 0, !series.wantRead), series.wantRead ? t('Taken off Want to Read.') : t('Added to Want to Read.'))}
            >
              <Icon name="bookmark" size={14} /> {t('Want to read')}
            </button>
            {issues.length > 0 && (
              <button className="btn-sm" onClick={() => mark(issues[0], 'series', unreadLeft, unreadLeft ? t('The whole series is marked read.') : t('The whole series is marked unread.'))}>
                <Icon name="check" size={14} /> {unreadLeft ? t('Mark series read') : t('Mark series unread')}
              </button>
            )}
          </div>
          {admin && (
            <div className="cx-actions">
              <label className="cx-check">
                <input type="checkbox" checked={series.monitorNew} onChange={(e) => act(comicsApi.update(series.id, { monitorNew: e.target.checked }), e.target.checked ? t('New issues will be wanted as they come out.') : t('New issues won’t be wanted automatically.'))} /> {t('Get new issues as they come out')}
              </label>
              <button className="btn-sm" onClick={() => act(comicsApi.searchSeries(series.id), t('Searching the wanted issues in the background.'))}>
                <Icon name="search" size={14} /> {t('Search wanted')}
              </button>
              <button className="btn-sm" onClick={() => act(comicsApi.refresh(series.id), t('Issue list refreshed from ComicVine, and the files in its folder checked.'))} title={t('Reads the issue list from ComicVine again, and counts files already in the folder for issues marked missing')}>
                <Icon name="refresh" size={14} /> {t('Refresh')}
              </button>
              <button className="btn-sm" data-walk="cx-fix-match" onClick={() => setFixing(true)} title={t('Matched to the wrong volume, or the wrong kind of series? Fix it here. Files stay where they are.')}>
                <Icon name="sliders" size={14} /> {t('Fix match')}
              </button>
              <button
                className="btn-sm"
                onClick={() => {
                  if (confirm(t('Remove {name} from the library? Its files stay where they are.', { name: series.name }))) comicsApi.remove(series.id).then(() => nav('/comics')).catch((e: Error) => setError(e.message))
                }}
              >
                <Icon name="trash" size={14} /> {t('Remove')}
              </button>
            </div>
          )}
        </div>
      </header>
      {note && <p className="hint">{note}</p>}
      {error && <p className="error-text">{error}</p>}

      {admin && picked.size > 0 && (
        <div className="cx-bulk" role="toolbar" aria-label={t('Selected issues')}>
          <strong>{t('{n} selected', { n: picked.size })}</strong>
          <button className="btn-sm primary" onClick={() => act(comicsApi.setWanted(ids, true, true), t('Wanted; searching them now.')).then(() => setPicked(new Set()))}>
            {t('Want and search')}
          </button>
          <button className="btn-sm" onClick={() => act(comicsApi.setWanted(ids, true), t('Marked wanted.')).then(() => setPicked(new Set()))}>
            {t('Want')}
          </button>
          <button className="btn-sm" onClick={() => act(comicsApi.setWanted(ids, false), t('No longer wanted.')).then(() => setPicked(new Set()))}>
            {t('Not wanted')}
          </button>
          <button className="btn-sm" onClick={() => setPicked(new Set())}>
            {t('Clear')}
          </button>
        </div>
      )}

      <table className="cx-issues">
        <thead>
          <tr>
            {admin && (
              <th>
                <input
                  type="checkbox"
                  aria-label={t('Select all issues')}
                  checked={picked.size > 0 && picked.size === issues.length}
                  onChange={(e) => setPicked(e.target.checked ? new Set(issues.map((i) => i.id)) : new Set())}
                />
              </th>
            )}
            <th>#</th>
            <th>{t('Title')}</th>
            <th className="cx-col-out">{t('Out')}</th>
            <th className="cx-col-status">{t('Status')}</th>
            <th />
          </tr>
        </thead>
        <tbody>
          {issues.map((i) => (
            <tr key={i.id} className={`${i.status === 'downloaded' ? 'have' : ''}${i.read ? ' read' : ''}`}>
              {admin && (
                <td>
                  <input type="checkbox" aria-label={t('Select issue {n}', { n: i.number })} checked={picked.has(i.id)} onChange={() => toggle(i.id)} />
                </td>
              )}
              <td className="cx-num">{i.number}</td>
              <td>
                {issueTitle(i) || <span className="hint">—</span>}
                {i.read && (
                  <span className="cx-read-tag">
                    <Icon name="check" size={12} /> {t('Read')}
                  </span>
                )}
                {!!i.page && !!i.pages && <span className="cx-read-tag hint">{t('Page {n} of {total}', { n: i.page, total: i.pages })}</span>}
                {i.cvId < 0 && <span className="cx-read-tag hint" title={t('This issue has a file, but the volume this series is matched to has no such number.')}>{t('Not on this volume')}</span>}
                {/* On a phone the Status column gives its room to the Read button. */}
                {i.status === 'downloading' ? <span className="cx-read-tag cx-phone-status">{t('Downloading')}</span> : i.status !== 'downloaded' && i.wanted && <span className="cx-read-tag cx-phone-status">{t('Wanted')}</span>}
              </td>
              <td className="hint cx-col-out">{day(i.storeDate || i.coverDate)}</td>
              <td className="cx-col-status">
                {i.status === 'downloaded' ? (
                  <span className="badge downloaded">{t('Have')} {size(i.fileSize)}</span>
                ) : i.status === 'downloading' ? (
                  <span className="badge">{t('Downloading')}</span>
                ) : i.wanted ? (
                  <span className="badge missing">{t('Wanted')}</span>
                ) : (
                  <span className="hint">—</span>
                )}
              </td>
              <td className="cx-row-actions">
                {i.status === 'downloaded' && (
                  <>
                    <button
                      className="btn-sm primary"
                      data-walk="cx-read"
                      onClick={() => {
                        setReadFrom(0)
                        setReading(i)
                      }}
                      aria-label={t('Read issue {n}', { n: i.number })}
                    >
                      <Icon name="book" size={14} /> {i.page ? t('Continue') : t('Read')}
                    </button>
                    <a className="btn-sm" href={comicsApi.fileUrl(i.id)} target="_blank" rel="noreferrer" aria-label={t('Download issue {n}', { n: i.number })}>
                      <Icon name="download" size={14} />
                    </a>
                    {!/\.pdf$/i.test(i.filePath ?? '') && (
                      <KeepButton
                        kind="issue"
                        id={i.id}
                        title={`${series.name} #${i.number}`}
                        subtitle={i.title || undefined}
                        open={`/bookshelf/comic/${i.id}`}
                        kept={keptIds.has(keptId('issue', i.id))}
                        count={() =>
                          comicsApi.pages(i.id).then((r) => {
                            if (r.pdf || !r.pages) throw new Error(t("This issue is a PDF, which can't be kept on a device yet."))
                            return r.pages
                          })
                        }
                        compact
                      />
                    )}
                  </>
                )}
                {admin && i.status === 'missing' && (
                  <>
                    <button className="btn-sm" title={t('Search now')} aria-label={t('Search issue {n}', { n: i.number })} onClick={() => act(comicsApi.searchIssue(i.id).then((r) => setNote(r.message)), '')}>
                      <Icon name="search" size={14} />
                    </button>
                    <button className="btn-sm" title={t('Choose a release')} aria-label={t('Choose a release for issue {n}', { n: i.number })} onClick={() => setReleasesFor(i)}>
                      <Icon name="list" size={14} />
                    </button>
                  </>
                )}
                <IssueMenu issue={i} onMark={mark} onWant={wantIssue} />
              </td>
            </tr>
          ))}
        </tbody>
      </table>
      <SeriesMarks
        seriesId={series.id}
        issues={issues}
        refresh={marksSeen}
        onOpen={(issue, page) => {
          setReadFrom(page)
          setReading(issue)
        }}
      />
      {reading && <ComicReader issue={reading} onClose={closeReader} startPage={readFrom} />}
      {releasesFor && <ComicReleases issue={releasesFor} onClose={() => setReleasesFor(null)} onGrabbed={() => { setReleasesFor(null); setNote(t('Downloading.')); load() }} />}
      {fixing && (
        <ComicFixMatch
          series={series}
          onClose={() => setFixing(false)}
          onChanged={(n) => {
            setNote(n)
            load()
          }}
        />
      )}
    </div>
  )
}

// Usherr: other parts of the app read these notes in this browser (Catch me up,
// story arcs, the reader's fallback), so marking keeps them in step.
function noteReadHere(ids: number[], read: boolean) {
  try {
    for (const id of ids) {
      localStorage.removeItem(`comic-page-${id}`)
      if (read) localStorage.setItem(`comic-done-${id}`, '1')
      else localStorage.removeItem(`comic-done-${id}`)
    }
  } catch {
    /* private mode */
  }
}

// Usherr: one issue's read and Want to Read choices.
function IssueMenu({ issue, onMark, onWant }: { issue: ComicIssue; onMark: (issue: ComicIssue, scope: 'issue' | 'upto', read: boolean, done: string) => unknown; onWant: (issue: ComicIssue) => unknown }) {
  const box = useRef<HTMLDetailsElement>(null)
  const pick = (run: () => unknown) => {
    if (box.current) box.current.open = false
    run()
  }
  return (
    <details ref={box} className="cx-issue-menu">
      <summary className="btn-sm" aria-label={t('More for issue {n}', { n: issue.number })} title={t('Read, unread and Want to Read')}>
        ⋯
      </summary>
      <div className="cx-issue-menu-list" role="menu">
        <button role="menuitem" className="btn-sm" onClick={() => pick(() => onMark(issue, 'issue', !issue.read, issue.read ? t('Marked unread.') : t('Marked read.')))}>
          {issue.read ? t('Mark unread') : t('Mark read')}
        </button>
        <button role="menuitem" className="btn-sm" onClick={() => pick(() => onMark(issue, 'upto', true, t('Marked read up to here.')))}>
          {t('Mark read up to here')}
        </button>
        <button role="menuitem" className="btn-sm" onClick={() => pick(() => onMark(issue, 'upto', false, t('Marked unread up to here.')))}>
          {t('Mark unread up to here')}
        </button>
        <button role="menuitem" className="btn-sm" onClick={() => pick(() => onWant(issue))}>
          {issue.wantRead ? t('Take off Want to Read') : t('Want to read')}
        </button>
      </div>
    </details>
  )
}

// Usherr: your bookmarks in this series' issues; each opens the issue there.
function SeriesMarks({ seriesId, issues, refresh, onOpen }: { seriesId: number; issues: ComicIssue[]; refresh: number; onOpen: (issue: ComicIssue, page: number) => void }) {
  const [list, setList] = useState<ReadingMark[]>([])
  useEffect(() => {
    marksApi
      .series(seriesId)
      .then(setList)
      .catch(() => setList([]))
  }, [seriesId, refresh])
  if (list.length === 0) return null
  const byId = new Map(issues.map((i) => [i.id, i]))
  return (
    <section className="cx-series-marks">
      <h2>
        <Icon name="bookmark" size={16} /> {t('Your bookmarks')}
      </h2>
      {issues
        .filter((i) => list.some((m) => m.item === `comic:${i.id}`))
        .map((i) => (
          <div key={i.id} className="cx-series-marks-issue">
            <h3>
              #{i.number} {issueTitle(i)}
            </h3>
            <Marks
              item={`comic:${i.id}`}
              list={list.filter((m) => m.item === `comic:${i.id}`)}
              onChange={(next) => setList((all) => [...all.filter((m) => m.item !== `comic:${i.id}`), ...next])}
              onJump={(m) => {
                const issue = byId.get(Number(m.item.split(':')[1]))
                if (issue && issue.status === 'downloaded') onOpen(issue, pageOfMark(m.position))
              }}
            />
          </div>
        ))}
    </section>
  )
}

function ComicReleases({ issue, onClose, onGrabbed }: { issue: ComicIssue; onClose: () => void; onGrabbed: () => void }) {
  const [list, setList] = useState<ComicRelease[] | null>(null)
  const [all, setAll] = useState(false)
  const [error, setError] = useState('')
  const [log, setLog] = useState<ComicSearchLog | null>(null)
  const [words, setWords] = useState('')
  const [own, setOwn] = useState('')
  useEffect(() => {
    setList(null)
    setError('')
    comicsApi.releases(issue.id, all, own).then(setList).catch((e: Error) => setError(e.message))
  }, [issue.id, all, own])
  useEffect(() => {
    comicsApi.searchLog(issue.id).then(setLog).catch(() => undefined)
  }, [issue.id])
  return (
    <div className="cx-modal" role="dialog" aria-label={t('Releases for issue {n}', { n: issue.number })} onClick={onClose}>
      <div className="cx-modal-box card" onClick={(e) => e.stopPropagation()}>
        <header>
          <h2>{t('Issue #{n}', { n: issue.number })}</h2>
          <button className="btn-sm" onClick={onClose} aria-label={t('Close')}>
            <Icon name="x" size={14} />
          </button>
        </header>
        <label className="cx-check">
          <input type="checkbox" checked={all} onChange={(e) => setAll(e.target.checked)} /> {t("Show releases that don't look like this issue too")}
        </label>
        <form
          className="cx-own-search"
          onSubmit={(e) => {
            e.preventDefault()
            setOwn(words.trim())
          }}
        >
          <input value={words} onChange={(e) => setWords(e.target.value)} placeholder={t('Search in your own words, e.g. Saga Vol. 6')} />
          <button className="btn-sm" type="submit">
            <Icon name="search" size={14} /> {t('Search')}
          </button>
          {own && (
            <button
              className="btn-sm"
              type="button"
              onClick={() => {
                setWords('')
                setOwn('')
              }}
            >
              {t("This issue's search")}
            </button>
          )}
        </form>
        {own && <p className="hint">{t('Whatever you get here is filed as issue #{n}.', { n: issue.number })}</p>}
        {log?.searched && log.details && (
          <details className="cx-searchlog">
            <summary>
              {t('Last automatic search {when}:', { when: log.at ? new Date(log.at).toLocaleString(locale()) : '' })}{' '}
              {log.outcome === 'grabbed'
                ? t('downloaded a release')
                : log.outcome === 'review'
                  ? t('only doubtful matches, put on the review list')
                  : t('nothing usable in {n} results', { n: log.details.found })}
            </summary>
            <ul>
              {(log.details.rejected ?? []).map((r, k) => (
                <li key={k}>
                  <span className="cx-rel-title">{r.title}</span> <span className="hint">· {r.reject}</span>
                </li>
              ))}
            </ul>
          </details>
        )}
        {error && <p className="error-text">{error}</p>}
        {!list && !error && <p className="hint">{t('Searching…')}</p>}
        {list && list.length === 0 && <p className="hint">{t('Nothing found.')}</p>}
        <ul className="cx-rel">
          {(list ?? []).map((r) => (
            <li key={r.downloadUrl} className={r.matches ? '' : 'dim'}>
              <div>
                <span className="cx-rel-title">{r.title}</span>
                <span className="hint">
                  {r.indexerName} · {size(r.sizeBytes)} {r.blocklisted ? `· ${t('blocklisted')}` : ''}
                  {r.verdict?.reject ? ` · ${r.verdict.reject}` : ''}
                  {r.verdict?.confidence && r.verdict.confidence !== 'high' ? ` · ${r.verdict.confidence === 'low' ? t('low confidence') : t('medium confidence')}${r.verdict.notes?.length ? ': ' + r.verdict.notes.join(', ') : ''}` : ''}
                </span>
              </div>
              <button className="btn-sm" onClick={() => comicsApi.grab(issue.id, r).then(onGrabbed).catch((e: Error) => setError(e.message))}>
                <Icon name="download" size={14} /> {t('Get')}
              </button>
            </li>
          ))}
        </ul>
      </div>
    </div>
  )
}

// ---- reader ----

// Usherr: where you are in a comic, kept for you (home_positions.go).
// Read offline (Usherr Books), it is sent once the connection is back.
function keepComicPage(id: number, page: number, total: number, keepalive = false) {
  saveComicPlace(id, page, total, keepalive)
}

// Usherr: the reader stays open from one issue to the next: each issue gets a
// fresh start (its own place, pages and choices).
export function ComicReader({ issue, onClose, startPage = 0 }: { issue: ComicIssue; onClose: () => void; startPage?: number }) {
  const [now, setNow] = useState({ id: issue.id, from: startPage })
  return <ComicReaderIssue key={now.id} issue={{ id: now.id }} onClose={onClose} startPage={now.from} onNext={(id) => setNow({ id, from: 0 })} />
}

function readStoredLayout(): ReaderLayout {
  try {
    const v = localStorage.getItem('comic-view')
    return isLayout(v) ? v : 'flip'
  } catch {
    return 'flip'
  }
}

const layoutNames = (): { id: ReaderLayout; label: string; title: string }[] => [
  { id: 'flip', label: t('Page turn'), title: t('Turn pages like a paper comic') },
  { id: 'slide', label: t('One page'), title: t('One page at a time, with fit to width') },
  { id: 'double', label: t('Two pages'), title: t('Two pages side by side, the cover on its own') },
  { id: 'webtoon', label: t('Webtoon'), title: t('One long scroll with no gaps') },
  { id: 'panels', label: t('Panels'), title: t('Read panel by panel (guided view)') },
]

function ComicReaderIssue({ issue, onClose, startPage, onNext }: { issue: { id: number }; onClose: () => void; startPage: number; onNext: (id: number) => void }) {
  const [marksOpen, setMarksOpen] = useState(false)
  const [optsOpen, setOptsOpen] = useState(false)
  const [info, setInfo] = useState<ComicPages | null>(null)
  const [error, setError] = useState('')
  const [direction, setDirection] = useState<ReadDirection>('ltr')
  const [defaultSaved, setDefaultSaved] = useState(false)
  const [page, setPage] = useState(() => {
    try {
      return Number(localStorage.getItem(`comic-page-${issue.id}`)) || 1
    } catch {
      return 1
    }
  })
  const [fit, setFit] = useState<'height' | 'width'>(() => (window.innerWidth < window.innerHeight ? 'width' : 'height'))
  // Page turn (like a paper comic), slide (one page, zoomable fit), double (two
  // pages), webtoon (one long scroll) or panels (guided view: panel by panel).
  const [view, setView] = useState<ReaderLayout>(readStoredLayout)
  const rtl = direction === 'rtl' && view !== 'webtoon' // a scroll has no sides
  const flip = useRef<ComicFlipHandle>(null)
  const guided = useRef<ComicGuidedHandle>(null)
  const guidedSource = useMemo(() => comicGuidedSource(issue.id), [issue.id])
  const webtoon = useRef<ComicWebtoonHandle>(null)
  const [panelAt, setPanelAt] = useState({ index: -1, count: 0 })
  const onPanel = useCallback((index: number, count: number) => setPanelAt({ index, count }), [])
  const [speed, setSpeed] = useState<FlipSpeed>(() => {
    try {
      const v = localStorage.getItem('comic-flip-speed')
      return v === 'slow' || v === 'fast' ? v : 'normal'
    } catch {
      return 'normal'
    }
  })
  const flipMs = flipSpeeds.find((s) => s.id === speed)?.ms ?? 650
  const nextSpeed = () => {
    const i = flipSpeeds.findIndex((s) => s.id === speed)
    const v = flipSpeeds[(i + 1) % flipSpeeds.length].id
    setFlipStart(page) // the book is rebuilt at the page you are on
    setSpeed(v)
    try {
      localStorage.setItem('comic-flip-speed', v)
    } catch {
      /* private mode */
    }
  }
  const touch = useRef<{ x: number; y: number } | null>(null)
  const total = info?.pages ?? 0
  const pageUrls = useMemo(() => Array.from({ length: total }, (_, i) => comicsApi.pageUrl(issue.id, i + 1)), [total, issue.id])
  // The page the flip book opens at; later turns come from the book itself.
  const [flipStart, setFlipStart] = useState(page)
  // A choice here is remembered for this series, for you. Saved quietly: if it
  // can't be (offline), the reader still works this way until it's closed.
  const remember = (body: { direction?: ReadDirection; layout?: ReaderLayout }) => {
    setDefaultSaved(false)
    if (info?.seriesId) comicsApi.readerPrefs(info.seriesId, body).catch(() => undefined)
  }
  const switchView = (to?: ReaderLayout) => {
    const v = to ?? (view === 'flip' ? 'slide' : 'flip')
    if (v === 'flip') setFlipStart(page)
    if (v === 'double') setPage((p) => spreadEnd(p, total))
    setView(v)
    remember({ layout: v })
    try {
      localStorage.setItem('comic-view', v)
    } catch {
      /* private mode */
    }
  }
  const switchDirection = (d: ReadDirection) => {
    setFlipStart(page) // the book is rebuilt at the page you are on
    setDirection(d)
    remember({ direction: d })
  }
  const makeDefault = () => {
    comicsApi
      .readerPrefs(0, { direction, layout: view })
      .then(() => setDefaultSaved(true))
      .catch((e: Error) => setError(e.message))
  }

  // A new onClose from the page behind mustn't open the issue over again.
  const closeNow = useRef(onClose)
  closeNow.current = onClose
  useEffect(() => {
    // Usherr: the page you got to is kept for you (home_positions.go), so it
    // opens there on any device; this browser's own note is the fallback. A
    // comic read to the end opens at the first page again.
    const kept = fetch(`/api/home/position?item=comic:${issue.id}`, { credentials: 'include' })
      .then((x) => (x.ok ? (x.json() as Promise<{ position?: number }>) : null))
      .catch(() => null)
    Promise.all([comicsApi.pages(issue.id), kept])
      .then(([r, at]) => {
        if (r.pdf) {
          window.open(comicsApi.fileUrl(issue.id), '_blank', 'noopener')
          closeNow.current()
          return
        }
        const pages = r.pages ?? 0
        const from = at?.position ? Math.round(at.position) : 0
        let local = 1
        try {
          local = Number(localStorage.getItem(`comic-page-${issue.id}`)) || 1
        } catch {
          /* private mode */
        }
        // Opened from a bookmark: that page.
        const n = startPage > 0 ? startPage : from > 1 ? from : pages && local >= pages ? 1 : local
        const p = pages ? Math.min(Math.max(1, n), pages) : n
        // How you read this one (the file, the series, your choices): the server
        // decides. A stored copy from before it said so reads the old way.
        const layout = isLayout(r.layout) ? r.layout : readStoredLayout()
        setDirection(r.direction === 'rtl' ? 'rtl' : 'ltr')
        setView(layout)
        setPage(layout === 'double' && pages ? spreadEnd(p, pages) : p)
        setFlipStart(p)
        setInfo(r)
      })
      .catch((e: Error) => setError(e.message))
  }, [issue.id, startPage])
  // and told as you turn the pages, and on closing
  const pageNow = useRef({ page, total: 0 })
  useEffect(() => {
    if (!info?.pages) return
    pageNow.current = { page, total: info.pages }
    const tm = window.setTimeout(() => keepComicPage(issue.id, page, info.pages ?? 0), 1500)
    return () => window.clearTimeout(tm)
  }, [page, info, issue.id])
  useEffect(
    () => () => {
      const at = pageNow.current
      if (at.total > 0) keepComicPage(issue.id, at.page, at.total, true)
    },
    [issue.id],
  )
  // Going to a page, from the slider, a bookmark or Start over.
  const jumpTo = (n: number) => {
    setPage(view === 'double' ? spreadEnd(n, total) : n)
    if (view === 'flip') flip.current?.turnTo(n)
    if (view === 'webtoon') webtoon.current?.toPage(n)
  }
  const startOver = () => {
    jumpTo(1)
    setFlipStart(1)
  }

  const go = useCallback(
    (d: number) => {
      if (view === 'webtoon') {
        webtoon.current?.scroll(d * 0.85)
        return
      }
      if (view === 'double') {
        setPage((p) => stepSpread(p, d, total || 1))
        return
      }
      if (view === 'flip' && flip.current) {
        if (d > 0) flip.current.next()
        else flip.current.prev()
        return
      }
      if (view === 'panels' && guided.current) {
        if (d > 0) guided.current.next()
        else guided.current.prev()
        return
      }
      setPage((p) => Math.min(Math.max(1, p + d), total || 1))
    },
    [total, view],
  )
  useEffect(() => {
    if (!total) return
    try {
      localStorage.setItem(`comic-page-${issue.id}`, String(page))
      if (page >= total) localStorage.setItem(`comic-done-${issue.id}`, '1') // read to the end (story arcs: continue reading)
    } catch {
      /* private mode */
    }
    // Load the next pages ahead (a webtoon loads them as they come near).
    if (view !== 'webtoon') for (const n of [page + 1, page + 2]) if (n <= total) new Image().src = comicsApi.pageUrl(issue.id, n)
  }, [page, total, issue.id, view])
  useEffect(() => {
    const key = (e: KeyboardEvent) => {
      // Typing a bookmark's note doesn't turn pages.
      if ((e.target as HTMLElement | null)?.closest?.('input, textarea, select')) return
      const turn = keyTurn(e.key, rtl)
      if (turn !== 0) go(turn)
      else if (view === 'webtoon' && e.key === 'ArrowDown') webtoon.current?.scroll(0.2)
      else if (view === 'webtoon' && e.key === 'ArrowUp') webtoon.current?.scroll(-0.2)
      else if (e.key === 'Escape') onClose()
      else return
      e.preventDefault()
    }
    window.addEventListener('keydown', key)
    document.body.style.overflow = 'hidden'
    return () => {
      window.removeEventListener('keydown', key)
      document.body.style.overflow = ''
    }
  }, [go, onClose, rtl, view])

  const shown = view === 'double' ? spreadOf(page, total) : [page]
  const nextUp = info?.next
  const nextHere = nextUp?.have ? nextUp : info?.nextHave

  return createPortal(
    <div className="cx-reader" data-walk="cx-reader" role="dialog" aria-label={info?.title ?? t('Comic reader')}>
      <div className="cx-reader-bar" data-walk="cx-reader-bar">
        <button className="btn-sm" onClick={onClose} aria-label={t('Close the reader')}>
          <Icon name="x" size={16} />
        </button>
        <span className="cx-reader-title">{info?.title}</span>
        <span className="cx-reader-count">
          {total ? `${page} / ${total}` : ''}
          {view === 'panels' && panelAt.count > 0 && panelAt.index >= 0 ? ` · ${t('panel {n} of {count}', { n: panelAt.index + 1, count: panelAt.count })}` : ''}
        </span>
        {page > 1 && (
          <button type="button" className="btn-sm btn-with-icon" onClick={startOver} title={t('Back to the first page')}>
            <Icon name="skip-prev" size={14} /> {t('Start over')}
          </button>
        )}
        {total > 0 && (
          <button
            className={`btn-sm${marksOpen ? ' primary' : ''}`}
            onClick={() => {
              setOptsOpen(false)
              setMarksOpen((v) => !v)
            }}
            title={t('Bookmarks')}
            aria-label={t('Bookmarks')}
            aria-pressed={marksOpen}
          >
            <Icon name="bookmark" size={14} />
          </button>
        )}
        {total > 0 && (
          <button
            className={`btn-sm${optsOpen ? ' primary' : ''}`}
            data-walk="cx-reader-opts"
            onClick={() => {
              setMarksOpen(false)
              setOptsOpen((v) => !v)
            }}
            title={t('Reading direction and how pages show')}
            aria-label={t('Reading direction and how pages show')}
            aria-pressed={optsOpen}
          >
            <Icon name="sliders" size={14} />
          </button>
        )}
        {view !== 'panels' && (
          <button className="btn-sm" data-walk="cx-view-toggle" onClick={() => switchView()} title={view === 'flip' ? t('Show one page at a time, with fit to width') : t('Turn pages like a paper comic')}>
            {view === 'flip' ? t('Slide') : t('Page turn')}
          </button>
        )}
        <button
          className={`btn-sm${view === 'panels' ? ' primary' : ''}`}
          data-walk="cx-view-panels"
          onClick={() => switchView(view === 'panels' ? 'flip' : 'panels')}
          title={view === 'panels' ? t('Back to whole pages') : t('Read panel by panel (guided view)')}
          aria-pressed={view === 'panels'}
        >
          {t('Panels')}
        </button>
        {view === 'panels' && panelAt.count > 0 && (
          <button className="btn-sm" onClick={() => guided.current?.wholePage()} title={t('See the whole page; the next tap goes on to the first panel')}>
            {t('Whole page')}
          </button>
        )}
        {view === 'flip' && (
          <button className="btn-sm" onClick={nextSpeed} title={t('How fast the pages turn')}>
            {t('Speed')}: {flipSpeeds.find((s) => s.id === speed)?.label}
          </button>
        )}
        {(view === 'slide' || view === 'double') && (
          <button className="btn-sm" onClick={() => setFit((f) => (f === 'height' ? 'width' : 'height'))} aria-label={t('Switch fit')}>
            {fit === 'height' ? t('Fit width') : t('Fit page')}
          </button>
        )}
      </div>
      {marksOpen && total > 0 && (
        <div className="cx-marks-panel" aria-label={t('Bookmarks')}>
          <Marks
            item={`comic:${issue.id}`}
            here={{ position: `page:${page}`, label: t('Page {n}', { n: page }), percent: Math.round((page / total) * 1000) / 10 }}
            onJump={(m) => {
              jumpTo(Math.min(pageOfMark(m.position), total))
              setMarksOpen(false)
            }}
          />
        </div>
      )}
      {optsOpen && total > 0 && (
        <div className="cx-marks-panel cx-reader-opts" aria-label={t('Reading direction and how pages show')}>
          <h3>{t('Reading direction')}</h3>
          <div className="seg" role="group" aria-label={t('Reading direction')}>
            <button className={direction === 'ltr' ? 'active' : ''} aria-pressed={direction === 'ltr'} disabled={view === 'webtoon'} onClick={() => switchDirection('ltr')}>
              {t('Left to right')}
            </button>
            <button className={direction === 'rtl' ? 'active' : ''} aria-pressed={direction === 'rtl'} disabled={view === 'webtoon'} onClick={() => switchDirection('rtl')}>
              {t('Right to left')}
            </button>
          </div>
          <h3>{t('How pages show')}</h3>
          <div className="seg cx-reader-layouts" role="group" aria-label={t('How pages show')}>
            {layoutNames().map((l) => (
              <button key={l.id} className={view === l.id ? 'active' : ''} aria-pressed={view === l.id} title={l.title} onClick={() => switchView(l.id)}>
                {l.label}
              </button>
            ))}
          </div>
          <p className="hint">{t('Remembered for this series, just for you.')}</p>
          <button className="btn-sm" disabled={defaultSaved} onClick={makeDefault}>
            {defaultSaved ? t('Saved as your default') : t('Use these for other comics too')}
          </button>
        </div>
      )}
      {error && <p className="error-text cx-reader-msg">{error}</p>}
      {!info && !error && <p className="hint cx-reader-msg">{t('Opening…')}</p>}
      {total > 0 && view === 'flip' && (
        <Suspense fallback={<p className="hint cx-reader-msg">{t('Opening…')}</p>}>
          <ComicFlip ref={flip} key={`${issue.id}-${flipStart}-${speed}-${direction}`} pages={pageUrls} start={flipStart} onPage={setPage} flipMs={flipMs} rtl={rtl} />
        </Suspense>
      )}
      {total > 0 && view === 'panels' && <ComicGuided ref={guided} key={direction} source={guidedSource} page={page} total={total} onPage={setPage} onPanel={onPanel} rtl={rtl} />}
      {total > 0 && view === 'webtoon' && <ComicWebtoon ref={webtoon} pageUrl={(n) => comicsApi.pageUrl(issue.id, n)} total={total} start={page} onPage={setPage} />}
      {total > 0 && (view === 'slide' || view === 'double') && (
        <div
          className={`cx-reader-page fit-${fit}${shown.length === 2 ? ' two' : ''}${rtl ? ' rtl' : ''}`}
          onClick={(e) => {
            const r = (e.currentTarget as HTMLElement).getBoundingClientRect()
            go(tapTurn((e.clientX - r.left) / r.width, rtl))
          }}
          onTouchStart={(e) => (touch.current = { x: e.touches[0].clientX, y: e.touches[0].clientY })}
          onTouchEnd={(e) => {
            const start = touch.current
            touch.current = null
            if (!start) return
            const dx = e.changedTouches[0].clientX - start.x
            const dy = e.changedTouches[0].clientY - start.y
            if (Math.abs(dx) > 50 && Math.abs(dx) > Math.abs(dy)) {
              e.preventDefault()
              go(swipeTurn(dx, rtl))
            }
          }}
        >
          {shown.map((n) => (
            <img key={n} src={comicsApi.pageUrl(issue.id, n)} alt={t('Page {n}', { n })} draggable={false} />
          ))}
        </div>
      )}
      {total > 0 && page >= total && !!info?.seriesId && (
        <div className="cx-reader-end" data-walk="cx-next-issue">
          {nextUp && !nextUp.have && <span className="hint">{t('Issue #{n} isn’t here yet.', { n: nextUp.number })}</span>}
          {nextHere ? (
            <button className="btn-sm primary btn-with-icon" onClick={() => onNext(nextHere.id)}>
              {nextUp?.have ? t('Next issue') : t('Go on to #{n}', { n: nextHere.number })} <Icon name="open" size={14} />
            </button>
          ) : (
            !nextUp && <span className="hint">{t('That’s the last issue of this series.')}</span>
          )}
        </div>
      )}
      {total > 0 && (
        <input
          className="cx-reader-slider"
          type="range"
          dir={rtl ? 'rtl' : 'ltr'}
          min={1}
          max={total}
          value={page}
          onChange={(e) => jumpTo(Number(e.target.value))}
          aria-label={t('Page')}
        />
      )}
    </div>,
    document.body,
  )
}
