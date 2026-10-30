// Home fork: the streamed library. Movies and shows Mediarium plays through
// StreamNZB from their saved NZBs, without keeping the files. Posters,
// descriptions and episodes come from TMDB; Jellyfin sees the same titles in
// its Streamed libraries.
import HelpLink from '../components/HelpLink'
import { useEffect, useMemo, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { Link, useSearchParams } from 'react-router-dom'
import Icon from '../components/Icon'
import KindGlyph from '../components/KindGlyph'
import PosterCard from '../components/PosterCard'
import { isAdmin } from '../api'
import { useAuth } from '../AuthContext'
import { playStream } from './Player'
import { showtimeAction } from './ShowtimePill' // Usherr: a showtime
import { rememberQuality, rememberedQuality, streamApi, type StreamStart } from './streamApi'
import './streamed.css'
import { plural, t } from '../i18n'
import { compareTitles } from './sortTitle'

interface Title {
  id: number
  kind: 'movie' | 'series'
  tmdbId: number
  title: string
  year: number
  overview?: string
  poster?: string
  backdrop?: string
  genres: string[]
  rating?: string
  score?: number
  anime?: boolean
  files: number
  release?: string
}
interface File {
  id: number
  season: number
  episode: number
  name?: string
  overview?: string
  airDate?: string
  release: string
  size: number
  health?: string
}
interface ImportState {
  running: boolean
  done: number
  total: number
  files: number
  failed?: string[]
  message?: string
}

async function call<T>(method: string, path: string): Promise<T> {
  const res = await fetch(`/api/streamed${path}`, { method, credentials: 'include' })
  const data = await res.json().catch(() => null)
  if (!res.ok) throw new Error(data?.error ?? t('Something went wrong (error {status}).', { status: res.status }))
  return data as T
}

type Tab = 'movies' | 'tv' | 'anime'
// The Library's kind tabs: same colours and glyphs.
const TABS: { key: Tab; label: string; color: string; glyph: 'movie' | 'tv' }[] = [
  { key: 'movies', label: t('Movies'), color: 'var(--c-movie)', glyph: 'movie' },
  { key: 'tv', label: t('TV'), color: 'var(--c-tv)', glyph: 'tv' },
  { key: 'anime', label: t('Anime'), color: 'var(--c-tv)', glyph: 'tv' },
]
const tabOf = (ti: Title): Tab => (ti.kind === 'movie' ? 'movies' : ti.anime ? 'anime' : 'tv')
const quality = (release = '') => (/2160p|\b4k\b|uhd/i.test(release) ? '4K' : /1080p/i.test(release) ? 'HD' : /720p/i.test(release) ? 'HD' : '')
const hdr = (release = '') => (/\b(dv|dovi|dolby.?vision)\b/i.test(release) ? 'DV' : /hdr/i.test(release) ? 'HDR' : '')
const atmos = (release = '') => (/atmos/i.test(release) ? 'Atmos' : '')

// Plays a streamed file at a quality. "Try another release" in the player
// asks for the next one in line at the same quality.
async function watch(file: File, label: string, ti: Title, quality: string, next = false): Promise<void> {
  const res = await fetch(`/api/streamed/files/${file.id}/watch`, {
    method: 'POST',
    credentials: 'include',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ quality, next }),
  })
  const data = await res.json().catch(() => null)
  if (!res.ok) throw new Error(data?.error ?? t("Couldn't start it (error {status}).", { status: res.status }))
  playStream(data as StreamStart, label, undefined, {
    onNext: () => watch(file, label, ti, quality, true),
    tvTitle: ti.kind === 'movie' ? { kind: 'movie', tmdbId: ti.tmdbId } : { kind: 'series', tmdbId: ti.tmdbId, season: file.season, episode: file.episode },
    watchIn: { tmdbId: ti.tmdbId, kind: ti.kind === 'movie' ? 'movie' : 'tv' },
  })
}

const QUALITY_LABEL: Record<string, string> = { '4K': t('Home TV (4K)'), '1080p': '1080p', '720p': t('Phone (720p)') }

