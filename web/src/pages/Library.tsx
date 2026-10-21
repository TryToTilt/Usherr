import { useCallback, useEffect, useMemo, useRef, useState, type CSSProperties } from 'react'
import InTheaters from '../home/InTheaters' // Usherr
import { CardVotes } from '../home/VoteButtons' // home fork
import { useTasteScores } from '../home/useTaste' // home fork
import { useVotes } from '../home/useVotes' // home fork
import Shelves from '../home/Shelves' // home fork
import { RowsByPick } from '../home/shelfKit' // home fork
import { CompleteTeaser } from '../home/Complete' // home fork
import { buildShelves, rowsByOptions, type RowsBy } from '../home/shelfGroups' // home fork
import { ActiveChips, CompactBar, FiltersButton, KindPicker, type ActiveFilter } from '../components/CompactBar' // Usherr
import type { ShowStatus } from '../home/showStatus' // home fork
import { Link, useNavigate } from 'react-router-dom'
import { api, can, isAdmin, type Book, type BookFormat, type BulkResult, type BulkTitle, type CollectionListItem, type Movie, type MusicArtist, type QualityProfile, type QueueItem, type Series, type SourcePref } from '../api'
import { useAuth } from '../AuthContext'
import { playTitle } from '../home/Player' // play hook (home fork)
import { useComicsOn, useNsfwOn } from '../home/libraryKinds' // library hook (home fork)
import { stepLabel, timeLeft } from '../components/Elapsed' // home fork
import '../home/library.css' // library hook (home fork)
import { defaultProfileFor } from '../profileDefaults'
import { useModules, type MediaKind } from '../ModulesContext'
import { activeLibraryTab, libraryTabs } from '../libraryTabs'
import MusicLibrary from './MusicLibrary'
import BookLibrary from './BookLibrary'
import ActionMenu from '../components/ActionMenu'
import BulkBar from '../components/BulkBar'
import { BulkTagMenu, TagChips } from '../components/Tags'
import Dropdown from '../components/Dropdown'
import Icon from '../components/Icon'
import type { GlyphKind } from '../components/KindGlyph'
import MoveToFolderDialog from '../components/MoveToFolderDialog'
import PosterCard, { PosterFallback, type CardAction } from '../components/PosterCard'
import { stackAction, useStack } from '../home/stack' // Usherr: your stack
import StarterPicks from '../components/StarterPicks'
import { WindowedBody, WindowedGrid } from '../components/Windowed'
import { sortProfiles } from '../components/qualityBlurb'
import { detailsState, movieState, progressFor, seriesState, type ItemState } from '../components/state'
import { useToast } from '../components/Toast'
import { formatBytes, withNameTag } from '../format'
import { useConfirm } from '../components/ConfirmProvider'
import { useLive } from '../useLive'
import { chosenOf, count, failureLines, nameList, selectAllKeys, selectionText, selectRange, toggleKey } from '../librarySelection'
import { removeOption } from '../diskUsage'
import InfoTip from '../components/InfoTip'
import HelpLink from '../components/HelpLink'
import { useUrlState } from '../useUrlState'
import { plural, t, tx, st } from '../i18n'
import { compareTitles } from '../titleSort'
import { looseIncludes } from '../looseText'

type Kind = 'movie' | 'tv'
type View = 'grid' | 'list' | 'shelves' // shelves: home fork
type Sort = 'added' | 'title' | 'released' | 'size' | 'status' | 'foryou' // foryou: home fork, the taste engine
const SORTS: Sort[] = ['added', 'title', 'released', 'size', 'status', 'foryou']

// One shape for a movie or a show, so filtering, sorting, selecting and the
// action buttons work the same for both.
interface Item {
  key: string
  kind: Kind
  id: number
  tmdbId: number
  title: string
  tag?: string // tells a show from another of the same name ("UK")
  year: number
  posterUrl?: string
  monitored: boolean
  profileId: number
  status: 'missing' | 'downloading' | 'downloaded' | 'partial'
  quality?: string
  have: number // episodes downloaded (shows)
  total: number // episodes in total (shows)
  filePath?: string
  state: ItemState
  live?: QueueItem
  genres: string[]
  tags: string[]
  gettingDetails: boolean // an import is still filling in this title's details
  copies: number // second copies kept in another quality
  released: string // "2024-03-01", or the year alone when that is all there is
  sizeBytes: number // on disk
  // home fork: the streamed layer. streamedId is set on a title that isn't
  // on disk but streams (its id is then negative); streams marks a library
  // title that can also be streamed.
  streamedId?: number
  streams?: boolean
  showStatus?: ShowStatus // home fork: shows that have ended wear a ribbon
}

// home fork: a title in the streamed collection, as /api/streamed lists it.
interface StreamedTitle {
  id: number
  kind: 'movie' | 'series'
  tmdbId: number
  title: string
  year: number
  poster?: string
  genres: string[]
  anime?: boolean
  files: number
  release?: string
}

const STREAMS_STATE: ItemState = { key: 'streams', label: t('Streams'), icon: 'play', hint: t('Not on disk: plays through StreamNZB.') }

function fromStreamed(t: StreamedTitle): Item {
  const q = /2160p|\b4k\b|uhd/i.test(t.release ?? '') ? '4K' : /1080p|720p/i.test(t.release ?? '') ? 'HD' : undefined
  return {
    key: `s-${t.id}`, kind: t.kind === 'movie' ? 'movie' : 'tv', id: -t.id, tmdbId: t.tmdbId, title: t.title, year: t.year, posterUrl: t.poster,
    monitored: true, profileId: 0, status: 'missing', quality: q, have: 0, total: t.kind === 'series' ? t.files : 1,
    state: STREAMS_STATE, genres: t.genres ?? [], tags: [], gettingDetails: false, streamedId: t.id,
    copies: 0, released: t.year ? String(t.year) : '', sizeBytes: 0,
  }
}

// liveLabel is a card's line for a running download: the step (or progress),
// the size, and how long the step has left (home fork).
function liveLabel(q: QueueItem): string {
  const size = q.sizeBytes ? formatBytes(q.sizeBytes) : ''
  const left = q.stage ? timeLeft(q.stageEnds) : ''
  return [q.stage ? stepLabel(q, size) : `${Math.round(q.progressPct)}%${size ? ` of ${size}` : ''}`, left, q.protocol].filter(Boolean).join(' · ')
}

// The most titles one "search now" for a selection looks up (the server enforces it).
const BULK_SEARCH_LIMIT = 25

const STORAGE = { kind: 'mediarium-library-tab', view: 'mediarium-library-view', view2: 'mediarium-library-view2', rows: 'mediarium-library-rows-', sort: 'mediarium-library-sort', reverse: 'mediarium-library-reverse', size: 'mediarium-library-poster-size' }

// Poster widths the size slider moves between, in pixels.
const POSTER_MIN = 110
const POSTER_MAX = 280
const POSTER_DEFAULT = 168
const remember = (k: string, v: string) => {
  try {
    localStorage.setItem(k, v)
  } catch {
    // best effort only
  }
}
const recall = (k: string) => {
  try {
    return localStorage.getItem(k)
  } catch {
    return null
  }
}

