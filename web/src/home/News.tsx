// Home fork: entertainment news on the Dashboard. Bite-sized stories from the
// news feeds set in Settings > Extras, each summarised by the AI with a link to
// the full article. Films, shows, books, comics and music a story names link to
// the library, or offer to add them.
// Usherr: on the Dashboard it is one line at the top, a headline that changes
// every few seconds or a ticker (each person picks, or turns it off); "All
// news" opens the full list.
import { useEffect, useRef, useState, useSyncExternalStore } from 'react'
import { createPortal } from 'react-dom'
import { Link, useNavigate } from 'react-router-dom'
import { useFocusTrap } from '../useFocusTrap'
import AddDialog, { type AddTarget } from '../components/AddDialog'
import Icon from '../components/Icon'
import { timeAgo } from '../format'
import { t } from '../i18n'
import './news.css'

interface Mention {
  title: string
  year?: number
  type: 'movie' | 'tv' | 'book' | 'comic' | 'album' | 'artist'
  creator?: string
  tmdbId?: number
  posterUrl?: string
  have: boolean
  to: string
  add: boolean
}

interface Story {
  id: number
  source: string
  category: string
  title: string
  link: string
  publishedAt: string
  summary: string
  mentions: Mention[]
}

const CAT_KEY = 'news-category'
const PAGE = 8

// Usherr: how the Dashboard shows the news, kept in this browser.
export type NewsStyle = 'rotate' | 'ticker' | 'off'
const STYLE_KEY = 'usherr-news-style'
const styleListeners = new Set<() => void>()
function readStyle(): NewsStyle {
  try {
    const v = localStorage.getItem(STYLE_KEY)
    return v === 'ticker' || v === 'off' ? v : 'rotate'
  } catch {
    return 'rotate'
  }
}
let newsStyle = readStyle()
export function setNewsStyle(v: NewsStyle): void {
  newsStyle = v
  try {
    localStorage.setItem(STYLE_KEY, v)
  } catch {
    // remembering is optional
  }
  styleListeners.forEach((fn) => fn())
}
export function useNewsStyle(): NewsStyle {
  return useSyncExternalStore(
    (fn) => {
      styleListeners.add(fn)
      return () => styleListeners.delete(fn)
    },
    () => newsStyle,
  )
}
const NEWS_STYLES: { key: NewsStyle; label: string }[] = [
  { key: 'rotate', label: t('One headline at a time') },
  { key: 'ticker', label: t('Ticker') },
  { key: 'off', label: t('Off') },
]

// The choice, as a row of buttons (in All news and in Settings > General).
export function NewsStyleChoice() {
  const style = useNewsStyle()
  return (
    <div className="seg news-style" role="group" aria-label={t('News on the Dashboard')}>
      {NEWS_STYLES.map((o) => (
        <button key={o.key} type="button" className={style === o.key ? 'active' : ''} aria-pressed={style === o.key} onClick={() => setNewsStyle(o.key)}>
          {o.label}
        </button>
      ))}
    </div>
  )
}

function categoryName(c: string): string {
  switch (c) {
    case 'film-tv':
      return t('Film & TV')
    case 'home-theater':
      return t('Home theater & tech')
    case 'books':
      return t('Books')
    case 'comics':
      return t('Comics')
    case 'music':
      return t('Music')
    default:
      return t('All')
  }
}

// safeLink keeps only web links from a feed.
function safeLink(u: string): string | undefined {
  try {
    const p = new URL(u)
    return p.protocol === 'https:' || p.protocol === 'http:' ? p.href : undefined
  } catch {
    return undefined
  }
}

const ROTATE_MS = 8000

export default function News() {
  const style = useNewsStyle()
  const [stories, setStories] = useState<Story[] | null>(null)
  const [cats, setCats] = useState<string[]>([])
  // undefined: closed; null: open on the list; a number: open on that story
  const [open, setOpen] = useState<number | null | undefined>(undefined)
  useEffect(() => {
    if (style === 'off') return
    fetch('/api/home/news', { credentials: 'include' })
      .then((r) => (r.ok ? r.json() : null))
      .then((d: { on: boolean; items: Story[]; categories?: string[] } | null) => {
        if (!d?.on) return
        setStories(d.items)
        setCats(d.categories ?? [])
      })
      .catch(() => undefined)
  }, [style])
  if (style === 'off' || !stories || stories.length === 0) return null
  return (
    <>
      <NewsLine stories={stories} style={style} onOpen={(id) => setOpen(id)} />
      {open !== undefined && <NewsDialog stories={stories} cats={cats} openFirst={open} onClose={() => setOpen(undefined)} />}
    </>
  )
}

