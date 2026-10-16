// Home fork: entertainment news on the Dashboard, under Tonight. Bite-sized
// stories from the news feeds set in Settings > Extras, each summarised by
// the AI with a link to the full article. Films, shows, books, comics and
// music a story names link to the library, or offer to add them.
import { useEffect, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
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
const FEW = 3 // Usherr: the Dashboard starts with a few headlines; the heading opens the rest

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

export default function News() {
  const navigate = useNavigate()
  const [stories, setStories] = useState<Story[] | null>(null)
  const [cats, setCats] = useState<string[]>([])
  const [cat, setCatState] = useState(() => {
    try {
      return localStorage.getItem(CAT_KEY) ?? ''
    } catch {
      return ''
    }
  })
  const [shown, setShown] = useState(FEW)
  const [adding, setAdding] = useState<AddTarget | null>(null)
  const [openId, setOpenId] = useState<number | null>(null)

  useEffect(() => {
    fetch('/api/home/news', { credentials: 'include' })
      .then((r) => (r.ok ? r.json() : null))
      .then((d: { on: boolean; items: Story[]; categories?: string[] } | null) => {
        if (!d?.on) return
        setStories(d.items)
        setCats(d.categories ?? [])
      })
      .catch(() => undefined)
  }, [])

  if (!stories || stories.length === 0) return null
  const setCat = (c: string) => {
    setCatState(c)
    setShown((n) => (n > FEW ? PAGE : FEW))
    try {
      localStorage.setItem(CAT_KEY, c)
    } catch {
      // remembering is optional
    }
  }
  const present = cats.filter((c) => stories.some((s) => s.category === c))
  const list = cat && present.includes(cat) ? stories.filter((s) => s.category === cat) : stories

  return (
    <section className="card news-card" aria-labelledby="news-title">
      <div className="news-head">
        <Icon name="globe" size={18} />
        <h2 id="news-title">
          <button className="news-toggle" onClick={() => setShown((n) => (n > FEW ? FEW : PAGE))} aria-expanded={shown > FEW} title={shown > FEW ? t('Show fewer headlines') : t('Show all headlines')}>
            {t('Entertainment news')} <Icon name={shown > FEW ? 'chevron-up' : 'chevron-down'} size={14} />
          </button>
        </h2>
        <select className="disc-who news-cat-select" value={cat && present.includes(cat) ? cat : ''} onChange={(e) => setCat(e.target.value)} aria-label={t('News topics')}>
          {['', ...present].map((c) => (
            <option key={c || 'all'} value={c}>
              {categoryName(c)}
            </option>
          ))}
        </select>
      </div>
      <ul className="news-list">
        {list.slice(0, shown).map((s) => {
          const href = safeLink(s.link)
          const isOpen = openId === s.id
          return (
            <li key={s.id} className={`news-item${isOpen ? ' open' : ''}`}>
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
      {(list.length > shown || shown > FEW) && (
        <div className="rail-foot">
          {list.length > shown && (
            <button className="btn-sm btn-with-icon" onClick={() => setShown((n) => (n === FEW ? PAGE : n + PAGE))}>
              <Icon name="plus" size={14} /> {shown === FEW ? t('Show all headlines') : t('More news')}
            </button>
          )}
          {shown > FEW && (
            <button className="btn-sm btn-with-icon" onClick={() => setShown(FEW)}>
              <Icon name="chevron-up" size={14} /> {t('Show fewer headlines')}
            </button>
          )}
        </div>
      )}
      {shown > FEW && <p className="hint news-note">{t('Tap a headline for a short summary written by AI, and a link to the full article.')}</p>}
      {adding && (
        <AddDialog
          target={adding}
          onClose={() => setAdding(null)}
          onAdded={(id) => {
            setAdding(null)
            navigate(adding.kind === 'movie' ? `/title/${adding.tmdbId}` : `/series/${id}`)
          }}
        />
      )}
    </section>
  )
}