function fromMovie(m: Movie, queue: QueueItem[]): Item {
  return {
    key: `movie-${m.id}`, kind: 'movie', id: m.id, tmdbId: m.tmdbId, title: m.title, year: m.year, posterUrl: m.posterUrl,
    monitored: m.monitored !== false, profileId: m.profileId ?? 0, status: m.status, quality: m.quality, have: m.status === 'downloaded' ? 1 : 0, total: 1, filePath: m.filePath,
    state: detailsState(m) ?? movieState(m, queue), live: progressFor(queue, (q) => q.movieId === m.id && !q.seriesId), genres: m.genres ?? [], tags: m.tags ?? [],
    gettingDetails: !!m.detailsState,
    copies: m.copies ?? 0,
    released: m.releaseDate || (m.year ? String(m.year) : ''),
    sizeBytes: m.sizeBytes ?? 0,
  }
}

function fromSeries(s: Series, queue: QueueItem[]): Item {
  const status: Item['status'] = s.episodeCount > 0 && s.downloadedCount === s.episodeCount ? 'downloaded' : s.downloadedCount > 0 ? 'partial' : 'missing'
  return {
    key: `tv-${s.id}`, kind: 'tv', id: s.id, tmdbId: s.tmdbId, title: s.title, tag: s.nameTag, year: s.year, posterUrl: s.posterUrl,
    monitored: s.monitored, profileId: s.profileId ?? 0, status, have: s.downloadedCount, total: s.episodeCount,
    state: detailsState(s) ?? seriesState(s, queue), live: progressFor(queue, (q) => q.seriesId === s.id), genres: s.genres ?? [], tags: s.tags ?? [],
    gettingDetails: !!s.detailsState,
    copies: s.copies ?? 0,
    released: s.firstAirDate || (s.year ? String(s.year) : ''),
    sizeBytes: s.sizeBytes ?? 0,
    showStatus: s.showStatus,
  }
}

const STATE_FILTERS: { id: string; label: string; test: (i: Item) => boolean }[] = [
  { id: 'all', label: t('All'), test: () => true },
  { id: 'downloaded', label: t('Downloaded'), test: (i) => i.state.key === 'downloaded' },
  { id: 'downloading', label: t('Downloading'), test: (i) => i.state.key === 'downloading' },
  { id: 'pending', label: t('Pending'), test: (i) => i.state.key === 'pending' },
  { id: 'searching', label: t('Waiting for a release'), test: (i) => i.state.key === 'searching' },
  { id: 'partial', label: t('Partial'), test: (i) => i.state.key === 'partial' },
  { id: 'added', label: t('Not monitored'), test: (i) => i.state.key === 'added' },
  { id: 'failed', label: t('Failed'), test: (i) => i.state.key === 'failed' },
  { id: 'streams', label: t('Streams'), test: (i) => i.state.key === 'streams' }, // home fork
]
const FILTERS: Record<Kind, { id: string; label: string; test: (i: Item) => boolean }[]> = { movie: STATE_FILTERS, tv: STATE_FILTERS }

// Your library: movies and shows as posters or a list, with the things you do
// most a click away on every item (search for a release, monitor, open,
// remove) and a select mode for doing them to many at once.
// A Library tab: a kind of media, or one of the two book formats.
// home fork: Anime, Comics and NSFW 18+ live in the Library too.
type Tab = MediaKind | BookFormat | 'anime' | 'comics' | 'nsfw'
// Each tab wears its kind's colour, as everywhere else.
const TAB_COLOR: Record<Tab, string> = { movie: 'var(--c-movie)', tv: 'var(--c-tv)', anime: 'var(--c-tv)', music: 'var(--c-music)', ebook: 'var(--c-book)', audiobook: 'var(--c-audiobook)', comics: 'var(--c-comics)', nsfw: 'var(--c-nsfw)' }

const TAB_LABEL: Record<Tab, string> = { movie: t('Movies'), tv: t('TV'), anime: t('Anime'), music: t('Music'), ebook: t('Ebooks'), audiobook: t('Audiobooks'), comics: t('Comics'), nsfw: t('NSFW 18+') }
const TAB_GLYPH: Record<Tab, GlyphKind> = { movie: 'movie', tv: 'tv', anime: 'tv', music: 'music', ebook: 'book', audiobook: 'audiobook', comics: 'comic', nsfw: 'movie' }