export default function Streamed() {
  const admin = isAdmin(useAuth().user)
  const [titles, setTitles] = useState<Title[] | null>(null)
  const [error, setError] = useState('')
  const [tab, setTab] = useState<Tab>('movies')
  const [q, setQ] = useState('')
  const [open, setOpen] = useState<Title | null>(null)
  // The Library opens a streamed title here (?open=<id>).
  const [params, setParams] = useSearchParams()
  const openId = Number(params.get('open') || 0)
  useEffect(() => {
    if (!openId || !titles) return
    const ti = titles.find((x) => x.id === openId)
    if (ti) {
      setTab(tabOf(ti))
      setOpen(ti)
    }
  }, [openId, titles])
  const closeSheet = () => {
    setOpen(null)
    if (openId) setParams({}, { replace: true })
  }
  const [imp, setImp] = useState<{ state: ImportState; waiting: boolean } | null>(null)
  const [note, setNote] = useState('')
  const [sort, setSort] = useState<'title' | 'year' | 'added'>(() => {
    try {
      return (localStorage.getItem('streamed-sort') as 'title' | 'year' | 'added') || 'title'
    } catch {
      return 'title'
    }
  })
  const [limit, setLimit] = useState(120)
  const more = useRef<HTMLDivElement>(null)

  const load = () =>
    call<Title[]>('GET', '')
      .then(setTitles)
      .catch((e: Error) => setError(e.message))
  useEffect(() => {
    load()
  }, [])
  // Admins: what's waiting to import, and how an import is going.
  useEffect(() => {
    if (!admin) return
    let ti = 0
    const poll = () =>
      call<{ state: ImportState; waiting: boolean }>('GET', '/import')
        .then((r) => {
          setImp(r)
          if (r.state.running) {
            ti = window.setTimeout(poll, 2000)
            load()
          }
        })
        .catch(() => undefined)
    poll()
    return () => window.clearTimeout(ti)
  }, [admin])

  const counts = useMemo(() => {
    const c: Record<Tab, number> = { movies: 0, tv: 0, anime: 0 }
    for (const ti of titles ?? []) c[tabOf(ti)]++
    return c
  }, [titles])
  const all = useMemo(() => {
    const list = (titles ?? []).filter((ti) => tabOf(ti) === tab && (!q || ti.title.toLowerCase().includes(q.toLowerCase())))
    if (sort === 'title') list.sort((a, b) => compareTitles(a.title, b.title))
    else if (sort === 'year') list.sort((a, b) => b.year - a.year || compareTitles(a.title, b.title))
    else if (sort === 'added') list.sort((a, b) => b.id - a.id)
    return list
  }, [titles, tab, q, sort])
  const shown = all.slice(0, limit)
  // Show more posters as the end of the list comes into view.
  useEffect(() => setLimit(120), [tab, q, sort])
  useEffect(() => {
    const el = more.current
    if (!el) return
    const io = new IntersectionObserver((e) => e[0]?.isIntersecting && setLimit((n) => n + 120), { rootMargin: '800px' })
    io.observe(el)
    return () => io.disconnect()
  }, [all.length, limit])

  const startImport = () => {
    setNote('')
    call('POST', '/import')
      .then(() => call<{ state: ImportState; waiting: boolean }>('GET', '/import').then((r) => setImp(r)))
      .then(() => window.setTimeout(() => setImp((x) => (x ? { ...x } : x)), 100))
      .catch((e: Error) => setNote(e.message))
    // keep polling while it runs
    const poll = () =>
      call<{ state: ImportState; waiting: boolean }>('GET', '/import').then((r) => {
        setImp(r)
        load()
        if (r.state.running) window.setTimeout(poll, 2000)
      })
    window.setTimeout(poll, 1500)
  }
  const toJellyfin = () => {
    setNote(t('Adding the Streamed libraries to Jellyfin…'))
    call<{ added: string[] }>('POST', '/jellyfin')
      .then((r) => setNote(r.added.length ? t('Added to Jellyfin: {names}. Jellyfin is scanning them now.', { names: r.added.join(', ') }) : t('Already in Jellyfin. Asked it to look for new titles.')))
      .catch((e: Error) => setNote(e.message))
  }

  return (
    <div className="sl-page">
      <div className="sl-head page-header">
        <h1>{t('Streamed')}</h1>
        <div className="seg kind-tabs" role="tablist">
          {TABS.map((ti) => (
            <button key={ti.key} role="tab" aria-selected={tab === ti.key} className={tab === ti.key ? 'active' : ''} style={{ ['--kc' as string]: ti.color }} onClick={() => setTab(ti.key)}>
              <KindGlyph kind={ti.glyph} size={17} />
              {ti.label} <small className="mono">{counts[ti.key]}</small>
            </button>
          ))}
        </div>
        <HelpLink topic="home-watching#play-or-stream" />
        <input className="sl-search" type="search" value={q} onChange={(e) => setQ(e.target.value)} placeholder={t('Find a title')} />
        <select
          className="sl-sort"
          value={sort}
          onChange={(e) => {
            const v = e.target.value as 'title' | 'year' | 'added'
            setSort(v)
            try {
              localStorage.setItem('streamed-sort', v)
            } catch {
              /* private mode */
            }
          }}
          aria-label={t('Sort by')}
        >
          <option value="title">{t('A to Z')}</option>
          <option value="year">{t('Newest first')}</option>
          <option value="added">{t('Recently added')}</option>
        </select>
        {admin && (
          <div className="sl-admin">
            {imp?.waiting && !imp.state.running && (
              <button className="primary btn-with-icon" onClick={startImport}>
                <Icon name="download" size={15} /> {t('Import waiting titles')}
              </button>
            )}
            <button className="btn-with-icon" onClick={toJellyfin} title={t('Show these titles in Jellyfin')}>
              <Icon name="tv" size={15} /> {t('Add to Jellyfin')}
            </button>
          </div>
        )}
      </div>
      <p className="hint">{t("Played through StreamNZB from each title's saved release. Nothing here is kept on disk.")}</p>
      {imp?.state.running && (
        <p className="hint">
          {t('Importing… {done} of {total} titles ({files} files)', { done: imp.state.done, total: imp.state.total, files: imp.state.files })}
        </p>
      )}
      {imp && !imp.state.running && (imp.state.failed?.length ?? 0) > 0 && <p className="error-text">{t("Couldn't import: {list}", { list: imp.state.failed?.join('; ') ?? '' })}</p>}
      {note && <p className="hint">{note}</p>}
      {error && <p className="error-text">{error}</p>}
      {titles && all.length === 0 && <p className="hint">{titles.length === 0 ? t('Nothing streamed yet.') : t('Nothing here.')}</p>}
      {all.length > 0 && <p className="hint">{plural(all.length, '{n} title', '{n} titles')}</p>}
      {/* The Library's poster cards; a card opens the title's sheet. */}
      <div className="poster-grid sl-cards">
        {shown.map((ti) => (
          <div key={ti.id} className="sl-cardwrap" onClick={() => setOpen(ti)}>
            <PosterCard
              poster={ti.poster}
              title={ti.title}
              kind={ti.kind === 'movie' ? 'movie' : 'tv'}
              meta={[ti.year || null, ti.kind === 'movie' ? [quality(ti.release), hdr(ti.release)].filter(Boolean).join(' ') || null : t('{n} ep', { n: ti.files })].filter(Boolean).join(' · ')}
              actions={[{ icon: 'play', label: t('Play'), primary: true, onClick: () => setOpen(ti) }, ...(ti.kind === 'movie' ? [showtimeAction({ tmdbId: ti.tmdbId, title: ti.title, year: ti.year, posterUrl: ti.poster })] : [])]}
            />
          </div>
        ))}
      </div>
      {shown.length < all.length && <div ref={more} className="sl-more" aria-hidden="true" />}
      {open && createPortal(<TitleSheet ti={open} onClose={closeSheet} onRemoved={() => void load()} />, document.body)}
    </div>
  )
}

