// Usherr: the Manga library: your series, with one word on each saying how it's
// doing, and what's waiting for you.
import { lazy, Suspense, useCallback, useEffect, useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import { can, isAdmin } from '../api'
import { useAuth } from '../AuthContext'
import { PullDrawer } from '../components/CompactBar'
import HelpLink from '../components/HelpLink'
import LibraryTabs from '../home/LibraryTabs'
import '../home/comics.css'
import { plural, t } from '../i18n'
import { useModules } from '../ModulesContext'
import { useLive } from '../useLive'
import MangaCover from './MangaCover'
import { mangaApi, type MangaContinue, type MangaReview, type MangaSeries, type MangaSources, type MangaStats } from './mangaApi'
import { FORMAT_LABEL, mangaStatus, sourcesOf, stateLabel } from './mangaStatus'
import './manga.css'

const MangaReader = lazy(() => import('./MangaReader'))

type Filter = 'all' | 'waiting' | 'done'

export default function Manga() {
  const { user } = useAuth()
  const admin = isAdmin(user)
  const { on } = useModules()
  const canAdd = admin || can(user, 'manga')
  const [list, setList] = useState<MangaSeries[] | null>(null)
  const [problems, setProblems] = useState<string[]>([])
  const [sources, setSources] = useState<MangaSources>({ official: true, fan: false, nyaa: false })
  const [reviews, setReviews] = useState<MangaReview[]>([])
  const [filter, setFilter] = useState<Filter>('all')
  const [error, setError] = useState('')
  const [reading, setReading] = useState<MangaContinue[]>([])
  const [stats, setStats] = useState<MangaStats | null>(null)
  const [open, setOpen] = useState<{ id: number; chapter: boolean } | null>(null)
  const enabled = on('manga')

  const load = useCallback(() => {
    if (!enabled) return
    mangaApi
      .list()
      .then((d) => {
        setList(d.series)
        setProblems(d.problems)
        setSources(d.sources)
        setError('')
      })
      .catch((e: Error) => setError(e.message))
    mangaApi
      .continueRow()
      .then(setReading)
      .catch(() => undefined)
    mangaApi
      .stats()
      .then(setStats)
      .catch(() => undefined)
    if (admin) {
      mangaApi
        .reviews()
        .then(setReviews)
        .catch(() => undefined)
    }
  }, [enabled, admin])
  useEffect(load, [load])
  useLive(load, 60_000, enabled)

  const shown = useMemo(() => {
    if (!list) return []
    if (filter === 'waiting') return list.filter((s) => s.waiting > 0)
    if (filter === 'done') return list.filter((s) => s.total > 0 && s.have >= s.total)
    return list
  }, [list, filter])

  if (!enabled) {
    return (
      <div className="page-pad mg-page">
        <h1>{t('Manga')}</h1>
        <p className="mg-hint">{admin ? t('Manga is switched off. Switch it on in Settings > Media types.') : t('Manga isn’t switched on.')}</p>
        {admin && (
          <div>
            <Link className="btn-sm" to="/settings/modules">
              {t('Open Media types')}
            </Link>
          </div>
        )}
      </div>
    )
  }

  const first = reviews[0]
  return (
    <div className="page-pad mg-page cx-page">
      <PullDrawer className="cx-pull">
        <div className="toolbar lib-toolbar cx-libbar">
          <LibraryTabs current="manga" />
          <HelpLink topic="manga" />
        </div>
      </PullDrawer>
      <div className="mg-head">
        <div>
          <h1>{t('Manga')}</h1>
        </div>
        {canAdd && (
          <div className="mg-tools">
            {admin && (
              <Link className="btn-sm" to="/settings/manga">
                {t('Settings')}
              </Link>
            )}
            <Link className="btn-sm" to="/manga/discover">
              {t('Discover')}
            </Link>
            <Link className="btn-sm primary" to="/manga/add" data-walk="manga-add">
              {t('+ Add a series')}
            </Link>
          </div>
        )}
      </div>
      {error && <p className="error-text">{error}</p>}
      {first && (
        <section className="mg-card mg-waiting">
          <div>
            <h2>{t('Waiting for you')}</h2>
            <p className="mg-hint">{reviews.length === 1 ? t('1 release might be {series}. Usherr wasn’t sure enough to take it.', { series: first.series }) : t('{n} releases might be what you want, starting with {series}. Usherr wasn’t sure enough to take them.', { n: reviews.length, series: first.series })}</p>
          </div>
          <Link className="btn-sm" to={`/manga/${first.seriesId}`}>
            {t('Look at them')}
          </Link>
        </section>
      )}
      {reading.length > 0 && (
        <section className="mg-stack" aria-label={t('Continue reading')}>
          <h2>{t('Continue reading')}</h2>
          {stats && stats.volumes + stats.chapters > 0 && <p className="mg-hint">{[stats.volumes > 0 ? plural(stats.volumes, '{n} volume read', '{n} volumes read') : '', stats.chapters > 0 ? plural(stats.chapters, '{n} chapter read', '{n} chapters read') : '', t('{n} this month', { n: stats.thisMonth })].filter(Boolean).join(' · ')}</p>}
          <div className="mg-continue">
            {reading.map((c) => {
              const pct = c.pages > 0 ? Math.round((c.page / c.pages) * 100) : 0
              return (
                <button key={c.chapterId ? `c${c.chapterId}` : `v${c.volumeId}`} className="mg-cont" onClick={() => setOpen({ id: c.chapterId ?? c.volumeId ?? 0, chapter: !!c.chapterId })}>
                  <MangaCover title={c.series} src={c.coverUrl} small />
                  <span style={{ minWidth: 0 }}>
                    <span className="t">{c.series}</span>
                    <span className="s">{c.chapterId ? (c.page > 0 && c.pages > 0 ? t('Chapter {n}, page {page} of {pages}', { n: c.number, page: c.page, pages: c.pages }) : t('Next: Chapter {n}', { n: c.number })) : c.page > 0 && c.pages > 0 ? t('Volume {n}, page {page} of {pages}', { n: c.number, page: c.page, pages: c.pages }) : t('Next: Volume {n}', { n: c.number })}</span>
                    <span className="mg-bar" aria-hidden="true">
                      <i style={{ width: `${pct}%` }} />
                    </span>
                  </span>
                </button>
              )
            })}
          </div>
        </section>
      )}
      {open !== null && (
        <Suspense fallback={null}>
          <MangaReader
            volumeId={open.id}
            chapter={open.chapter}
            onClose={() => {
              setOpen(null)
              window.setTimeout(load, 400)
            }}
          />
        </Suspense>
      )}
      {!list && !error && <div className="skeleton" style={{ height: 240 }} />}
      {list && list.length === 0 && (
        <section className="mg-card mg-empty">
          <h2>{t('No manga yet')}</h2>
          <p className="mg-hint">{canAdd ? t('Add a series by typing its English title. Usherr finds the rest, and if nothing comes, it tells you why.') : t('Nobody has added a series yet.')}</p>
          {canAdd && (
            <Link className="btn-sm primary" to="/manga/add">
              {t('+ Add a series')}
            </Link>
          )}
          {admin && (
            <p className="mg-hint">
              {t('Already have files?')} <Link to="/settings/manga">{t('Look through your manga folder')}</Link>
            </p>
          )}
        </section>
      )}
      {list && list.length > 0 && (
        <section className="mg-stack">
          <div className="mg-head">
            <h2>{t('Your series')}</h2>
            <div className="seg" role="group" aria-label={t('Show')}>
              {(
                [
                  ['all', t('All')],
                  ['waiting', t('Waiting for you')],
                  ['done', t('All here')],
                ] as const
              ).map(([key, label]) => (
                <button key={key} className={filter === key ? 'active' : ''} aria-pressed={filter === key} onClick={() => setFilter(key)}>
                  {label}
                </button>
              ))}
            </div>
          </div>
          {shown.length === 0 && <p className="mg-hint">{filter === 'waiting' ? t('Nothing is waiting for you.') : t('No series has all its volumes yet.')}</p>}
          <div className="mg-grid">
            {shown.map((s) => (
              <Tile key={s.id} s={s} sources={sources} problems={problems} />
            ))}
          </div>
        </section>
      )}
    </div>
  )
}

function Tile({ s, sources, problems }: { s: MangaSeries; sources: MangaSources; problems: string[] }) {
  const st = mangaStatus(s, sourcesOf(s, sources), problems)
  const pct = s.total > 0 ? Math.round((s.have / s.total) * 100) : 0
  const sub = [FORMAT_LABEL[s.format]?.() ?? '', stateLabel(s.status)].filter(Boolean).join(' · ')
  return (
    <Link className="mg-tile" to={`/manga/${s.id}`}>
      <span className="mg-slot">
        <MangaCover title={s.title} src={s.coverUrl} />
      </span>
      <span>
        <span className="t">{s.title}</span>
        <span className="s">{sub}</span>
      </span>
      <span className={`mg-pill ${st.tone}`}>{st.label}</span>
      <span className="mg-bar" aria-hidden="true">
        <i style={{ width: `${pct}%` }} />
      </span>
    </Link>
  )
}
