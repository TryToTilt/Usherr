import { useEffect, useMemo, useState, type ReactNode } from 'react'
import { Link } from 'react-router-dom'
import { api, isRequested, type Book, type BookFormat, type BookFound, type QueueItem } from '../api'
import BookImport from '../components/BookImport'
import { listenPath, openBookApp, readPath } from '../bookshelf/open'
import Dropdown from '../components/Dropdown'
import Icon from '../components/Icon'
import PosterCard, { PosterFallback } from '../components/PosterCard'
import { describeState, type ItemState } from '../components/state'
import { useToast } from '../components/Toast'
import FilterToggle from '../components/FilterToggle'
import { t } from '../i18n'
import { useUrlState } from '../useUrlState'
import { useModules } from '../ModulesContext'
import Shelves from '../home/Shelves' // home fork
import { groupShelves, letterShelves, monthShelves, RowsByPick, ShelfControls, useShelfView } from '../home/shelfKit' // home fork

type Sort = 'added' | 'title' | 'author'
type BookRows = 'author' | 'genre' | 'series' | 'narrator' | 'az' | 'added' // home fork
const BOOK_ROWS = ['author', 'genre', 'series', 'narrator', 'az', 'added'] as const
// The AI's book genres (home fork), by their English name.
const GENRE_NAME: Record<string, string> = {
  "Fantasy": t("Fantasy"),
  "Science fiction": t("Science fiction"),
  "Mystery": t("Mystery"),
  "Thriller": t("Thriller"),
  "Horror": t("Horror"),
  "Romance": t("Romance"),
  "Historical fiction": t("Historical fiction"),
  "Literary fiction": t("Literary fiction"),
  "Humor": t("Humor"),
  "Young adult": t("Young adult"),
  "Children's": t("Children's"),
  "Biography & memoir": t("Biography & memoir"),
  "History": t("History"),
  "Science & nature": t("Science & nature"),
  "Self-help": t("Self-help"),
  "Business": t("Business"),
  "True crime": t("True crime"),
  "Other nonfiction": t("Other nonfiction"),
}

export const FORMAT_WORD: Record<BookFormat, string> = { ebook: 'ebook', audiobook: 'audiobook' }
export const FORMAT_PLURAL: Record<BookFormat, string> = { ebook: 'ebooks', audiobook: 'audiobooks' }

// Where one format of a book stands, with the queue's word on it when a
// download for it is running or waiting.
export function bookState(b: Book, f: BookFormat, queue: QueueItem[] = []): ItemState {
  const q = queue.filter((x) => x.bookId === b.id && x.bookFormat === f)
  const running = q.find((x) => x.status === 'downloading' || x.status === 'importing')
  if (running) return describeState('downloading', `${Math.round(running.progressPct)}%`)
  if (q.some((x) => x.status === 'queued')) return describeState('pending')
  if (q.some((x) => x.status === 'paused')) return describeState('paused')
  const st = b[f].status
  if (st === 'downloaded') return describeState('downloaded')
  if (st === 'downloading') return describeState('downloading')
  if (!b[f].wanted) return describeState('added')
  return describeState('searching')
}

const FILTERS: { id: string; label: string; test: (s: ItemState) => boolean }[] = [
  { id: 'all', label: t('All'), test: () => true },
  { id: 'downloaded', label: t('Downloaded'), test: (s) => s.key === 'downloaded' },
  { id: 'downloading', label: t('Downloading'), test: (s) => s.key === 'downloading' || s.key === 'pending' || s.key === 'paused' },
  { id: 'missing', label: t('Waiting for a release'), test: (s) => s.key === 'searching' },
]

