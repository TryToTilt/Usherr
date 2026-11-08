// Usherr: Discover, the one place to find manga: what's trending, popular or new on
// AniList, a genre's best, what someone has on their AniList list, and with the
// MangaDex source on, MangaDex's rows, its search and reading online
// (BrowseHub.tsx). One press adds a series, and nothing is saved before that. No
// key is needed.
import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { createPortal } from 'react-dom'
import { Link, useSearchParams } from 'react-router-dom'
import { can, isAdmin } from '../api'
import { useAuth } from '../AuthContext'
import { useToast } from '../components/Toast'
import { t } from '../i18n'
import { browseApi, type DexSearch, type DexTitle } from './browseApi'
import { BrowseSearch, DexContinueRow, LatestUpdates, OfficialRow, PickPreview, PickRow, PickTile, PopularCarousel, useBrowseAdd, useDexHub, useDexReader } from './BrowseHub'
import { fromDex, fromFound, mergePicks, type Pick } from './browsePicks'
import MangaCover from './MangaCover'
import { mangaApi, type MangaDiscover as Answer, type MangaFound } from './mangaApi'
import { FORMAT_LABEL, stateLabel } from './mangaStatus'
import './manga.css'

export type Want = 'all' | 'new' | 'none'

// A dialog belongs on the screen, not the page: inside the page's own layer it opens
// wherever the page happens to end, which on a phone is off the bottom.
export function MangaPortal({ children }: { children: ReactNode }) {
  return createPortal(<div className="mg-page">{children}</div>, document.body)
}

const WANT_KEY = 'usherr.manga.one-press'

function storedWant(): Want {
  try {
    const v = localStorage.getItem(WANT_KEY)
    return v === 'new' || v === 'none' ? v : 'all'
  } catch {
    return 'all'
  }
}

// useOnePress adds a series with one press, saying what the device's choice is.
export function useOnePress(want?: Want) {
  const toast = useToast()
  const [added, setAdded] = useState<Record<number, number>>({})
  const [adding, setAdding] = useState(0)
  const add = useCallback(
    (f: MangaFound) => {
      setAdding(f.anilistId)
      mangaApi
        .add({ anilistId: f.anilistId, want: want ?? storedWant() })
        .then((r) => {
          setAdded((m) => ({ ...m, [f.anilistId]: r.series.id }))
          toast.success(t('{title} added.', { title: r.series.title }))
        })
        .catch((e: Error) => toast.error(e.message))
        .finally(() => setAdding(0))
    },
    [toast, want],
  )
  return { added, adding, add }
}

export type OnePress = ReturnType<typeof useOnePress>

const RELATION: Record<string, () => string> = {
  sequel: () => t('Sequel'),
  prequel: () => t('Prequel'),
  side: () => t('Side story'),
  spinoff: () => t('Spin-off'),
  alternative: () => t('Another version'),
  parent: () => t('The main series'),
}

export function FoundTile({ f, press, canAdd, onOpen }: { f: MangaFound; press: OnePress; canAdd: boolean; onOpen?: (f: MangaFound) => void }) {
  const library = press.added[f.anilistId] ?? f.inLibrary
  const sub = f.relation ? RELATION[f.relation]?.() : [FORMAT_LABEL[f.format]?.() ?? '', stateLabel(f.status)].filter(Boolean).join(' · ')
  const cover = (
    <span className="mg-slot">
      <MangaCover title={f.title} src={f.coverUrl} />
    </span>
  )
  return (
    <article className="mg-found">
      {library ? (
        <Link to={`/manga/${library}`} aria-label={f.title}>
          {cover}
        </Link>
      ) : onOpen ? (
        <button className="cover" onClick={() => onOpen(f)} aria-label={t('More about {title}', { title: f.title })}>
          {cover}
        </button>
      ) : (
        cover
      )}
      <span>
        <span className="t">{f.title}</span>
        <span className="s">{sub}</span>
      </span>
      <span className="mg-chips">
        {f.score ? <span className="mg-pill">{t('Score {n}%', { n: f.score })}</span> : null}
        {f.listStatus && <span className="mg-pill accent">{f.listStatus === 'reading' ? t('Reading') : t('Planning')}</span>}
      </span>
      {library ? (
        <Link className="btn-sm act done" to={`/manga/${library}`}>
          {t('In your library')}
        </Link>
      ) : (
        canAdd && (
          <button className="btn-sm primary act" disabled={press.adding !== 0} onClick={() => press.add(f)}>
            {press.adding === f.anilistId ? t('Adding…') : t('Add')}
          </button>
        )
      )}
    </article>
  )
}

