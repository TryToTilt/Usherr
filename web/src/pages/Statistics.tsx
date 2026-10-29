import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { api, isAdmin, type DashboardData, type IndexerStat, type LibraryStats, type WatchStats, type WatchedStatTitle } from '../api'
import { useAuth } from '../AuthContext'
import { formatBytes, timeAgo } from '../format'
import KindGlyph, { type GlyphKind } from '../components/KindGlyph'
import Icon from '../components/Icon'
import { useModules } from '../ModulesContext'
import { useComicsOn } from '../home/libraryKinds' // Usherr
import Loading from '../components/Loading'
import OverTime from '../components/OverTime'
import { locale, plural, t } from '../i18n'
import { useConfirm } from '../components/ConfirmProvider'
import { useToast } from '../components/Toast'
import '../stats.css'
import TipLabel from '../components/TipLabel'

// How much of a kind is in, as a ring around its shape.
function Ring({ pct }: { pct: number }) {
  return (
    <svg className="stat-ring" viewBox="0 0 44 44" aria-hidden="true">
      <circle cx="22" cy="22" r="19" pathLength={100} className="track" />
      <circle cx="22" cy="22" r="19" pathLength={100} className="fill" strokeDasharray={`${Math.max(pct, 0.5)} 100`} />
    </svg>
  )
}

// Usherr: one card per kind of media, with the totals the dashboard used to
// show (streams too); the card opens that library.
function KindStat({ k, glyph, label, value, unit, have, of, sub, to }: { k: string; glyph?: GlyphKind; label: string; value: number; unit?: string; have: number; of: number; sub: string; to: string }) {
  return (
    <Link to={to} className={`card stat-card k-${k}`}>
      <span className="stat-shape">
        <Ring pct={of > 0 ? Math.round((have * 100) / of) : 0} />
        {glyph ? <KindGlyph kind={glyph} size={26} variant="solid" /> : <Icon name="trophy" size={24} />}
      </span>
      <small>{label}</small>
      <strong>
        {value.toLocaleString(locale())}
        {unit && <em className="stat-unit">{unit}</em>}
      </strong>
      <span>{sub}</span>
    </Link>
  )
}