const reducedMotion = () => {
  try {
    return window.matchMedia('(prefers-reduced-motion: reduce)').matches
  } catch {
    return false
  }
}

// The line at the top of the Dashboard.
function NewsLine({ stories, style, onOpen }: { stories: Story[]; style: NewsStyle; onOpen: (id: number | null) => void }) {
  const ticker = style === 'ticker' && !reducedMotion()
  const [i, setI] = useState(0)
  const [paused, setPaused] = useState(false)
  useEffect(() => {
    if (ticker || paused || stories.length < 2) return
    const id = window.setInterval(() => setI((n) => (n + 1) % stories.length), ROTATE_MS)
    return () => window.clearInterval(id)
  }, [ticker, paused, stories.length])
  const top = stories.slice(0, 20)
  const cur = stories[i % stories.length]
  // A ticker moves at about the same speed however many headlines it has.
  const chars = top.reduce((n, s) => n + s.title.length + 6, 0)
  return (
    <div className={`news-line${ticker ? ' ticker' : ''}`} onMouseEnter={() => setPaused(true)} onMouseLeave={() => setPaused(false)} onFocus={() => setPaused(true)} onBlur={() => setPaused(false)}>
      <span className="news-line-tag">
        <Icon name="globe" size={14} /> <span className="news-line-label">{t('News')}</span>
      </span>
      <div className="news-line-track" aria-live={ticker ? 'off' : 'polite'}>
        {ticker ? (
          <div className="news-line-crawl" style={{ animationDuration: `${Math.max(30, Math.round(chars / 7))}s` }}>
            {[0, 1].map((copy) => (
              <span key={copy} className="news-line-run" aria-hidden={copy === 1}>
                {top.map((s) => (
                  <button key={s.id} type="button" className="news-line-item" tabIndex={copy === 1 ? -1 : 0} onClick={() => onOpen(s.id)}>
                    {s.title}
                    <span className="news-line-dot" aria-hidden="true">
                      •
                    </span>
                  </button>
                ))}
              </span>
            ))}
          </div>
        ) : (
          <button key={cur.id} type="button" className="news-line-item fade" onClick={() => onOpen(cur.id)} title={`${cur.source} · ${timeAgo(cur.publishedAt)}`}>
            {cur.title}
          </button>
        )}
      </div>
      <button type="button" className="news-line-all" onClick={() => onOpen(null)}>
        {t('All news')}
      </button>
    </div>
  )
}

// All news: the full list, with its topics and the choice of how the line looks.
function NewsDialog({ stories, cats, openFirst, onClose }: { stories: Story[]; cats: string[]; openFirst: number | null; onClose: () => void }) {
  const box = useRef<HTMLDivElement>(null)
  useFocusTrap(box)
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && onClose()
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [onClose])
  return createPortal(
    <div className="modal-backdrop" onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      <div ref={box} tabIndex={-1} className="modal news-modal" role="dialog" aria-modal="true" aria-label={t('Entertainment news')}>
        <NewsList stories={stories} cats={cats} openFirst={openFirst} onClose={onClose} />
        <div className="news-modal-foot">
          <span className="hint">{t('On the Dashboard:')}</span>
          <NewsStyleChoice />
        </div>
      </div>
    </div>,
    document.body,
  )
}

