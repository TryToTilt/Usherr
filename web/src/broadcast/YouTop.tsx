// Usherr Broadcast: the top of You, as in the mock-up: you and this year's
// numbers, a row of buttons (Your year, Diary, Stubs and badges, Your
// settings, Show me how), then on the main page your Stack and "Keep going":
// where you got to reading, listening and watching. Each is one stop for the
// remote; the first button has focus when the page opens. The rest of You
// (taste, votes, the household) follows below it as before.
import { useEffect, useState, type ReactNode } from 'react'
import { Link } from 'react-router-dom'
import { api, isAdmin, type Book, type BookProgress } from '../api'
import { useAuth } from '../AuthContext'
import { plural, t } from '../i18n'
import { stackPath, useStack } from '../home/stack'
import { mangaContinueLink, mangaContinueMeta, useMangaContinue } from '../manga/useMangaContinue'
import { openSmhMenu } from '../smh/engine'
import './screens.css'

type Tab = 'taste' | 'diary' | 'wrapped' | 'stubs'
type Watching = { item: string; title: string; episode?: string; link: string; position: number; duration: number }

async function getJSON<T>(path: string): Promise<T | null> {
  try {
    const r = await fetch(path, { credentials: 'include', cache: 'no-store' })
    return r.ok ? ((await r.json()) as T) : null
  } catch {
    return null
  }
}

interface Card {
  key: string
  kind: string
  title: string
  sub: string
  to: string
}

// Where you got to: the manga (or book) you're reading, the audiobook you're
// listening to and what you were watching, one of each, the newest first.
function useKeepGoing(): Card[] {
  const manga = useMangaContinue()
  const [books, setBooks] = useState<{ list: Book[]; progress: BookProgress[] } | null>(null)
  const [watching, setWatching] = useState<Watching | null>(null)
  useEffect(() => {
    void Promise.all([api.listBookProgress().catch(() => [] as BookProgress[]), api.listBooks().catch(() => [] as Book[])]).then(([progress, list]) => setBooks({ progress, list }))
    void getJSON<Watching[]>('/api/home/continue').then((x) => setWatching(Array.isArray(x) && x.length ? x[0] : null))
  }, [])
  const open = (books?.progress ?? []).filter((p) => !p.finished && p.percent > 0).sort((a, b) => (b.updatedAt ?? '').localeCompare(a.updatedAt ?? ''))
  const titleOf = (id: number) => books?.list.find((b) => b.id === id)?.title
  const cards: Card[] = []
  const m = manga[0]
  const ebook = open.find((p) => p.format === 'ebook' && titleOf(p.bookId))
  if (m) cards.push({ key: 'read', kind: t('Manga'), title: m.series, sub: mangaContinueMeta(m), to: mangaContinueLink(m) })
  else if (ebook) cards.push({ key: 'read', kind: t('Reading'), title: titleOf(ebook.bookId) ?? '', sub: t('{n}% read', { n: Math.round(ebook.percent) }), to: `/bookshelf/read/${ebook.bookId}` })
  const audio = open.find((p) => p.format === 'audiobook' && titleOf(p.bookId))
  if (audio) cards.push({ key: 'listen', kind: t('Audiobook'), title: titleOf(audio.bookId) ?? '', sub: t('{n}% listened', { n: Math.round(audio.percent) }), to: `/bookshelf/listen/${audio.bookId}` })
  if (watching) {
    const left = Math.max(1, Math.round((watching.duration - watching.position) / 60))
    cards.push({ key: 'watch', kind: t('Watching'), title: watching.title, sub: [watching.episode?.split(' · ')[0], plural(left, '{n} min left', '{n} min left')].filter(Boolean).join(' · '), to: watching.link })
  }
  return cards
}

function ThisYear() {
  const [year, setYear] = useState<{ films: number; episodes: number } | null>(null)
  useEffect(() => {
    void getJSON<{ films: number; episodes: number }>('/api/home/wrapped').then(setYear)
  }, [])
  if (!year) return <span className="b-you-year">&nbsp;</span>
  return (
    <span className="b-you-year">
      {t('This year')}: {plural(year.films, 'one film', '{n} films')} · {plural(year.episodes, 'one episode', '{n} episodes')}
    </span>
  )
}

export default function YouTop({ name, tab, extra }: { name: string; tab: Tab; extra?: ReactNode }) {
  const admin = isAdmin(useAuth().user)
  const stack = useStack()
  const keep = useKeepGoing()
  const buttons: { key: string; to?: string; on?: () => void; label: string; here?: boolean }[] = [
    ...(tab !== 'taste' ? [{ key: 'taste', to: '/you', label: t('Taste and votes') }] : []),
    { key: 'wrapped', to: '/you/wrapped', label: t('Your year'), here: tab === 'wrapped' },
    { key: 'diary', to: '/you/diary', label: t('Diary'), here: tab === 'diary' },
    { key: 'stubs', to: '/you/stubs', label: t('Stubs and badges'), here: tab === 'stubs' },
    { key: 'settings', to: '/settings/general', label: t('Your settings') },
    ...(admin ? [{ key: 'server', to: '/server', label: t('Server') }] : []),
    { key: 'smh', on: openSmhMenu, label: t('Show me how') },
    { key: 'help', to: '/help', label: t('Help') },
  ]
  return (
    <div className="b-you">
      <div className="b-you-head">
        <span className="b-you-avatar" aria-hidden="true">
          {(name || '?').slice(0, 1).toUpperCase()}
        </span>
        <div className="b-you-name">
          <h2>{t('Your corner')}</h2>
          <ThisYear />
        </div>
        {extra && <div className="b-you-extra">{extra}</div>}
      </div>
      <nav className="b-you-buttons" aria-label={t('Your page')}>
        {buttons.map((b, i) =>
          b.to ? (
            <Link key={b.key} to={b.to} className={`b-btn${b.here ? ' active' : ''}`} aria-current={b.here ? 'page' : undefined} data-autofocus={i === 0 ? '' : undefined}>
              {b.label}
            </Link>
          ) : (
            <button key={b.key} type="button" className="b-btn" onClick={b.on}>
              {b.label}
            </button>
          ),
        )}
      </nav>
      {tab === 'taste' && stack && stack.length > 0 && (
        <section aria-label={t('Your stack')}>
          <h2 className="b-sec">{t('Your stack')}</h2>
          <div className="b-you-stack">
            {stack.slice(0, 12).map((it) => (
              <Link key={`${it.kind}-${it.tmdbId}`} to={stackPath(it)} className="b-you-poster" aria-label={it.title} title={it.title}>
                {it.posterUrl ? <img src={it.posterUrl} alt="" loading="lazy" /> : <span>{it.title}</span>}
              </Link>
            ))}
          </div>
        </section>
      )}
      {tab === 'taste' && keep.length > 0 && (
        <section aria-label={t('Keep going')}>
          <h2 className="b-sec">{t('Keep going')}</h2>
          <div className="b-you-keep">
            {keep.map((c) => (
              <Link key={c.key} to={c.to} className="b-you-card">
                <span className="b-you-kind">{c.kind}</span>
                <span className="b-you-title">{c.title}</span>
                <span className="b-you-sub">{c.sub}</span>
              </Link>
            ))}
          </div>
        </section>
      )}
    </div>
  )
}
