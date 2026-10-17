import { Fragment, useEffect, useMemo, useRef, useState } from 'react'
import type { SearchResult } from '../api'
import { formatBytes, nameParts } from '../format'
import { locale, t } from '../i18n'
import { FIRST_DIRECTION, ageOf, filterReleases, indexersIn, publishedAt, qualitiesIn, sortReleases, type ReleaseSortKey, type SortDir } from '../releaseSort'
import Dropdown from './Dropdown'
import Icon from './Icon'

// Interactive-search results: every release with quality info, badges for
// packs / blocklisted / "automation would skip this" (with the reason on
// hover), and a Grab button — grabbing a rejected release by hand is always
// allowed. The columns sort when you click their names, the list can be
// narrowed by indexer, quality or words, and the card can be closed.
export default function ReleaseTable({
  results,
  grabbing,
  onGrab,
  onClose,
  showPackBadge = false,
  qualityLabel,
  onStream,
  streaming = null,
}: {
  results: SearchResult[]
  grabbing: string | null
  onGrab: (r: SearchResult) => void
  // Gives the card a Close button.
  onClose?: () => void
  showPackBadge?: boolean
  // What to show in the Quality column when a release has no resolution (music).
  qualityLabel?: (r: SearchResult) => string | undefined
  // Home fork: "Stream now" for a Usenet release (movie and TV pages only).
  onStream?: (r: SearchResult) => void
  streaming?: string | null
}) {
  const showLanguage = results.some((r) => r.language)
  const showScore = results.some((r) => r.formatScore !== undefined)
  const [sort, setSort] = useState<{ key: ReleaseSortKey; dir: SortDir } | null>(null)
  const [words, setWords] = useState('')
  const [indexer, setIndexer] = useState('')
  const [quality, setQuality] = useState('')
  const indexers = useMemo(() => indexersIn(results), [results])
  const qualities = useMemo(() => qualitiesIn(results), [results])
  const shown = useMemo(() => sortReleases(filterReleases(results, { text: words, indexer, quality }), sort?.key ?? null, sort?.dir ?? 1), [results, words, indexer, quality, sort])
  const filtering = words !== '' || indexer !== '' || quality !== ''
  // New results scroll into view, so they aren't missed above the page.
  const top = useRef<HTMLDivElement>(null)
  useEffect(() => {
    const el = top.current
    if (!el) return
    const box = (el.closest('.card') as HTMLElement | null) ?? el
    const r = box.getBoundingClientRect()
    if (r.top < 70 || r.top > window.innerHeight - 120) box.scrollIntoView({ behavior: 'smooth', block: 'start' })
  }, [results])

  // First click sorts the natural way for that column, the second reverses it, the third goes back to the order the server gave.
  function clickSort(key: ReleaseSortKey) {
    setSort((cur) => {
      if (!cur || cur.key !== key) return { key, dir: FIRST_DIRECTION[key] }
      if (cur.dir === FIRST_DIRECTION[key]) return { key, dir: (-FIRST_DIRECTION[key]) as SortDir }
      return null
    })
  }
  const head = (key: ReleaseSortKey, label: string, title?: string) => (
    <th aria-sort={sort?.key === key ? (sort.dir === 1 ? 'ascending' : 'descending') : 'none'} title={title}>
      <button type="button" className="th-sort" onClick={() => clickSort(key)}>
        {label}
        <span className="th-arrow" aria-hidden="true">
          {sort?.key === key ? (sort.dir === 1 ? '▲' : '▼') : ''}
        </span>
      </button>
    </th>
  )
  const qualityName = (q: string) => (q === 'other' ? t('No resolution') : q)

  return (
    <>
      <div className="rel-tools" ref={top}>
        <input type="search" value={words} onChange={(e) => setWords(e.target.value)} placeholder={t('Find in these releases')} aria-label={t('Find in these releases')} />
        {indexers.length > 1 && <Dropdown label={t('Indexer')} value={indexer} onChange={setIndexer} options={[{ value: '', label: t('Any indexer') }, ...indexers.map((i) => ({ value: i, label: i }))]} />}
        {qualities.length > 1 && <Dropdown label={t('Quality')} value={quality} onChange={setQuality} options={[{ value: '', label: t('Any quality') }, ...qualities.map((q) => ({ value: q, label: qualityName(q) }))]} />}
        <span className="rel-count">{t('{shown} of {total}', { shown: shown.length, total: results.length })}</span>
        {filtering && (
          <button
            type="button"
            className="btn-sm"
            onClick={() => {
              setWords('')
              setIndexer('')
              setQuality('')
            }}
          >
            {t('Clear filters')}
          </button>
        )}
        {onClose && (
          <button type="button" className="btn-sm btn-with-icon rel-close" onClick={onClose}>
            <Icon name="x" size={14} /> {t('Close')}
          </button>
        )}
      </div>
      {shown.length === 0 && <p style={{ color: 'var(--text-dim)' }}>{t('Nothing matches these filters.')}</p>}
      {shown.length > 0 && (
        <div className="rel-scroll">
          <table className="rel-table">
            <thead>
              <tr>
                {head('release', t('Release'))}
                {head('quality', t('Quality'))}
                {showLanguage && head('language', t('Language'))}
                {showScore && head('score', t('Score'), t("Custom format score in this title's quality profile"))}
                {head('indexer', t('Indexer'))}
                {head('size', t('Size'))}
                {head('age', t('Age'), t('How long ago the indexer posted it'))}
                {head('seeders', t('Seeders'))}
                <th></th>
              </tr>
            </thead>
            <tbody>
              {shown.map((r) => (
                <tr key={r.downloadUrl} style={r.rejections?.length || r.blocklisted ? { opacity: 0.75 } : undefined}>
                  <td className="rel-name">
                    {nameParts(r.title).map((p, i) => (
                      <Fragment key={i}>
                        {p}
                        <wbr />
                      </Fragment>
                    ))}
                    {r.matchedTitle && <div className="lang-hint lang-hint-wrap">{t('Matched on the title "{title}"', { title: r.matchedTitle })}</div>}
                    {showPackBadge && (r.episodes?.length ?? 0) === 0 && (
                      <span className="badge" style={{ marginLeft: 8 }}>
                        {t('season pack')}
                      </span>
                    )}
                    {r.cached && (
                      <span className="badge downloaded" style={{ marginLeft: 8 }} title={t('Your debrid service already has this torrent, so it starts at once')}>
                        {t('Cached')}
                      </span>
                    )}
                    {r.blocklisted && (
                      <span className="badge failed" style={{ marginLeft: 8 }} title={t('This release failed before')}>
                        {t('blocklisted')}
                      </span>
                    )}
                    {r.acceptedBy?.fallback && (
                      <span className="badge fallback-badge" style={{ marginLeft: 8 }} title={t('Only used if nothing is found at the title\'s own quality: accepted by the "{name}" fallback', { name: r.acceptedBy.profileName })}>
                        {t('fallback: {name}', { name: r.acceptedBy.profileName })}
                      </span>
                    )}
                    {r.rejections && r.rejections.length > 0 && !r.acceptedBy?.fallback && (
                      <span className="badge conflict" style={{ marginLeft: 8 }} title={r.rejections.join('\n')}>
                        {r.rejections[0]}
                      </span>
                    )}
                  </td>
                  <td className="rel-fit">{qualityLabel?.(r) || [r.resolution, r.source, r.codec].filter(Boolean).join(' · ') || '—'}</td>
                  {showLanguage && (
                    <td className="rel-fit">
                      {r.language ? (
                        <span
                          className={r.languageFit === 'other' ? 'badge conflict' : 'lang-hint'}
                          title={
                            r.languageFit === 'other'
                              ? t('Automatic downloads skip releases in another language. You can still grab it yourself.')
                              : r.languageFit === 'mixed'
                                ? t('Has another language as well. Automatic downloads take a plain release first.')
                                : undefined
                          }
                        >
                          {r.language}
                        </span>
                      ) : (
                        '—'
                      )}
                    </td>
                  )}
                  {showScore && (
                    <td className={`rel-fit format-score-cell${(r.formatScore ?? 0) < 0 ? ' neg' : ''}`} title={r.formats?.length ? r.formats.join('\n') : t('No custom format matches')}>
                      {r.formatScore === undefined ? '—' : r.formatScore > 0 ? `+${r.formatScore}` : r.formatScore}
                    </td>
                  )}
                  <td className="rel-fit">
                    <span className="badge" title={r.protocol}>
                      {r.protocol === 'torrent' ? '⇅' : '⇩'} {r.indexerName}
                    </span>
                  </td>
                  <td className="rel-fit">{formatBytes(r.sizeBytes)}</td>
                  <td className="rel-fit" title={r.publishDate && publishedAt(r) ? new Date(publishedAt(r)).toLocaleString(locale()) : undefined}>{ageOf(r) || '—'}</td>
                  <td className="rel-fit">{r.protocol === 'torrent' ? `${r.seeders ?? 0} / ${r.peers ?? 0}` : '—'}</td>
                  <td className="rel-fit" style={{ whiteSpace: 'nowrap' }}>
                    {onStream && r.protocol !== 'torrent' && (
                      // stream hook (home fork): watch this release now
                      <button className="btn-sm" style={{ marginRight: 6 }} disabled={streaming === r.downloadUrl} onClick={() => onStream(r)} title={t('Watch this release now without downloading it')}>
                        {streaming === r.downloadUrl ? t('Starting…') : t('Stream')}
                      </button>
                    )}
                    <button className="primary" disabled={grabbing !== null} onClick={() => onGrab(r)}>
                      {grabbing === r.downloadUrl ? t('Grabbing…') : t('Grab')}
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </>
  )
}
