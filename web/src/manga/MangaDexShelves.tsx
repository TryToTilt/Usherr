// Usherr: your MangaDex shelves on the Manga page: new chapters of series you
// follow, what you read lately on MangaDex, and your MDLists. Only with your own
// account connected; 18+ series stay out unless the NSFW section is on for you.
import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { t } from '../i18n'
import MangaCover from './MangaCover'
import { mangaDexAccountApi, mangaDexChapterUrl, mangaDexTitleUrl, type MangaDexShelfItem, type MangaDexShelves as Shelves } from './mangaDexAccountApi'
import './mangaDex.css'

const WHY: Record<string, () => string> = {
  adult: () => t('18+ on MangaDex'),
  "can't": () => t('Your account can’t add series'),
  later: () => t('Comes in at the next sync'),
  novel: () => t('A novel, not manga'),
  failed: () => t('Couldn’t be added yet'),
}

function Item({ it, line }: { it: MangaDexShelfItem; line: string }) {
  const body = (
    <>
      <MangaCover title={it.title} src={it.cover} small />
      <span style={{ minWidth: 0 }}>
        <span className="t">{it.title}</span>
        {line && <span className="s">{line}</span>}
        {!it.seriesId && <span className="s">{t('On MangaDex')}</span>}
      </span>
    </>
  )
  if (it.seriesId) {
    return (
      <Link className="mg-cont" to={`/manga/${it.seriesId}`}>
        {body}
      </Link>
    )
  }
  return (
    <a className="mg-cont" href={it.chapterId ? mangaDexChapterUrl(it.chapterId) : mangaDexTitleUrl(it.dexId)} target="_blank" rel="noreferrer">
      {body}
    </a>
  )
}

function Row({ title, hint, items, line }: { title: string; hint?: string; items: MangaDexShelfItem[]; line: (it: MangaDexShelfItem) => string }) {
  if (items.length === 0) return null
  return (
    <section className="mg-stack" aria-label={title}>
      <h2>{title}</h2>
      {hint && <p className="mg-hint">{hint}</p>}
      <div className="mg-continue">
        {items.map((it) => (
          <Item key={`${it.dexId}-${it.chapterId ?? ''}`} it={it} line={line(it)} />
        ))}
      </div>
    </section>
  )
}

export default function MangaDexShelves({ refreshKey }: { refreshKey?: number }) {
  const [d, setD] = useState<Shelves | null>(null)
  useEffect(() => {
    mangaDexAccountApi
      .shelves()
      .then(setD)
      .catch(() => setD(null))
  }, [refreshKey])
  if (!d?.connected) return null
  const chapter = (it: MangaDexShelfItem) => (it.number ? (it.group ? t('Chapter {n}, by {group}', { n: it.number, group: it.group }) : t('Chapter {n}', { n: it.number })) : '')
  return (
    <>
      {d.state === 'refused' && (
        <section className="mg-card">
          <p className="mg-mdx-bad">{d.problem || t('MangaDex didn’t accept the sign-in.')}</p>
          <Link className="btn-sm" to="/settings/manga-account">
            {t('Check your MangaDex account')}
          </Link>
        </section>
      )}
      <Row title={t('New chapters for series you follow')} items={d.newChapters ?? []} line={chapter} />
      <Row title={t('Recently read on MangaDex')} items={d.recent ?? []} line={(it) => (it.number ? t('Read chapter {n}', { n: it.number }) : '')} />
      {(d.lists ?? []).map((l) => (
        <Row key={l.id} title={l.name || t('A list')} hint={l.more ? t('And {n} more on MangaDex.', { n: l.more }) : undefined} items={l.items} line={() => ''} />
      ))}
      <Row title={t('Followed on MangaDex, not in Manga')} items={d.outside ?? []} line={(it) => (it.reason ? (WHY[it.reason]?.() ?? '') : '')} />
      <p className="mg-hint">{t('From MangaDex, with thanks to the groups who translate.')}</p>
    </>
  )
}