// Usherr: the episode list of a streamed show, opened from the show's own page.
export function StreamedSheet({ id, tmdbId, title, onClose }: { id: number; tmdbId: number; title: string; onClose: () => void }) {
  return <TitleSheet ti={{ id, kind: 'series', tmdbId, title, year: 0, genres: [], files: 0 }} onClose={onClose} />
}

function TitleSheet({ ti: card, onClose, onRemoved }: { ti: Title; onClose: () => void; onRemoved?: () => void }) {
  const admin = isAdmin(useAuth().user)
  const [removing, setRemoving] = useState(false)
  // The list has what a card needs; the rest (overview, backdrop) comes
  // with the title.
  const [full, setFull] = useState<Title | null>(null)
  const ti: Title = { ...card, ...(full ?? {}) }
  const [files, setFiles] = useState<File[] | null>(null)
  const [season, setSeason] = useState(0)
  const [err, setErr] = useState('')
  const [starting, setStarting] = useState(0)
  // The quality to stream at, as in Stremio: remembered on this device.
  const [qualities, setQualities] = useState<string[]>([])
  const [streamQ, setStreamQ] = useState(() => rememberedQuality(['4K', '1080p', '720p']))
  useEffect(() => {
    streamApi
      .status()
      .then((st) => {
        const qs = st.qualities ?? []
        setQualities(qs)
        setStreamQ((q) => (qs.includes(q) ? q : rememberedQuality(qs)))
      })
      .catch(() => undefined)
  }, [])
  const pickQuality = (q: string) => {
    setStreamQ(q)
    rememberQuality(q)
  }
  useEffect(() => {
    call<{ title: Title; files: File[] }>('GET', `/${card.id}`)
      .then((r) => {
        setFull(r.title)
        setFiles(r.files)
        if (r.files.length) setSeason(r.files[0].season)
        // Get the movie (or the first episode) ready while you look, as
        // Stremio does, so Play starts at once.
        if (r.files.length) fetch(`/api/streamed/files/${r.files[0].id}/ready?quality=${encodeURIComponent(streamQ)}`, { method: 'POST', credentials: 'include' }).catch(() => undefined)
      })
      .catch((e: Error) => setErr(e.message))
    // eslint-disable-next-line react-hooks/exhaustive-deps -- once per title; the quality at opening is the one to get ready
  }, [card.id])
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && onClose()
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [onClose])
  const seasons = [...new Set((files ?? []).map((f) => f.season))]
  const play = (f: File) => {
    setErr('')
    setStarting(f.id)
    const label = ti.kind === 'movie' ? ti.title : `${ti.title} · S${f.season}E${f.episode}${f.name ? ' · ' + f.name : ''}`
    watch(f, label, ti, streamQ)
      .then(onClose)
      .catch((e: Error) => setErr(e.message))
      .finally(() => setStarting(0))
  }
  const movieFile = ti.kind === 'movie' ? files?.[0] : undefined
  const remove = () => {
    setErr('')
    fetch(`/api/streamed/titles/${ti.id}`, { method: 'DELETE', credentials: 'include' })
      .then(async (r) => {
        if (!r.ok) throw new Error((await r.json().catch(() => null))?.error ?? t("Couldn't remove it (error {status}).", { status: r.status }))
        onClose()
        onRemoved?.()
      })
      .catch((e: Error) => setErr(e.message))
  }
  return (
    <div className="sl-sheet" role="dialog" aria-modal="true" aria-label={ti.title} onClick={onClose}>
      <div className="sl-sheet-box" onClick={(e) => e.stopPropagation()}>
        <div className="sl-hero" style={ti.backdrop ? { backgroundImage: `linear-gradient(90deg, var(--surface) 30%, transparent), url(${ti.backdrop})` } : undefined}>
          {ti.poster && <img className="sl-hero-poster" src={ti.poster} alt="" />}
          <div className="sl-hero-text">
            <h2>
              {ti.title} {ti.year ? <span className="hint">({ti.year})</span> : null}
            </h2>
            <div className="sl-chips">
              {ti.rating && <span>{ti.rating}</span>}
              {ti.score ? <span>{t('Score {n}', { n: ti.score.toFixed(1) })}</span> : null}
              {movieFile && quality(movieFile.release) && <span>{quality(movieFile.release)}</span>}
              {movieFile && hdr(movieFile.release) && <span>{hdr(movieFile.release)}</span>}
              {movieFile && atmos(movieFile.release) && <span>{atmos(movieFile.release)}</span>}
              {ti.genres.slice(0, 3).map((g) => (
                <span key={g} className="soft">
                  {g}
                </span>
              ))}
            </div>
            {ti.overview && <p className="sl-overview">{ti.overview}</p>}
            {qualities.length > 1 && (
              <div className="sl-quality" role="group" aria-label={t('Stream quality')}>
                {qualities.map((q) => (
                  <button key={q} className={`btn-sm${q === streamQ ? ' primary' : ''}`} onClick={() => pickQuality(q)}>
                    {QUALITY_LABEL[q] ?? q}
                  </button>
                ))}
              </div>
            )}
            {movieFile && (
              <div className="sl-actions">
                <button className="primary btn-with-icon" onClick={() => play(movieFile)} disabled={starting === movieFile.id}>
                  <Icon name="play" size={15} /> {starting === movieFile.id ? t('Starting…') : t('Play')}
                </button>
                <Link className="btn-with-icon" to={`/title/${ti.tmdbId}`} onClick={onClose}>
                  <Icon name="download" size={15} /> {t('Download it')}
                </Link>
                <small className="hint sl-release">{movieFile.release}</small>
              </div>
            )}
          </div>
          <button className="btn-sm sl-close" onClick={onClose} aria-label={t('Close')}>
            <Icon name="x" size={14} />
          </button>
        </div>
        {err && <p className="error-text">{err}</p>}
        {admin && (
          <div className="sl-remove">
            {removing ? (
              <>
                <span>{t("Take {title} out of your streams? Files on disk aren't touched.", { title: ti.title })}</span>
                <button className="btn-sm danger" onClick={remove}>
                  {t('Remove')}
                </button>
                <button className="btn-sm" onClick={() => setRemoving(false)}>
                  {t('Keep')}
                </button>
              </>
            ) : (
              <button className="btn-sm" onClick={() => setRemoving(true)}>
                {t('Remove from streams')}
              </button>
            )}
          </div>
        )}
        {ti.kind === 'series' && files && (
          <div className="sl-episodes">
            <div className="sl-actions">
              <Link className="btn-with-icon" to={`/show/${ti.tmdbId}`} onClick={onClose}>
                <Icon name="download" size={15} /> {t('Download the show')}
              </Link>
            </div>
            {seasons.length > 1 && (
              <div className="sl-tabs">
                {seasons.map((s) => (
                  <button key={s} className={`btn-sm${s === season ? ' primary' : ''}`} onClick={() => setSeason(s)}>
                    {t('Season {n}', { n: s })}
                  </button>
                ))}
              </div>
            )}
            <ol>
              {files
                .filter((f) => f.season === season)
                .map((f) => (
                  <li key={f.id}>
                    <button className="sl-ep" onClick={() => play(f)} disabled={starting === f.id}>
                      <span className="sl-ep-num">{f.episode}</span>
                      <span className="sl-ep-text">
                        <strong>{f.name || t('Episode {n}', { n: f.episode })}</strong>
                        {f.overview && <small>{f.overview}</small>}
                        <small className="hint">
                          {f.airDate} {quality(f.release) && `· ${quality(f.release)}`} {f.health === 'dead' && `· ${t('saved release gone, will search')}`}
                        </small>
                      </span>
                      <Icon name="play" size={18} />
                    </button>
                  </li>
                ))}
            </ol>
          </div>
        )}
      </div>
    </div>
  )
}