// Statistics: what is in the library, at which quality, how much space it
// takes, and how downloads went month by month.
export default function Statistics() {
  const [s, setS] = useState<LibraryStats | null>(null)
  const [error, setError] = useState('')
  const admin = isAdmin(useAuth().user)
  const confirm = useConfirm()
  const toast = useToast()
  const { on } = useModules()
  const comicsOn = useComicsOn(admin)
  // Usherr: the library totals (streams, anime, books, comics, sports) come from the dashboard numbers.
  const [dash, setDash] = useState<DashboardData | null>(null)
  useEffect(() => {
    api.dashboard().then(setDash).catch(() => undefined)
  }, [])
  const load = () =>
    api
      .libraryStats()
      .then(setS)
      .catch((e) => setError(e instanceof Error ? e.message : String(e)))
  useEffect(() => {
    void load()
  }, [])

  // Starts the download numbers again from zero. Nothing else is touched.
  async function reset() {
    const ok = await confirm({
      title: t('Reset the statistics?'),
      body: (
        <p>
          {t('The download numbers on this page and on the dashboard start again from zero: how many downloads were grabbed, finished and failed, how big they were, and the share from Usenet. Your library, your history and your files are not touched.')}
        </p>
      ),
      confirmLabel: t('Reset statistics'),
      danger: true,
    })
    if (!ok) return
    try {
      await api.resetStats()
      toast.success(t('Statistics reset.'))
      await load()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    }
  }

  if (error) return <p className="error-text">{error}</p>
  if (!s) return <Loading height={300} />
  const qualityMax = Math.max(1, ...s.quality.map((q) => q.count))
  const monthMax = Math.max(1, ...s.months.map((m) => m.completed + m.failed))
  const size = (n: number) => (n > 0 ? ` · ${formatBytes(n)}` : '')
  const join = (parts: string[]) => parts.filter(Boolean).join(' · ')
  const lib = dash?.library
  const kinds = lib ? (
    <>
      {on('movies') && (
        <KindStat k="movie" glyph="movie" label={t('Movies')} value={lib.movies.total} have={lib.movies.downloaded + (lib.movies.streams ?? 0)} of={lib.movies.total} to="/library?kind=movie"
          sub={join([t('{n} on disk', { n: lib.movies.downloaded }), lib.movies.streams ? t('{n} stream', { n: lib.movies.streams }) : '', t('{n} missing', { n: lib.movies.missing })]) + size(s.movieBytes)} />
      )}
      {on('tv') && (
        <KindStat k="tv" glyph="tv" label={t('TV shows')} value={lib.series.total} have={lib.series.episodesDownloaded + (lib.series.episodesStreams ?? 0)} of={lib.series.episodes} to="/library?kind=tv"
          sub={join([plural(lib.series.episodes, '{n} episode', '{n} episodes'), t('{n} on disk', { n: lib.series.episodesDownloaded }), lib.series.episodesStreams ? t('{n} stream', { n: lib.series.episodesStreams }) : '']) + size(s.episodeBytes)} />
      )}
      {on('tv') && lib.anime && lib.anime.total > 0 && (
        <KindStat k="anime" glyph="tv" label={t('Anime')} value={lib.anime.total} have={lib.anime.episodesDownloaded + lib.anime.episodesStreams} of={lib.anime.episodes} to="/library?kind=anime"
          sub={join([plural(lib.anime.episodes, '{n} episode', '{n} episodes'), t('{n} on disk', { n: lib.anime.episodesDownloaded }), lib.anime.episodesStreams ? t('{n} stream', { n: lib.anime.episodesStreams }) : ''])} />
      )}
      {on('music') && lib.music && lib.music.artists > 0 && (
        <KindStat k="music" glyph="music" label={t('Music')} value={lib.music.artists} unit={plural(lib.music.artists, 'artist', 'artists')} have={lib.music.downloaded} of={lib.music.albums} to="/library?kind=music"
          sub={plural(lib.music.albums, '{n} album · {missing} missing', '{n} albums · {missing} missing', { missing: lib.music.missing })} />
      )}
      {on('ebooks') && lib.ebooks && lib.ebooks.books > 0 && (
        <KindStat k="book" glyph="book" label={t('Ebooks')} value={lib.ebooks.books} unit={plural(lib.ebooks.books, 'book', 'books')} have={lib.ebooks.downloaded} of={lib.ebooks.books} to="/library?kind=ebook"
          sub={t('{downloaded} downloaded · {missing} missing', { downloaded: lib.ebooks.downloaded, missing: lib.ebooks.missing })} />
      )}
      {on('audiobooks') && lib.audiobooks && lib.audiobooks.books > 0 && (
        <KindStat k="audiobook" glyph="audiobook" label={t('Audiobooks')} value={lib.audiobooks.books} unit={plural(lib.audiobooks.books, 'book', 'books')} have={lib.audiobooks.downloaded} of={lib.audiobooks.books} to="/library?kind=audiobook"
          sub={t('{downloaded} downloaded · {missing} missing', { downloaded: lib.audiobooks.downloaded, missing: lib.audiobooks.missing })} />
      )}
      {comicsOn && lib.comics && (
        <KindStat k="comics" glyph="comic" label={t('Comics')} value={lib.comics.series} unit={plural(lib.comics.series, 'series', 'series')} have={lib.comics.downloaded} of={lib.comics.downloaded + lib.comics.missing} to="/comics"
          sub={t('{downloaded} issues downloaded · {missing} missing', { downloaded: lib.comics.downloaded, missing: lib.comics.missing })} />
      )}
      {lib.sports?.on && (
        <KindStat k="sports" label={t('Sports')} value={lib.sports.games + lib.sports.streams} unit={plural(lib.sports.games + lib.sports.streams, 'game', 'games')} have={lib.sports.games + lib.sports.streams} of={lib.sports.games + lib.sports.streams + lib.sports.wanted} to="/sports"
          sub={join([plural(lib.sports.leagues, '{n} league', '{n} leagues'), t('{n} on disk', { n: lib.sports.games }), lib.sports.streams ? t('{n} stream', { n: lib.sports.streams }) : '', t('{n} wanted', { n: lib.sports.wanted })])} />
      )}
    </>
  ) : null
  return (
    <div className="stats-page">
      <div className="page-header">
        <h1>{t('Statistics')}</h1>
      </div>
      <div className="stat-cards">
        {kinds}
        <div className="card stat-card k-size">
          <small>{t('Library size')}</small>
          <strong>{formatBytes(Math.max(s.movieBytes + s.episodeBytes, dash?.library.sizeBytes ?? 0))}</strong>
          <span>{s.usenetShare > 0 || s.months.length > 0 ? t('{pct}% of downloads came from Usenet', { pct: s.usenetShare }) : t('No downloads yet')}</span>
          {(s.usenetShare > 0 || s.months.length > 0) && (
            <span className="stat-split" aria-hidden="true">
              <i style={{ width: `${s.usenetShare}%` }} />
            </span>
          )}
        </div>
      </div>

      <OverTime />

      <div className="half-cols">
        <section className="card">
          <h2>{t('By quality')}</h2>
          {s.quality.length === 0 && <p style={{ color: 'var(--text-dim)' }}>{t('Nothing downloaded yet.')}</p>}
          <div className="bar-list">
            {s.quality.map((q) => (
              <div key={q.label} className="bar-row">
                <span className="bar-label">{q.label}</span>
                <span className="bar-track">
                  <span className="bar-fill" style={{ width: `${(q.count * 100) / qualityMax}%` }} />
                </span>
                <span className="bar-value">
                  {q.count} · {formatBytes(q.bytes ?? 0)}
                </span>
              </div>
            ))}
          </div>
        </section>
        <section className="card">
          <h2>{t('Downloads by month')}</h2>
          {s.months.length === 0 && <p style={{ color: 'var(--text-dim)' }}>{t('No downloads counted yet.')}</p>}
          <div className="bar-list">
            {s.months.map((m) => (
              <div key={m.month} className="bar-row">
                <span className="bar-label">{new Date(`${m.month}-15`).toLocaleDateString(locale(), { month: 'short', year: 'numeric' })}</span>
                <span className="bar-track">
                  <span className="bar-fill" style={{ width: `${(m.completed * 100) / monthMax}%` }} />
                  <span className="bar-fill bad" style={{ width: `${(m.failed * 100) / monthMax}%` }} />
                </span>
                <span className="bar-value">
                  {m.failed > 0 ? t('{done} done, {failed} failed', { done: m.completed, failed: m.failed }) : t('{done} done', { done: m.completed })} · {formatBytes(m.bytes)}
                </span>
              </div>
            ))}
          </div>
          {(s.totals.grabs > 0 || s.totals.completed > 0 || s.totals.failed > 0) && (
            <p className="stats-totals">
              <b>{t('{grabbed} grabbed, {done} done, {failed} failed, {size} downloaded', { grabbed: s.totals.grabs, done: s.totals.completed, failed: s.totals.failed, size: formatBytes(s.totals.bytes) })}</b>
            </p>
          )}
          <p className="field-hint">{t('These numbers are counted as downloads happen, so clearing the history list does not change them.')}</p>
          {admin && (
            <button type="button" className="btn-sm btn-danger" style={{ marginTop: 10 }} onClick={() => void reset()}>
              {t('Reset statistics')}
            </button>
          )}
        </section>
      </div>
      <IndexerStats />
      {s.watched && <WatchedStats w={s.watched} />}
    </div>
  )
}

