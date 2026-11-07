// Usherr: Discover: what's trending, popular or new on AniList, a genre's best, and
// what someone has on their AniList list. One press adds a series, and nothing is
// saved before that. No key is needed.
import { useCallback, useEffect, useRef, useState, type ReactNode } from 'react'
import { createPortal } from 'react-dom'
import { Link, useSearchParams } from 'react-router-dom'
import { useToast } from '../components/Toast'
import { t } from '../i18n'
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

function Details({ f, press, onClose }: { f: MangaFound; press: OnePress; onClose: () => void }) {
  const library = press.added[f.anilistId] ?? f.inLibrary
  return (
    <div className="mg-modal" role="dialog" aria-modal="true" aria-label={f.title} onClick={onClose}>
      <div className="mg-modal-box" onClick={(e) => e.stopPropagation()}>
        <header>
          <h2>{f.title}</h2>
          <button className="btn-sm" onClick={onClose}>
            {t('Close')}
          </button>
        </header>
        <div className="mg-detail-head">
          <MangaCover title={f.title} src={f.coverUrl} />
          <div className="mg-stack">
            <div className="mg-chips">
              <span className="mg-pill manga">{FORMAT_LABEL[f.format]?.() ?? f.format}</span>
              {stateLabel(f.status) && <span className="mg-pill">{stateLabel(f.status)}</span>}
              {f.score ? <span className="mg-pill">{t('Score {n}%', { n: f.score })}</span> : null}
              {(f.genres ?? []).map((g) => (
                <span key={g} className="mg-pill">
                  {g}
                </span>
              ))}
            </div>
            {f.volumes > 0 && <p className="mg-hint">{t('{n} volumes known', { n: f.volumes })}</p>}
          </div>
        </div>
        {f.description && <p className="mg-desc">{f.description}</p>}
        <div className="mg-tools">
          {library ? (
            <Link className="btn-sm primary" to={`/manga/${library}`}>
              {t('In your library')}
            </Link>
          ) : (
            <button className="btn-sm primary" disabled={press.adding !== 0} onClick={() => press.add(f)}>
              {press.adding === f.anilistId ? t('Adding…') : t('Add {title}', { title: f.title })}
            </button>
          )}
          {f.siteUrl && (
            <a className="btn-sm" href={f.siteUrl} target="_blank" rel="noreferrer">
              {t('See it on AniList')}
            </a>
          )}
        </div>
      </div>
    </div>
  )
}

export default function MangaDiscover() {
  const [params, setParams] = useSearchParams()
  const shelf = params.get('shelf') === 'popular' || params.get('shelf') === 'new' ? (params.get('shelf') as string) : 'trending'
  const genre = params.get('genre') ?? ''
  const [pages, setPages] = useState(1)
  const [answer, setAnswer] = useState<Answer | null>(null)
  const [list, setList] = useState<MangaFound[]>([])
  const [busy, setBusy] = useState(false)
  const [open, setOpen] = useState<MangaFound | null>(null)
  const [want, setWant] = useState<Want>(storedWant)
  const [user, setUser] = useState('')
  const [fromList, setFromList] = useState<Answer | null>(null)
  const [listBusy, setListBusy] = useState(false)
  const press = useOnePress(want)
  const seq = useRef(0)

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
    const name = user.trim()
    if (!name) return
    setListBusy(true)
    mangaApi
      .aniListList(name)
      .then(setFromList)
      .catch((e: Error) => setFromList({ results: [], problem: e.message }))
      .finally(() => setListBusy(false))
  }

  return (
    <div className="page-pad mg-page">
      <div className="mg-head">
        <Link className="btn-sm" to="/manga">
          {`‹ ${t('Manga')}`}
        </Link>
      </div>
      <div>
        <h1>{t('Discover')}</h1>
        <p className="mg-hint">{t('From AniList. Nothing is saved until you press Add.')}</p>
      </div>
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
      <div className="mg-ask">
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
      </div>
      {answer?.problem && (
        <div className="mg-well">
          <b>{t('Couldn’t look just now.')}</b> <span className="mg-hint">{answer.problem}</span>
        </div>
      )}
      {busy && list.length === 0 && <div className="skeleton" style={{ height: 240 }} />}
      {!busy && !answer?.problem && list.length === 0 && <p className="mg-hint">{t('AniList has nothing here right now.')}</p>}
      <section className="mg-grid" aria-live="polite">
        {list.map((f) => (
          <FoundTile key={f.anilistId} f={f} press={press} canAdd onOpen={setOpen} />
        ))}
      </section>
      {answer?.more && (
        <div>
          <button className="btn-sm" disabled={busy} onClick={() => setPages((p) => p + 1)}>
            {busy ? t('Searching…') : t('Show more')}
          </button>
        </div>
      )}
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
          <input value={user} onChange={(e) => setUser(e.target.value)} aria-label={t('AniList user name')} placeholder={t('AniList user name')} autoComplete="off" />
          <button className="btn-sm" type="submit" disabled={listBusy || !user.trim()}>
            {listBusy ? t('Searching…') : t('Look')}
          </button>
        </form>
        {fromList?.problem && <p className="mg-hint">{fromList.problem}</p>}
        {fromList && !fromList.problem && fromList.results.length === 0 && <p className="mg-hint">{t('That list has nothing being read or planned.')}</p>}
        {fromList && fromList.results.length > 0 && (
          <div className="mg-grid">
            {fromList.results.map((f) => (
              <FoundTile key={f.anilistId} f={f} press={press} canAdd onOpen={setOpen} />
            ))}
          </div>
        )}
      </section>
      {open && <MangaPortal><Details f={open} press={press} onClose={() => setOpen(null)} /></MangaPortal>}
    </div>
  )
}