export default function Library() {
  const [filtersOpen, setFiltersOpen] = useState(false)
  const confirm = useConfirm()
  const navigate = useNavigate()
  const toast = useToast()
  // Removing titles, changing their quality profile and importing a library
  // are for administrators.
  const me = useAuth().user
  const admin = isAdmin(me)
  // Searching and monitoring need the "manage" permission; a viewer only looks.
  const canManage = can(me, 'manage')
  const canPlay = can(me, 'play') // play hook (home fork)
  const stack = useStack() // Usherr
  const comicsOn = useComicsOn(admin) // library hook (home fork)
  const nsfwOn = useNsfwOn(admin) // library hook (home fork)
  // The tab and the filters live in the address, so Back from a title brings the list back as it was.
  const [ui, patchUi] = useUrlState({ kind: 'movie', st: 'all', q: '', genre: '', tag: '', decade: '', collection: '' })
  const { on } = useModules()
  const musicOn = on('music')
  const booksOn = on('ebooks') || on('audiobooks')
  // The tabs: movies, TV and music (whichever are on), then ebooks and audiobooks.
  // The left menu lists the same ones (see libraryTabs.ts); Usherr adds Anime, Comics and NSFW 18+.
  const kinds: Tab[] = libraryTabs(on, { comics: comicsOn, nsfw: nsfwOn })
  // The Library opens on the first tab (Movies) unless the address asks for another;
  // a kind that is switched off cannot be shown, so the first that is on opens instead.
  const active: Tab = activeLibraryTab(kinds, ui.kind)
  const kind: Kind = active === 'tv' || active === 'anime' ? 'tv' : 'movie'
  const onMovies = active === 'movie'
  const onTV = active === 'tv' || active === 'anime'
  const [bookList, setBookList] = useState<Book[] | null>(null)
  const [queueNow, setQueueNow] = useState<QueueItem[]>([])
  const [artists, setArtists] = useState<MusicArtist[] | null>(null)
  // home fork: the Library opens on shelves (rows that scroll sideways) unless grid or list was chosen
  const [view, setView] = useState<View>(() => {
    const v = recall(STORAGE.view2)
    return v === 'list' || v === 'grid' ? v : 'shelves'
  })
  const [movies, setMovies] = useState<Item[] | null>(null)
  const [shows, setShows] = useState<Item[] | null>(null)
  // home fork: the streamed collection, and which library shows are anime.
  const [streamed, setStreamed] = useState<StreamedTitle[]>([])
  const [animeIds, setAnimeIds] = useState<ReadonlySet<number>>(new Set())
  useEffect(() => {
    if (!canPlay) return
    fetch('/api/streamed', { credentials: 'include' })
      .then((r) => (r.ok ? r.json() : []))
      .then((l: StreamedTitle[]) => setStreamed(l))
      .catch(() => undefined)
    fetch('/api/home/anime-series', { credentials: 'include' })
      .then((r) => (r.ok ? r.json() : []))
      .then((ids: number[]) => setAnimeIds(new Set(ids)))
      .catch(() => undefined)
  }, [canPlay])
  // How many movies and shows there are, for the tab whose list is not loaded.
  const [titleCounts, setTitleCounts] = useState<{ movies: number; shows: number } | null>(null)
  const [profiles, setProfiles] = useState<QualityProfile[]>([])
  const [defaultProfile, setDefaultProfile] = useState(0)
  const [defaultTvProfile, setDefaultTvProfile] = useState(0)
  const [error, setError] = useState('')
  const text = ui.q
  const setText = (v: string) => patchUi({ q: v })
  const filter = ui.st
  const setFilter = (v: string) => patchUi({ st: v })
  // The sort and its direction are remembered in this browser.
  const [sort, setSortState] = useState<Sort>(() => {
    const saved = recall(STORAGE.sort)
    if (saved === 'year') return 'released' // the older name of this sort
    return SORTS.includes(saved as Sort) ? (saved as Sort) : 'added'
  })
  // The poster size in the grid, remembered in this browser.
  const [posterSize, setPosterSizeState] = useState(() => {
    const n = Number(recall(STORAGE.size))
    return n >= POSTER_MIN && n <= POSTER_MAX ? n : POSTER_DEFAULT
  })
  const setPosterSize = (n: number) => {
    setPosterSizeState(n)
    remember(STORAGE.size, String(n))
  }
  const [reverse, setReverseState] = useState(() => recall(STORAGE.reverse) === '1')
  const setSort = (s: Sort) => {
    setSortState(s)
    remember(STORAGE.sort, s)
  }
  const setReverse = (r: boolean) => {
    setReverseState(r)
    remember(STORAGE.reverse, r ? '1' : '0')
  }
  const genre = ui.genre
  const setGenre = (v: string) => patchUi({ genre: v })
  const tag = ui.tag
  const setTag = (v: string) => patchUi({ tag: v })
  const decade = ui.decade
  const setDecade = (v: string) => patchUi({ decade: v })
  const collection = ui.collection
  const setCollection = (v: string) => patchUi({ collection: v })
  // The series of films (trilogies, franchises) with a film in the library, for the Collection filter.
  const [collections, setCollections] = useState<CollectionListItem[]>([])
  const [selecting, setSelecting] = useState(false)
  const [selected, setSelected] = useState<ReadonlySet<string>>(new Set())
  const anchor = useRef<string | null>(null) // the last one ticked, for shift-click
  const [note, setNote] = useState<{ message: string; lines: string[] } | null>(null)
  const [busy, setBusy] = useState(false)
  // The folders a title can be kept in (the main one first), and the folder being moved to.
  const [libraryFolders, setLibraryFolders] = useState<{ movies: string[]; tv: string[] }>({ movies: [], tv: [] })
  const [moveTo, setMoveTo] = useState<{ folder: string; titles: BulkTitle[] } | null>(null)

  // Every few seconds the lists are read again; an answer that is the same as what is on screen changes nothing, so
  // a big library is not redrawn for no reason.
  const seen = useRef<{ movies?: [QueueItem[], Movie[]]; shows?: [QueueItem[], Series[]] }>({})
  const load = useCallback(() => {
    // All of it is asked at once; the queue only adds each title's progress, so nothing waits for it.
    const queue = api.listQueue().catch(() => [] as QueueItem[])
    const same = <T,>(next: T) => (prev: T | null): T => (prev !== null && JSON.stringify(prev) === JSON.stringify(next) ? prev : next)
    // A list that arrives clears an earlier message, so one failed refresh does not stay on the page.
    const failed = (e: unknown) => setError(e instanceof Error ? e.message : String(e))
    // The server answers "nothing changed" with the very same objects as last time. Then neither the titles nor the
    // queue are new, and there is nothing to build, compare or draw.
    // Only the open tab's list is read (on a big TV library the show list takes a while); the other tab's number
    // comes from the counts.
    if (onMovies)
      Promise.all([queue, api.listMovies()]).then(([q, m]) => {
        setError('')
        if (seen.current.movies?.[0] === q && seen.current.movies[1] === m) return
        seen.current.movies = [q, m]
        setMovies(same(m.map((x) => fromMovie(x, q))))
      }).catch(failed)
    if (onTV)
      Promise.all([queue, api.listSeries()]).then(([q, s]) => {
        setError('')
        if (seen.current.shows?.[0] === q && seen.current.shows[1] === s) return
        seen.current.shows = [q, s]
        setShows(same(s.map((x) => fromSeries(x, q))))
      }).catch(failed)
    api.libraryCounts().then((c) => setTitleCounts((prev) => (prev && prev.movies === c.movies && prev.shows === c.shows ? prev : c))).catch(() => undefined)
    if (musicOn) api.listArtists().then((a) => { setArtists(same(a)); setError('') }).catch(failed)
    if (booksOn) api.listBooks().then((b) => { setBookList(same(b)); setError('') }).catch(failed)
    queue.then((q) => setQueueNow(same(q)))
  }, [musicOn, booksOn, onMovies, onTV])
  useEffect(() => {
    load()
  }, [load])
  useLive(load, 4000)
  useEffect(() => {
    api.listProfiles().then((r) => {
      setProfiles(r.profiles)
      setDefaultProfile(r.defaultId)
      setDefaultTvProfile(defaultProfileFor('tv', r))
    }).catch(() => undefined)
  }, [])

  const movieCount = movies?.length
  useEffect(() => {
    api.collections().then(setCollections).catch(() => undefined)
  }, [movieCount])

  useEffect(() => {
    if (admin) api.libraryFolders().then(setLibraryFolders).catch(() => undefined)
  }, [admin])
  const folderChoices = kind === 'movie' ? libraryFolders.movies : libraryFolders.tv

  // home fork: one library in layers. A title on disk shows once (marked if
  // it also streams); a streamed title with nothing on disk joins it.
  const items = useMemo(() => {
    if (kind === 'movie') {
      if (!movies) return null
      const onDisk = new Set(movies.map((m) => m.tmdbId))
      const streams = new Set(streamed.filter((t) => t.kind === 'movie').map((t) => t.tmdbId))
      return [
        ...movies.map((m) => (streams.has(m.tmdbId) ? { ...m, streams: true } : m)),
        ...streamed.filter((t) => t.kind === 'movie' && !onDisk.has(t.tmdbId)).map(fromStreamed),
      ]
    }
    if (!shows) return null
    const anime = active === 'anime'
    const animeTmdb = new Set(shows.filter((s) => animeIds.has(s.id)).map((s) => s.tmdbId))
    const inLibrary = new Set(shows.map((s) => s.tmdbId))
    const streams = new Set(streamed.filter((t) => t.kind === 'series').map((t) => t.tmdbId))
    return [
      ...shows.filter((s) => animeIds.has(s.id) === anime).map((s) => (streams.has(s.tmdbId) ? { ...s, streams: true } : s)),
      ...streamed.filter((t) => t.kind === 'series' && !inLibrary.has(t.tmdbId) && (!!t.anime || animeTmdb.has(t.tmdbId)) === anime).map(fromStreamed),
    ]
  }, [kind, movies, shows, streamed, animeIds, active])
  // home fork: "For you" puts what fits your taste first: what you haven't
  // seen by its match, then what you have, a thumbs down last.
  const voteOf = useVotes().get
  const tasteIds = useMemo(() => ((sort === 'foryou' || view === 'shelves') && items ? items.map((i) => i.tmdbId).filter((id) => id > 0) : null), [sort, view, items])
  const taste = useTasteScores(kind, tasteIds)
  const matchOf = useCallback((i: Item) => (voteOf(kind, i.tmdbId) || taste.seen.has(i.tmdbId) ? undefined : taste.scores[i.tmdbId]), [voteOf, kind, taste])
  const shown = useMemo(() => {
    if (!items) return null
    const test = FILTERS[kind].find((f) => f.id === filter)?.test ?? (() => true)
    const needle = text.trim().toLowerCase()
    const inCollection = kind === 'movie' && collection ? new Set(collections.find((c) => String(c.id) === collection)?.movieIds ?? []) : null
    const list = items.filter(
      (i) =>
        test(i) &&
        (!needle || looseIncludes(i.title, needle)) &&
        (!genre || i.genres.includes(genre)) &&
        (!tag || i.tags.some((t) => t.toLowerCase() === tag.toLowerCase())) &&
        (!decade || (i.year >= Number(decade) && i.year < Number(decade) + 10)) &&
        (!inCollection || inCollection.has(i.id)),
    )
    // home fork: titles compare without a leading "The", "A" or "An"
    if (sort === 'title') list.sort((a, b) => compareTitles(a.title, b.title))
    else if (sort === 'released') list.sort((a, b) => b.released.localeCompare(a.released) || compareTitles(a.title, b.title))
    else if (sort === 'size') list.sort((a, b) => b.sizeBytes - a.sizeBytes || compareTitles(a.title, b.title))
    else if (sort === 'status') list.sort((a, b) => a.state.key.localeCompare(b.state.key) || compareTitles(a.title, b.title))
    else if (sort === 'foryou') {
      const rank = (i: Item) => {
        const v = voteOf(kind, i.tmdbId)
        if (v === -1) return -2
        if (v === 1 || taste.seen.has(i.tmdbId)) return -1 + (taste.scores[i.tmdbId] ?? 0) / 1000
        return taste.scores[i.tmdbId] ?? 0
      }
      list.sort((a, b) => rank(b) - rank(a) || compareTitles(a.title, b.title))
    }
    if (reverse) list.reverse()
    return list
  }, [items, kind, filter, text, sort, reverse, genre, tag, decade, collection, collections, voteOf, taste])
  // Everything that narrows the list (sorting does not), and the way back.
  const filtersActive = text.trim() !== '' || filter !== 'all' || genre !== '' || tag !== '' || decade !== '' || (kind === 'movie' && collection !== '')
  function clearFilters() {
    patchUi({ q: '', st: 'all', genre: '', tag: '', decade: '', collection: '' })
  }
  // Usherr: the filters that are on, each a chip with an x.
  const activeFilters: ActiveFilter[] = [
    ...(text.trim() ? [{ key: 'q', label: `“${text.trim()}”`, clear: () => setText('') }] : []),
    ...(filter !== 'all' ? [{ key: 'st', label: FILTERS[kind].find((f) => f.id === filter)?.label ?? filter, clear: () => setFilter('all') }] : []),
    ...(tag ? [{ key: 'tag', label: tag, clear: () => setTag('') }] : []),
    ...(genre ? [{ key: 'genre', label: genre, clear: () => setGenre('') }] : []),
    ...(decade ? [{ key: 'decade', label: t('{decade}s', { decade }), clear: () => setDecade('') }] : []),
    ...(kind === 'movie' && collection ? [{ key: 'collection', label: collections.find((c) => String(c.id) === collection)?.name ?? t('Collection'), clear: () => setCollection('') }] : []),
  ]
  const genreOptions = useMemo(() => [...new Set((items ?? []).flatMap((i) => i.genres))].sort(), [items])
  const tagOptions = useMemo(() => [...new Set((items ?? []).flatMap((i) => i.tags))].sort((a, b) => a.localeCompare(b)), [items])
  const decadeOptions = useMemo(() => [...new Set((items ?? []).filter((i) => i.year).map((i) => Math.floor(i.year / 10) * 10))].sort((a, b) => b - a), [items])

  function chooseKind(k: Tab) {
    // home fork: Comics and NSFW 18+ have pages of their own.
    if (k === 'comics') return navigate('/comics')
    if (k === 'nsfw') return navigate('/nsfw')
    patchUi({ kind: k, st: 'all' })
    setSelected(new Set())
  }
  function chooseView(v: View) {
    setView(v)
    remember(STORAGE.view, v === 'shelves' ? 'grid' : v)
    remember(STORAGE.view2, v)
  }

  // A streamed title opens its playback sheet (home fork).
  // Usherr: a streamed title opens its own page like any other (it says Streams there), not the Streamed list.
  const openPath = (i: Item) => (i.kind === 'movie' ? `/title/${i.tmdbId}` : i.streamedId ? `/show/${i.tmdbId}` : `/series/${i.id}`)

  async function run(fn: () => Promise<unknown>, done: string) {
    setBusy(true)
    try {
      await fn()
      toast.success(done)
      load()
      return true
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
      return false
    } finally {
      setBusy(false)
    }
  }

  async function searchNow(i: Item) {
    setBusy(true)
    try {
      const r = await (i.kind === 'movie' ? api.searchNowMovie(i.id) : api.searchNowSeries(i.id))
      toast.info(`${i.title}: ${st(r.message)}`)
      load()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }
  const setMonitored = (i: Item, v: boolean) =>
    run(() => (i.kind === 'movie' ? api.setMovieMonitored(i.id, v) : api.setSeriesMonitored(i.id, v)), v ? t('{title} is now monitored.', { title: i.title }) : t('{title} is no longer monitored.', { title: i.title }))

  // One title's Remove button on its card or row: the question and the
  // "also delete the files" box are the same as ever.
  async function removeOne(i: Item) {
    const usage = await (i.kind === 'movie' ? api.movieDiskUsage(i.id) : api.seriesDiskUsage(i.id)).catch(() => null)
    const answer = await confirm({
      title: t('Remove {title} from your library?', { title: i.title }),
      body: <p>{t('Mediarium stops tracking it. You can add it back any time.')}</p>,
      confirmLabel: t('Remove'),
      danger: true,
      option: removeOption(i.kind === 'movie' ? 'movie' : 'show', usage),
    })
    if (!answer) return
    const files = answer.checked
    await run(() => (i.kind === 'movie' ? api.deleteMovie(i.id, files) : api.deleteSeries(i.id, files)), files ? t('Removed {title} and its files.', { title: i.title }) : t('Removed {title}.', { title: i.title }))
  }

  const onStackOf = (i: Item): CardAction[] => (i.kind === 'movie' || i.kind === 'tv' ? [stackAction(stack, { kind: i.kind, tmdbId: i.tmdbId, title: i.title, year: i.year, posterUrl: i.posterUrl }, (e) => toast.error(e instanceof Error ? e.message : String(e)))] : []) // Usherr: your stack
  const actionsFor = (i: Item): CardAction[] => [...actionsOf(i), ...onStackOf(i)]
  const actionsOf = (i: Item): CardAction[] =>
    i.streamedId
      ? [
          // home fork: a streamed title: watch it, or get it for good.
          // Usherr: both open the title's page, where Watch (streams) and Add live.
          { icon: 'play', label: t('Watch (streams)'), onClick: () => navigate(openPath(i)), primary: true },
          { icon: 'download', label: t('Download it'), onClick: () => navigate(openPath(i)) },
        ]
      : [
    // play hook (home fork)
    ...(i.kind === 'movie' && i.status === 'downloaded' && canPlay ? [{ icon: 'play', label: t('Play'), onClick: () => playTitle('movie', i.id, i.title), primary: true } satisfies CardAction] : []),
    ...(canManage
      ? [
          { icon: 'search', label: t('Search for a release now'), onClick: () => void searchNow(i), disabled: busy } satisfies CardAction,
          { icon: i.monitored ? 'eye' : 'eye-off', label: i.monitored ? t('Monitored (click to stop)') : t('Not monitored (click to monitor)'), onClick: () => void setMonitored(i, !i.monitored), active: i.monitored, disabled: busy } satisfies CardAction,
        ]
      : []),
    { icon: 'open', label: t('Open'), onClick: () => navigate(openPath(i)) },
    ...(admin ? [{ icon: 'trash', label: t('Remove from library'), onClick: () => void removeOne(i), danger: true, disabled: busy } satisfies CardAction] : []),
        ]

  // Ticking one, or a whole stretch with shift held.
  function pick(key: string, shift: boolean) {
    setSelected((cur) => (shift ? selectRange(cur, shown ?? [], anchor.current, key) : toggleKey(cur, key)))
    anchor.current = key
  }
  function stopSelecting() {
    setSelecting(false)
    setSelected(new Set())
    setNote(null)
  }
  // The bar counts, and every action reaches, only what is in the view: after a
  // filter or a search, nothing you can no longer see is touched.
  const chosen = useMemo(() => chosenOf(shown ?? [], selected), [shown, selected])
  const bulkTitles = (): BulkTitle[] => chosen.filter((i) => !i.streamedId).map((i) => ({ kind: i.kind, id: i.id })) // streamed titles aren't library titles (home fork)
  const noun = kind === 'movie' ? 'movie' : 'show'
  const selText = selectionText({ selected: chosen.length, shown: shown?.length ?? 0, total: items?.length ?? 0, noun })

  // Says how a bulk change went: a short toast when all is well, and a note
  // that stays on the page, with the reasons, when something could not be done.
  function report(r: { message: string; failed: BulkResult['failed'] }) {
    if (r.failed.length === 0) {
      setNote(null)
      toast.success(st(r.message))
      return
    }
    const titleOf = (k: string, id: number) => (items ?? []).find((i) => i.kind === k && i.id === id)?.title
    setNote({ message: r.message, lines: failureLines(r.failed, titleOf) })
    toast.error(st(r.message))
  }
  async function runBulk(action: () => Promise<{ message: string; failed: BulkResult['failed'] }>) {
    setBusy(true)
    setNote(null)
    try {
      report(await action())
      load()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  async function searchChosen() {
    // The same test the server uses: monitored, and something missing.
    const wanted = chosen.filter((i) => i.monitored && (i.kind === 'movie' ? i.status === 'missing' : i.total > 0 && i.have < i.total))
    if (wanted.length === 0) {
      toast.info(t('Nothing to search for. Only monitored titles that are missing something are searched.'))
      return
    }
    const over = Math.max(0, wanted.length - BULK_SEARCH_LIMIT)
    const answer = await confirm({
      title: t('Search for {what} now?', { what: count(Math.min(wanted.length, BULK_SEARCH_LIMIT), noun) }),
      body: (
        <>
          <p>
            {plural(wanted.length, '{n} of the {total} selected is monitored and missing something, so that one is searched. The rest are left alone.', '{n} of the {total} selected are monitored and missing something, so those are searched. The rest are left alone.', { total: chosen.length })}
          </p>
          {over > 0 && (
            <p>
              {t('It searches at most {limit} titles at a time. The other {over} are left to the automatic search.', { limit: BULK_SEARCH_LIMIT, over })}
            </p>
          )}
          <p>{t('The search runs in the background. Anything it finds shows up in Activity.')}</p>
        </>
      ),
      confirmLabel: t('Search {n}', { n: Math.min(wanted.length, BULK_SEARCH_LIMIT) }),
    })
    if (!answer) return
    setBusy(true)
    setNote(null)
    try {
      const r = await api.bulkSearchNow(wanted.map((i) => ({ kind: i.kind, id: i.id })))
      if (r.failed.length > 0) report(r)
      else toast.info(st(r.message))
      load()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  async function replaceChosen() {
    const withFile = chosen.filter((i) => i.kind === 'movie' && i.status === 'downloaded')
    if (withFile.length === 0) {
      toast.info(t('Nothing to replace. Only movies that have a file can be replaced with a smaller version.'))
      return
    }
    const todo = withFile.slice(0, BULK_SEARCH_LIMIT)
    const total = todo.reduce((sum, i) => sum + i.sizeBytes, 0)
    const answer = await confirm({
      title: t('Replace {what} with a smaller version?', { what: count(todo.length, noun) }),
      body: (
        <>
          <p>
            <strong>{nameList(todo.map((i) => i.title))}</strong>
          </p>
          <p>{t('For each one, Mediarium looks for a release that its quality profile accepts and that is smaller than the file it has now. The files take up {size} together.', { size: formatBytes(total) })}</p>
          <p>{t('A file is only removed once its new version is downloaded and in your library. If nothing suitable is found, or a download fails, that file stays.')}</p>
          {withFile.length > todo.length && <p>{t('It does at most {limit} titles at a time. Run it again for the other {over}.', { limit: BULK_SEARCH_LIMIT, over: withFile.length - todo.length })}</p>}
        </>
      ),
      confirmLabel: t('Look for smaller versions'),
    })
    if (!answer) return
    setBusy(true)
    setNote(null)
    try {
      const r = await api.bulkReplaceSmaller(todo.map((i) => ({ kind: i.kind, id: i.id })))
      if (r.failed.length > 0) report(r)
      else toast.info(st(r.message))
      load()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  async function removeChosen() {
    const n = chosen.length
    if (n === 0) return
    const what = count(n, noun)
    const answer = await confirm({
      title: t('Remove {title} from your library?', { title: what }),
      body: (
        <>
          <p>
            <strong>{nameList(chosen.map((i) => i.title))}</strong>
          </p>
          <p>
            {plural(n, 'Mediarium stops tracking it and cancels anything still downloading for it. You can add it back any time.', 'Mediarium stops tracking all {n} and cancels anything still downloading for them. You can add them back any time.')}
          </p>
          <p>
            {tx('{files} unless you tick the box below.', { files: <strong>{t('Your files stay where they are')}</strong> })}
          </p>
        </>
      ),
      confirmLabel: t('Remove {what}', { what }),
      danger: true,
      option: {
        label: n === 1 ? t('Also delete its files from the disk') : t('Also delete the files of all {n} from the disk', { n }),
        hint: t('This deletes their folders in your library too. It cannot be undone.'),
        defaultChecked: false,
        warning:
          n === 1
            ? noun === 'movie'
              ? t('The files of this movie will be deleted for good.')
              : t('The files of this show will be deleted for good.')
            : t('The files of all {what} will be deleted for good.', { what }),
      },
    })
    if (!answer) return
    await runBulk(() => api.bulkRemove(bulkTitles(), answer.checked))
  }

  const defaultChoiceLabel = useMemo(() => {
    const movieName = profiles.find((p) => p.id === defaultProfile)?.name
    const tvName = profiles.find((p) => p.id === defaultTvProfile)?.name
    if (movieName && tvName && movieName !== tvName) return t('Default ({movies} for movies, {tv} for TV)', { movies: movieName, tv: tvName })
    return t('Default ({name})', { name: movieName ?? t('the default profile') })
  }, [profiles, defaultProfile, defaultTvProfile])
  const profileChoices = useMemo(
    () => [
      { id: '0', label: defaultChoiceLabel },
      ...sortProfiles(profiles).map((p) => ({ id: String(p.id), label: p.name })),
    ],
    [profiles, defaultChoiceLabel],
  )

  const total = items?.length ?? 0

  // Only the kinds of media that are switched on get a tab.
  const bookCount = (f: BookFormat) => bookList?.filter((b) => b[f].wanted || b[f].status === 'downloaded').length
  // home fork: the counts include the streamed layer, and Anime apart from TV.
  const layerCounts = useMemo(() => {
    const movieTmdb = new Set((movies ?? []).map((m) => m.tmdbId))
    const showTmdb = new Set((shows ?? []).map((s) => s.tmdbId))
    const animeShows = (shows ?? []).filter((s) => animeIds.has(s.id)).length
    let sm = 0
    let stv = 0
    let san = 0
    for (const st of streamed) {
      if (st.kind === 'movie') {
        if (!movieTmdb.has(st.tmdbId)) sm++
      } else if (!showTmdb.has(st.tmdbId)) {
        if (st.anime) san++
        else stv++
      }
    }
    return { sm, stv, san, animeShows }
  }, [movies, shows, streamed, animeIds])
  // Only the open tab's list is loaded (3.1.3); the other tabs' numbers come from the server's counts.
  const counts: Record<Tab, number | undefined> = {
    movie: active === 'movie' && movies ? movies.length + layerCounts.sm : titleCounts?.movies,
    tv: onTV && shows ? shows.length - layerCounts.animeShows + layerCounts.stv : titleCounts ? titleCounts.shows - animeIds.size : undefined,
    anime: onTV && shows ? layerCounts.animeShows + layerCounts.san : animeIds.size,
    music: artists?.length, ebook: bookCount('ebook'), audiobook: bookCount('audiobook'), comics: undefined, nsfw: undefined,
  }
  // Usherr: the kinds as one drop-down (CompactBar.tsx); music and books show it in their own toolbars too.
  const kindSwitch = <KindPicker options={kinds.map((k) => ({ key: k, label: TAB_LABEL[k], glyph: TAB_GLYPH[k], color: TAB_COLOR[k], count: counts[k] }))} value={active} onChange={(k) => chooseKind(k as Tab)} />

  // home fork: how the shelves are cut, remembered for each tab
  const [rowsByMap, setRowsByMap] = useState<Record<string, RowsBy>>({})
  const rowsBy: RowsBy = rowsByMap[active] ?? ((recall(STORAGE.rows + active) as RowsBy | null) || 'smart')
  const setRowsBy = (v: RowsBy) => {
    setRowsByMap((m) => ({ ...m, [active]: v }))
    remember(STORAGE.rows + active, v)
  }
  const [moods, setMoods] = useState<Record<number, string[]>>({})
  useEffect(() => {
    if (view !== 'shelves' || rowsBy !== 'mood') return
    fetch(`/api/home/moods?kind=${kind}`, { credentials: 'include' })
      .then((r) => (r.ok ? r.json() : {}))
      .then((m: Record<number, string[]>) => setMoods(m))
      .catch(() => undefined)
  }, [view, rowsBy, kind])
  const shelves = useMemo(
    () =>
      view === 'shelves' && shown
        ? buildShelves(shown, kind, rowsBy, {
            seeGenre: (g) => (setGenre(g), chooseView('grid')),
            seeDecade: (d) => (setDecade(String(d)), chooseView('grid')),
            seeCollection: (id) => (setCollection(String(id)), chooseView('grid')),
            collections,
            moods,
          })
        : [],
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [view, shown, kind, rowsBy, collections, moods],
  )
  // home fork: the taste engine's shelf, first among the smart shelves: what
  // you haven't seen that fits your taste best.
  const forYouShelf = useMemo(() => {
    if (view !== 'shelves' || rowsBy !== 'smart' || !shown) return null
    const list = shown.filter((i) => (matchOf(i) ?? 0) >= 65).sort((a, b) => (matchOf(b) ?? 0) - (matchOf(a) ?? 0))
    return list.length >= 3 ? { key: 'foryou', title: t('For you'), items: list.slice(0, 24), seeAll: () => (setSort('foryou'), chooseView('grid')) } : null
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [view, rowsBy, shown, matchOf])
  const allShelves = forYouShelf ? [forYouShelf, ...shelves] : shelves

  // Usherr: search and AI are in the top bar
  if (active === 'music') return <><MusicLibrary artists={artists} error={error} switcher={kindSwitch} admin={admin} reload={load} /></>
  if (active === 'ebook' || active === 'audiobook') return <><BookLibrary format={active} books={bookList} queue={queueNow} error={error} switcher={kindSwitch} reload={load} admin={admin} /></>

  // The poster card, shared by the grid and the shelves.
  const cardFor = (i: Item) => (
    <PosterCard
      key={i.key}
      to={openPath(i)}
      poster={i.posterUrl}
      title={withNameTag(i.title, i.tag, i.year)}
      meta={[
        i.year || null,
        i.streamedId ? (i.kind === 'movie' ? i.quality || t('streams') : t('{n} episodes, streams', { n: i.total })) : i.kind === 'movie' ? i.quality : i.gettingDetails ? null : t('{have}/{total} episodes', { have: i.have, total: i.total }),
        i.copies ? plural(i.copies, '+{n} copy', '+{n} copies') : null,
        i.monitored ? null : t('unmonitored'),
        i.streams && !i.streamedId ? t('also streams') : null, // home fork
      ].filter(Boolean).join(' · ')}
      state={i.state}
      kind={i.kind}
      ribbon={i.showStatus === 'ended' ? { label: t('Ended'), tone: 'ended' } : undefined} // home fork
      progress={i.live ? { pct: i.live.progressPct, label: liveLabel(i.live) } : undefined}
      dim={!i.monitored && !i.streamedId}
      actions={actionsFor(i)}
      selection={selecting ? { selected: selected.has(i.key), onToggle: (e) => pick(i.key, e.shiftKey) } : undefined}
      footer={
        <>
          {i.kind === 'tv' && i.total > 0 && !i.gettingDetails && (
            <div className="bar" style={{ width: '100%', marginTop: 6 }} title={t('{have} of {total} episodes', { have: i.have, total: i.total })}>
              <span style={{ width: `${(i.have / i.total) * 100}%` }} />
            </div>
          )}
          {i.tags.length > 0 && (
            <span className="pcard-tags">
              <TagChips tags={i.tags} />
            </span>
          )}
          <CardVotes kind={i.kind} tmdbId={i.tmdbId} title={i.title} year={i.year} image={i.posterUrl} match={sort === 'foryou' || view === 'shelves' ? matchOf(i) : undefined} /> {/* home fork: your vote */}
        </>
      }
    />
  )

  return (
    <div>
      {/* Usherr: one slim row, pinned under the top bar */}
      <CompactBar
        open={filtersOpen}
        row={
          <>
            {kindSwitch}
            <FiltersButton open={filtersOpen} onToggle={() => setFiltersOpen((o) => !o)} count={activeFilters.length} />
            <span className="cbar-sort">
              <Dropdown
                icon="sort"
                label={t('Sort')}
                value={sort}
                onChange={(v) => setSort(v as Sort)}
                options={[
                  { value: 'added', label: t('Recently added') },
                  { value: 'title', label: t('Title A–Z') },
                  { value: 'released', label: t('Newest release') },
                  { value: 'size', label: t('Largest on disk') },
                  { value: 'status', label: t('Status') },
                  { value: 'foryou', label: t('For you') }, // home fork: the taste engine
                ]}
              />
            </span>
            <button className="icon-btn cbar-reverse" onClick={() => setReverse(!reverse)} aria-pressed={reverse} title={reverse ? t('Reversed order. Click for the normal order.') : t('Reverse the order')} aria-label={t('Reverse the order')}>
              <Icon name="sort" size={16} />
            </button>
            <span className="spacer" />
            <button className="primary btn-with-icon toolbar-add" onClick={() => navigate('/search')} aria-label={t('Add')}>
              <Icon name="plus" size={16} /> <span className="cbar-add-label">{t('Add')}</span>
            </button>
          </>
        }
        panel={
          <>
            <input type="search" className="cbar-find" placeholder={kind === 'movie' ? t('Find in your movies…') : t('Find in your shows…')} value={text} onChange={(e) => setText(e.target.value)} aria-label={t('Find in your library')} />
            <Dropdown
              icon="grid"
              label={t('Status')}
              value={filter}
              onChange={setFilter}
              options={FILTERS[kind].map((f) => ({ value: f.id, label: items ? `${f.label} (${items.filter(f.test).length})` : f.label }))}
            />
            <InfoTip
              text={t(
                'Downloaded: the file is in your library.\nDownloading: coming in now.\nPending: queued and waiting its turn.\nWaiting for a release: monitored, and Mediarium keeps checking.\nPartial: some episodes are downloaded.\nNot monitored: in your library, but nothing downloads by itself.\nFailed: the last attempt failed.',
              )}
            />
            {tagOptions.length > 0 && (
              <Dropdown icon="bookmark" label={t('Tag')} value={tag} onChange={setTag} options={[{ value: '', label: t('All tags') }, ...tagOptions.map((t) => ({ value: t, label: t }))]} />
            )}
            {genreOptions.length > 0 && (
              <Dropdown icon="sparkles" label={t('Genre')} value={genre} onChange={setGenre} options={[{ value: '', label: t('All genres') }, ...genreOptions.map((g) => ({ value: g, label: g }))]} />
            )}
            {kind === 'movie' && collections.length > 0 && (
              <Dropdown icon="film" label={t('Collection')} value={collection} onChange={setCollection} options={[{ value: '', label: t('All collections') }, ...collections.map((c) => ({ value: String(c.id), label: c.name }))]} />
            )}
            <Dropdown icon="calendar" label={t('Year')} value={decade} onChange={setDecade} options={[{ value: '', label: t('Any year') }, ...decadeOptions.map((d) => ({ value: String(d), label: t('{decade}s', { decade: String(d) }) }))]} />
            <button className={`btn-with-icon cbar-reverse-p${reverse ? ' primary' : ''}`} onClick={() => setReverse(!reverse)} aria-pressed={reverse}>
              <Icon name="sort" size={16} /> {t('Reverse the order')}
            </button>
            <span className="spacer" />
            {(view === 'grid' || view === 'shelves') && (
              <label className="poster-size" title={t('Poster size')}>
                <Icon name="image" size={14} />
                <input type="range" min={POSTER_MIN} max={POSTER_MAX} step={2} value={posterSize} onChange={(e) => setPosterSize(Number(e.target.value))} aria-label={t('Poster size')} />
              </label>
            )}
            <div className="seg">
              <button className={view === 'shelves' ? 'active' : ''} onClick={() => chooseView('shelves')} aria-label={t('Shelves')} title={t('Shelves: rows that scroll sideways')}>
                <Icon name="menu" size={16} />
              </button>
              <button className={view === 'grid' ? 'active' : ''} onClick={() => chooseView('grid')} aria-label={t('Grid view')}>
                <Icon name="grid" size={16} />
              </button>
              <button className={view === 'list' ? 'active' : ''} onClick={() => chooseView('list')} aria-label={t('List view')}>
                <Icon name="list" size={16} />
              </button>
            </div>
            {admin && (
              <button className={`btn-with-icon${selecting ? ' primary' : ''}`} aria-pressed={selecting} onClick={() => (selecting ? stopSelecting() : setSelecting(true))}>
                <Icon name="check" size={16} /> {t('Select')}
              </button>
            )}
            {admin && <HelpLink topic="library#selecting-many-titles" text={t('Pick several titles, then change them all at once: monitoring, quality profile, where they download from, tags, folder, searching or removing. Nothing changes until you press one of the actions.')} />}
            {admin && (
              <button className="btn-with-icon" onClick={() => navigate(`/import?kind=${kind}`)}>
                <Icon name="folder" size={16} /> {t('Import')}
              </button>
            )}
          </>
        }
        chips={<ActiveChips filters={activeFilters} onClearAll={clearFilters} />}
      />

      {selecting && admin && (
        <BulkBar text={selText} onSelectAll={() => shown && setSelected(selectAllKeys(shown))} onSelectNone={() => setSelected(new Set())} onDone={stopSelecting} busy={busy}>
          <button className="btn-sm btn-with-icon" disabled={chosen.length === 0 || busy} onClick={() => void runBulk(() => api.bulkMonitored(bulkTitles(), true))}>
            <Icon name="eye" size={15} /> {t('Monitor')}
          </button>
          <button className="btn-sm btn-with-icon" disabled={chosen.length === 0 || busy} onClick={() => void runBulk(() => api.bulkMonitored(bulkTitles(), false))}>
            <Icon name="eye-off" size={15} /> {t('Stop monitoring')}
          </button>
          <InfoTip text={t('Monitor turns automatic searching on for the selected titles; Stop monitoring turns it off and also removes downloads that were only waiting for them.')} />
          <ActionMenu
            label={t('Better versions')}
            disabled={chosen.length === 0 || busy}
            onPick={(id) => void runBulk(() => api.bulkNoUpgrade(bulkTitles(), id === 'leave'))}
            choices={[
              { id: 'look', label: t('Look for better versions'), hint: t('Swap a download for a better one when its quality profile allows it.') },
              { id: 'leave', label: t('Leave what I have alone'), hint: t('Keep the files as they are, even if a better one is out there.') },
            ]}
          />
          <InfoTip text={t("Look for better versions lets Mediarium swap a download for a better one when the title's quality profile allows it. Leave what I have alone stops that for these titles, whatever their profile says.")} />
          <ActionMenu label={t('Quality profile')} disabled={chosen.length === 0 || busy || profiles.length === 0} choices={profileChoices} onPick={(id) => void runBulk(() => api.bulkProfile(bulkTitles(), Number(id)))} />
          <InfoTip text={t("Gives the selected titles another quality profile, or the default. A title whose file is below the new profile's cutoff starts looking for that quality straight away.")} />
          <BulkTagMenu
            disabled={chosen.length === 0 || busy}
            inSelection={[...new Set(chosen.flatMap((i) => i.tags))].sort((a, b) => a.localeCompare(b))}
            onAdd={(t) => void runBulk(() => api.bulkTags(bulkTitles(), [t], []))}
            onRemove={(t) => void runBulk(() => api.bulkTags(bulkTitles(), [], [t]))}
          />
          <ActionMenu
            label={t('Download from')}
            disabled={chosen.length === 0 || busy}
            onPick={(id) => void runBulk(() => api.bulkSources(bulkTitles(), (id === 'default' ? '' : id) as SourcePref))}
            choices={[
              { id: 'default', label: t('Use the default') },
              { id: 'usenet', label: t('Usenet only') },
              { id: 'torrent', label: t('Torrents only') },
              { id: 'both', label: t('Usenet and torrents') },
            ]}
          />
          <InfoTip text={t('Which downloaders the selected titles may use: Usenet only, torrents only, both, or whatever your settings say.')} />
          {folderChoices.length > 1 && (
            <ActionMenu
              label={t('Move to folder')}
              disabled={chosen.length === 0 || busy}
              choices={folderChoices.map((f, i) => ({ id: String(i), label: f, hint: i === 0 ? t('The main folder') : undefined }))}
              onPick={(id) => setMoveTo({ folder: folderChoices[Number(id)] ?? folderChoices[0], titles: bulkTitles() })}
            />
          )}
          {folderChoices.length > 1 && <InfoTip text={t('Sends the next files of the selected titles to another library folder. You can also move the files that are already there; Mediarium shows what would happen before it starts.')} />}
          <button className="btn-sm btn-with-icon" disabled={chosen.length === 0 || busy} onClick={() => void searchChosen()}>
            <Icon name="search" size={15} /> {t('Search now')}
          </button>
          <InfoTip text={t('Looks for releases of the selected titles that are monitored and missing something, at most 25 at a time. The search runs in the background and anything it finds shows up in Activity.')} />
          {kind === 'movie' && (
            <>
              <button className="btn-sm btn-with-icon" disabled={chosen.length === 0 || busy} onClick={() => void replaceChosen()}>
                <Icon name="download" size={15} /> {t('Replace with smaller')}
              </button>
              <InfoTip text={t('For movies you moved to a lower quality profile. Looks for a smaller release the profile accepts, downloads it, and only then removes the big file. At most 25 at a time.')} />
            </>
          )}
          <button className="btn-sm btn-danger btn-with-icon" disabled={chosen.length === 0 || busy} onClick={() => void removeChosen()}>
            <Icon name="trash" size={15} /> {t('Remove from library')}
          </button>
        </BulkBar>
      )}

      {moveTo !== null && (
        <MoveToFolderDialog
          kind={kind}
          titles={moveTo.titles}
          folders={folderChoices}
          initial={moveTo.folder}
          onClose={() => setMoveTo(null)}
          onChanged={load}
        />
      )}

      {note && (
        <div className="bulk-note" role="status">
          <div>
            <strong>{note.message}</strong>
            <ul>
              {note.lines.map((l, n) => (
                <li key={n}>{l}</li>
              ))}
            </ul>
          </div>
          <button className="btn-sm" onClick={() => setNote(null)}>
            {t('Dismiss')}
          </button>
        </div>
      )}

      {kind === 'movie' && !filtersActive && <InTheaters />} {/* Usherr: films in cinemas */}
      {error && <p className="error-text">{error}</p>}

      {shown === null ? (
        <div className="poster-grid" key={`grid-${active}`} style={{ '--poster-min': `${posterSize}px` } as CSSProperties}>
          {Array.from({ length: 12 }, (_, i) => (
            <div key={i} className="skeleton" style={{ aspectRatio: '2 / 3.5' }} />
          ))}
        </div>
      ) : shown.length === 0 ? (
        <>
        <div className="empty-state" key={`empty-${active}`}>
          <Icon name={kind === 'movie' ? 'film' : 'tv'} size={44} />
          {total === 0 ? (
            <>
              <p>{kind === 'movie' ? t('Your movie library is empty.') : t('Your TV library is empty.')}</p>
              <div className="row-actions">
                <button className="primary btn-with-icon" onClick={() => navigate('/search')}>
                  <Icon name="search" size={16} /> {kind === 'movie' ? t('Search to add a movie') : t('Search to add a show')}
                </button>
                {admin && (
                  <button className="btn-with-icon" onClick={() => navigate(`/import?kind=${kind}`)}>
                    <Icon name="folder" size={16} /> {t('Import what you already have')}
                  </button>
                )}
              </div>
            </>
          ) : (
            <>
              <p>{t('Nothing matches these filters.')}</p>
              <button className="btn-with-icon" onClick={clearFilters}>
                {t('Clear filters')}
              </button>
            </>
          )}
        </div>
        {total === 0 && <StarterPicks key={`picks-${kind}`} kind={kind} onAdded={load} />}
        </>
      ) : view === 'shelves' ? (
        <div style={{ ['--shelf-w' as string]: `${posterSize}px` } as CSSProperties}>
          <Shelves key={`shelves-${active}-${rowsBy}`} memory={active} bar={<><RowsByPick value={rowsBy} onChange={setRowsBy} options={rowsByOptions(kind)} /></>} shelves={allShelves} render={cardFor} itemKey={(i) => i.key} empty={rowsBy === 'mood' ? <p className="hint">{t('Moods are still being worked out for your library. They appear here as they’re ready.')}</p> : undefined} />
          {view === 'shelves' && active === 'movie' && <CompleteTeaser />} {/* Usherr: after the shelves, not before the posters */}
        </div>
      ) : view === 'grid' ? (
        <WindowedGrid
          key={`grid-${active}`}
          style={{ '--poster-min': `${posterSize}px` } as CSSProperties}
          items={shown}
          render={cardFor}
        />
      ) : (
        <table className="data-table lib-list">
          <thead>
            <tr>
              {selecting && (
                <th style={{ width: 34 }}>
                  <input
                    type="checkbox"
                    checked={selText.allSelected}
                    onChange={() => setSelected(selText.allSelected ? new Set() : selectAllKeys(shown))}
                    aria-label={t('Select all in this view')}
                  />
                </th>
              )}
              <th>{t('Title')}</th>
              <th>{t('Status')}</th>
              <th>{kind === 'movie' ? t('Quality') : t('Episodes')}</th>
              <th>{t('Profile')}</th>
              <th />
            </tr>
          </thead>
          <WindowedBody
            items={shown}
            columns={selecting ? 6 : 5}
            render={(i) => (
              <tr key={i.key} className={selecting && selected.has(i.key) ? 'selected' : undefined} style={i.monitored ? undefined : { opacity: 0.65 }}>
                {selecting && (
                  <td>
                    <input type="checkbox" checked={selected.has(i.key)} onChange={() => undefined} onClick={(e) => pick(i.key, e.shiftKey)} aria-label={t('Select {title}', { title: i.title })} />
                  </td>
                )}
                <td>
                  <Link to={openPath(i)} className="lib-title" onClick={selecting ? (e) => { e.preventDefault(); pick(i.key, e.shiftKey) } : undefined}>
                    <span className="lib-thumb">{i.posterUrl ? <img src={i.posterUrl} alt="" loading="lazy" /> : <PosterFallback />}</span>
                    <span>
                      <strong>{withNameTag(i.title, i.tag, i.year)}</strong>
                      <small>
                        {i.year || '—'}
                        {i.monitored ? '' : ` · ${t('unmonitored')}`}
                      </small>
                    </span>
                  </Link>
                </td>
                <td>
                  <span className={`state-tag st-${i.state.key}`} title={i.state.hint}>
                    <Icon name={i.state.icon} size={12} /> {i.state.label}
                  </span>
                  {i.live && (
                    <div className="bar active" style={{ marginTop: 6, minWidth: 110, ['--tc' as string]: 'var(--c-activity)' }}>
                      <span style={{ width: `${Math.max(3, i.live.progressPct)}%` }} />
                    </div>
                  )}
                </td>
                <td>
                  {i.kind === 'movie' ? (
                    i.quality || '—'
                  ) : (
                    <div style={{ minWidth: 110 }}>
                      {i.have} / {i.total}
                      <div className="bar" style={{ marginTop: 4 }}>
                        <span style={{ width: `${i.total ? (i.have / i.total) * 100 : 0}%` }} />
                      </div>
                    </div>
                  )}
                </td>
                <td style={{ color: 'var(--text-dim)' }}>{profiles.find((p) => p.id === (i.profileId || (i.kind === 'movie' ? defaultProfile : defaultTvProfile)))?.name ?? '—'}</td>
                <td>
                  <div className="row-actions">
                    {actionsFor(i).map((a) => (
                      <button key={a.label} className={`icon-btn${a.active ? ' on' : ''}${a.danger ? ' danger' : ''}`} title={a.label} aria-label={a.label} aria-pressed={a.active === undefined ? undefined : a.active} disabled={a.disabled} onClick={a.onClick}>
                        <Icon name={a.icon} size={17} />
                      </button>
                    ))}
                  </div>
                </td>
              </tr>
            )}
          />
        </table>
      )}
    </div>
  )
}