// The Ebooks or Audiobooks tab of the library: the books wanted in that
// format, and a search to add more.
export default function BookLibrary({
  format,
  books,
  queue,
  error,
  switcher,
  reload,
  admin,
}: {
  format: BookFormat
  books: Book[] | null
  queue: QueueItem[]
  error: string
  switcher: ReactNode
  reload: () => void
  admin: boolean
}) {
  // The search and filters live in the address, so Back from a book returns to the same list.
  const [filtersOpen, setFiltersOpen] = useState(false)
  const [ui, patchUi] = useUrlState({ bq: '', bf: 'all', bs: 'added' })
  const text = ui.bq
  const setText = (v: string) => patchUi({ bq: v })
  const filter = ui.bf
  const setFilter = (v: string) => patchUi({ bf: v })
  const sort = ui.bs as Sort
  const setSort = (v: Sort) => patchUi({ bs: v })
  const [adding, setAdding] = useState(false)
  const [importing, setImporting] = useState(false)
  const [folder, setFolder] = useState('')
  useEffect(() => {
    if (admin && importing) api.getSettings().then((s) => setFolder((format === 'ebook' ? s.ebooksPath : s.audiobooksPath) ?? '')).catch(() => undefined)
  }, [admin, importing, format])

  const mine = useMemo(() => (books ?? []).filter((b) => b[format].wanted || b[format].status === 'downloaded'), [books, format])
  const shown = useMemo(() => {
    if (!books) return null
    const test = FILTERS.find((f) => f.id === filter)?.test ?? (() => true)
    const needle = text.trim().toLowerCase()
    const list = mine.filter((b) => test(bookState(b, format, queue)) && (!needle || b.title.toLowerCase().includes(needle) || b.author.toLowerCase().includes(needle)))
    if (sort === 'title') list.sort((a, b) => a.title.localeCompare(b.title))
    else if (sort === 'author') list.sort((a, b) => a.author.localeCompare(b.author) || a.title.localeCompare(b.title))
    else list.sort((a, b) => b.addedAt.localeCompare(a.addedAt))
    return list
  }, [books, mine, filter, text, sort, format, queue])

  // home fork: shelves, rows that scroll sideways, by author, series…
  const shelfView = useShelfView<BookRows>(`books-${format}`, BOOK_ROWS, 'author')
  const rowsBy = format === 'ebook' && shelfView.by === 'narrator' ? 'author' : shelfView.by
  const [genres, setGenres] = useState<Record<number, string[]> | null>(null)
  useEffect(() => {
    if (!shelfView.on || rowsBy !== 'genre' || genres) return
    fetch('/api/home/book-genres', { credentials: 'include' })
      .then((r) => (r.ok ? r.json() : {}))
      .then((g: Record<number, string[]>) => setGenres(g))
      .catch(() => setGenres({}))
  }, [shelfView.on, rowsBy, genres])
  const shelves = useMemo(() => {
    if (!shown || !shelfView.on) return []
    const byYear = (a: Book, b: Book) => (a.year ?? 0) - (b.year ?? 0) || a.title.localeCompare(b.title)
    const inSeries = (a: Book, b: Book) => parseFloat(a.seriesPosition ?? '') - parseFloat(b.seriesPosition ?? '') || byYear(a, b)
    switch (rowsBy) {
      case 'series':
        return groupShelves(shown, (b) => [b.seriesName ?? ''], { prefix: 'series', min: 2, rest: t('Standalone books'), order: inSeries })
      case 'genre':
        return groupShelves(shown, (b) => (genres?.[b.id] ?? []).map((g) => GENRE_NAME[g] ?? g), { prefix: 'genre', rest: genres ? t('Genre not known yet') : undefined, order: byYear })
      case 'narrator':
        return groupShelves(shown, (b) => (b.narrators ?? '').split(',').map((n) => n.trim()), { prefix: 'narrator', min: 2, rest: t('More narrators'), order: byYear })
      case 'az':
        return letterShelves(shown, (b) => b.title)
      case 'added':
        return monthShelves(shown, (b) => b.addedAt)
      default:
        return groupShelves(shown, (b) => [b.author], { prefix: 'author', min: 2, rest: t('More authors'), order: inSeries })
    }
  }, [shown, shelfView.on, rowsBy, genres])
  const card = (b: Book) => {
    const st = b[format]
    return (
      <PosterCard
        key={b.id}
        to={`/book/${b.id}`}
        kind={format === 'audiobook' ? 'audiobook' : 'book'}
        poster={b.coverUrl}
        title={b.title}
        meta={[b.author || null, b.year ? String(b.year) : null, st.format ? st.format.toUpperCase() : null].filter(Boolean).join(' · ')}
        state={bookState(b, format, queue)}
        dim={!st.wanted && st.status !== 'downloaded'}
        actions={
          st.status === 'downloaded'
            ? [
                format === 'ebook'
                  ? { icon: 'book', label: t('Read'), onClick: () => openBookApp(readPath(b.id)) }
                  : { icon: 'play', label: t('Listen'), onClick: () => openBookApp(listenPath(b.id)) },
              ]
            : undefined
        }
      />
    )
  }

  return (
    <div>
      <div className={`toolbar lib-toolbar collapsible${filtersOpen ? ' filters-open' : ''}`}>
        {switcher}
        <input type="search" placeholder={format === 'ebook' ? t('Find in your ebooks…') : t('Find in your audiobooks…')} value={text} onChange={(e) => setText(e.target.value)} aria-label={format === 'ebook' ? t('Find in your ebooks') : t('Find in your audiobooks')} />
        <FilterToggle open={filtersOpen} onToggle={() => setFiltersOpen((o) => !o)} active={text.trim() !== '' || filter !== 'all'} />
        <div className="fold">
        <Dropdown
          label={t('Sort')}
          value={sort}
          onChange={(v) => setSort(v as Sort)}
          options={[
            { value: 'added', label: t('Recently added') },
            { value: 'title', label: t('Title') },
            { value: 'author', label: t('Author') },
          ]}
        />
        <ShelfControls view={shelfView} />
        <div className="toolbar-end">
          {admin && (
            <button className="btn-with-icon" onClick={() => setImporting((v) => !v)} aria-expanded={importing}>
              <Icon name={importing ? 'x' : 'folder'} size={16} /> {importing ? t('Close') : t('Import')}
            </button>
          )}
        </div>
        </div>
        <button className="primary btn-with-icon toolbar-add" onClick={() => setAdding((a) => !a)} aria-expanded={adding}>
          <Icon name={adding ? 'x' : 'plus'} size={16} /> {adding ? t('Close') : t('Add')}
        </button>
      </div>

      {importing && admin && <BookImport format={format} folder={folder} onDone={reload} />}
      {adding && <AddBook format={format} onAdded={reload} />}

      <div className="chip-row" style={{ marginBottom: 18, alignItems: 'center' }}>
        {FILTERS.map((f) => (
          <button key={f.id} className={`chip${filter === f.id ? ' active' : ''}`} onClick={() => setFilter(f.id)}>
            {f.label} {books ? <small>{mine.filter((b) => f.test(bookState(b, format, queue))).length}</small> : null}
          </button>
        ))}
      </div>

      {error && <p className="error-text">{error}</p>}

      {shown === null ? (
        <div className="poster-grid">
          {Array.from({ length: 12 }, (_, i) => (
            <div key={i} className="skeleton" style={{ aspectRatio: '2 / 3' }} />
          ))}
        </div>
      ) : shown.length === 0 ? (
        <div className="empty-state">
          <Icon name={format === 'ebook' ? 'book' : 'headphones'} size={44} />
          {mine.length === 0 ? (
            <>
              <p>{format === 'ebook' ? t('No ebooks yet.') : t('No audiobooks yet.')}</p>
              {!adding && (
                <button className="primary btn-with-icon" onClick={() => setAdding(true)}>
                  <Icon name="search" size={16} /> {t('Find a book to add')}
                </button>
              )}
            </>
          ) : (
            <>
              <p>{t('Nothing matches these filters.')}</p>
              <button
                className="btn-with-icon"
                onClick={() => {
                  setText('')
                  setFilter('all')
                }}
              >
                {t('Clear filters')}
              </button>
            </>
          )}
        </div>
      ) : shelfView.on ? (
        <Shelves key={`${format}-${rowsBy}`} memory={format} shelves={shelves} render={card} itemKey={(b) => String(b.id)} bar={<RowsByPick value={shelfView.by} onChange={shelfView.setBy} options={[
            { value: 'author', label: t('Author') },
            { value: 'genre', label: t('Genre') },
            { value: 'series', label: t('Series') },
            ...(format === 'audiobook' ? [{ value: 'narrator' as const, label: t('Narrator') }] : []),
            { value: 'az', label: t('A–Z') },
            { value: 'added', label: t('Recently added') },
          ]} />} />
      ) : (
        <div className="poster-grid">{shown.map(card)}</div>
      )}
    </div>
  )
}