// How each indexer has answered over the last days: searches, failures, the
// share that worked, releases found and grabs. Administrators only, because it
// names the indexers.
function IndexerStats() {
  const admin = isAdmin(useAuth().user)
  const [data, setData] = useState<{ days: number; indexers: IndexerStat[] } | null>(null)
  useEffect(() => {
    if (admin) api.indexerStats().then(setData).catch(() => undefined)
  }, [admin])
  if (!admin || !data || data.indexers.length === 0) return null
  const busiest = Math.max(1, ...data.indexers.map((i) => i.queries))
  return (
    <section className="card" style={{ marginTop: 16 }}>
      <h2><TipLabel help="indexers#how-each-indexer-is-doing">{plural(data.days, 'Indexers, last {n} day', 'Indexers, last {n} days')}</TipLabel></h2>
      <div style={{ overflowX: 'auto' }}>
        <table className="indexer-stats" style={{ width: '100%' }}>
          <thead>
            <tr>
              <th style={{ textAlign: 'left' }}>{t('Indexer')}</th>
              <th style={{ textAlign: 'right' }}>{t('Searches')}</th>
              <th style={{ textAlign: 'right' }}>{t('Failed')}</th>
              <th style={{ textAlign: 'right' }}>{t('Worked')}</th>
              <th style={{ textAlign: 'right' }}>{t('Releases found')}</th>
              <th style={{ textAlign: 'right' }}>{t('Grabbed')}</th>
            </tr>
          </thead>
          <tbody>
            {data.indexers.map((i) => (
              <tr key={i.id} style={{ opacity: i.enabled ? 1 : 0.55 }}>
                <td>
                  <strong>{i.name}</strong> <span className="badge">{i.protocol === 'torrent' ? t('Torrent') : t('Usenet')}</span>
                  {!i.enabled && <small> · {t('Off')}</small>}
                  {i.pausedUntil && <small> · {t('paused')}</small>}
                  <span className="bar-track" style={{ display: 'block', marginTop: 4, maxWidth: 220 }}>
                    <span className="bar-fill" style={{ width: `${(i.queries * 100) / busiest}%` }} />
                  </span>
                </td>
                <td style={{ textAlign: 'right' }}>{i.queries}</td>
                <td style={{ textAlign: 'right' }}>{i.failures}</td>
                <td style={{ textAlign: 'right' }}>{i.successRate < 0 ? '–' : `${Math.round(i.successRate * 100)}%`}</td>
                <td style={{ textAlign: 'right' }}>{i.results}</td>
                <td style={{ textAlign: 'right' }}>{i.grabs}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <p className="field-hint">{t('A search is one question to one indexer. Grabs count the releases taken from it, by hand or by Mediarium. An indexer that is paused after repeated failures is not asked, and those skipped searches are not counted.')}</p>
    </section>
  )
}

function titleLink(item: WatchedStatTitle): string {
  return item.kind === 'series' ? `/series/${item.id}` : `/title/${item.tmdbId ?? 0}`
}

// What gets watched, read from Plex, Jellyfin and Emby (Settings > Media
// servers > "Read what's been watched").
function WatchedStats({ w }: { w: WatchStats }) {
  const pct = (a: number, b: number) => (a + b > 0 ? Math.round((a * 100) / (a + b)) : 0)
  const topMax = Math.max(1, ...w.top.map((item) => item.plays))
  return (
    <>
      <h2 style={{ marginTop: 28 }}>{t('What gets watched')}</h2>
      <div className="stat-cards">
        <div className="card stat-card">
          <small>{t('Movies watched')}</small>
          <strong>{w.moviesWatched}</strong>
          <span>
            {t('{pct}% of the {total} you have', { pct: pct(w.moviesWatched, w.moviesUnwatched), total: w.moviesWatched + w.moviesUnwatched })}
          </span>
        </div>
        <div className="card stat-card">
          <small>{t('Episodes watched')}</small>
          <strong>{w.episodesWatched}</strong>
          <span>
            {t('{pct}% of the {total} you have', { pct: pct(w.episodesWatched, w.episodesUnwatched), total: w.episodesWatched + w.episodesUnwatched })}
          </span>
        </div>
        <div className="card stat-card">
          <small>{t('Never watched')}</small>
          <strong>{formatBytes(w.unwatchedBytes)}</strong>
          <span>
            {t('{movies} and {episodes} nobody has played', {
              movies: plural(w.moviesUnwatched, '{n} movie', '{n} movies'),
              episodes: plural(w.episodesUnwatched, '{n} episode', '{n} episodes'),
            })}
          </span>
        </div>
        <div className="card stat-card">
          <small>{t('Plays')}</small>
          <strong>{w.plays}</strong>
          <span>{w.lastSync ? t('Checked {when}', { when: timeAgo(w.lastSync) }) : t('Not checked yet')}</span>
        </div>
      </div>
      <div className="half-cols">
        <section className="card">
          <h2>{t('Most watched')}</h2>
          {w.top.length === 0 && <p style={{ color: 'var(--text-dim)' }}>{t('Nothing watched yet.')}</p>}
          <div className="bar-list">
            {w.top.map((item) => (
              <div key={`${item.kind}-${item.id}`} className="bar-row">
                <Link className="bar-label" to={titleLink(item)} title={item.title}>
                  {item.title}
                </Link>
                <span className="bar-track">
                  <span className="bar-fill" style={{ width: `${(item.plays * 100) / topMax}%` }} />
                </span>
                <span className="bar-value">
                  {plural(item.plays, '{n} play', '{n} plays')}
                  {item.kind === 'series' ? ` · ${plural(item.episodes ?? 0, '{n} episode', '{n} episodes')}` : ''}
                </span>
              </div>
            ))}
          </div>
        </section>
        <section className="card">
          <h2>{t('Watched lately')}</h2>
          {w.recent.length === 0 && (
            <p style={{ color: 'var(--text-dim)' }}>{w.top.length === 0 ? t('Nothing watched yet.') : t("Your media servers haven't said when things were played.")}</p>
          )}
          <div className="bar-list">
            {w.recent.map((item) => (
              <div key={`${item.kind}-${item.id}`} className="bar-row recent-row">
                <Link className="bar-label" to={titleLink(item)} title={item.title}>
                  {item.title}
                </Link>
                <span className="bar-value">{timeAgo(item.lastPlayed)}</span>
              </div>
            ))}
          </div>
        </section>
      </div>
    </>
  )
}
