// Home fork: the NSFW module's pages (administrators only).
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import LibraryTabs from './LibraryTabs' // library hook (home fork)
import { PullDrawer } from '../components/CompactBar' // Usherr
import { createPortal } from 'react-dom'
import { Link, useNavigate, useParams, useSearchParams } from 'react-router-dom'
import Icon from '../components/Icon'
import { imageSrc, nsfwApi, type LookedUpVideo, type Video, type VideoCounts, type VideoRelease, type VideoPerformer, type VideoSettings, type VideoStudio, type SearchStatus, type StashTag, type WhisparrReport } from './nsfwApi'
import './nsfw.css'
import { playTitle } from './Player'
import VideoQueue from './NsfwQueue'
import { TaskRuleEditor } from './SchedulesSettings'
import { schedulesApi, type SchedulesView, type TaskRule } from './schedulesApi'
import { locale, plural, t, tx } from '../i18n'
import { useAuth } from '../AuthContext' // home fork
import { isAdmin } from '../api' // home fork
import Shelves from './Shelves' // home fork
import { groupShelves, monthShelves, RowsByPick, ShelfControls, useShelfView } from './shelfKit' // home fork
import { PosterSizeSlider, usePosterSize } from './posterSize' // Usherr

type Tab = 'library' | 'following' | 'find' | 'queue' | 'settings'

const TABS: { key: Tab; label: string }[] = [
  { key: 'library', label: t('Library') },
  { key: 'following', label: t('Following') },
  { key: 'find', label: t('Find') },
  { key: 'queue', label: t('Queue') },
]

function fmtSize(bytes: number): string {
  if (!bytes) return ''
  const gb = bytes / 1e9
  return gb >= 1 ? `${gb.toFixed(1)} GB` : `${Math.round(bytes / 1e6)} MB`
}

function fmtDuration(sec: number): string {
  if (!sec) return ''
  const m = Math.round(sec / 60)
  return m >= 60 ? t('{h} h {m} min', { h: Math.floor(m / 60), m: m % 60 }) : t('{n} min', { n: m })
}

function statusLabel(s: Video): string {
  if (s.status === 'downloaded') return s.quality ? `${t('Downloaded')} · ${s.quality}` : t('Downloaded')
  if (s.status === 'downloading') return t('Downloading')
  return s.wanted ? t('Wanted') : t('Not wanted')
}

function VideoCard({ s, to, action, selected, onToggle, onPlay }: { s: Video | LookedUpVideo; to?: string; action?: React.ReactNode; selected?: boolean; onToggle?: () => void; onPlay?: () => void }) {
  const thumb = <div className="sc-thumb">{s.imageUrl ? <img src={imageSrc(s.imageUrl)} alt="" loading="lazy" /> : <Icon name="film" size={32} />}</div>
  if (onPlay && !onToggle) {
    return (
      <div className={`sc-card st-${s.status}`}>
        <button type="button" className="sc-playthumb" onClick={onPlay} aria-label={s.title ? t('Play {title}', { title: s.title }) : t('Play this video')}>
          {thumb}
          <span className="sc-playicon" aria-hidden="true">
            <Icon name="play" size={28} />
          </span>
        </button>
        <Link to={to ?? '#'} className="sc-metalink">
          <div className="sc-meta">
            <div className="sc-title">{s.title || t('Untitled video')}</div>
            <div className="sc-sub">
              {s.studioName}
              {s.releaseDate && ` · ${s.releaseDate}`}
            </div>
            {s.performers.length > 0 && <div className="sc-sub sc-people">{s.performers.map((p) => p.name).join(', ')}</div>}
          </div>
        </Link>
        <div className="sc-foot">
          <span className="sc-status">{statusLabel(s)}</span>
          {action}
        </div>
      </div>
    )
  }
  const body = (
    <>
      {thumb}
      <div className="sc-meta">
        <div className="sc-title">{s.title || t('Untitled video')}</div>
        <div className="sc-sub">
          {s.studioName}
          {s.releaseDate && ` · ${s.releaseDate}`}
        </div>
        {s.performers.length > 0 && <div className="sc-sub sc-people">{s.performers.map((p) => p.name).join(', ')}</div>}
      </div>
    </>
  )
  return (
    <div className={`sc-card st-${s.status}${selected ? ' sc-selected' : ''}`}>
      {onToggle ? (
        <button type="button" className="sc-pick" onClick={onToggle} aria-pressed={selected} aria-label={selected ? t('Unselect this video') : t('Select this video')}>
          <input type="checkbox" checked={!!selected} readOnly tabIndex={-1} />
          {body}
        </button>
      ) : to ? (
        <Link to={to}>{body}</Link>
      ) : (
        body
      )}
      <div className="sc-foot">
        <span className="sc-status">{'inLibrary' in s && !s.inLibrary ? t('Not in library') : statusLabel(s)}</span>
        {action}
      </div>
    </div>
  )
}

export default function Nsfw() {
  const [params, setParams] = useSearchParams()
  const tab = (params.get('tab') as Tab) || 'library'
  const [settings, setSettings] = useState<VideoSettings | null>(null)
  const [error, setError] = useState('')
  // home fork: an account given the NSFW 18+ switch sees the section, not its settings or queue
  const admin = isAdmin(useAuth().user)
  const [memberReady, setMemberReady] = useState(false)
  const posterSize = usePosterSize('nsfw', 260, 180, 480) // landscape cards

  const loadSettings = useCallback(() => {
    if (!admin) {
      fetch('/api/nsfw/enabled', { credentials: 'include' })
        .then((r) => (r.ok ? r.json() : { enabled: false }))
        .then((d: { enabled: boolean }) => setMemberReady(d.enabled))
        .catch(() => setMemberReady(false))
      return
    }
    nsfwApi.settings().then(setSettings).catch((e: Error) => setError(e.message))
  }, [admin])
  useEffect(loadSettings, [loadSettings])

  const setTab = (tg: Tab) => setParams(tg === 'library' ? {} : { tab: tg })
  const ready = admin ? settings?.enabled && settings.keySet && settings.path : memberReady
  const shown: Tab = admin && settings && !ready ? 'settings' : !admin && (tab === 'settings' || tab === 'queue') ? 'library' : tab

  return (
    <div className="nsfw-page" style={posterSize.style}>
      <PullDrawer className="sc-pull">{/* Usherr: pulls down from under the top bar, like the Library's */}
      <div className="lib-tabs-row">
        <LibraryTabs current="nsfw" />
      </div>
      <div className="toolbar sc-tabs">
        <div className="seg" role="tablist" aria-label={t('NSFW')}>
          {TABS.filter((tg) => admin || (tg.key !== 'queue' && tg.key !== 'settings')).map((tg) => (
            <button key={tg.key} role="tab" aria-selected={shown === tg.key} className={shown === tg.key ? 'active' : ''} onClick={() => setTab(tg.key)} disabled={!ready}>
              {tg.label}
            </button>
          ))}
        </div>
        {shown === 'library' && ready && <PosterSizeSlider value={posterSize} />}
        {admin && <button className={`btn-sm sc-gear${shown === 'settings' ? ' primary' : ''}`} onClick={() => setTab(shown === 'settings' && ready ? 'library' : 'settings')} aria-label={t('NSFW settings')} title={t('NSFW settings')}>
          <Icon name="sliders" size={16} />
        </button>}
      </div>
      </PullDrawer>
      {error && <p className="error-text">{error}</p>}
      {!admin && !memberReady && <p className="hint">{t('The NSFW 18+ section isn’t set up yet.')}</p>}
      {admin && settings && shown === 'settings' && <SettingsTab settings={settings} onSaved={setSettings} />}
      {ready && shown === 'library' && <LibraryTab />}
      {ready && shown === 'following' && <FollowingTab />}
      {ready && shown === 'find' && <FindTab />}
      {admin && ready && shown === 'queue' && <VideoQueue />}
    </div>
  )
}

// Popover draws a panel or menu on top of everything, attached to <body>,
// under the element anchor (a bottom sheet on phones, see videos.css).
function Popover({ anchor, align, className, label, role, onClose, children }: { anchor: HTMLElement | null; align: 'left' | 'right'; className: string; label: string; role: string; onClose: () => void; children: React.ReactNode }) {
  const r = anchor?.getBoundingClientRect()
  const style: React.CSSProperties = r ? (align === 'left' ? { top: r.bottom + 6, left: Math.max(8, r.left) } : { top: r.bottom + 6, right: Math.max(8, window.innerWidth - r.right) }) : {}
  return createPortal(
    <>
      <div className="sc-pop-backdrop" onClick={onClose} />
      <div className={`sc-pop ${className}`} style={style} role={role} aria-label={label}>
        {children}
      </div>
    </>,
    document.body,
  )
}

