import { BRAND } from '../brand' // Usherr
import { useEffect, useMemo, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { api, type Book, type BookFormat, type BookProgress } from '../api'
import BrandMark from '../components/BrandMark'
import Icon from '../components/Icon'
import { PosterFallback } from '../components/PosterCard'
import { useDocumentTitle } from '../documentTitle'
import { t, tc, tx } from '../i18n'
import { listenPath, readPath } from './open'
import { KeepButton, OnDevice, bookCount, ebookKind, useKept, useOnline } from './Keep'
import { keptId } from './offline'
import { withBase } from '../base'
import { useModules } from '../ModulesContext'
import { looseIncludes } from '../looseText'

type Tab = 'all' | BookFormat

// The shelf: what you were reading or listening to last, then every book
// that is downloaded, with Read and Listen.
export default function Shelf() {
  useDocumentTitle(t('Mediarium Books'))
  const [books, setBooks] = useState<Book[] | null>(null)
  const [progress, setProgress] = useState<BookProgress[]>([])
  const [error, setError] = useState('')
  // The menu in the header opens the shelf on the ebooks or the audiobooks.
  const [params] = useSearchParams()
  const [tab, setTab] = useState<Tab>(() => (params.get('tab') === 'ebook' ? 'ebook' : params.get('tab') === 'audiobook' ? 'audiobook' : 'all'))
  const [q, setQ] = useState('')
  const [kept] = useKept()
  const online = useOnline()
  const keptIds = new Set(kept.map((k) => k.id))
  // With no connection only the books kept on this device can open.
  const usable = (b: Book, f: BookFormat) => b[f].status === 'downloaded' && (online || keptIds.has(f === 'audiobook' ? keptId('audiobook', b.id) : `book:${b.id}:ebook`))

  // Books are only asked for while ebooks or audiobooks are switched on.
  const { on, loaded } = useModules()
  const booksOff = loaded && !on('ebooks') && !on('audiobooks')
  useEffect(() => {
    if (booksOff) return
    api.listBooks().then(setBooks).catch((e) => setError(e instanceof Error ? e.message : String(e)))
    api.listBookProgress().then(setProgress).catch(() => undefined)
  }, [booksOff])

  const byId = useMemo(() => new Map((books ?? []).map((b) => [b.id, b])), [books])
  const progressOf = (id: number, f: BookFormat) => progress.find((p) => p.bookId === id && p.format === f)
  const continuing = progress.filter((p) => !p.finished && byId.has(p.bookId) && usable(byId.get(p.bookId)!, p.format) && (tab === 'all' || tab === p.format)).slice(0, 6)
  const needle = q.trim().toLowerCase()
  const shelf = (books ?? []).filter(
    (b) =>
      (tab === 'all' ? usable(b, 'ebook') || usable(b, 'audiobook') : usable(b, tab)) &&
      (!needle || looseIncludes(b.title, needle) || looseIncludes(b.author, needle)),
  )
  shelf.sort((a, b) => a.title.localeCompare(b.title))

  return (
    <div className="bks-shelf">
      <header className="bks-top">
        <span className="bks-brand">
          <BrandMark className="" /> {tx('{brand} Books', { brand: <strong>{BRAND}</strong> })}
        </span>
        <div className="seg">
          {(['all', 'ebook', 'audiobook'] as Tab[]).map((x) => (
            <button key={x} aria-pressed={tab === x} className={tab === x ? 'active' : ''} onClick={() => setTab(x)}>
              {x === 'all' ? t('All') : x === 'ebook' ? t('Reading') : t('Listening')}
            </button>
          ))}
        </div>
        <input type="search" placeholder={t('Find a book')} value={q} onChange={(e) => setQ(e.target.value)} aria-label={t('Find a book')} />
        <a className="bks-back" href={withBase('/library?kind=ebook')} target="_blank" rel="noreferrer" title={t('Open Mediarium to add more books')}>
          <Icon name="plus" size={15} /> {t('Get more books')}
        </a>
      </header>

      {!online && <p className="bks-offline">{t("You're offline. The books kept on this device still open, and your place is sent once you're back.")}</p>}
      {error && <p className="error-text">{error}</p>}

      {continuing.length > 0 && !needle && (
        <section className="bks-section">
          <h2>{tc('reading', 'Continue')}</h2>
          <div className="bks-continue">
            {continuing.map((p) => {
              const b = byId.get(p.bookId)!
              return (
                <Link key={`${p.bookId}-${p.format}`} className="bks-cont" to={p.format === 'ebook' ? readPath(b.id) : listenPath(b.id)}>
                  <span className="bks-cont-cover">{b.coverUrl ? <img src={b.coverUrl} alt="" /> : <PosterFallback />}</span>
                  <span className="bks-cont-text">
                    <strong>{b.title}</strong>
                    <small>
                      <Icon name={p.format === 'ebook' ? 'book' : 'headphones'} size={12} /> {p.format === 'ebook' ? t('Reading') : t('Listening')} · {Math.round(p.percent)}%
                    </small>
                    <span className="bks-bar">
                      <span style={{ width: `${Math.min(100, p.percent)}%` }} />
                    </span>
                  </span>
                </Link>
              )
            })}
          </div>
        </section>
      )}

      <OnDevice kept={kept} />

      <section className="bks-section">
        <h2>{tab === 'ebook' ? t('Ebooks') : tab === 'audiobook' ? t('Audiobooks') : t('Your books')}</h2>
        {books === null ? (
          <div className="bks-grid">
            {Array.from({ length: 8 }, (_, i) => (
              <div key={i} className="skeleton" style={{ aspectRatio: '2 / 3' }} />
            ))}
          </div>
        ) : shelf.length === 0 ? (
          <div className="empty-state">
            <Icon name="book" size={44} />
            <p>{needle ? t('No book matches that.') : t('No books downloaded yet. Add some in Mediarium and they appear here once they are downloaded.')}</p>
          </div>
        ) : (
          <div className="bks-grid">
            {shelf.map((b) => {
              const read = usable(b, 'ebook') && tab !== 'audiobook'
              const listen = usable(b, 'audiobook') && tab !== 'ebook'
              // The button keeps what this tab shows: the ebook when it can be read here, else the audiobook.
              const ek = ebookKind(b.ebook.format)
              const keepKind = read && ek ? ek : listen ? 'audiobook' : ''
              const pe = progressOf(b.id, 'ebook')
              const pa = progressOf(b.id, 'audiobook')
              const pct = Math.max(read ? (pe?.percent ?? 0) : 0, listen ? (pa?.percent ?? 0) : 0)
              return (
                <div key={b.id} className="bks-book">
                  <Link className="bks-cover" to={read ? readPath(b.id) : listenPath(b.id)} title={b.title}>
                    {b.coverUrl ? <img src={b.coverUrl} alt="" loading="lazy" /> : <PosterFallback />}
                    {pct > 0 && (
                      <span className="bks-bar on-cover">
                        <span style={{ width: `${Math.min(100, pct)}%` }} />
                      </span>
                    )}
                  </Link>
                  <strong className="bks-title">{b.title}</strong>
                  <small className="bks-author">{b.author}</small>
                  <div className="bks-actions">
                    {read && (
                      <Link className="btn-sm primary btn-with-icon" to={readPath(b.id)}>
                        <Icon name="book" size={14} /> {t('Read')}
                      </Link>
                    )}
                    {listen && (
                      <Link className="btn-sm btn-with-icon" to={listenPath(b.id)}>
                        <Icon name="headphones" size={14} /> {t('Listen')}
                      </Link>
                    )}
                    {keepKind && (
                      <KeepButton
                        kind={keepKind}
                        id={b.id}
                        title={b.title}
                        subtitle={keepKind === 'audiobook' ? t('Audiobook') : b.author}
                        cover={b.coverUrl}
                        open={keepKind === 'audiobook' ? listenPath(b.id) : readPath(b.id)}
                        kept={keptIds.has(keptId(keepKind, b.id))}
                        count={bookCount(b, keepKind)}
                        compact
                      />
                    )}
                  </div>
                </div>
              )
            })}
          </div>
        )}
      </section>
    </div>
  )
}