function NewsList({ stories, cats, openFirst, onClose }: { stories: Story[]; cats: string[]; openFirst: number | null; onClose: () => void }) {
  const navigate = useNavigate()
  const [cat, setCatState] = useState(() => {
    if (openFirst !== null) return '' // a headline was tapped: show it whatever the topic
    try {
      return localStorage.getItem(CAT_KEY) ?? ''
    } catch {
      return ''
    }
  })
  const [shown, setShown] = useState(() => Math.max(PAGE, stories.findIndex((s) => s.id === openFirst) + 1))
  const [adding, setAdding] = useState<AddTarget | null>(null)
  const [openId, setOpenId] = useState<number | null>(openFirst)
  const opened = useRef<HTMLLIElement>(null)
  useEffect(() => {
    opened.current?.scrollIntoView({ block: 'nearest' })
  }, [])

  const setCat = (c: string) => {
    setCatState(c)
    setShown(PAGE)
    try {
      localStorage.setItem(CAT_KEY, c)
    } catch {
      // remembering is optional
    }
  }
  const present = cats.filter((c) => stories.some((s) => s.category === c))
  const list = cat && present.includes(cat) ? stories.filter((s) => s.category === cat) : stories

  return (
    <section className="news-card" aria-labelledby="news-title">
      <div className="news-head modal-head">
        <Icon name="globe" size={18} />
        <h2 id="news-title">{t('Entertainment news')}</h2>
        <select className="disc-who news-cat-select" value={cat && present.includes(cat) ? cat : ''} onChange={(e) => setCat(e.target.value)} aria-label={t('News topics')}>
          {['', ...present].map((c) => (
            <option key={c || 'all'} value={c}>
              {categoryName(c)}
            </option>
          ))}
        </select>
        <button className="icon-btn" onClick={onClose} aria-label={t('Close')}>
          <Icon name="x" />
        </button>
      </div>
      <ul className="news-list">
        {list.slice(0, shown).map((s) => {
          const href = safeLink(s.link)
          const isOpen = openId === s.id
          return (
            <li key={s.id} ref={s.id === openFirst ? opened : undefined} className={`news-item${isOpen ? ' open' : ''}`}>
              <button className="news-head-btn" onClick={() => setOpenId(isOpen ? null : s.id)} aria-expanded={isOpen}>
                <span className="news-title">{s.title}</span>
                <span className="news-meta">
                  {s.source} · {timeAgo(s.publishedAt)}
                  {s.mentions.some((m) => m.have) && (
                    <>
                      {' '}
                      · <Icon name="check" size={11} /> {t('On your server')}
                    </>
                  )}
                </span>
              </button>
              {isOpen && (
                <div className="news-body">
                  <p className="news-summary">{s.summary}</p>
                  {s.mentions.length > 0 && (
                    <div className="news-mentions">
                      {s.mentions.map((m) =>
                        m.have ? (
                          <Link key={m.type + m.title} to={m.to} className="news-chip have" title={t('In your library')}>
                            <Icon name="open" size={12} /> {m.title}
                          </Link>
                        ) : m.add && m.tmdbId && (m.type === 'movie' || m.type === 'tv') ? (
                          <span key={m.type + m.title} className="news-chip-pair">
                            <Link to={m.to} className="news-chip">
                              {m.title}
                              {m.year ? ` (${m.year})` : ''}
                            </Link>
                            <button className="news-chip add" onClick={() => setAdding({ kind: m.type as 'movie' | 'tv', tmdbId: m.tmdbId!, title: m.title, year: m.year, posterUrl: m.posterUrl })} aria-label={t('Add {title}', { title: m.title })}>
                              <Icon name="plus" size={12} /> {t('Add')}
                            </button>
                          </span>
                        ) : (
                          <Link key={m.type + m.title} to={m.to} className="news-chip find" title={t('Look for it')}>
                            <Icon name="search" size={12} /> {m.title}
                          </Link>
                        ),
                      )}
                    </div>
                  )}
                  {href && (
                    <a className="news-read" href={href} target="_blank" rel="noopener noreferrer">
                      {t('Read the article')} <Icon name="external" size={12} />
                    </a>
                  )}
                </div>
              )}
            </li>
          )
        })}
      </ul>
      {list.length > shown && (
        <div className="rail-foot">
          <button className="btn-sm btn-with-icon" onClick={() => setShown((n) => n + PAGE)}>
            <Icon name="plus" size={14} /> {t('More news')}
          </button>
        </div>
      )}
      <p className="hint news-note">{t('Tap a headline for a short summary written by AI, and a link to the full article.')}</p>
      {adding && (
        <AddDialog
          target={adding}
          onClose={() => setAdding(null)}
          onAdded={(id) => {
            setAdding(null)
            onClose()
            navigate(adding.kind === 'movie' ? `/title/${adding.tmdbId}` : `/series/${id}`)
          }}
        />
      )}
    </section>
  )
}