// FollowAllMenu: "Follow + add all" with a choice to search right away.
function FollowAllMenu({ kind, stashId, name, onDone, onError }: { kind: 'performer' | 'studio'; stashId: string; name: string; onDone: (msg: string) => void; onError: (msg: string) => void }) {
  const [open, setOpen] = useState(false)
  const [busy, setBusy] = useState(false)
  const btn = useRef<HTMLButtonElement>(null)
  const go = (search: boolean) => {
    setOpen(false)
    setBusy(true)
    onDone(search ? t('Adding every video of {name} and starting the searches…', { name }) : t('Adding every video of {name}…', { name }))
    nsfwApi
      .follow(kind, stashId, true, search)
      .then((r) => {
        const added = plural(r.added, 'Following {name}. Added {n} video as wanted', 'Following {name}. Added {n} videos as wanted', { name })
        onDone(
          search
            ? t('{added}; searching {n} now (see Library for progress, Queue for downloads).', { added, n: r.searching })
            : t("{added}; they're searched a few at a time, or select them in Library and press Search.", { added }),
        )
      })
      .catch((e: Error) => onError(e.message))
      .finally(() => setBusy(false))
  }
  return (
    <>
      <button ref={btn} className="btn-sm btn-with-icon" disabled={busy} aria-haspopup="menu" aria-expanded={open} onClick={() => setOpen(!open)}>
        {busy ? t('Adding…') : t('Follow + add all')} <Icon name="open" size={12} style={{ transform: 'rotate(90deg)' }} />
      </button>
      {open && (
        <Popover anchor={btn.current} align="left" className="sc-menu" role="menu" label={t('Follow and add all')} onClose={() => setOpen(false)}>
          <button role="menuitem" onClick={() => go(false)}>
            {t('Add all')} <span className="hint">— {t('search later')}</span>
          </button>
          <button role="menuitem" onClick={() => go(true)}>
            {t('Add all and search now')}
          </button>
        </Popover>
      )}
    </>
  )
}

// ---- Library ----

type Filter = 'all' | 'wanted' | 'notwanted' | 'downloaded'
const FILTERS: { key: Filter; label: string }[] = [
  { key: 'all', label: t('All') },
  { key: 'wanted', label: t('Wanted') },
  { key: 'notwanted', label: t('Not wanted') },
  { key: 'downloaded', label: t('Downloaded') },
]

type Sort = 'added' | 'newest' | 'oldest' | 'az' | 'za'
const SORTS: { key: Sort; label: string }[] = [
  { key: 'added', label: t('Newly added') },
  { key: 'newest', label: t('Release date: newest') },
  { key: 'oldest', label: t('Release date: oldest') },
  { key: 'az', label: t('Title: A to Z') },
  { key: 'za', label: t('Title: Z to A') },
]

function readSort(): Sort {
  try {
    const v = localStorage.getItem('nsfw-sort') as Sort | null
    if (v && SORTS.some((s) => s.key === v)) return v
  } catch {
    // no storage: use the default
  }
  return 'added'
}

function sortNsfw(list: Video[], sort: Sort): Video[] {
  // Leading quotes, brackets and the like don't count when sorting by title.
  const title = (s: Video) => (s.title || '').toLocaleLowerCase().replace(/^[^\p{L}\p{N}]+/u, '')
  const out = [...list]
  switch (sort) {
    case 'added':
      return out.sort((a, b) => b.addedAt.localeCompare(a.addedAt) || b.id - a.id)
    case 'newest':
      return out.sort((a, b) => b.releaseDate.localeCompare(a.releaseDate) || title(a).localeCompare(title(b)))
    case 'oldest':
      return out.sort((a, b) => (a.releaseDate || '9999').localeCompare(b.releaseDate || '9999') || title(a).localeCompare(title(b)))
    case 'az':
      return out.sort((a, b) => title(a).localeCompare(title(b)))
    case 'za':
      return out.sort((a, b) => title(b).localeCompare(title(a)))
  }
}

// matchesText: every word typed appears in the title, studio, a performer or the code.
function matchesText(s: Video, text: string): boolean {
  const words = text.toLocaleLowerCase().split(/\s+/).filter(Boolean)
  if (words.length === 0) return true
  const hay = [s.title, s.studioName, s.code, s.releaseDate, ...s.performers.map((p) => `${p.name} ${p.as ?? ''}`)].join(' ').toLocaleLowerCase()
  return words.every((w) => hay.includes(w))
}

// useStashSuggestions looks text up on StashDB once typing pauses (debounced),
// cancelling a lookup that a newer one replaces.
function useStashSuggestions(text: string, enabled: boolean) {
  const [found, setFound] = useState<LookedUpVideo[]>([])
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  useEffect(() => {
    const tg = text.trim()
    setError('')
    if (!enabled || tg.length < 3) {
      setFound([])
      setBusy(false)
      return
    }
    const ctl = new AbortController()
    setBusy(true)
    const timer = window.setTimeout(() => {
      nsfwApi
        .lookupNsfw(tg, ctl.signal)
        .then((r) => setFound(r.nsfw.filter((s) => !s.inLibrary)))
        .catch((e: Error) => {
          if (!ctl.signal.aborted) setError(e.message)
        })
        .finally(() => {
          if (!ctl.signal.aborted) setBusy(false)
        })
    }, 500)
    return () => {
      window.clearTimeout(timer)
      ctl.abort()
    }
  }, [text, enabled])
  return { found, busy, error, setFound }
}

// Who is in a video, worked out from the performers' genders on StashDB.
type Who = 'all' | 'straight' | 'lesbian' | 'gay' | 'trans' | 'solo'
const WHO: { key: Who; label: string }[] = [
  { key: 'all', label: t('Everyone') },
  { key: 'straight', label: t('Straight') },
  { key: 'lesbian', label: t('Lesbian') },
  { key: 'gay', label: t('Gay') },
  { key: 'trans', label: t('Trans') },
  { key: 'solo', label: t('Solo') },
]

// knowsWho: every performer's gender is known.
function knowsWho(s: Video): boolean {
  return s.performers.length > 0 && s.performers.every((p) => !!p.gender)
}

function isWho(s: Video, who: Who): boolean {
  if (who === 'all') return true
  if (!knowsWho(s)) return false
  const g = s.performers.map((p) => p.gender as string)
  const trans = g.some((x) => x.startsWith('TRANSGENDER'))
  const female = g.filter((x) => x === 'FEMALE').length
  const male = g.filter((x) => x === 'MALE').length
  switch (who) {
    case 'trans':
      return trans
    case 'solo':
      return g.length === 1
    case 'straight':
      return !trans && female > 0 && male > 0
    case 'lesbian':
      return !trans && g.length > 1 && female === g.length
    case 'gay':
      return !trans && g.length > 1 && male === g.length
  }
}

function hasTag(s: Video, tag: string): boolean {
  return !tag || (s.tags ?? []).some((tg) => tg.toLocaleLowerCase() === tag.toLocaleLowerCase())
}