// Discover: the one place to find manga, without leaving Usherr. AniList's shelves
// always; with MangaDex on, its rows, its search and reading online too; and the
// new official volumes your indexers brought in.
export default function MangaDiscover() {
  const [params, setParams] = useSearchParams()
  const { user } = useAuth()
  const canAdd = isAdmin(user) || can(user, 'manga')
  const shelf = params.get('shelf') === 'popular' || params.get('shelf') === 'new' ? (params.get('shelf') as string) : 'trending'
  const genre = params.get('genre') ?? ''
  const [pages, setPages] = useState(1)
  const [answer, setAnswer] = useState<Answer | null>(null)
  const [list, setList] = useState<MangaFound[]>([])
  const [busy, setBusy] = useState(false)
  const [open, setOpen] = useState<Pick | null>(null)
  const [want, setWant] = useState<Want>(storedWant)
  const [user2, setUser2] = useState('')
  const [fromList, setFromList] = useState<Answer | null>(null)
  const [listBusy, setListBusy] = useState(false)
  const [adult, setAdult] = useState(false)
  const [found, setFound] = useState<{ picks: Pick[]; more: boolean; problem?: string; query: DexSearch } | null>(null)
  const [moreBusy, setMoreBusy] = useState(false)
  const press = useBrowseAdd(want)
  const { hub, cont, reloadContinue } = useDexHub(adult)
  const { readChapter, readTitle, reader } = useDexReader(reloadContinue)
  const seq = useRef(0)
  const dexOn = !!hub?.dexOn
  const dex = hub?.mangadex

  useEffect(() => {
    const mine = ++seq.current
    setBusy(true)
    mangaApi
      .discover(shelf, genre, pages)
      .then((d) => {
        if (mine !== seq.current) return
        setAnswer(d)
        setList((old) => (pages === 1 ? d.results : [...old, ...d.results.filter((n) => !old.some((o) => o.anilistId === n.anilistId))]))
      })
      .catch((e: Error) => {
        if (mine === seq.current) setAnswer({ results: [], problem: e.message })
      })
      .finally(() => {
        if (mine === seq.current) setBusy(false)
      })
  }, [shelf, genre, pages])

  const choose = (key: string, value: string) => {
    // From the address as it is now: two quick choices shouldn't lose the first.
    const next = new URLSearchParams(window.location.search)
    if (value) next.set(key, value)
    else next.delete(key)
    setPages(1)
    setList([])
    setParams(next, { replace: true })
  }
  const chooseWant = (w: Want) => {
    setWant(w)
    try {
      localStorage.setItem(WANT_KEY, w)
    } catch {
      /* it lasts until the page closes */
    }
  }
  const lookAtList = () => {
    const name = user2.trim()
    if (!name) return
    setListBusy(true)
    mangaApi
      .aniListList(name)
      .then(setFromList)
      .catch((e: Error) => setFromList({ results: [], problem: e.message }))
      .finally(() => setListBusy(false))
  }
  const stoppedIn = (dexId: string) => cont.find((c) => c.dexMangaId === dexId)?.dexChapterId
  const onRead = (p: Pick) => {
    if (p.dexId) readTitle(p.dexId, adult, stoppedIn(p.dexId))
  }
  const showMore = () => {
    if (!found) return
    const q = { ...found.query, offset: (found.query.offset ?? 0) + 24 }
    setMoreBusy(true)
    browseApi
      .search(q)
      .then((d) => setFound({ picks: mergePicks(found.picks, d.results.map(fromDex)), more: !!d.more, problem: d.problem, query: q }))
      .catch(() => undefined)
      .finally(() => setMoreBusy(false))
  }
  const tile = { press, canAdd, dexOn, onOpen: setOpen, onRead }
  const anilistPicks = useMemo(() => list.map((f) => fromFound(f, dexOn)), [list, dexOn])
  const listRow = (row?: { name: string; titles: DexTitle[] } | null, fallback = '') => (row ? <PickRow title={row.name || fallback} picks={row.titles.map(fromDex)} {...tile} /> : null)

  return (
    <div className="page-pad mg-page">
      <div className="mg-head">
        <Link className="btn-sm" to="/manga">
          {`‹ ${t('Manga')}`}
        </Link>
      </div>
      <div>
        <h1>{t('Discover')}</h1>
        <p className="mg-hint">{dexOn ? t('From AniList and MangaDex. Nothing is saved until you press Add.') : t('From AniList. Nothing is saved until you press Add.')}</p>
      </div>
      <BrowseSearch dexOn={dexOn} adult={adult} onResults={setFound} onRandom={(d) => setOpen(fromDex(d))} />
      <div className="mg-ask" data-walk="manga-discover-want">
        <span className="mg-hint">{t('Add gets:')}</span>
        <div className="seg" role="group" aria-label={t('What to get')}>
          {(
            [
              ['all', t('All volumes')],
              ['new', t('New volumes only')],
              ['none', t('Nothing yet')],
            ] as const
          ).map(([key, label]) => (
            <button key={key} className={want === key ? 'active' : ''} aria-pressed={want === key} onClick={() => chooseWant(key)}>
              {label}
            </button>
          ))}
        </div>
        {hub?.adultAllowed && dexOn && (
          <label className="mr-check">
            <input type="checkbox" checked={adult} onChange={(e) => setAdult(e.target.checked)} />
            <span>{t('Show 18+ titles from MangaDex')}</span>
          </label>
        )}
      </div>
      {found && (
        <section className="bh-section" aria-label={t('Search results')}>
          <div className="bh-head">
            <h2>{t('Search results')}</h2>
            <button className="btn-sm" onClick={() => setFound(null)}>
              {t('Clear')}
            </button>
          </div>
          {found.problem && <p className="mg-hint">{found.problem}</p>}
          {found.picks.length === 0 && !found.problem && <p className="mg-hint">{t('Nothing matched. Try fewer words or filters.')}</p>}
          <div className="mg-grid">
            {found.picks.map((p) => (
              <PickTile key={p.key} p={p} {...tile} />
            ))}
          </div>
          {found.more && (
            <button className="btn-sm" disabled={moreBusy} onClick={showMore}>
              {moreBusy ? t('Searching…') : t('Show more')}
            </button>
          )}
        </section>
      )}
      {dexOn && <DexContinueRow items={cont} onRead={readChapter} />}
      {dex?.problem && (
        <div className="mg-well">
          <b>{t('MangaDex couldn’t be asked just now.')}</b> <span className="mg-hint">{dex.problem}</span>
        </div>
      )}
      {dex && <PopularCarousel titles={dex.popular} onOpen={setOpen} onRead={onRead} press={press} canAdd={canAdd} />}
      {dex && <LatestUpdates updates={dex.latest} onRead={readChapter} onOpen={setOpen} />}
      <section className="bh-section" aria-label={t('From AniList')}>
        <h2>{t('From AniList')}</h2>
        <div className="mg-discover-tools">
          <div className="seg" role="group" aria-label={t('Show')}>
            {(
              [
                ['trending', t('Trending')],
                ['popular', t('Popular')],
                ['new', t('New')],
              ] as const
            ).map(([key, label]) => (
              <button key={key} className={shelf === key ? 'active' : ''} aria-pressed={shelf === key} onClick={() => choose('shelf', key === 'trending' ? '' : key)}>
                {label}
              </button>
            ))}
          </div>
          <select value={genre} onChange={(e) => choose('genre', e.target.value)} aria-label={t('Genre')}>
            <option value="">{t('All genres')}</option>
            {(answer?.genres ?? (genre ? [genre] : [])).map((g) => (
              <option key={g} value={g}>
                {g}
              </option>
            ))}
          </select>
        </div>
        {answer?.problem && (
          <div className="mg-well">
            <b>{t('Couldn’t look just now.')}</b> <span className="mg-hint">{answer.problem}</span>
          </div>
        )}
        {busy && list.length === 0 && <div className="skeleton" style={{ height: 240 }} />}
        {!busy && !answer?.problem && list.length === 0 && <p className="mg-hint">{t('AniList has nothing here right now.')}</p>}
        <div className="mg-grid" aria-live="polite">
          {anilistPicks.map((p) => (
            <PickTile key={p.key} p={p} {...tile} />
          ))}
        </div>
        {answer?.more && (
          <div>
            <button className="btn-sm" disabled={busy} onClick={() => setPages((p) => p + 1)}>
              {busy ? t('Searching…') : t('Show more')}
            </button>
          </div>
        )}
      </section>
      {dex && listRow(dex.seasonal, t('Seasonal'))}
      {dex && listRow(dex.recommended, t('Recommended'))}
      {dex && listRow(dex.selfPublished, t('Self-published'))}
      {dex && <PickRow title={t('Recently added to MangaDex')} picks={dex.recent.map(fromDex)} {...tile} />}
      <OfficialRow volumes={hub?.official ?? []} />
      {hub && !dexOn && <p className="mg-hint">{isAdmin(user) ? t('Turning on MangaDex in Settings > Manga adds more here: new chapters, staff picks, and reading online.') : t('With MangaDex switched on, there’s more here: new chapters, staff picks, and reading online.')}</p>}
      <section className="mg-card mg-stack">
        <h2>{t('From an AniList list')}</h2>
        <p className="mg-hint">{t('Type an AniList user name to see what they’re reading and plan to read. The list has to be public, and no sign-in is needed.')}</p>
        <form
          className="mg-listform"
          onSubmit={(e) => {
            e.preventDefault()
            lookAtList()
          }}
        >
          <input value={user2} onChange={(e) => setUser2(e.target.value)} aria-label={t('AniList user name')} placeholder={t('AniList user name')} autoComplete="off" />
          <button className="btn-sm" type="submit" disabled={listBusy || !user2.trim()}>
            {listBusy ? t('Searching…') : t('Look')}
          </button>
        </form>
        {fromList?.problem && <p className="mg-hint">{fromList.problem}</p>}
        {fromList && !fromList.problem && fromList.results.length === 0 && <p className="mg-hint">{t('That list has nothing being read or planned.')}</p>}
        {fromList && fromList.results.length > 0 && (
          <div className="mg-grid">
            {fromList.results.map((f) => (
              <PickTile key={f.anilistId} p={fromFound(f, dexOn)} {...tile} />
            ))}
          </div>
        )}
      </section>
      {open && (
        <MangaPortal>
          <PickPreview
            p={open}
            press={press}
            canAdd={canAdd}
            dexOn={dexOn}
            adult={adult}
            onRead={(id) => {
              setOpen(null)
              readChapter(id)
            }}
            onClose={() => setOpen(null)}
          />
        </MangaPortal>
      )}
      {reader}
    </div>
  )
}