// The search that adds a book: Open Library by title or author, then Add.
function AddBook({ format, onAdded }: { format: BookFormat; onAdded: () => void }) {
  const toast = useToast()
  const { on } = useModules()
  const other: BookFormat = format === 'ebook' ? 'audiobook' : 'ebook'
  const otherOn = on(other === 'ebook' ? 'ebooks' : 'audiobooks')
  const [q, setQ] = useState('')
  const [results, setResults] = useState<BookFound[] | null>(null)
  const [error, setError] = useState('')
  const [both, setBoth] = useState(false)
  const [asked, setAsked] = useState<ReadonlySet<string>>(new Set())
  const [busy, setBusy] = useState('')

  useEffect(() => {
    const term = q.trim()
    if (term.length < 2) {
      setResults(null)
      return
    }
    setError('')
    // A slow answer for an earlier search must not replace the one for what is typed now.
    let stale = false
    const timer = setTimeout(() => {
      api
        .searchBooks(term)
        .then((r) => !stale && setResults(r))
        .catch((e) => !stale && setError(e instanceof Error ? e.message : String(e)))
    }, 350)
    return () => {
      stale = true
      clearTimeout(timer)
    }
  }, [q])

  async function add(f: BookFound) {
    setBusy(f.key)
    try {
      const wantOther = both && otherOn
      const b = await api.addBook({
        olKey: f.key,
        title: f.title,
        author: f.author,
        authorKey: f.authorKey,
        year: f.year,
        coverId: f.coverId,
        ebook: format === 'ebook' || wantOther,
        audiobook: format === 'audiobook' || wantOther,
        searchNow: true,
      })
      if (isRequested(b)) {
        toast.success(b.message)
        setAsked((s) => new Set(s).add(f.key)) // the button stays off, so the same request isn't sent twice
        return
      }
      toast.success(t('{title} added. Mediarium is looking for it now.', { title: b.title }))
      setResults((r) => r?.map((x) => (x.key === f.key ? { ...x, libraryId: b.id } : x)) ?? r)
      onAdded()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy('')
    }
  }

  return (
    <section className="card book-add">
      <div className="book-add-head">
        <input type="search" autoFocus placeholder={t('Title or author, for example The Hobbit')} value={q} onChange={(e) => setQ(e.target.value)} aria-label={t('Find a book')} />
        {otherOn && (
          <label className="check-inline">
            <input type="checkbox" checked={both} onChange={(e) => setBoth(e.target.checked)} /> {other === 'ebook' ? t('Get the ebook too') : t('Get the audiobook too')}
          </label>
        )}
      </div>
      {error && <p className="error-text">{error}</p>}
      {results !== null && results.length === 0 && <p className="hint">{t("No books found. Try the author's name, or fewer words.")}</p>}
      {results && results.length > 0 && (
        <div className="book-results">
          {results.map((f) => (
            <div key={f.key} className="book-result">
              <span className="book-thumb">{f.coverUrl ? <img src={f.coverUrl.replace('-M.jpg', '-S.jpg')} alt="" loading="lazy" /> : <PosterFallback />}</span>
              <span className="book-result-text">
                <strong>{f.title}</strong>
                <small>{[f.author, f.year].filter(Boolean).join(' · ')}</small>
              </span>
              {f.libraryId ? (
                <Link className="btn-sm" to={`/book/${f.libraryId}`}>
                  {t('In your library')}
                </Link>
              ) : asked.has(f.key) ? (
                <span className="btn-sm">{t('Requested')}</span>
              ) : (
                <button className="btn-sm primary" disabled={busy !== ''} onClick={() => void add(f)}>
                  {busy === f.key ? t('Adding…') : t('Add')}
                </button>
              )}
            </div>
          ))}
        </div>
      )}
      <p className="hint">{t('Book details and covers come from Open Library.')}</p>
    </section>
  )
}