function LibraryTab() {
  const [filter, setFilter] = useState<Filter>('all')
  const [list, setList] = useState<Video[] | null>(null)
  const [counts, setCounts] = useState<VideoCounts | null>(null)
  const [error, setError] = useState('')
  const [selecting, setSelecting] = useState(false)
  const [picked, setPicked] = useState<Set<number>>(new Set())
  const [busy, setBusy] = useState(false)
  const [note, setNote] = useState('')
  const [sort, setSortState] = useState<Sort>(readSort)
  const [text, setText] = useState('')
  const [who, setWho] = useState<Who>('all')
  const [tag, setTag] = useState('')
  const [tagText, setTagText] = useState('')
  const [open, setOpen] = useState<null | 'filters' | 'more'>(null)
  const filtersBtn = useRef<HTMLButtonElement>(null)
  const moreBtn = useRef<HTMLButtonElement>(null)
  useEffect(() => {
    if (!open) return
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && setOpen(null)
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [open])
  const setSort = (v: Sort) => {
    setSortState(v)
    try {
      localStorage.setItem('nsfw-sort', v)
    } catch {
      // remembering the choice is optional
    }
  }
  const load = useCallback(() => {
    setList(null)
    nsfwApi
      .list(filter === 'all' ? {} : { status: filter })
      .then((r) => {
        setList(r.nsfw)
        setCounts(r.counts)
      })
      .catch((e: Error) => setError(e.message))
  }, [filter])
  useEffect(load, [load])
  useEffect(() => setPicked(new Set()), [filter])

  const toggle = (id: number) =>
    setPicked((p) => {
      const n = new Set(p)
      if (n.has(id)) n.delete(id)
      else n.add(id)
      return n
    })
  const shown = useMemo(() => (list ? sortNsfw(list.filter((s) => matchesText(s, text) && isWho(s, who) && hasTag(s, tag)), sort) : null), [list, text, sort, who, tag])
  const suggest = useStashSuggestions(text, !!list && (shown?.length ?? 0) === 0)
  const suggested = suggest.found.filter((s) => isWho(s, who) && hasTag(s, tag))
  // Tags in the library, most used first, for the tag menu.
  const tagCounts = useMemo(() => {
    const m = new Map<string, number>()
    for (const s of list ?? []) for (const tg of s.tags ?? []) m.set(tg, (m.get(tg) ?? 0) + 1)
    return [...m.entries()].sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0]))
  }, [list])
  const noDetails = (list ?? []).filter((s) => !s.detailsAt).length
  const activeFilters = (filter !== 'all' ? 1 : 0) + (who !== 'all' ? 1 : 0) + (tag ? 1 : 0)
  const clearFilters = () => {
    setFilter('all')
    setWho('all')
    setTag('')
    setTagText('')
  }
  const [filling, setFilling] = useState(false)
  const addSuggested = (s: LookedUpVideo, search: boolean) =>
    nsfwApi
      .add(s.stashId, search)
      .then(() => {
        suggest.setFound((l) => l.filter((x) => x.stashId !== s.stashId))
        setNote(search ? t('Added {title}. Searching for it now.', { title: s.title || t('the video') }) : t('Added {title} as wanted.', { title: s.title || t('the video') }))
        load()
      })
      .catch((e: Error) => setError(e.message))
  const shownIDs = (shown ?? []).map((s) => s.id)
  const searchPicked = (shown ?? []).filter((s) => picked.has(s.id) && s.wanted && s.status === 'missing').length
  const [progress, setProgress] = useState<SearchStatus | null>(null)
  const pollProgress = useCallback(() => {
    nsfwApi.searchStatus().then(setProgress).catch(() => undefined)
  }, [])
  useEffect(() => {
    pollProgress()
    const tg = window.setInterval(pollProgress, 4000)
    return () => window.clearInterval(tg)
  }, [pollProgress])
  const emptyPicked = (shown ?? []).filter((s) => picked.has(s.id) && s.status !== 'downloaded' && !s.filePath).length

  const bulk = (action: 'want' | 'unwant' | 'removeEmpty' | 'search') => {
    const ids = [...picked]
    if (ids.length === 0) return
    if (action === 'removeEmpty' && !confirm(plural(emptyPicked, 'Remove {n} video without a file from the library? Videos with a file, or still downloading, are kept. No file is deleted.', 'Remove {n} videos without a file from the library? Videos with a file, or still downloading, are kept. No file is deleted.'))) return
    setBusy(true)
    setError('')
    nsfwApi
      .bulk(ids, action)
      .then((r) => {
        if (action === 'search') {
          setNote(
            r.skipped
              ? t('Searching {n} videos in the background; {skipped} skipped (already downloaded, downloading or not wanted). Found releases appear in Queue.', { n: r.changed, skipped: r.skipped })
              : t('Searching {n} videos in the background. Found releases appear in Queue.', { n: r.changed }),
          )
          pollProgress()
        } else {
          const done = action === 'want' ? t('{n} marked wanted', { n: r.changed }) : action === 'unwant' ? t('{n} marked not wanted', { n: r.changed }) : t('{n} removed', { n: r.changed })
          setNote(r.skipped ? t('{done}, {skipped} kept because they have a file or are downloading.', { done, skipped: r.skipped }) : `${done}.`)
        }
        setPicked(new Set())
        load()
      })
      .catch((e: Error) => setError(e.message))
      .finally(() => setBusy(false))
  }

  // home fork: shelves, rows that scroll sideways, by studio, performer…
  const shelfView = useShelfView<'studio' | 'performer' | 'tag' | 'added'>('nsfw', ['studio', 'performer', 'tag', 'added'], 'studio')
  const shelves = useMemo(() => {
    if (!shown || !shelfView.on) return []
    const newest = (a: Video, b: Video) => (b.releaseDate || '').localeCompare(a.releaseDate || '')
    switch (shelfView.by) {
      case 'performer':
        return groupShelves(shown, (s) => s.performers.filter((p) => p.gender !== 'MALE').map((p) => p.name), { prefix: 'who', min: 2, order: newest })
      case 'tag':
        return groupShelves(shown, (s) => s.tags, { prefix: 'tag', min: 3, order: newest, seeAll: (g) => (setTag(g), shelfView.setOn(false)) })
      case 'added':
        return monthShelves(shown, (s) => s.addedAt)
      default:
        return groupShelves(shown, (s) => [s.studioName], { prefix: 'studio', rest: t('Studio unknown'), order: newest })
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [shown, shelfView.on, shelfView.by])
  const videoCard = (s: Video) => (
    <VideoCard key={s.id} s={s} to={`/nsfw/${s.id}`} selected={picked.has(s.id)} onToggle={selecting ? () => toggle(s.id) : undefined} onPlay={s.status === 'downloaded' && s.filePath ? () => playTitle('video', s.id, s.title || t('Video')) : undefined} />
  )

  return (
    <section>
      <div className="sc-bar">
        <input className="sc-search" type="search" value={text} onChange={(e) => setText(e.target.value)} placeholder={t('Search your videos: title, studio, performer')} aria-label={t('Search your videos')} />
        <div className="sc-bar-row">
          <div className="sc-pop-wrap">
            <button ref={filtersBtn} className={`btn-sm btn-with-icon${activeFilters > 0 ? ' primary' : ''}`} aria-haspopup="dialog" aria-expanded={open === 'filters'} onClick={() => setOpen(open === 'filters' ? null : 'filters')}>
              <Icon name="sliders" size={14} /> {t('Filters')}{activeFilters > 0 ? ` · ${activeFilters}` : ''}
            </button>
            {open === 'filters' && (
              <Popover anchor={filtersBtn.current} align="left" className="sc-sheet" role="dialog" label={t('Filters')} onClose={() => setOpen(null)}>
                  <div className="sc-sheet-head">
                    <strong>{t('Filters')}</strong>
                    <button className="btn-sm" onClick={() => setOpen(null)} aria-label={t('Close filters')}>
                      <Icon name="x" size={14} />
                    </button>
                  </div>
                  <label className="sc-field">
                    <span>{t('Status')}</span>
                    <select value={filter} onChange={(e) => setFilter(e.target.value as Filter)}>
                      {FILTERS.map((f) => (
                        <option key={f.key} value={f.key}>
                          {f.label}
                        </option>
                      ))}
                    </select>
                  </label>
                  <label className="sc-field">
                    <span>{t('Who')}</span>
                    <select value={who} onChange={(e) => setWho(e.target.value as Who)}>
                      {WHO.map((w) => (
                        <option key={w.key} value={w.key}>
                          {w.label}
                        </option>
                      ))}
                    </select>
                  </label>
                  <div className="sc-field">
                    <span>{t('Tag')}{tag ? `: ${tag}` : ''}</span>
                    <input type="search" value={tagText} onChange={(e) => setTagText(e.target.value)} placeholder={t('Find a tag ({n})', { n: tagCounts.length })} aria-label={t('Find a tag')} />
                    <div className="sc-taglist" role="listbox" aria-label={t('Tags')}>
                      <button role="option" aria-selected={!tag} className={!tag ? 'active' : ''} onClick={() => setTag('')}>
                        {t('Any tag')}
                      </button>
                      {tagCounts
                        .filter(([tg]) => tg.toLocaleLowerCase().includes(tagText.trim().toLocaleLowerCase()))
                        .slice(0, 80)
                        .map(([tg, n]) => (
                          <button key={tg} role="option" aria-selected={tag === tg} className={tag === tg ? 'active' : ''} onClick={() => setTag(tg)}>
                            {tg} <span className="hint">{n}</span>
                          </button>
                        ))}
                    </div>
                  </div>
                  <div className="sc-sheet-foot">
                    <button className="btn-sm" disabled={activeFilters === 0} onClick={clearFilters}>
                      {t('Clear all')}
                    </button>
                    <button className="btn-sm primary" onClick={() => setOpen(null)}>
                      {plural(shown?.length ?? 0, 'Show {n} video', 'Show {n} videos')}
                    </button>
                  </div>
              </Popover>
            )}
          </div>
          <select className="sc-sort-compact" value={sort} onChange={(e) => setSort(e.target.value as Sort)} aria-label={t('Sort')}>
            {SORTS.map((o) => (
              <option key={o.key} value={o.key}>
                {o.label}
              </option>
            ))}
          </select>
          <ShelfControls view={shelfView} />
          <div className="sc-pop-wrap sc-more">
            <button ref={moreBtn} className="btn-sm sc-more-btn" aria-haspopup="menu" aria-expanded={open === 'more'} aria-label={t('More')} onClick={() => setOpen(open === 'more' ? null : 'more')}>
              <Icon name="more" size={16} />
              {noDetails > 0 && <span className="sc-dot" aria-hidden="true" />}
            </button>
            {open === 'more' && (
              <Popover anchor={moreBtn.current} align="right" className="sc-menu" role="menu" label={t('More')} onClose={() => setOpen(null)}>
                  <button
                    role="menuitem"
                    onClick={() => {
                      setSelecting(!selecting)
                      setPicked(new Set())
                      setOpen(null)
                    }}
                  >
                    {selecting ? t('Done selecting') : t('Select videos')}
                  </button>
                  <button
                    role="menuitem"
                    onClick={() => {
                      setOpen(null)
                      nsfwApi
                        .checkNow()
                        .then((r) => setNote(r.message))
                        .catch((e: Error) => setError(e.message))
                    }}
                  >
                    {t('Look for new videos now')}
                  </button>
                  {noDetails > 0 && (
                    <button
                      role="menuitem"
                      disabled={filling}
                      onClick={() => {
                        setOpen(null)
                        setFilling(true)
                        nsfwApi
                          .fillDetails()
                          .then((r) => setNote(`${r.message} ${plural(noDetails, '{n} video to do; reload in a minute or two.', '{n} videos to do; reload in a minute or two.')}`))
                          .catch((e: Error) => setError(e.message))
                      }}
                    >
                      {t('Get missing details ({n})', { n: noDetails })}
                    </button>
                  )}
              </Popover>
            )}
          </div>
        </div>
        <div className="sc-chips">
          {filter !== 'all' && (
            <button className="sc-chip" onClick={() => setFilter('all')} aria-label={t('Remove filter {name}', { name: FILTERS.find((f) => f.key === filter)?.label ?? '' })}>
              {FILTERS.find((f) => f.key === filter)?.label} <Icon name="x" size={12} />
            </button>
          )}
          {who !== 'all' && (
            <button className="sc-chip" onClick={() => setWho('all')} aria-label={t('Remove filter {name}', { name: WHO.find((w) => w.key === who)?.label ?? '' })}>
              {WHO.find((w) => w.key === who)?.label} <Icon name="x" size={12} />
            </button>
          )}
          {tag && (
            <button className="sc-chip" onClick={() => setTag('')} aria-label={t('Remove tag {name}', { name: tag })}>
              {tag} <Icon name="x" size={12} />
            </button>
          )}
          {progress?.running && progress.held && (
            <span className="sc-progress" role="status">
              {progress.heldUntil ? t('{n} waiting for the schedule ({time})', { n: progress.waiting, time: clock(progress.heldUntil) }) : t('{n} waiting for the schedule', { n: progress.waiting })} ·{' '}
              <button className="linkish" onClick={() => nsfwApi.runHeldSearches().then(setProgress).catch((e: Error) => setError(e.message))}>
                {t('Search now anyway')}
              </button>
            </span>
          )}
          {progress?.running && !progress.held && (
            <span className="sc-progress" role="status">
              {t('Searching {done} of {total} · {found} found', { done: Math.min(progress.done + 1, progress.total), total: progress.total, found: progress.found })}
            </span>
          )}
          {counts && <span className="sc-count">{shown && (activeFilters > 0 || text.trim()) ? t('{shown} of {total}', { shown: shown.length, total: counts.nsfw }) : plural(counts.nsfw, '{n} video', '{n} videos')}</span>}
        </div>
      </div>
      {selecting && (
        <div className="toolbar sc-bulkbar">
          <strong>{t('{n} selected', { n: picked.size })}</strong>
          <button className="btn-sm" onClick={() => setPicked(new Set(shownIDs))} disabled={shownIDs.length === 0}>
            {t('Select all {n} in view', { n: shownIDs.length })}
          </button>
          <button className="btn-sm" onClick={() => setPicked(new Set())} disabled={picked.size === 0}>
            {t('Clear')}
          </button>
          <span className="sc-bulk-actions">
            <button className="btn-sm primary btn-with-icon" disabled={busy || searchPicked === 0} onClick={() => bulk('search')} title={t('Searches the selected wanted videos that have no file yet')}>
              <Icon name="search" size={14} /> {t('Search ({n})', { n: searchPicked })}
            </button>
            <button className="btn-sm" disabled={busy || picked.size === 0} onClick={() => bulk('want')}>
              {t('Want')}
            </button>
            <button className="btn-sm" disabled={busy || picked.size === 0} onClick={() => bulk('unwant')}>
              {t('Not wanted')}
            </button>
            <button className="btn-sm" disabled={busy || emptyPicked === 0} onClick={() => bulk('removeEmpty')} title={t('Removes selected videos that have no file. Files are never deleted.')}>
              {t('Remove empty ({n})', { n: emptyPicked })}
            </button>
          </span>
        </div>
      )}
      {note && <p className="hint">{note}</p>}
      {error && <p className="error-text">{error}</p>}
      {list === null ? (
        <div className="sc-grid">
          {Array.from({ length: 8 }, (_, i) => (
            <div key={i} className="skeleton sc-skel" />
          ))}
        </div>
      ) : shown && shown.length === 0 && !text.trim() && (who !== 'all' || tag) ? (
        <div className="empty-state">
          <Icon name="film" size={44} />
          <p>
            {t('No videos match these filters.')}
            {noDetails > 0 ? ` ${t('Some videos have no performer or tag details yet.')}` : ''}
          </p>
        </div>
      ) : shown && shown.length === 0 && text.trim() ? (
        <div className="sc-suggest">
          <p className="hint">
            {t('Nothing in your library matches “{text}”.', { text: text.trim() })}{' '}
            {text.trim().length < 3
              ? t('Type a little more to look it up on StashDB.')
              : suggest.busy
                ? t('Looking it up on StashDB…')
                : suggested.length > 0
                  ? t('On StashDB:')
                  : suggest.error
                    ? ''
                    : t('StashDB has nothing new for that either.')}
          </p>
          {suggest.error && <p className="error-text">{suggest.error}</p>}
          {suggested.length > 0 && (
            <div className="sc-grid">
              {suggested.map((s) => (
                <VideoCard
                  key={s.stashId}
                  s={s}
                  action={
                    <span className="sc-actions">
                      <button className="btn-sm" onClick={() => addSuggested(s, false)}>{t('Add')}</button>
                      <button className="btn-sm primary" onClick={() => addSuggested(s, true)}>{t('Add + search')}</button>
                    </span>
                  }
                />
              ))}
            </div>
          )}
        </div>
      ) : list.length === 0 ? (
        <div className="empty-state">
          <Icon name="film" size={44} />
          <p>{t('No videos here. Follow a performer or studio, or find a video to add.')}</p>
        </div>
      ) : shelfView.on ? (
        <Shelves key={shelfView.by} memory="nsfw" shelves={shelves} render={videoCard} itemKey={(s) => String(s.id)} rowClass="nsfw-shelf" bar={<RowsByPick value={shelfView.by} onChange={shelfView.setBy} options={[
              { value: 'studio', label: t('Studio') },
              { value: 'performer', label: t('Performer') },
              { value: 'tag', label: t('Tag') },
              { value: 'added', label: t('Recently added') },
            ]} />} />
      ) : (
        <div className="sc-grid">{(shown ?? []).map(videoCard)}</div>
      )}
    </section>
  )
}

// ---- Following ----

function FollowingTab() {
  const [performers, setPerformers] = useState<VideoPerformer[] | null>(null)
  const [studios, setStudios] = useState<VideoStudio[] | null>(null)
  const [msg, setMsg] = useState('')
  const load = useCallback(() => {
    nsfwApi.performers().then(setPerformers).catch((e: Error) => setMsg(e.message))
    nsfwApi.studios().then(setStudios).catch((e: Error) => setMsg(e.message))
  }, [])
  useEffect(load, [load])
  const unfollow = (kind: 'performer' | 'studio', id: number) => nsfwApi.unfollow(kind, id).then(load).catch((e: Error) => setMsg(e.message))
  return (
    <section>
      <div className="toolbar">
        <button className="btn-sm btn-with-icon" onClick={() => nsfwApi.checkNow().then((r) => setMsg(r.message)).catch((e: Error) => setMsg(e.message))}>
          <Icon name="search" size={14} /> {t('Look for new videos now')}
        </button>
        {msg && <span className="hint">{msg}</span>}
      </div>
      {performers && studios && (
        <p className="hint">
          {t('Following {performers} and {studios}.', { performers: plural(performers.length, '{n} performer', '{n} performers'), studios: plural(studios.length, '{n} studio', '{n} studios') })}
        </p>
      )}
      <h2 className="rail-head">{t('Performers')}</h2>
      <FollowList items={performers ?? []} loading={performers === null} kind="performer" onUnfollow={unfollow} />
      <h2 className="rail-head">{t('Studios')}</h2>
      <FollowList items={studios ?? []} loading={studios === null} kind="studio" onUnfollow={unfollow} />
    </section>
  )
}

function FollowList({ items, loading, kind, onUnfollow }: { items: (VideoPerformer | VideoStudio)[]; loading: boolean; kind: 'performer' | 'studio'; onUnfollow: (k: 'performer' | 'studio', id: number) => void }) {
  if (loading) return <div className="skeleton" style={{ height: 60 }} />
  if (items.length === 0) return <p className="hint">{t('Nobody followed yet. Use Find to look one up.')}</p>
  return (
    <ul className="sc-follow">
      {items.map((p) => (
        <li key={p.id}>
          <span className="sc-avatar">{p.imageUrl ? <img src={imageSrc(p.imageUrl)} alt="" loading="lazy" /> : <Icon name="user" size={18} />}</span>
          <Link to={`/nsfw/browse/${kind}/${p.stashId}?name=${encodeURIComponent(p.name)}`}>{p.name}</Link>
          <span className="hint">{p.checkedAt ? t('checked {date}', { date: p.checkedAt.slice(0, 10) }) : t('not checked yet')}</span>
          <button className="btn-sm" onClick={() => onUnfollow(kind, p.id)}>
            {t('Unfollow')}
          </button>
        </li>
      ))}
    </ul>
  )
}

// ---- Find ----

// FindTab browses StashDB like the Library: results update as you type
// (after a short pause, older lookups cancelled), with filters, tags with
// autocomplete, sorting and paging. The server keeps answers for ten
// minutes and the next page is fetched ahead, so paging is instant.
type FindKind = 'video' | 'performer' | 'studio'
const FIND_SORTS: Record<FindKind, { key: string; label: string }[]> = {
  video: [
    { key: 'trending', label: t('Trending') },
    { key: 'newest', label: t('Release date: newest') },
    { key: 'oldest', label: t('Release date: oldest') },
    { key: 'title', label: t('Title: A to Z') },
    { key: 'added', label: t('Newly on StashDB') },
  ],
  performer: [
    { key: 'nsfw', label: t('Most videos') },
    { key: 'recent', label: t('Latest video') },
    { key: 'newest', label: t('Newest debut') },
    { key: 'name', label: t('Name: A to Z') },
    { key: 'added', label: t('Newly on StashDB') },
  ],
  studio: [
    { key: 'name', label: t('Name: A to Z') },
    { key: 'added', label: t('Newly on StashDB') },
  ],
}
const GENDERS = [
  { key: '', label: t('Any gender') },
  { key: 'FEMALE', label: t('Female') },
  { key: 'MALE', label: t('Male') },
  { key: 'TRANSGENDER_FEMALE', label: t('Trans woman') },
  { key: 'TRANSGENDER_MALE', label: t('Trans man') },
  { key: 'NON_BINARY', label: t('Non-binary') },
  { key: 'INTERSEX', label: t('Intersex') },
]
const SINCE_YEARS = (() => {
  const now = new Date().getFullYear()
  return [0, now, now - 1, now - 2, now - 3, now - 5, now - 10, 2010, 2000]
})()

export function FindTab() {
  const [kind, setKind] = useState<FindKind>('video')
  const posterSize = usePosterSize('nsfw', 260, 180, 480) // the same size as the NSFW library
  const [text, setText] = useState('')
  const [who, setWho] = useState<Who>('all')
  const [tags, setTags] = useState<StashTag[]>([])
  const [tagAll, setTagAll] = useState(true)
  const [since, setSince] = useState(0)
  const [gender, setGender] = useState('')
  const [sort, setSort] = useState<Record<FindKind, string>>({ video: 'trending', performer: 'nsfw', studio: 'name' })
  const [foundNsfw, setFoundNsfw] = useState<LookedUpVideo[]>([])
  const [foundPeople, setFoundPeople] = useState<(VideoPerformer | VideoStudio)[]>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [more, setMore] = useState(false)
  const [filtered, setFiltered] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [note, setNote] = useState('')
  const [open, setOpen] = useState(false)
  const filtersBtn = useRef<HTMLButtonElement>(null)
  const ctl = useRef<AbortController | null>(null)

  const params = useCallback(
    (p: number): Record<string, string | number | undefined> => ({
      kind,
      q: text.trim() || undefined,
      sort: sort[kind],
      page: p,
      ...(kind === 'video'
        ? { tags: tags.length ? tags.map((tg) => tg.id).join(',') : undefined, tagAll: tags.length > 1 && tagAll ? 1 : undefined, since: since || undefined, who: who !== 'all' ? who : undefined }
        : {}),
      ...(kind === 'performer' ? { gender: gender || undefined } : {}),
    }),
    [kind, text, sort, tags, tagAll, since, who, gender],
  )

  const fetchPage = useCallback(
    (p: number, append: boolean) => {
      ctl.current?.abort()
      const c = new AbortController()
      ctl.current = c
      setBusy(true)
      setError('')
      nsfwApi
        .discover(params(p), c.signal)
        .then((r) => {
          if (c.signal.aborted) return
          if (kind === 'video') {
            const list = r.nsfw ?? []
            setFoundNsfw((l) => (append ? [...l, ...list.filter((s) => !l.some((x) => x.stashId === s.stashId))] : list))
          } else {
            const list = (kind === 'performer' ? r.performers : r.studios) ?? []
            setFoundPeople((l) => (append ? [...l, ...list.filter((s) => !l.some((x) => x.stashId === s.stashId))] : list))
          }
          setTotal(r.total)
          setMore(r.more)
          setFiltered(!!r.filtered)
          setPage(p)
          // Warm the next page, so "Show more" is instant.
          if (r.more) nsfwApi.discover(params(p + 1)).catch(() => undefined)
        })
        .catch((e: Error) => {
          if (!c.signal.aborted) setError(e.message)
        })
        .finally(() => {
          if (!c.signal.aborted) setBusy(false)
        })
    },
    [params, kind],
  )

  // New results whenever the search or a filter changes; typing waits for a pause.
  const typed = useRef(text)
  useEffect(() => {
    const wait = typed.current !== text ? 350 : 0
    typed.current = text
    const tg = window.setTimeout(() => fetchPage(1, false), wait)
    return () => window.clearTimeout(tg)
  }, [fetchPage, text])
  useEffect(() => () => ctl.current?.abort(), [])

  const switchKind = (k: FindKind) => {
    if (k === kind) return
    setFoundNsfw([])
    setFoundPeople([])
    setTotal(0)
    setMore(false)
    setNote('')
    setKind(k)
  }

  // Tag autocomplete.
  const [tagText, setTagText] = useState('')
  const [tagHits, setTagHits] = useState<StashTag[]>([])
  const [tagBusy, setTagBusy] = useState(false)
  useEffect(() => {
    const tg = tagText.trim()
    if (tg.length < 2) {
      setTagHits([])
      setTagBusy(false)
      return
    }
    const c = new AbortController()
    setTagBusy(true)
    const timer = window.setTimeout(() => {
      nsfwApi
        .stashTags(tg, c.signal)
        .then((hits) => !c.signal.aborted && setTagHits(hits))
        .catch(() => undefined)
        .finally(() => !c.signal.aborted && setTagBusy(false))
    }, 200)
    return () => {
      window.clearTimeout(timer)
      c.abort()
    }
  }, [tagText])
  const addTag = (tg: StashTag) => {
    setTags((l) => (l.some((x) => x.id === tg.id) ? l.filter((x) => x.id !== tg.id) : [...l, tg].slice(0, 10)))
    setTagText('')
    setTagHits([])
  }

  const activeFilters = kind === 'video' ? (who !== 'all' ? 1 : 0) + tags.length + (since ? 1 : 0) : kind === 'performer' ? (gender ? 1 : 0) : 0
  const clearFilters = () => {
    setWho('all')
    setTags([])
    setSince(0)
    setGender('')
  }

  const follow = (p: VideoPerformer | VideoStudio, backfill: boolean) => {
    const k = kind === 'studio' ? 'studio' : 'performer'
    nsfwApi
      .follow(k, p.stashId, backfill)
      .then((r) => {
        setFoundPeople((list) => list.map((x) => (x.stashId === p.stashId ? { ...x, followed: true } : x)))
        setNote(
          backfill
            ? plural(r.added, 'Following {name}. Added {n} video to your library as wanted.', 'Following {name}. Added {n} videos to your library as wanted.', { name: p.name })
            : t('Following {name}. New videos from now on will be added.', { name: p.name }),
        )
      })
      .catch((e: Error) => setError(e.message))
  }

  const add = (s: LookedUpVideo, search: boolean) =>
    nsfwApi
      .add(s.stashId, search)
      .then((added) => {
        setFoundNsfw((list) => list.map((x) => (x.stashId === s.stashId ? { ...x, inLibrary: true, libraryId: added.id, status: added.status, wanted: true } : x)))
        setNote(search ? t('Added. Searching for it now.') : t('Added to the library as wanted.'))
      })
      .catch((e: Error) => setError(e.message))

  const shown = kind === 'video' ? foundNsfw.length : foundPeople.length
  const whoLabel = WHO.find((w) => w.key === who)?.label
  return (
    <section style={posterSize.style}>
      <div className="sc-bar">
        <div className="seg sc-find-kind" role="tablist" aria-label={t('Look for')}>
          {(['video', 'performer', 'studio'] as const).map((k) => (
            <button type="button" role="tab" aria-selected={kind === k} key={k} className={kind === k ? 'active' : ''} onClick={() => switchKind(k)}>
              {k === 'performer' ? t('Performers') : k === 'studio' ? t('Studios') : t('Videos')}
            </button>
          ))}
        </div>
        <input
          className="sc-search"
          type="search"
          value={text}
          onChange={(e) => setText(e.target.value)}
          onKeyDown={(e) => e.key === 'Enter' && fetchPage(1, false)}
          placeholder={kind === 'video' ? t('Search StashDB: title, studio or performer') : kind === 'performer' ? t('Search performers by name') : t('Search studios by name')}
          aria-label={t('Search StashDB')}
        />
        <div className="sc-bar-row">
          <PosterSizeSlider value={posterSize} />
          {kind !== 'studio' && (
            <div className="sc-pop-wrap">
              <button ref={filtersBtn} className={`btn-sm btn-with-icon${activeFilters > 0 ? ' primary' : ''}`} aria-haspopup="dialog" aria-expanded={open} onClick={() => setOpen(!open)}>
                <Icon name="sliders" size={14} /> {t('Filters')}{activeFilters > 0 ? ` · ${activeFilters}` : ''}
              </button>
              {open && (
                <Popover anchor={filtersBtn.current} align="left" className="sc-sheet" role="dialog" label={t('Filters')} onClose={() => setOpen(false)}>
                  <div className="sc-sheet-head">
                    <strong>{t('Filters')}</strong>
                    <button className="btn-sm" onClick={() => setOpen(false)} aria-label={t('Close filters')}>
                      <Icon name="x" size={14} />
                    </button>
                  </div>
                  {kind === 'video' && (
                    <>
                      <label className="sc-field">
                        <span>{t('Who')}</span>
                        <select value={who} onChange={(e) => setWho(e.target.value as Who)}>
                          {WHO.map((w) => (
                            <option key={w.key} value={w.key}>
                              {w.label}
                            </option>
                          ))}
                        </select>
                      </label>
                      <label className="sc-field">
                        <span>{t('Released')}</span>
                        <select value={since} onChange={(e) => setSince(Number(e.target.value))}>
                          {SINCE_YEARS.map((y) => (
                            <option key={y} value={y}>
                              {y === 0 ? t('Any time') : t('{year} or later', { year: String(y) })}
                            </option>
                          ))}
                        </select>
                      </label>
                      <div className="sc-field">
                        <span>
                          {t('Tags')}
                          {tags.length > 1 && (
                            <>
                              {' · '}
                              <button className="linkish" onClick={() => setTagAll(!tagAll)}>
                                {tagAll ? t('videos with all of them') : t('videos with any of them')}
                              </button>
                            </>
                          )}
                        </span>
                        {tags.length > 0 && (
                          <div className="sc-taglist">
                            {tags.map((tg) => (
                              <button key={tg.id} className="active" onClick={() => addTag(tg)} aria-label={t('Remove tag {name}', { name: tg.name })}>
                                {tg.name} ×
                              </button>
                            ))}
                          </div>
                        )}
                        <input type="search" value={tagText} onChange={(e) => setTagText(e.target.value)} placeholder={t('Type a tag, e.g. massage')} aria-label={t('Find a tag')} autoComplete="off" />
                        {(tagHits.length > 0 || tagBusy) && (
                          <div className="sc-taglist" role="listbox" aria-label={t('Matching tags')}>
                            {tagBusy && tagHits.length === 0 && <span className="hint">{t('Looking…')}</span>}
                            {tagHits.map((tg) => {
                              const on = tags.some((x) => x.id === tg.id)
                              return (
                                <button key={tg.id} role="option" aria-selected={on} className={on ? 'active' : ''} onClick={() => addTag(tg)}>
                                  {tg.name}
                                </button>
                              )
                            })}
                          </div>
                        )}
                      </div>
                    </>
                  )}
                  {kind === 'performer' && (
                    <label className="sc-field">
                      <span>{t('Gender')}</span>
                      <select value={gender} onChange={(e) => setGender(e.target.value)}>
                        {GENDERS.map((g) => (
                          <option key={g.key} value={g.key}>
                            {g.label}
                          </option>
                        ))}
                      </select>
                    </label>
                  )}
                  <div className="sc-sheet-foot">
                    <button className="btn-sm" disabled={activeFilters === 0} onClick={clearFilters}>
                      {t('Clear all')}
                    </button>
                    <button className="btn-sm primary" onClick={() => setOpen(false)}>
                      {busy ? t('Searching…') : filtered ? t('Show results') : plural(total, 'Show {n} result', 'Show {n} results')}
                    </button>
                  </div>
                </Popover>
              )}
            </div>
          )}
          <select className="sc-sort-compact" value={sort[kind]} onChange={(e) => setSort((s) => ({ ...s, [kind]: e.target.value }))} aria-label={t('Sort')}>
            {FIND_SORTS[kind].map((o) => (
              <option key={o.key} value={o.key}>
                {o.label}
              </option>
            ))}
          </select>
        </div>
        <div className="sc-chips">
          {kind === 'video' && who !== 'all' && (
            <button className="sc-chip" onClick={() => setWho('all')} aria-label={t('Remove filter {name}', { name: whoLabel ?? '' })}>
              {whoLabel} <Icon name="x" size={12} />
            </button>
          )}
          {kind === 'video' && since > 0 && (
            <button className="sc-chip" onClick={() => setSince(0)} aria-label={t('Remove the release year filter')}>
              {t('{year} or later', { year: String(since) })} <Icon name="x" size={12} />
            </button>
          )}
          {kind === 'video' &&
            tags.map((tg) => (
              <button key={tg.id} className="sc-chip" onClick={() => setTags((l) => l.filter((x) => x.id !== tg.id))} aria-label={t('Remove tag {name}', { name: tg.name })}>
                {tg.name} <Icon name="x" size={12} />
              </button>
            ))}
          {kind === 'video' && tags.length > 1 && (
            <button className="sc-chip" onClick={() => setTagAll(!tagAll)} title={t('Switch between videos with every tag and videos with any of them')}>
              {tagAll ? t('All tags') : t('Any tag')}
            </button>
          )}
          {kind === 'performer' && gender && (
            <button className="sc-chip" onClick={() => setGender('')} aria-label={t('Remove the gender filter')}>
              {GENDERS.find((g) => g.key === gender)?.label} <Icon name="x" size={12} />
            </button>
          )}
          {activeFilters > 1 && (
            <button className="linkish" onClick={clearFilters}>
              {t('Clear all')}
            </button>
          )}
          <span className="sc-count">{busy && shown === 0
              ? t('Searching…')
              : filtered
                ? t('{n} {who} so far · {total} before Who', { n: shown, who: whoLabel?.toLocaleLowerCase() ?? '', total })
                : t('{n} on StashDB', { n: total })}</span>
        </div>
      </div>
      {error && <p className="error-text">{error}</p>}
      {note && <p className="hint">{note}</p>}
      {kind !== 'video' && foundPeople.length > 0 && (
        <ul className="sc-follow">
          {foundPeople.map((p) => (
            <li key={p.stashId}>
              <span className="sc-avatar">{p.imageUrl ? <img src={imageSrc(p.imageUrl)} alt="" loading="lazy" /> : <Icon name="user" size={18} />}</span>
              <Link to={`/nsfw/browse/${kind === 'studio' ? 'studio' : 'performer'}/${p.stashId}?name=${encodeURIComponent(p.name)}`}>
                {p.name}
                {'disambiguation' in p && p.disambiguation ? ` (${p.disambiguation})` : ''}
              </Link>
              <span className="hint">{'videoCount' in p && p.videoCount ? plural(p.videoCount, '{n} video', '{n} videos') : 'parentName' in p && p.parentName ? p.parentName : ''}</span>
              {p.followed ? (
                <span className="badge">{t('Following')}</span>
              ) : (
                <span className="sc-actions">
                  <button className="btn-sm" onClick={() => follow(p, false)} title={t('Only videos released from now on')}>
                    {t('Follow')}
                  </button>
                  <FollowAllMenu
                    kind={kind === 'studio' ? 'studio' : 'performer'}
                    stashId={p.stashId}
                    name={p.name}
                    onDone={(m) => {
                      setFoundPeople((list) => list.map((x) => (x.stashId === p.stashId ? { ...x, followed: true } : x)))
                      setNote(m)
                    }}
                    onError={setError}
                  />
                </span>
              )}
            </li>
          ))}
        </ul>
      )}
      {kind === 'video' && (
        <div className="sc-grid">
          {foundNsfw.length === 0 && busy
            ? Array.from({ length: 8 }, (_, i) => <div key={i} className="skeleton sc-skel" />)
            : foundNsfw.map((s) => (
                <VideoCard
                  key={s.stashId}
                  s={s}
                  to={s.inLibrary && s.libraryId ? `/nsfw/${s.libraryId}` : undefined}
                  action={
                    !s.inLibrary && (
                      <span className="sc-actions">
                        <button className="btn-sm" onClick={() => add(s, false)}>
                          {t('Add')}
                        </button>
                        <button className="btn-sm primary" onClick={() => add(s, true)}>
                          {t('Add + search')}
                        </button>
                      </span>
                    )
                  }
                />
              ))}
        </div>
      )}
      {!busy && shown === 0 && !error && <p className="hint">{more ? t('Nothing in this stretch of StashDB. Show more looks further.') : t('Nothing on StashDB matches that.')}</p>}
      {more && (
        <div className="sc-more-row">
          <button className="btn-sm" disabled={busy} onClick={() => fetchPage(page + 1, true)}>
            {busy ? t('Loading…') : filtered ? t('Show more') : t('Show more ({shown} of {total})', { shown, total })}
          </button>
        </div>
      )}
    </section>
  )
}

// ---- Settings ----

function clock(iso: string) {
  const d = new Date(iso)
  return isNaN(d.getTime()) ? '' : d.toLocaleTimeString(locale(), { hour: 'numeric', minute: '2-digit' })
}

// VideoSchedule: video tasks pick one of the schedules made on Settings >
// Schedules (the shared page never shows video tasks).
const VIDEO_TASKS = [
  { key: 'nsfw.search', label: t('Video searches'), hint: t('Automatic video searches and batch searches (Search on a selection, Follow + search now). Searching one video by hand always runs at once.') },
  { key: 'nsfw.follow', label: t('Checks for new videos'), hint: t('Asking StashDB for new videos from followed performers and studios (light, no downloads).') },
]

function VideoSchedule() {
  const [view, setView] = useState<SchedulesView | null>(null)
  const [error, setError] = useState('')
  useEffect(() => {
    schedulesApi.nsfw().then(setView).catch((e: Error) => setError(e.message))
  }, [])
  if (!view) return error ? <p className="error-text">{error}</p> : null
  const save = (task: string, rule: TaskRule | null) =>
    schedulesApi
      .saveNsfw({ [task]: rule })
      .then((v) => {
        setView(v)
        setError('')
        return true
      })
      .catch((e: Error) => {
        setError(e.message)
        return false
      })
  return (
    <div className="sc-import">
      <h3>{t('Schedule')}</h3>
      {view.schedules.length === 0 ? (
        <p className="hint">
          {tx('Make a schedule first in {link} (for example "Overnight"), then pick it here.', { link: <Link to="/settings/schedules">{t('Settings › Schedules')}</Link> })}
        </p>
      ) : (
        VIDEO_TASKS.map((tg) => <TaskRuleEditor key={tg.key} task={tg.key} label={tg.label} hint={tg.hint} state={view.tasks[tg.key]} schedules={view.schedules} onSave={save} />)
      )}
      {error && <p className="error-text">{error}</p>}
    </div>
  )
}

function SettingsTab({ settings, onSaved }: { settings: VideoSettings; onSaved: (s: VideoSettings) => void }) {
  const [path, setPath] = useState(settings.path)
  const [endpoint, setEndpoint] = useState(settings.endpoint)
  const [key, setKey] = useState('')
  const [busy, setBusy] = useState(false)
  const [msg, setMsg] = useState('')
  const [error, setError] = useState('')
  const save = (body: Parameters<typeof nsfwApi.saveSettings>[0], done: string) => {
    setBusy(true)
    setError('')
    setMsg('')
    nsfwApi
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
    <section className="card sc-settings">
      <h2>{t('NSFW 18+')}</h2>
      <p className="hint">{t("Only administrators ever see NSFW. Video downloads are left out of everyone else's Activity and Dashboard, and NSFW 18+ never sends notifications.")}</p>

      <label className="sc-field">
        <span>{t('NSFW folder (inside the container)')}</span>
        <input value={path} onChange={(e) => setPath(e.target.value)} placeholder="/data/nsfw" />
      </label>
      <label className="sc-field">
        <span>{t('stash-box address')}</span>
        <input value={endpoint} onChange={(e) => setEndpoint(e.target.value)} placeholder="https://stashdb.org/graphql" />
      </label>
      <button className="btn-sm" disabled={busy} onClick={() => save({ path, endpoint }, t('Saved.'))}>
        {t('Save folder and address')}
      </button>

      <label className="sc-field">
        <span>{settings.keySet ? t('StashDB API key (saved — paste a new one to replace it)') : t('StashDB API key (stashdb.org → your profile → API key)')}</span>
        <input type="password" autoComplete="off" value={key} onChange={(e) => setKey(e.target.value)} placeholder={settings.keySet ? '••••••••' : t('Paste your API key')} />
      </label>
      <div className="sc-actions">
        <button className="btn-sm" disabled={busy || !key.trim()} onClick={() => save({ key: key.trim() }, t('Key checked with StashDB and saved.'))}>
          {t('Check and save key')}
        </button>
        {settings.keySet && (
          <button className="btn-sm" disabled={busy} onClick={() => save({ removeKey: true }, t('Key removed.'))}>
            {t('Remove key')}
          </button>
        )}
      </div>

      <div className="sc-switch">
        <strong>{settings.enabled ? t('NSFW 18+ is on') : t('NSFW 18+ is off')}</strong>
        <button className={settings.enabled ? 'btn-sm' : 'btn-sm primary'} disabled={busy} onClick={() => save({ enabled: !settings.enabled }, settings.enabled ? t('Switched off.') : t('Switched on.'))}>
          {settings.enabled ? t('Switch off') : t('Switch on')}
        </button>
      </div>
      {!settings.path && <p className="hint">{t('Choose the folder first: it is where videos are filed, by studio.')}</p>}
      {msg && <p className="hint">{msg}</p>}
      {error && <p className="error-text">{error}</p>}
      {settings.enabled && <VideoSchedule />}
      {settings.enabled && settings.keySet && settings.path && <WhisparrImport nsfwPath={settings.path} />}
    </section>
  )
}

function WhisparrImport({ nsfwPath }: { nsfwPath: string }) {
  const [url, setUrl] = useState('http://host.docker.internal:6969')
  const [apiKey, setApiKey] = useState('')
  const [from, setFrom] = useState('/movies')
  const [to, setTo] = useState(nsfwPath)
  const [busy, setBusy] = useState(false)
  const [report, setReport] = useState<WhisparrReport | null>(null)
  const [error, setError] = useState('')
  const run = (dryRun: boolean) => {
    setBusy(true)
    setError('')
    nsfwApi
      .importWhisparr({ url, apiKey: apiKey.trim(), from, to, dryRun })
      .then(setReport)
      .catch((e: Error) => setError(e.message))
      .finally(() => setBusy(false))
  }
  return (
    <div className="sc-import">
      <h3>{t('Import from Whisparr')}</h3>
      <p className="hint">{t('Reads Whisparr only (it is never changed). Followed studios and performers, every video and its wanted flag come over; downloaded files are registered where they are, never moved. Safe to run again.')}</p>
      <label className="sc-field">
        <span>{t('Whisparr address')}</span>
        <input value={url} onChange={(e) => setUrl(e.target.value)} />
      </label>
      <label className="sc-field">
        <span>{t('Whisparr API key (Whisparr: Settings, General, API Key)')}</span>
        <input type="password" autoComplete="off" value={apiKey} onChange={(e) => setApiKey(e.target.value)} />
      </label>
      <div className="sc-pathmap">
        <label className="sc-field">
          <span>{t("Whisparr's folder")}</span>
          <input value={from} onChange={(e) => setFrom(e.target.value)} />
        </label>
        <label className="sc-field">
          <span>{t('Same folder in Mediarium')}</span>
          <input value={to} onChange={(e) => setTo(e.target.value)} />
        </label>
      </div>
      <div className="sc-actions">
        <button className="btn-sm" disabled={busy || !apiKey.trim()} onClick={() => run(true)}>
          {busy ? t('Working…') : t('Preview')}
        </button>
        <button className="btn-sm primary" disabled={busy || !apiKey.trim() || !report?.dryRun} onClick={() => run(false)} title={report?.dryRun ? '' : t('Preview first')}>
          {t('Import')}
        </button>
      </div>
      {error && <p className="error-text">{error}</p>}
      {report && (
        <div className="sc-report">
          <p>
            <strong>{report.dryRun ? t('Preview:') : t('Done:')}</strong>{' '}
            {t('{videos} videos ({added} new, {updated} already here), {found} of {files} files found.', { nsfw: report.nsfw, added: report.nsfwAdded, updated: report.nsfwUpdated, found: report.filesFound, files: report.files })}
          </p>
          <p className="hint">
            {t('Following {studios} studios{names} and {performers} performers; {known} other performers known.', {
              studios: report.studiosFollowed.length,
              names: report.studiosFollowed.length > 0 ? ` (${report.studiosFollowed.join(', ')})` : '',
              performers: report.performersFollowed.length,
              known: report.performersKnown,
            })}
            {report.skippedNoStashId > 0 && ` ${t('{n} videos without a StashDB id are skipped.', { n: report.skippedNoStashId })}`}
            {report.skippedMovies > 0 && ` ${t('{n} full-length movies are skipped.', { n: report.skippedMovies })}`}
          </p>
          {report.filesMissing.length > 0 && (
            <p className="error-text">
              {t("Some files weren't found where expected, for example {file}. Check the two folders above (and that the folder is mounted into Mediarium), then preview again.", { file: report.filesMissing[0] })}
            </p>
          )}
        </div>
      )}
    </div>
  )
}

// ---- One video ----

export function VideoDetail() {
  const adminHere = isAdmin(useAuth().user) // home fork: searching and removing stay with administrators
  const { id } = useParams()
  const navigate = useNavigate()
  const videoID = Number(id)
  const [video, setVideo] = useState<Video | null>(null)
  const [downloading, setDownloading] = useState(false)
  const [releases, setReleases] = useState<VideoRelease[] | null>(null)
  const [busy, setBusy] = useState(false)
  const [msg, setMsg] = useState('')
  const [error, setError] = useState('')
  const load = useCallback(() => {
    nsfwApi
      .get(videoID)
      .then((r) => {
        setVideo(r.video)
        setDownloading(r.downloading)
      })
      .catch((e: Error) => setError(e.message))
  }, [videoID])
  useEffect(load, [load])
  if (error && !video) return <p className="error-text">{error}</p>
  if (!video) return <div className="skeleton" style={{ height: 240 }} />

  const act = (p: Promise<unknown>, done: string) => {
    setBusy(true)
    setError('')
    p.then(() => {
      setMsg(done)
      load()
    })
      .catch((e: Error) => setError(e.message))
      .finally(() => setBusy(false))
  }
  return (
    <div className="nsfw-page sc-detail">
      <Link to="/nsfw" className="btn-sm btn-with-icon">
        <Icon name="open" size={14} style={{ transform: 'rotate(180deg)' }} /> {t('NSFW 18+')}
      </Link>
      <div className="sc-hero">
        {video.filePath ? (
          <button type="button" className="sc-playthumb big" onClick={() => playTitle('video', video.id, video.title || t('Video'))} aria-label={t('Play')}>
            <div className="sc-thumb big">{video.imageUrl ? <img src={imageSrc(video.imageUrl)} alt="" /> : <Icon name="film" size={48} />}</div>
            <span className="sc-playicon" aria-hidden="true">
              <Icon name="play" size={40} />
            </span>
          </button>
        ) : (
          <div className="sc-thumb big">{video.imageUrl ? <img src={imageSrc(video.imageUrl)} alt="" /> : <Icon name="film" size={48} />}</div>
        )}
        <div>
          <h1>{video.title || t('Untitled video')}</h1>
          <p className="sc-sub">
            {video.studioName}
            {video.releaseDate && ` · ${video.releaseDate}`}
            {video.duration > 0 && ` · ${fmtDuration(video.duration)}`}
          </p>
          <p className="sc-sub">
            {video.performers.map((p, i) => (
              <span key={p.id}>
                {i > 0 && ', '}
                <Link to={`/nsfw/browse/performer/${p.id}?name=${encodeURIComponent(p.name)}`}>{p.name}</Link>
              </span>
            ))}
          </p>
          <p>
            <span className="badge">{downloading ? t('Downloading') : statusLabel(video)}</span>
          </p>
          {video.filePath && <p className="hint">{video.filePath}</p>}
          {video.details && <p className="sc-details">{video.details}</p>}
          <div className="sc-actions">
            {video.filePath && (
              <button className="primary btn-with-icon" onClick={() => playTitle('video', video.id, video.title || t('Video'))}>
                <Icon name="play" size={16} /> {t('Play')}
              </button>
            )}
            {adminHere && (
              <button className={video.filePath ? 'btn-sm btn-with-icon' : 'primary btn-with-icon'} disabled={busy || downloading} onClick={() => act(nsfwApi.searchNow(video.id).then((r) => setMsg(r.message)), '')}>
                <Icon name="search" size={16} /> {t('Search now')}
              </button>
            )}
            {adminHere && (
              <button className="btn-sm" disabled={busy} onClick={() => { setReleases(null); setBusy(true); nsfwApi.releases(video.id, true).then(setReleases).catch((e: Error) => setError(e.message)).finally(() => setBusy(false)) }}>
                {t('Choose release')}
              </button>
            )}
            <button className="btn-sm" disabled={busy} onClick={() => act(nsfwApi.setWanted(video.id, !video.wanted), video.wanted ? t('No longer wanted.') : t('Wanted again.'))}>
              {video.wanted ? t('Not wanted') : t('Want it')}
            </button>
            {adminHere && (
              <button className="btn-sm" disabled={busy || downloading} onClick={() => { if (confirm(t('Remove this video from the library? Its file stays on disk.'))) nsfwApi.remove(video.id).then(() => navigate('/nsfw')).catch((e: Error) => setError(e.message)) }}>
                {t('Remove')}
              </button>
            )}
          </div>
          {msg && <p className="hint">{msg}</p>}
          {error && <p className="error-text">{error}</p>}
        </div>
      </div>
      {releases && (
        <div className="table-scroll">
          {releases.length === 0 ? (
            <p className="hint">{t('No releases found on your indexers.')}</p>
          ) : (
            <table className="sc-releases">
              <thead>
                <tr>
                  <th>{t('Release')}</th>
                  <th>{t('Quality')}</th>
                  <th>{t('Size')}</th>
                  <th>{t('Indexer')}</th>
                  <th />
                </tr>
              </thead>
              <tbody>
                {releases.map((r) => (
                  <tr key={r.downloadUrl} className={r.matches ? '' : 'sc-nomatch'}>
                    <td title={r.matches ? t('Matches this video') : t('Does not look like this video')}>
                      {r.title}
                      {r.blocklisted && <span className="badge">{t('Blocklisted')}</span>}
                    </td>
                    <td>{r.quality}</td>
                    <td>{fmtSize(r.sizeBytes)}</td>
                    <td>{r.indexerName}</td>
                    <td>
                      <button className="btn-sm" disabled={busy} onClick={() => act(nsfwApi.grab(video.id, r), t('Downloading. Follow it in Activity.'))}>
                        {t('Download')}
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>
      )}
    </div>
  )
}

// ---- A performer's or studio's videos on StashDB ----

export function VideoBrowse() {
  const { kind, stashId } = useParams()
  const posterSize = usePosterSize('nsfw', 260, 180, 480)
  const [params] = useSearchParams()
  const name = params.get('name') || ''
  const k = kind === 'studio' ? 'studio' : 'performer'
  const [page, setPage] = useState(1)
  const [list, setList] = useState<LookedUpVideo[] | null>(null)
  const [count, setCount] = useState(0)
  const [error, setError] = useState('')
  const [note, setNote] = useState('')
  const [reload, setReload] = useState(0)
  useEffect(() => {
    if (!stashId) return
    setList(null)
    nsfwApi
      .browse(k, stashId, page)
      .then((r) => {
        setList(r.nsfw)
        setCount(r.count)
      })
      .catch((e: Error) => setError(e.message))
  }, [k, stashId, page, reload])
  const add = (s: LookedUpVideo, search: boolean) =>
    nsfwApi
      .add(s.stashId, search)
      .then((added) => {
        setList((l) => (l ?? []).map((x) => (x.stashId === s.stashId ? { ...x, inLibrary: true, libraryId: added.id, status: added.status, wanted: true } : x)))
        setNote(search ? t('Added. Searching for it now.') : t('Added to the library as wanted.'))
      })
      .catch((e: Error) => setError(e.message))
  const pages = Math.max(1, Math.ceil(count / 40))
  return (
    <div className="nsfw-page" style={posterSize.style}>
      <Link to="/nsfw?tab=following" className="btn-sm btn-with-icon">
        <Icon name="open" size={14} style={{ transform: 'rotate(180deg)' }} /> {t('NSFW 18+')}
      </Link>
      <h1>{name || (k === 'studio' ? t('Studio') : t('Performer'))}</h1>
      <div className="toolbar">
        <PosterSizeSlider value={posterSize} />
        <button className="btn-sm" onClick={() => stashId && nsfwApi.follow(k, stashId, false).then(() => setNote(t('Following. New videos from now on will be added.'))).catch((e: Error) => setError(e.message))}>
          {t('Follow')}
        </button>
        {stashId && (
          <FollowAllMenu
            kind={k}
            stashId={stashId}
            name={name || (k === 'studio' ? t('this studio') : t('this performer'))}
            onDone={(m) => {
              setNote(m)
              setReload((n) => n + 1)
            }}
            onError={setError}
          />
        )}
        <span className="hint">{plural(count, '{n} video on StashDB', '{n} videos on StashDB')}</span>
      </div>
      {note && <p className="hint">{note}</p>}
      {error && <p className="error-text">{error}</p>}
      {list === null ? (
        <div className="skeleton" style={{ height: 200 }} />
      ) : (
        <div className="sc-grid">
          {list.map((s) => (
            <VideoCard
              key={s.stashId}
              s={s}
              to={s.inLibrary && s.libraryId ? `/nsfw/${s.libraryId}` : undefined}
              action={
                !s.inLibrary && (
                  <span className="sc-actions">
                    <button className="btn-sm" onClick={() => add(s, false)}>{t('Add')}</button>
                    <button className="btn-sm primary" onClick={() => add(s, true)}>{t('Add + search')}</button>
                  </span>
                )
              }
            />
          ))}
        </div>
      )}
      {pages > 1 && (
        <div className="toolbar">
          <button className="btn-sm" disabled={page <= 1} onClick={() => setPage(page - 1)}>{t('Newer')}</button>
          <span className="hint">{t('Page {page} of {pages}', { page, pages })}</span>
          <button className="btn-sm" disabled={page >= pages} onClick={() => setPage(page + 1)}>{t('Older')}</button>
        </div>
      )}
    </div>
  )
}

// useNsfwOn tells the sidebar whether to show NSFW (administrators only).
export { useNsfwOn } from './libraryKinds'
