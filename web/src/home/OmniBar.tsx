// Home fork: the one bar for search and AI, at the top of the Dashboard,
// Discover, the Library and each media page (and in the header). The switch
// on its left turns it between plain Search (the usual live search) and Ask
// the AI. The AI works out what is wanted and opens that feature right here:
// find titles, Tonight, Pick for the Room, Catch me up, Skip guide, What's it
// like?, books, read-alikes, comic reading orders, playlists, artists like
// one, why downloads failed, or a short answer. Nothing changes in the
// library without a tap.
import { useEffect, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import type { DiscoverMovie } from '../api'
import AddDialog, { type AddTarget } from '../components/AddDialog'
import DiscoverRail from '../components/DiscoverRail'
import Icon from '../components/Icon'
import SearchBox from '../components/SearchBox'
import { useToast } from '../components/Toast'
import { t } from '../i18n'
import { useOwned } from '../useOwned'
import { BookAIButtons, BookList } from './AIBooks'
import { SkipGuide, WhatsItLike } from './AIGuides'
import { MoreLikeArtist } from './AIMusic'
import { askAI, ScrollText, useAIOn } from './aiShared'
import CatchUp, { ComicCatchUpDialog } from './CatchUp'
import { ComicSteps, type Guide } from './ComicGuide'
import RoomPick from './RoomPick'
import Tonight from './Tonight'
import './news.css'

// Where the bar is: it shapes the examples and what the AI expects.
export type OmniPage = 'dashboard' | 'movie' | 'tv' | 'anime' | 'ebook' | 'audiobook' | 'music' | 'comics'

const MODE_KEY = 'omnibar-mode'

function examples(page: OmniPage): string[] {
  switch (page) {
    case 'movie':
      return [t('A 90s heist movie, not too violent'), t('What’s Dune: Part Two like?'), t('Every Denis Villeneuve film')]
    case 'tv':
      return [t('Catch me up on The Bear'), t('What can I skip in Lost?'), t('Shows like Severance')]
    case 'anime':
      return [t('Anime like Cowboy Bebop'), t('Which Naruto episodes are filler?'), t('Catch me up on Frieren')]
    case 'ebook':
      return [t('A cozy mystery set in Scotland'), t('Books like Project Hail Mary'), t('Catch me up on The Expanse')]
    case 'audiobook':
      return [t('A sci-fi audiobook with a great narrator'), t('A short, funny audiobook for a road trip'), t('Audiobooks like The Martian')]
    case 'music':
      return [t('A playlist for a rainy Sunday'), t('Artists like Portishead'), t('Music for a dinner party')]
    case 'comics':
      return [t('Where do I start with Daredevil?'), t('What do I read before Secret Wars?'), t('Catch me up on Saga')]
    default:
      return [t('What should I watch tonight?'), t('Pick a film for the three of us'), t('Make a playlist for cooking')]
  }
}

function placeholderFor(page: OmniPage, ai: boolean): string {
  if (!ai) {
    switch (page) {
      case 'comics':
        return t('Search comics')
      case 'ebook':
      case 'audiobook':
        return t('Search books')
      case 'music':
        return t('Search artists')
      default:
        return t('Search movies, shows, artists and books')
    }
  }
  switch (page) {
    case 'movie':
      return t('Ask about films: find one, what it’s like, add it…')
    case 'tv':
    case 'anime':
      return t('Ask about shows: find one, catch up, what to skip…')
    case 'ebook':
    case 'audiobook':
      return t('Ask about books: find one, read-alikes, catch up…')
    case 'music':
      return t('Ask about music: a playlist, artists like one…')
    case 'comics':
      return t('Ask about comics: where to start, reading orders, catch up…')
    default:
      return t('Ask Mediarium anything…')
  }
}

interface Reply {
  action: string
  query: string
  title: string
  type: string
  year: number
  season: number
  episode: number
  issue: string
  mood: string
  answer: string
  tmdbId?: number
  kind?: 'movie' | 'tv'
  posterUrl?: string
  bookId?: number
  artistId?: number
  seriesId?: number
  note?: string
}

export default function OmniBar({ page, header }: { page: OmniPage; header?: boolean }) {
  const aiOn = useAIOn()
  const [mode, setModeState] = useState<'search' | 'ai'>(() => {
    try {
      return localStorage.getItem(MODE_KEY) === 'search' ? 'search' : 'ai'
    } catch {
      return 'ai'
    }
  })
  const ai = aiOn && mode === 'ai'
  const setMode = (m: 'search' | 'ai') => {
    setModeState(m)
    try {
      localStorage.setItem(MODE_KEY, m)
    } catch {
      // remembering is optional
    }
  }
  const [q, setQ] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [reply, setReply] = useState<Reply | null>(null)
  const navigate = useNavigate()

  const toggle = aiOn ? (
    <button type="button" className={`omni-switch${ai ? ' on' : ''}`} onClick={() => setMode(ai ? 'search' : 'ai')} title={ai ? t('Switch to plain search') : t('Switch to Ask the AI')} aria-label={ai ? t('Switch to plain search') : t('Switch to Ask the AI')} aria-pressed={ai}>
      <Icon name={ai ? 'sparkles' : 'search'} size={16} />
    </button>
  ) : null

  const ask = async (text: string) => {
    if (!text.trim()) return
    setBusy(true)
    setError('')
    setReply(null)
    try {
      const r = await askAI<Reply>('/api/home/ai/ask', { query: text, page })
      if (r.action === 'search') {
        navigate(page === 'comics' ? `/comics?tab=add&q=${encodeURIComponent(r.query)}` : `/search?q=${encodeURIComponent(r.query)}`)
        return
      }
      setReply(r)
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  if (!ai) {
    // Plain search: the usual live search (comics search their own catalogue).
    if (page === 'comics') {
      return (
        <div className={`omni${header ? ' omni-header' : ''}`}>
          <form
            className="omni-bar"
            onSubmit={(e) => {
              e.preventDefault()
              if (q.trim()) navigate(`/comics?tab=add&q=${encodeURIComponent(q.trim())}`)
            }}
          >
            {toggle}
            <input value={q} onChange={(e) => setQ(e.target.value)} placeholder={placeholderFor(page, false)} aria-label={placeholderFor(page, false)} type="search" />
            <button className="primary btn-sm" type="submit" disabled={!q.trim()}>
              {t('Search')}
            </button>
          </form>
        </div>
      )
    }
    // The same bar as Ask: only the icon and the button change.
    return (
      <div className={`omni${header ? ' omni-header' : ''}`}>
        <div className="omni-bar search">
          {toggle}
          <SearchBox placeholderText={placeholderFor(page, false)} noShortcut={!header} onQuery={setQ} />
          <button className="primary btn-sm" type="button" disabled={!q.trim()} onClick={() => navigate(`/search?q=${encodeURIComponent(q.trim())}`)}>
            {t('Search')}
          </button>
        </div>
      </div>
    )
  }

  return (
    <div className={`omni${header ? ' omni-header' : ''}`}>
      <form
        className="omni-bar ai"
        data-walk="ask-bar"
        onSubmit={(e) => {
          e.preventDefault()
          void ask(q)
        }}
      >
        {toggle}
        <input value={q} onChange={(e) => setQ(e.target.value)} placeholder={placeholderFor(page, true)} aria-label={placeholderFor(page, true)} maxLength={500} />
        {q && !busy && (
          <button
            type="button"
            className="icon-btn"
            onClick={() => {
              setQ('')
              setReply(null)
              setError('')
            }}
            aria-label={t('Clear')}
          >
            <Icon name="x" size={14} />
          </button>
        )}
        <button className="primary btn-sm" type="submit" disabled={busy || !q.trim()}>
          {busy ? t('Thinking…') : t('Ask')}
        </button>
      </form>
      {!reply && !busy && !error && !header && (
        <div className="omni-examples">
          {examples(page).map((ex) => (
            <button
              key={ex}
              type="button"
              className="room-chip"
              onClick={() => {
                setQ(ex)
                void ask(ex)
              }}
            >
              {ex}
            </button>
          ))}
        </div>
      )}
      {error && <p className="error-text">{error}</p>}
      {reply && (
        <div className="omni-result">
          <ReplyView reply={reply} />
          <button
            className="btn-sm"
            onClick={() => {
              setReply(null)
              setQ('')
            }}
          >
            {t('Clear')}
          </button>
        </div>
      )}
    </div>
  )
}

// ReplyView opens the feature the AI chose.
function ReplyView({ reply: r }: { reply: Reply }) {
  if (r.note) return <p className="hint ai-note">{r.note}</p>
  switch (r.action) {
    case 'titles':
      return <TitlesResult query={r.query} />
    case 'room':
      return <RoomResult />
    case 'tonight':
      return <Tonight autoAsk />
    case 'recap':
      return (
        <p className="hint">
          {t('Catching you up on {title}…', { title: r.title })} <CatchUp tmdbId={r.tmdbId ?? 0} autoOpen season={r.season || undefined} episode={r.episode || undefined} />
        </p>
      )
    case 'skip':
      return <SkipGuide tmdbId={r.tmdbId ?? 0} title={r.title} autoOpen />
    case 'vibe':
      return <WhatsItLike kind={r.kind ?? 'movie'} tmdbId={r.tmdbId ?? 0} title={r.title} autoOpen />
    case 'add':
      return <AddResult reply={r} />
    case 'books':
      return <BooksResult query={r.query} />
    case 'read_alikes':
      return <BookAIButtons bookId={r.bookId ?? 0} title={r.title} inSeries={false} autoOpen="alike" />
    case 'book_recap':
      return <BookAIButtons bookId={r.bookId ?? 0} title={r.title} inSeries autoOpen="recap" />
    case 'comic_guide':
      return <ComicGuideResult question={r.query} />
    case 'comic_recap':
      return <ComicRecapResult seriesId={r.seriesId ?? 0} issue={r.issue} />
    case 'playlist':
      return <PlaylistResult mood={r.mood} />
    case 'similar_artists':
      return <MoreLikeArtist artistId={r.artistId ?? 0} name={r.title} autoOpen />
    case 'failures':
      return (
        <p className="recap-text">
          {t('Failed downloads are listed under Problems in Activity. Each one has an “Explain this” button that says what went wrong and what to try.')} <Link to="/queue#act-problems">{t('Open Activity')}</Link>
        </p>
      )
    default:
      return (
        <ScrollText className="recap-text">{r.answer}</ScrollText>
      )
  }
}

// useRun runs one AI request when the result first shows.
function useRun<T>(p: () => Promise<T>) {
  const [data, setData] = useState<T | null>(null)
  const [error, setError] = useState('')
  useEffect(() => {
    let live = true
    p()
      .then((d) => live && setData(d))
      .catch((e: Error) => live && setError(e.message))
    return () => {
      live = false
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])
  return { data, error }
}

function TitlesResult({ query }: { query: string }) {
  const owned = useOwned()
  const [adding, setAdding] = useState<AddTarget | null>(null)
  const run = useRun<{ note: string; movies: DiscoverMovie[]; shows: DiscoverMovie[] }>(() => askAI('/api/home/ai/search', { query, image: '' }))
  if (run.error) return <p className="error-text">{run.error}</p>
  if (!run.data) return <p className="hint">{t('Thinking…')}</p>
  return (
    <>
      {run.data.note && <p className="hint ai-note">{run.data.note}</p>}
      {run.data.movies.length > 0 && <DiscoverRail title={t('Movies')} items={run.data.movies} kind="movie" owned={owned} onAdd={setAdding} rows={1} keepOwned scroll />}
      {run.data.shows.length > 0 && <DiscoverRail title={t('TV shows')} items={run.data.shows} kind="tv" owned={owned} onAdd={setAdding} rows={1} keepOwned scroll />}
      {adding && <AddDialog target={adding} onClose={() => setAdding(null)} onAdded={() => setAdding(null)} />}
    </>
  )
}

function RoomResult() {
  const owned = useOwned()
  const [adding, setAdding] = useState<AddTarget | null>(null)
  return (
    <>
      <RoomPick owned={owned} onAdd={setAdding} />
      {adding && <AddDialog target={adding} onClose={() => setAdding(null)} onAdded={() => setAdding(null)} />}
    </>
  )
}

function AddResult({ reply: r }: { reply: Reply }) {
  const [adding, setAdding] = useState(false)
  if (!r.tmdbId || !r.kind) return <p className="hint">{t('I couldn’t find that title. Try its full name.')}</p>
  const to = r.kind === 'movie' ? `/title/${r.tmdbId}` : `/show/${r.tmdbId}`
  return (
    <div className="omni-add">
      {r.posterUrl && <img src={r.posterUrl} alt="" />}
      <div>
        <b>
          {r.title} {r.year ? <span className="hint">({r.year})</span> : null}
        </b>
        <div className="omni-add-actions">
          <button className="primary btn-sm btn-with-icon" onClick={() => setAdding(true)}>
            <Icon name="plus" size={14} /> {t('Add to library')}
          </button>
          <Link to={to} className="btn-sm">
            {t('Open')}
          </Link>
        </div>
      </div>
      {adding && <AddDialog target={{ kind: r.kind, tmdbId: r.tmdbId, title: r.title, year: r.year, posterUrl: r.posterUrl }} onClose={() => setAdding(false)} onAdded={() => setAdding(false)} />}
    </div>
  )
}

function BooksResult({ query }: { query: string }) {
  const run = useRun<{ note: string; books: Parameters<typeof BookList>[0]['books'] }>(() => askAI('/api/home/ai/book-search', { query }))
  if (run.error) return <p className="error-text">{run.error}</p>
  if (!run.data) return <p className="hint">{t('Thinking…')}</p>
  return (
    <>
      {run.data.note && <p className="hint ai-note">{run.data.note}</p>}
      <BookList books={run.data.books} />
    </>
  )
}

function ComicGuideResult({ question }: { question: string }) {
  const run = useRun<Guide>(() => askAI('/api/home/ai/comic-guide', { question }))
  if (run.error) return <p className="error-text">{run.error}</p>
  if (!run.data) return <p className="hint">{t('Thinking…')}</p>
  return (
    <>
      {run.data.note && <p className="hint ai-note">{run.data.note}</p>}
      <ComicSteps guide={run.data} />
    </>
  )
}

function ComicRecapResult({ seriesId, issue }: { seriesId: number; issue: string }) {
  const [open, setOpen] = useState(true)
  return (
    <>
      <button className="btn-sm btn-with-icon" onClick={() => setOpen(true)}>
        <Icon name="sparkles" size={14} /> {t('Catch me up')}
      </button>
      {open && <ComicCatchUpDialog seriesId={seriesId} lastRead={issue} onClose={() => setOpen(false)} />}
    </>
  )
}

function PlaylistResult({ mood }: { mood: string }) {
  const navigate = useNavigate()
  const toast = useToast()
  const [busy, setBusy] = useState(false)
  const make = async () => {
    setBusy(true)
    try {
      const r = await askAI<{ id: number; name: string; note: string }>('/api/home/ai/playlist', { mood, shared: false })
      toast.success(r.note ? `${r.name}: ${r.note}` : r.name)
      navigate(`/music/playlists/${r.id}`)
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
      setBusy(false)
    }
  }
  return (
    <div className="omni-confirm">
      <p className="recap-text">{t('Make a playlist from your music for “{mood}”?', { mood })}</p>
      <button className="primary btn-sm btn-with-icon" onClick={() => void make()} disabled={busy}>
        <Icon name="sparkles" size={14} /> {busy ? t('Picking tracks…') : t('Make it')}
      </button>
    </div>
  )
}
