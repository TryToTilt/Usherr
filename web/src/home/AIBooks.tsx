// Home fork: AI for books and audiobooks.
//   - BookAISearch: books from a description (on the Books page).
//   - BookAIButtons: Read-alikes and Catch me up (on a book's page).
import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import Icon from '../components/Icon'
import { t } from '../i18n'
import { AIButton, AIDialog, askAI, ScrollText, useAIOn, useAsk } from './aiShared'

interface AIBook {
  title: string
  author?: string
  year?: number
  why?: string
  have: boolean
  to: string
}

export function BookList({ books }: { books: AIBook[] }) {
  return (
    <ul className="ai-list ai-cards">
      {books.map((b) => (
        <li key={b.title + (b.author ?? '')}>
          <Link to={b.to} className={`news-chip ${b.have ? 'have' : 'find'}`}>
            <Icon name={b.have ? 'open' : 'search'} size={12} /> {b.title}
          </Link>{' '}
          {b.author && <span className="hint">{t('by {author}', { author: b.author })}</span>}
          {b.year ? <span className="hint"> ({b.year})</span> : null}
          {b.why && <div className="ai-why">{b.why}</div>}
        </li>
      ))}
    </ul>
  )
}

export function BookAISearch() {
  const on = useAIOn()
  const [q, setQ] = useState('')
  const ask = useAsk<{ note: string; books: AIBook[] }>()
  if (!on) return null
  return (
    <section className="ai-search">
      <form
        className="ai-search-bar"
        onSubmit={(e) => {
          e.preventDefault()
          if (q.trim()) void ask.run(askAI('/api/home/ai/book-search', { query: q }))
        }}
      >
        <Icon name="sparkles" size={18} />
        <input value={q} onChange={(e) => setQ(e.target.value)} placeholder={t('Ask the AI: “a cozy mystery set in Scotland”, “an audiobook narrated like Ray Porter”…')} aria-label={t('Describe the book you want')} />
        <button className="primary btn-sm" type="submit" disabled={ask.busy || !q.trim()}>
          {ask.busy ? t('Thinking…') : t('Ask')}
        </button>
      </form>
      {ask.error && <p className="error-text">{ask.error}</p>}
      {ask.data && (
        <div className="ai-search-results">
          {ask.data.note && <p className="hint ai-note">{ask.data.note}</p>}
          <BookList books={ask.data.books} />
          <button className="btn-sm" onClick={() => ask.setData(null)}>
            {t('Clear')}
          </button>
        </div>
      )}
    </section>
  )
}

export function BookAIButtons({ bookId, title, inSeries, autoOpen }: { bookId: number; title: string; inSeries: boolean; autoOpen?: 'alike' | 'recap' }) {
  const on = useAIOn()
  const [open, setOpen] = useState<'' | 'alike' | 'recap'>('')
  const alike = useAsk<{ note: string; have: AIBook[]; discover: AIBook[] }>()
  const recap = useAsk<{ title: string; series: string; position: string; recap: string }>()
  useEffect(() => {
    if (!autoOpen || !on || !bookId) return
    setOpen(autoOpen)
    if (autoOpen === 'alike') void alike.run(askAI('/api/home/ai/read-alikes', { bookId }))
    else void recap.run(askAI('/api/home/ai/book-recap', { bookId }))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [autoOpen, on, bookId])
  if (!on) return null
  return (
    <>
      <AIButton
        label={t('Read-alikes')}
        title={t('Books on your shelves and new ones that go with this one')}
        onClick={() => {
          setOpen('alike')
          if (!alike.data) void alike.run(askAI('/api/home/ai/read-alikes', { bookId }))
        }}
      />
      <AIButton
        label={t('Catch me up')}
        title={inSeries ? t('The story so far, up to the end of this book, before the next one') : t('A spoiler-free recap of this book')}
        onClick={() => {
          setOpen('recap')
          if (!recap.data) void recap.run(askAI('/api/home/ai/book-recap', { bookId }))
        }}
      />
      {open === 'alike' && (
        <AIDialog title={t('If you liked {title}', { title })} onClose={() => setOpen('')}>
          {alike.busy && <p className="hint">{t('Thinking…')}</p>}
          {alike.error && <p className="error-text">{alike.error}</p>}
          {alike.data && (
            <>
              {alike.data.note && <p className="recap-text">{alike.data.note}</p>}
              {alike.data.have.length > 0 && (
                <>
                  <h3 className="ai-sub">{t('On your shelves')}</h3>
                  <BookList books={alike.data.have} />
                </>
              )}
              {alike.data.discover.length > 0 && (
                <>
                  <h3 className="ai-sub">{t('New to you')}</h3>
                  <BookList books={alike.data.discover} />
                </>
              )}
            </>
          )}
        </AIDialog>
      )}
      {open === 'recap' && (
        <AIDialog
          title={t('Catch me up: {title}', { title: recap.data?.series || title })}
          subtitle={recap.data?.series ? t('Up to the end of book {n}, {title}', { n: recap.data.position || '?', title: recap.data.title }) : undefined}
          onClose={() => setOpen('')}
        >
          {recap.busy && <p className="hint">{t('Writing your recap…')}</p>}
          {recap.error && <p className="error-text">{recap.error}</p>}
          {recap.data?.recap && <ScrollText className="recap-text tall">{recap.data.recap}</ScrollText>}
          <p className="hint">{t('Written by AI. It stops at the end of this book.')}</p>
        </AIDialog>
      )}
    </>
  )
}
