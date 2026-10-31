// Home fork: story arcs (after Pullbox's story arcs). A reading list across
// series in ComicVine's reading order: what you have, what's wanted, what's
// missing, and "Get the whole arc" to fill it in.
import { useCallback, useEffect, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import Icon from '../components/Icon'
import { isAdmin } from '../api'
import { useAuth } from '../AuthContext'
import { ComicReader } from './Comics'
import { comicsApi, type ComicArc, type ComicArcIssue, type ComicIssue } from './comicsApi'
import LibraryTabs from './LibraryTabs'
import './comics.css'
import { t } from '../i18n'

function Progress({ a }: { a: ComicArc }) {
  const pct = a.total ? Math.round((a.have / a.total) * 100) : 0
  return (
    <div className="cx-arc-progress" title={t('{have} of {total} issues', { have: a.have, total: a.total })}>
      <span style={{ width: `${pct}%` }} />
    </div>
  )
}

// The Story arcs tab of the Comics page: your arcs, and (admins) finding more.
export function ComicArcList({ admin }: { admin: boolean }) {
  const nav = useNavigate()
  const [arcs, setArcs] = useState<ComicArc[] | null>(null)
  const [q, setQ] = useState('')
  const [found, setFound] = useState<ComicArc[] | null>(null)
  const [busy, setBusy] = useState(0)
  const [error, setError] = useState('')
  useEffect(() => {
    comicsApi
      .arcs()
      .then(setArcs)
      .catch((e: Error) => setError(e.message))
  }, [])
  const search = () => {
    setFound(null)
    setError('')
    comicsApi
      .lookupArcs(q)
      .then(setFound)
      .catch((e: Error) => setError(e.message))
  }
  const add = (a: ComicArc, want: boolean) => {
    setBusy(a.cvId)
    setError('')
    comicsApi
      .addArc(a.cvId, want)
      .then((r) => nav(`/comics/arc/${r.id}`))
      .catch((e: Error) => setError(e.message))
      .finally(() => setBusy(0))
  }
  const mine = new Set((arcs ?? []).map((a) => a.cvId))
  return (
    <div className="cx-arcs">
      {admin && (
        <form
          className="cx-arc-find"
          data-walk="cx-arc-find"
          onSubmit={(e) => {
            e.preventDefault()
            if (q.trim().length > 1) search()
          }}
        >
          <input value={q} onChange={(e) => setQ(e.target.value)} placeholder={t('Find a story arc: Civil War, Secret Wars, Knightfall…')} />
          <button className="primary" type="submit">
            <Icon name="search" size={15} /> {t('Find')}
          </button>
        </form>
      )}
      {error && <p className="error-text">{error}</p>}
      {found && (
        <ul className="cx-arc-grid" data-walk="cx-arc-results">
          {found.length === 0 && <p className="hint">{t('ComicVine has no story arc by that name.')}</p>}
          {found.map((a) => (
            <li key={a.cvId} className="card">
              {a.imageUrl ? <img src={a.imageUrl} alt="" loading="lazy" /> : <div className="cx-noimg"><Icon name="book" size={28} /></div>}
              <div>
                <strong>{a.name}</strong>
                <small className="hint">{a.publisher}</small>
                {a.description && <p className="cx-arc-desc">{a.description}</p>}
                {mine.has(a.cvId) ? (
                  <span className="hint">{t('In your story arcs')}</span>
                ) : (
                  <div className="cx-arc-actions">
                    <button className="btn-sm primary" disabled={busy === a.cvId} onClick={() => add(a, true)} title={t('Adds the arc and searches for every issue')}>
                      {busy === a.cvId ? t('Adding…') : t('Get the whole arc')}
                    </button>
                    <button className="btn-sm" disabled={busy === a.cvId} onClick={() => add(a, false)} title={t('Adds the reading list only')}>
                      {t('Just the list')}
                    </button>
                  </div>
                )}
              </div>
            </li>
          ))}
        </ul>
      )}
      {!found && arcs && arcs.length === 0 && <p className="hint">{admin ? t('No story arcs yet. Find one above.') : t('No story arcs yet.')}</p>}
      {!found && arcs && arcs.length > 0 && (
        <ul className="cx-arc-grid" data-walk="cx-arc-mine">
          {arcs.map((a) => (
            <li key={a.id} className="card">
              <Link to={`/comics/arc/${a.id}`} className="cx-arc-link">
                {a.imageUrl ? <img src={a.imageUrl} alt="" loading="lazy" /> : <div className="cx-noimg"><Icon name="book" size={28} /></div>}
                <div>
                  <strong>{a.name}</strong>
                  <small className="hint">
                    {a.publisher ? `${a.publisher} · ` : ''}
                    {t('{have} of {total} issues', { have: a.have, total: a.total })}
                    {a.wanted ? ` · ${t('{n} wanted', { n: a.wanted })}` : ''}
                  </small>
                  <Progress a={a} />
                  {a.busy && <small className="hint">{a.busy}…</small>}
                </div>
              </Link>
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}

// /comics/arc/:id: an arc's issues in reading order.
export function ComicArcPage() {
  const { id } = useParams()
  const nav = useNavigate()
  const admin = isAdmin(useAuth().user)
  const [data, setData] = useState<{ arc: ComicArc; issues: ComicArcIssue[] } | null>(null)
  const [error, setError] = useState('')
  const [note, setNote] = useState('')
  const [reading, setReading] = useState<ComicIssue | null>(null)
  const load = useCallback(() => {
    comicsApi
      .arc(Number(id))
      .then(setData)
      .catch((e: Error) => setError(e.message))
  }, [id])
  useEffect(load, [load])
  // While "get the whole arc" runs, keep the page up to date.
  useEffect(() => {
    if (!data?.arc.busy) return
    const timer = window.setInterval(load, 5000)
    return () => window.clearInterval(timer)
  }, [data?.arc.busy, load])

  const act = (p: Promise<{ message?: string }>) =>
    p
      .then((r) => {
        setNote(r.message ?? t('Done.'))
        load()
      })
      .catch((e: Error) => setError(e.message))

  if (!data) return <div className="page-pad">{error ? <p className="error-text">{error}</p> : <p className="hint">{t('Loading…')}</p>}</div>
  const { arc, issues } = data
  const read = (m: ComicArcIssue) =>
    setReading({ id: m.issueId!, seriesId: m.seriesId!, cvId: m.cvId, number: m.number, title: m.title ?? '', coverDate: m.coverDate ?? '', storeDate: m.storeDate ?? '', imageUrl: m.imageUrl ?? '', wanted: !!m.wanted, status: 'downloaded' })
  const nextToRead = issues.find((m) => m.status === 'downloaded' && !readDone(m.issueId!))
  return (
    <div className="page-pad cx-page">
      <div className="lib-tabs-row">
        <LibraryTabs current="comics" />
      </div>
      <p>
        <Link to="/comics?tab=arcs">‹ {t('Story arcs')}</Link>
      </p>
      <section className="card cx-arc-hero">
        {arc.imageUrl && <img src={arc.imageUrl} alt="" />}
        <div>
          <h1>{arc.name}</h1>
          <p className="hint">
            {arc.publisher ? `${arc.publisher} · ` : ''}
            {t('{have} of {total} issues', { have: arc.have, total: arc.total })}
            {arc.wanted ? ` · ${t('{n} wanted', { n: arc.wanted })}` : ''}
            {arc.downloading ? ` · ${t('{n} downloading', { n: arc.downloading })}` : ''}
          </p>
          <Progress a={arc} />
          {arc.description && <p className="cx-arc-desc">{arc.description}</p>}
          <div className="cx-arc-actions">
            {nextToRead && (
              <button className="btn-sm primary" data-walk="cx-arc-start" onClick={() => read(nextToRead)}>
                <Icon name="book" size={14} /> {issues.some((m) => m.status === 'downloaded' && readDone(m.issueId!)) ? t('Continue reading') : t('Start reading')}
              </button>
            )}
            {admin && arc.have + arc.downloading < arc.total && (
              <button className="btn-sm" disabled={!!arc.busy} onClick={() => act(comicsApi.wantArc(arc.id))}>
                <Icon name="download" size={14} /> {arc.busy ? `${arc.busy}…` : t('Get the whole arc')}
              </button>
            )}
            {admin && (
              <button className="btn-sm" onClick={() => act(comicsApi.refreshArc(arc.id))} title={t('Read the arc from ComicVine again')}>
                <Icon name="refresh" size={14} /> {t('Refresh')}
              </button>
            )}
            {admin && (
              <button
                className="btn-sm"
                onClick={() => {
                  if (window.confirm(t('Remove the story arc "{name}"? Its series, issues and files stay.', { name: arc.name })))
                    comicsApi.removeArc(arc.id).then(() => nav('/comics?tab=arcs')).catch((e: Error) => setError(e.message))
                }}
              >
                {t('Remove')}
              </button>
            )}
          </div>
        </div>
      </section>
      {note && <p className="notice notice-info cx-note">{note}</p>}
      {error && <p className="error-text">{error}</p>}
      <ol className="cx-arc-issues" data-walk="cx-arc-order">
        {issues.map((m) => (
          <li key={m.position} className={m.status === 'downloaded' ? 'have' : ''}>
            <span className="cx-arc-pos">{m.position}</span>
            {m.imageUrl ? <img src={m.imageUrl} alt="" loading="lazy" /> : <span className="cx-noimg small" />}
            <div className="cx-arc-text">
              {m.seriesId ? <Link to={`/comics/${m.seriesId}`}>{m.series}</Link> : <span>{m.series}</span>} #{m.number}
              <small className="hint">
                {m.title ? `${m.title} · ` : ''}
                {(m.coverDate || m.storeDate || '').slice(0, 7)}
              </small>
            </div>
            <span className={`cx-arc-state ${m.status || 'none'}`}>
              {m.status === 'downloaded' ? (
                <button className="btn-sm primary" onClick={() => read(m)}>
                  <Icon name="book" size={13} /> {readDone(m.issueId!) ? t('Read again') : t('Read')}
                </button>
              ) : m.status === 'downloading' ? (
                t('Downloading')
              ) : m.wanted ? (
                t('Wanted')
              ) : (
                t('Missing')
              )}
            </span>
          </li>
        ))}
      </ol>
      {reading && (
        <ComicReader
          issue={reading}
          onClose={() => {
            setReading(null)
            load()
          }}
        />
      )}
    </div>
  )
}

// readDone: the reader has reached the issue's last page (the reader keeps
// its place in this browser).
function readDone(issueId: number): boolean {
  try {
    return localStorage.getItem(`comic-done-${issueId}`) === '1'
  } catch {
    return false
  }
}
