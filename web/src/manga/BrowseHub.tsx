// Usherr: the parts of Manga > Discover that come from MangaDex, and the tile every
// title uses there, from AniList or MangaDex: popular new titles, latest updates,
// the staff's lists, recently added, new official volumes, the search panel with
// Random, a title's preview, and reading a chapter online in Usherr's own reader.
// MangaDex's parts show only when its source is on in Settings > Manga.
import { lazy, Suspense, useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import { isAdmin } from '../api'
import { useAuth } from '../AuthContext'
import { useToast } from '../components/Toast'
import { timeAgo } from '../format'
import { t } from '../i18n'
import { browseApi, type DexChapterRow, type DexContinue, type DexHub, type DexSearch, type DexTag, type DexTitle, type DexUpdate, type OfficialVolume } from './browseApi'
import { firstReadable, fromDex, fromFound, mergePicks, nextTagState, type Pick, type TagState } from './browsePicks'
import MangaCover from './MangaCover'
import { mangaApi } from './mangaApi'
import { stateLabel } from './mangaStatus'
import './browse.css'

const MangaReader = lazy(() => import('./MangaReader'))

export type Want = 'all' | 'new' | 'none'

// ---- adding ----

// useBrowseAdd adds a title from either service with the usual add. A MangaDex
// title is linked to its MangaDex entry straight away (for admins), so its chapters
// can be followed without a second look.
export function useBrowseAdd(want: Want) {
  const toast = useToast()
  const { user } = useAuth()
  const admin = isAdmin(user)
  const [added, setAdded] = useState<Record<string, number>>({})
  const [adding, setAdding] = useState('')
  const add = useCallback(
    (p: Pick, follow = false) => {
      setAdding(p.key)
      mangaApi
        .add(p.anilistId ? { anilistId: p.anilistId, want } : { title: p.title, want })
        .then(async (r) => {
          setAdded((m) => ({ ...m, [p.key]: r.series.id }))
          if (admin && p.dexId) await mangaApi.setMangaDex(r.series.id, { dexId: p.dexId, ...(follow ? { follow: 'all' } : {}) }).catch(() => undefined)
          toast.success(follow ? t('{title} added, and its chapters followed.', { title: r.series.title }) : t('{title} added.', { title: r.series.title }))
        })
        .catch((e: Error) => toast.error(e.message))
        .finally(() => setAdding(''))
    },
    [toast, want, admin],
  )
  return { added, adding, add, admin }
}

export type BrowseAdd = ReturnType<typeof useBrowseAdd>

// ---- reading online ----

// useDexReader opens a MangaDex chapter in Usherr's manga reader.
export function useDexReader(onClosed?: () => void) {
  const [open, setOpen] = useState('')
  const toast = useToast()
  const readTitle = useCallback(
    (dexId: string, adult: boolean, stopped?: string) => {
      browseApi
        .title(dexId, adult)
        .then((d) => {
          const first = firstReadable(d.chapters, stopped)
          if (first) setOpen(first)
          else toast.error(d.problem ?? t('MangaDex has no chapters of this to read here in your language.'))
        })
        .catch((e: Error) => toast.error(e.message))
    },
    [toast],
  )
  const reader = open ? (
    <Suspense fallback={null}>
      <MangaReader
        dex={open}
        onClose={() => {
          setOpen('')
          onClosed?.()
        }}
      />
    </Suspense>
  ) : null
  return { readChapter: setOpen, readTitle, reader }
}

// ---- the tile ----

export function PickTile({ p, press, canAdd, dexOn, onOpen, onRead }: { p: Pick; press: BrowseAdd; canAdd: boolean; dexOn: boolean; onOpen: (p: Pick) => void; onRead: (p: Pick) => void }) {
  const library = press.added[p.key] ?? p.inLibrary
  const sub = [stateLabel(p.status ?? ''), p.year ? String(p.year) : ''].filter(Boolean).join(' · ')
  return (
    <article className="mg-found bh-tile">
      <button className="cover" onClick={() => onOpen(p)} aria-label={t('More about {title}', { title: p.title })}>
        <span className="mg-slot">
          <MangaCover title={p.title} src={p.coverUrl} />
        </span>
      </button>
      <span>
        <span className="t">{p.title}</span>
        <span className="s">{sub}</span>
      </span>
      <span className="mg-chips">{p.score ? <span className="mg-pill">{t('Score {n}%', { n: p.score })}</span> : null}</span>
      <span className="bh-acts">
        {library ? (
          <Link className="btn-sm act done" to={`/manga/${library}`}>
            {t('In your library')}
          </Link>
        ) : (
          canAdd && (
            <button className="btn-sm primary act" disabled={press.adding !== ''} onClick={() => press.add(p)}>
              {press.adding === p.key ? t('Adding…') : t('Add')}
            </button>
          )
        )}
        {dexOn && p.canRead && p.dexId && (
          <button className="btn-sm act" onClick={() => onRead(p)}>
            {t('Read now')}
          </button>
        )}
      </span>
    </article>
  )
}

export function PickRow({ title, note, picks, ...rest }: { title: string; note?: string; picks: Pick[]; press: BrowseAdd; canAdd: boolean; dexOn: boolean; onOpen: (p: Pick) => void; onRead: (p: Pick) => void }) {
  if (picks.length === 0) return null
  return (
    <section className="bh-section" aria-label={title}>
      <h2>{title}</h2>
      {note && <p className="mg-hint">{note}</p>}
      <div className="bh-row">
        {picks.map((p) => (
          <PickTile key={p.key} p={p} {...rest} />
        ))}
      </div>
    </section>
  )
}

// ---- popular new titles ----

export function PopularCarousel({ titles, onOpen, onRead, press, canAdd }: { titles: DexTitle[]; onOpen: (p: Pick) => void; onRead: (p: Pick) => void; press: BrowseAdd; canAdd: boolean }) {
  const [at, setAt] = useState(0)
  const strip = useRef<HTMLDivElement>(null)
  const go = (i: number) => {
    const n = (i + titles.length) % titles.length
    setAt(n)
    strip.current?.children[n]?.scrollIntoView({ behavior: 'smooth', inline: 'start', block: 'nearest' })
  }
  if (titles.length === 0) return null
  return (
    <section className="bh-section" aria-label={t('Popular new titles')}>
      <div className="bh-head">
        <h2>{t('Popular new titles')}</h2>
        <span className="bh-nav">
          <button className="btn-sm" onClick={() => go(at - 1)} aria-label={t('Previous')}>
            ‹
          </button>
          <span className="mg-hint">{`${at + 1} / ${titles.length}`}</span>
          <button className="btn-sm" onClick={() => go(at + 1)} aria-label={t('Next')}>
            ›
          </button>
        </span>
      </div>
      <div className="bh-carousel" ref={strip}>
        {titles.map((d) => {
          const p = fromDex(d)
          const library = press.added[p.key] ?? p.inLibrary
          return (
            <article key={d.id} className="bh-hero">
              <button className="cover" onClick={() => onOpen(p)} aria-label={t('More about {title}', { title: d.title })}>
                <MangaCover title={d.title} src={d.coverUrl ? `${d.coverUrl}?s=512` : undefined} />
              </button>
              <div className="bh-hero-text">
                <h3>{d.title}</h3>
                <div className="mg-chips">
                  {(d.tags ?? []).slice(0, 6).map((g) => (
                    <span key={g} className="mg-pill">
                      {g}
                    </span>
                  ))}
                </div>
                {d.description && <p className="mg-desc bh-clamp">{d.description}</p>}
                {d.authors && d.authors.length > 0 && <p className="mg-hint">{d.authors.join(', ')}</p>}
                <div className="mg-tools">
                  {library ? (
                    <Link className="btn-sm" to={`/manga/${library}`}>
                      {t('In your library')}
                    </Link>
                  ) : (
                    canAdd && (
                      <button className="btn-sm primary" disabled={press.adding !== ''} onClick={() => press.add(p)}>
                        {press.adding === p.key ? t('Adding…') : t('Add')}
                      </button>
                    )
                  )}
                  {d.canRead && (
                    <button className="btn-sm" onClick={() => onRead(p)}>
                      {t('Read now')}
                    </button>
                  )}
                </div>
              </div>
            </article>
          )
        })}
      </div>
    </section>
  )
}

// ---- latest updates ----

function chapterLine(u: { volume?: string; number?: string; title?: string }): string {
  const parts = [u.volume ? t('Vol. {n}', { n: u.volume }) : '', u.number ? t('Ch. {n}', { n: u.number }) : t('Oneshot')].filter(Boolean).join(' ')
  return u.title ? `${parts} · ${u.title}` : parts
}

export function LatestUpdates({ updates, onRead, onOpen }: { updates: DexUpdate[]; onRead: (chapterId: string) => void; onOpen: (p: Pick) => void }) {
  const [all, setAll] = useState(false)
  if (updates.length === 0) return null
  const shown = all ? updates : updates.slice(0, 12)
  return (
    <section className="bh-section" aria-label={t('Latest updates')}>
      <h2>{t('Latest updates')}</h2>
      <div className="bh-updates">
        {shown.map((u) => (
          <article key={u.chapterId} className="bh-update">
            {u.manga && (
              <button className="cover" onClick={() => u.manga && onOpen(fromDex(u.manga))} aria-label={t('More about {title}', { title: u.manga.title })}>
                <MangaCover title={u.manga.title} src={u.manga.coverUrl} small />
              </button>
            )}
            <button className="bh-update-text" onClick={() => onRead(u.chapterId)} title={t('Read this chapter')}>
              <span className="t">{u.manga?.title}</span>
              <span className="s">
                <span className="mg-pill bh-lang">{u.language.toUpperCase()}</span> {chapterLine(u)}
              </span>
              <span className="s">
                {(u.groups ?? []).map((g) => g.Name).join(' & ') || t('No group')}
                {u.readableAt ? ` · ${timeAgo(u.readableAt)}` : ''}
                {u.comments ? ` · ${t('{n} comments', { n: u.comments })}` : ''}
              </span>
            </button>
          </article>
        ))}
      </div>
      {updates.length > 12 && (
        <button className="btn-sm" onClick={() => setAll((v) => !v)}>
          {all ? t('Show fewer') : t('Show more')}
        </button>
      )}
    </section>
  )
}

// ---- reading online, part way ----

export function DexContinueRow({ items, onRead }: { items: DexContinue[]; onRead: (chapterId: string) => void }) {
  if (items.length === 0) return null
  return (
    <section className="bh-section" aria-label={t('Reading online')}>
      <h2>{t('Reading online')}</h2>
      <div className="mg-continue">
        {items.map((c) => {
          const pct = c.pages > 0 ? Math.round((c.page / c.pages) * 100) : 0
          return (
            <button key={c.dexChapterId} className="mg-cont" onClick={() => onRead(c.dexChapterId)}>
              <MangaCover title={c.series} src={c.coverUrl} small />
              <span style={{ minWidth: 0 }}>
                <span className="t">{c.series}</span>
                <span className="s">{t('Chapter {n}, page {page} of {pages}', { n: c.number, page: c.page, pages: c.pages })}</span>
                <span className="mg-bar" aria-hidden="true">
                  <i style={{ width: `${pct}%` }} />
                </span>
              </span>
            </button>
          )
        })}
      </div>
    </section>
  )
}

// ---- new official volumes ----

export function OfficialRow({ volumes }: { volumes: OfficialVolume[] }) {
  if (volumes.length === 0) return null
  return (
    <section className="bh-section" aria-label={t('New official volumes')}>
      <h2>{t('New official volumes')}</h2>
      <p className="mg-hint">{t('Official English volumes your indexers brought in lately.')}</p>
      <div className="bh-row">
        {volumes.map((v) => (
          <Link key={v.volumeId} className="mg-found bh-tile" to={`/manga/${v.seriesId}`}>
            <span className="mg-slot">
              <MangaCover title={v.title} src={v.coverUrl} />
            </span>
            <span>
              <span className="t">{v.title}</span>
              <span className="s">{[t('Volume {n}', { n: v.number }), timeAgo(v.at)].filter(Boolean).join(' · ')}</span>
            </span>
            <span className="mg-chips">
              <span className={`mg-pill ${v.have ? 'good' : ''}`}>{v.have ? t('Here') : t('Downloading')}</span>
            </span>
          </Link>
        ))}
      </div>
    </section>
  )
}

// ---- a title's preview ----

export function PickPreview({ p, press, canAdd, dexOn, adult, onRead, onClose }: { p: Pick; press: BrowseAdd; canAdd: boolean; dexOn: boolean; adult: boolean; onRead: (chapterId: string) => void; onClose: () => void }) {
  const [chapters, setChapters] = useState<DexChapterRow[] | null>(null)
  const [problem, setProblem] = useState('')
  const [title, setTitle] = useState<DexTitle | undefined>(p.dex)
  const [follow, setFollow] = useState(false)
  const library = press.added[p.key] ?? p.inLibrary
  useEffect(() => {
    if (!dexOn || !p.dexId) return
    let gone = false
    browseApi
      .title(p.dexId, adult)
      .then((d) => {
        if (gone) return
        setChapters(d.chapters)
        setTitle(d.title)
        if (d.problem) setProblem(d.problem)
      })
      .catch((e: Error) => !gone && setProblem(e.message))
    return () => {
      gone = true
    }
  }, [p.dexId, dexOn, adult])
  const readable = (chapters ?? []).filter((c) => !c.externalUrl)
  const tags = title?.tags ?? p.tags ?? []
  const description = title?.description || p.description
  return (
    <div className="mg-modal" role="dialog" aria-modal="true" aria-label={p.title} onClick={onClose}>
      <div className="mg-modal-box" onClick={(e) => e.stopPropagation()}>
        <header>
          <h2>{p.title}</h2>
          <button className="btn-sm" onClick={onClose}>
            {t('Close')}
          </button>
        </header>
        <div className="mg-detail-head">
          <MangaCover title={p.title} src={p.coverUrl} />
          <div className="mg-stack">
            <div className="mg-chips">
              {stateLabel(p.status ?? '') && <span className="mg-pill">{stateLabel(p.status ?? '')}</span>}
              {p.year ? <span className="mg-pill">{p.year}</span> : null}
              {p.score ? <span className="mg-pill">{t('Score {n}%', { n: p.score })}</span> : null}
              {tags.slice(0, 10).map((g) => (
                <span key={g} className="mg-pill">
                  {g}
                </span>
              ))}
            </div>
            {title?.authors && title.authors.length > 0 && <p className="mg-hint">{title.authors.join(', ')}</p>}
            {title?.follows ? <p className="mg-hint">{t('{n} people follow it on MangaDex', { n: title.follows })}</p> : null}
            {title?.licensedLink && (
              <p className="mg-hint">
                {t('There’s an official English edition.')}{' '}
                <a href={title.licensedLink} target="_blank" rel="noreferrer">
                  {t('Where to buy it')}
                </a>
              </p>
            )}
          </div>
        </div>
        {description && <p className="mg-desc">{description}</p>}
        <div className="mg-tools">
          {library ? (
            <Link className="btn-sm primary" to={`/manga/${library}`}>
              {t('In your library')}
            </Link>
          ) : (
            canAdd && (
              <>
                <button className="btn-sm primary" disabled={press.adding !== ''} onClick={() => press.add(p, follow)}>
                  {press.adding === p.key ? t('Adding…') : t('Add {title}', { title: p.title })}
                </button>
                {press.admin && dexOn && p.dexId && (
                  <label className="mr-check">
                    <input type="checkbox" checked={follow} onChange={(e) => setFollow(e.target.checked)} />
                    <span>{t('Follow its chapters from MangaDex')}</span>
                  </label>
                )}
              </>
            )
          )}
          {readable.length > 0 && (
            <button className="btn-sm" onClick={() => onRead(readable[0].id)}>
              {t('Read now')}
            </button>
          )}
          {p.found?.siteUrl && (
            <a className="btn-sm" href={p.found.siteUrl} target="_blank" rel="noreferrer">
              {t('See it on AniList')}
            </a>
          )}
        </div>
        {dexOn && p.dexId && (
          <section className="mg-stack">
            <h3>{t('Chapters to read online')}</h3>
            {problem && <p className="mg-hint">{problem}</p>}
            {!chapters && !problem && <div className="skeleton" style={{ height: 80 }} />}
            {chapters && readable.length === 0 && !problem && <p className="mg-hint">{t('MangaDex has no chapters of this to read here in your language.')}</p>}
            {readable.length > 0 && (
              <ul className="bh-chapters">
                {readable
                  .slice()
                  .reverse()
                  .map((c) => (
                    <li key={c.id}>
                      <button className="bh-chapter" onClick={() => onRead(c.id)}>
                        <span className="t">{chapterLine(c)}</span>
                        <span className="s">
                          {c.group || t('No group')}
                          {c.publishedAt ? ` · ${timeAgo(c.publishedAt)}` : ''}
                        </span>
                      </button>
                    </li>
                  ))}
              </ul>
            )}
            <p className="mg-hint">{t('Chapters come from MangaDex and the groups who translated them. Read here, nothing is downloaded.')}</p>
          </section>
        )}
      </div>
    </div>
  )
}

// ---- search ----

const STATUS = (): [string, string][] => [
  ['ongoing', t('Ongoing')],
  ['completed', t('Completed')],
  ['hiatus', t('On hiatus')],
  ['cancelled', t('Cancelled')],
]
const DEMOS = (): [string, string][] => [
  ['shounen', t('Shounen')],
  ['shoujo', t('Shoujo')],
  ['seinen', t('Seinen')],
  ['josei', t('Josei')],
  ['none', t('None')],
]
const LANGS = (): [string, string][] => [
  ['ja', t('Japanese (manga)')],
  ['ko', t('Korean (manhwa)')],
  ['zh', t('Chinese (manhua)')],
  ['en', t('English')],
]
const ORDERS = (): [string, string][] => [
  ['', t('Best match')],
  ['followedCount', t('Most followed')],
  ['latestUploadedChapter', t('Latest chapter')],
  ['createdAt', t('Newest on MangaDex')],
  ['rating', t('Best rated')],
  ['year', t('Year')],
  ['title', t('Title')],
]

export function BrowseSearch({ dexOn, adult, onResults, onRandom }: { dexOn: boolean; adult: boolean; onResults: (r: { picks: Pick[]; more: boolean; problem?: string; query: DexSearch } | null) => void; onRandom: (d: DexTitle) => void }) {
  const toast = useToast()
  const [title, setTitle] = useState('')
  const [open, setOpen] = useState(false)
  const [tags, setTags] = useState<DexTag[]>([])
  const [tagState, setTagState] = useState<Record<string, TagState>>({})
  const [status, setStatus] = useState<string[]>([])
  const [demo, setDemo] = useState<string[]>([])
  const [lang, setLang] = useState<string[]>([])
  const [year, setYear] = useState('')
  const [order, setOrder] = useState('')
  const [readable, setReadable] = useState(false)
  const [busy, setBusy] = useState(false)
  useEffect(() => {
    if (!open || !dexOn || tags.length) return
    browseApi
      .tags()
      .then((r) => setTags(r.tags))
      .catch(() => undefined)
  }, [open, dexOn, tags.length])
  const toggle = (list: string[], v: string) => (list.includes(v) ? list.filter((x) => x !== v) : [...list, v])
  const query = useMemo<DexSearch>(
    () => ({
      title,
      tags: Object.keys(tagState).filter((k) => tagState[k] === 'in'),
      notTags: Object.keys(tagState).filter((k) => tagState[k] === 'out'),
      status,
      demo,
      lang,
      year,
      order,
      readable,
      adult,
    }),
    [title, tagState, status, demo, lang, year, order, readable, adult],
  )
  const filtered = !!(query.tags?.length || query.notTags?.length || status.length || demo.length || lang.length || year || order || readable)
  const run = () => {
    if (!title.trim() && !filtered) {
      onResults(null)
      return
    }
    setBusy(true)
    const fromAniList = title.trim().length >= 2 ? mangaApi.lookup(title.trim()).then((r) => r.results.map((f) => fromFound(f, dexOn))) : Promise.resolve([] as Pick[])
    const fromDexNow = dexOn ? browseApi.search(query) : Promise.resolve({ results: [] as DexTitle[], more: false, problem: undefined as string | undefined })
    Promise.all([fromAniList.catch(() => [] as Pick[]), fromDexNow])
      .then(([a, d]) => onResults({ picks: mergePicks(a, d.results.map(fromDex)), more: !!d.more, problem: d.problem, query }))
      .catch((e: Error) => onResults({ picks: [], more: false, problem: e.message, query }))
      .finally(() => setBusy(false))
  }
  const random = () => {
    browseApi
      .random(adult)
      .then(onRandom)
      .catch((e: Error) => toast.error(e.message))
  }
  const groups = useMemo(() => {
    const by: Record<string, DexTag[]> = {}
    for (const tag of tags) (by[tag.group] ??= []).push(tag)
    return Object.entries(by)
  }, [tags])
  const groupName = (g: string) => ({ genre: t('Genres'), theme: t('Themes'), format: t('Format'), content: t('Content') })[g] ?? g
  return (
    <section className="mg-card mg-stack bh-search" aria-label={t('Search')}>
      <form
        className="mg-search"
        onSubmit={(e) => {
          e.preventDefault()
          run()
        }}
      >
        <input value={title} onChange={(e) => setTitle(e.target.value)} placeholder={dexOn ? t('Search AniList and MangaDex') : t('Search AniList')} aria-label={t('Title')} autoComplete="off" />
        <button className="btn-sm primary" type="submit" disabled={busy}>
          {busy ? t('Searching…') : t('Search')}
        </button>
      </form>
      {dexOn && (
        <div className="mg-tools">
          <button className="btn-sm" onClick={() => setOpen((v) => !v)} aria-expanded={open}>
            {open ? t('Fewer filters') : t('More filters')}
          </button>
          <button className="btn-sm" onClick={random}>
            {t('Random title')}
          </button>
        </div>
      )}
      {dexOn && open && (
        <div className="bh-filters">
          <p className="mg-hint">{t('Filters search MangaDex. A tag pressed once is wanted, twice is left out, three times is neither.')}</p>
          <div className="bh-filter-row">
            <label>
              <span>{t('Sort by')}</span>
              <select value={order} onChange={(e) => setOrder(e.target.value)}>
                {ORDERS().map(([v, l]) => (
                  <option key={v} value={v}>
                    {l}
                  </option>
                ))}
              </select>
            </label>
            <label>
              <span>{t('Year')}</span>
              <input value={year} onChange={(e) => setYear(e.target.value.replace(/\D/g, '').slice(0, 4))} inputMode="numeric" placeholder="2024" style={{ width: '6em' }} />
            </label>
            <label className="mr-check">
              <input type="checkbox" checked={readable} onChange={(e) => setReadable(e.target.checked)} />
              <span>{t('Has chapters to read in your language')}</span>
            </label>
          </div>
          {(
            [
              [t('Status'), STATUS(), status, setStatus],
              [t('Readers'), DEMOS(), demo, setDemo],
              [t('Original language'), LANGS(), lang, setLang],
            ] as const
          ).map(([label, list, chosen, set]) => (
            <div key={label} className="bh-filter-row" role="group" aria-label={label}>
              <b>{label}</b>
              {list.map(([v, l]) => (
                <button key={v} className={`mg-pill bh-chip${chosen.includes(v) ? ' in' : ''}`} aria-pressed={chosen.includes(v)} onClick={() => set(toggle(chosen, v))}>
                  {l}
                </button>
              ))}
            </div>
          ))}
          {groups.map(([g, list]) => (
            <div key={g} className="bh-filter-row" role="group" aria-label={groupName(g)}>
              <b>{groupName(g)}</b>
              {list.map((tag) => {
                const s = tagState[tag.id]
                return (
                  <button key={tag.id} className={`mg-pill bh-chip${s ? ` ${s}` : ''}`} aria-pressed={!!s} title={s === 'in' ? t('Wanted') : s === 'out' ? t('Left out') : undefined} onClick={() => setTagState((m) => ({ ...m, [tag.id]: nextTagState(s) }))}>
                    {s === 'out' ? `− ${tag.name}` : s === 'in' ? `+ ${tag.name}` : tag.name}
                  </button>
                )
              })}
            </div>
          ))}
          <div className="mg-tools">
            <button className="btn-sm primary" onClick={run} disabled={busy}>
              {busy ? t('Searching…') : t('Search')}
            </button>
            <button
              className="btn-sm"
              onClick={() => {
                setTagState({})
                setStatus([])
                setDemo([])
                setLang([])
                setYear('')
                setOrder('')
                setReadable(false)
              }}
            >
              {t('Clear filters')}
            </button>
          </div>
        </div>
      )}
    </section>
  )
}

// ---- the MangaDex rows of the hub ----

export function useDexHub(adult: boolean) {
  const [hub, setHub] = useState<DexHub | null>(null)
  const [cont, setCont] = useState<DexContinue[]>([])
  const load = useCallback(() => {
    browseApi
      .hub(adult)
      .then(setHub)
      .catch(() => setHub(null))
    browseApi
      .continueRow()
      .then(setCont)
      .catch(() => setCont([]))
  }, [adult])
  useEffect(load, [load])
  return { hub, cont, reloadContinue: load }
}
