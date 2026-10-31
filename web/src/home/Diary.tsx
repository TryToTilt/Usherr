// Home fork: the Diary, on the You page. What you watched and when, newest
// first, by month: from Jellyfin by itself, logged by hand, or imported
// from Letterboxd or Trakt. Rate with stars, write a note, take an entry out.
import Dropdown from '../components/Dropdown' // Usherr
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { Link } from 'react-router-dom'
import { api, type TitleResult } from '../api'
import Icon from '../components/Icon'
import { currentLanguage, plural, t } from '../i18n'
import Stars from './Stars'
import './news.css'

interface Entry {
  id: number
  kind: 'movie' | 'tv'
  tmdbId: number
  title: string
  year?: number
  image?: string
  watchedOn: string
  dated: boolean
  episodes?: number
  episode?: string
  runtime?: number
  stars?: number
  note?: string
  rewatch?: boolean
  source: 'jellyfin' | 'manual' | 'letterboxd' | 'trakt'
}
interface ImportState {
  running: boolean
  source?: string
  done: number
  total: number
  added: number
  unmatched?: string[]
  missed?: number
  error?: string
  finished?: string
}

async function send<T>(path: string, init?: RequestInit): Promise<T> {
  const r = await fetch(path, { credentials: 'include', ...init })
  const d = await r.json().catch(() => ({}))
  if (!r.ok) throw new Error((d as { error?: string }).error || r.statusText)
  return d as T
}
const json = (method: string, body: unknown): RequestInit => ({ method, headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) })

const SOURCE: Record<Entry['source'], () => string> = {
  jellyfin: () => t('From Jellyfin'),
  manual: () => t('Logged by hand'),
  letterboxd: () => t('From Letterboxd'),
  trakt: () => t('From Trakt'),
}

function today(): string {
  const d = new Date()
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
}

function monthTitle(day: string): string {
  const [y, m] = day.split('-').map(Number)
  return new Date(y, m - 1, 1).toLocaleDateString(currentLanguage(), { month: 'long', year: 'numeric' })
}

function dayLabel(day: string): { n: string; w: string } {
  const [y, m, d] = day.split('-').map(Number)
  return { n: String(d), w: new Date(y, m - 1, d).toLocaleDateString(currentLanguage(), { weekday: 'short' }) }
}

function Row({ e, onChange, onRemove }: { e: Entry; onChange: (e: Entry) => void; onRemove: (e: Entry) => void }) {
  const [editing, setEditing] = useState(false)
  const [note, setNote] = useState(e.note ?? '')
  const save = (patch: Partial<Entry>) => {
    const next = { ...e, ...patch }
    onChange(next)
    void send(`/api/home/diary/${e.id}`, json('PUT', { stars: next.stars ?? 0, note: next.note ?? '', rewatch: !!next.rewatch })).catch(() => onChange(e))
  }
  const day = dayLabel(e.watchedOn)
  const to = e.kind === 'tv' ? `/show/${e.tmdbId}` : `/title/${e.tmdbId}`
  return (
    <li className="diary-row" data-walk="diary-row">
      <span className="diary-day" title={e.dated ? undefined : t('No watch date: the day it was rated or logged')}>
        <strong>{day.n}</strong>
        <small>{day.w}</small>
      </span>
      <Link to={to} className="diary-poster" tabIndex={-1} aria-hidden="true">
        {e.image ? <img src={e.image} alt="" loading="lazy" /> : <Icon name={e.kind === 'tv' ? 'tv' : 'film'} size={18} />}
      </Link>
      <div className="diary-main">
        <div className="diary-title">
          <Link to={to}>{e.title}</Link>
          {e.year ? <span className="hint"> {e.year}</span> : null}
          {e.rewatch && (
            <span className="diary-tag" title={t('Watched again')}>
              <Icon name="refresh" size={12} /> {t('Again')}
            </span>
          )}
          {!e.dated && <span className="diary-tag">{t('No watch date')}</span>}
        </div>
        <div className="diary-meta hint">
          {e.kind === 'tv' && e.episodes ? `${plural(e.episodes, 'One episode', '{n} episodes')}${e.episode ? ` · ${e.episode}` : ''} · ` : ''}
          {SOURCE[e.source]()}
        </div>
        {editing ? (
          <div className="diary-note-edit">
            <textarea value={note} onChange={(x) => setNote(x.target.value)} rows={3} maxLength={2000} placeholder={t('What did you think?')} autoFocus />
            <div>
              <button
                className="btn-sm primary"
                onClick={() => {
                  save({ note })
                  setEditing(false)
                }}
              >
                {t('Save')}
              </button>
              <button className="btn-sm" onClick={() => (setNote(e.note ?? ''), setEditing(false))}>
                {t('Cancel')}
              </button>
            </div>
          </div>
        ) : e.note ? (
          <p className="diary-note">{e.note}</p>
        ) : null}
      </div>
      <div className="diary-side">
        <Stars value={e.stars ?? 0} onChange={(v) => save({ stars: v })} />
        <div className="diary-actions">
          <button className="icon-btn" onClick={() => setEditing(true)} title={e.note ? t('Change the note') : t('Write a note')} aria-label={e.note ? t('Change the note') : t('Write a note')}>
            <Icon name="edit" size={14} />
          </button>
          <button className="icon-btn" onClick={() => onRemove(e)} title={t('Take it out of the Diary')} aria-label={t('Take it out of the Diary')}>
            <Icon name="x" size={14} />
          </button>
        </div>
      </div>
    </li>
  )
}

function LogDialog({ onClose, onLogged }: { onClose: () => void; onLogged: () => void }) {
  const [q, setQ] = useState('')
  const [found, setFound] = useState<TitleResult[]>([])
  const [pick, setPick] = useState<TitleResult | null>(null)
  const [day, setDay] = useState(today())
  const [stars, setStars] = useState(0)
  const [note, setNote] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  useEffect(() => {
    if (q.trim().length < 2) {
      setFound([])
      return
    }
    const ctl = new AbortController()
    const id = window.setTimeout(() => {
      api
        .discoverSearch(q.trim(), ctl.signal)
        .then((r) => setFound(r.slice(0, 8)))
        .catch(() => undefined)
    }, 300)
    return () => (window.clearTimeout(id), ctl.abort())
  }, [q])
  const log = () => {
    if (!pick) return
    setBusy(true)
    setError('')
    send('/api/home/diary', json('POST', { kind: pick.kind, tmdbId: pick.tmdbId, watchedOn: day, stars, note }))
      .then(onLogged)
      .catch((e: Error) => setError(e.message))
      .finally(() => setBusy(false))
  }
  return createPortal(
    <div className="modal-backdrop" onMouseDown={(e) => e.target === e.currentTarget && onClose()} onKeyDown={(e) => e.key === 'Escape' && onClose()}>
      <div className="modal roulette" role="dialog" aria-modal="true" aria-label={t('Log something')}>
        <div className="roulette-head">
          <h2>
            <Icon name="plus" size={18} /> {t('Log something')}
          </h2>
          <button className="icon-btn" onClick={onClose} aria-label={t('Close')}>
            <Icon name="x" size={16} />
          </button>
        </div>
        {pick ? (
          <div className="diary-picked">
            {pick.posterUrl ? <img src={pick.posterUrl} alt="" /> : null}
            <div>
              <strong>{pick.title}</strong> {pick.year ? <span className="hint">{pick.year}</span> : null}
              <div>
                <button className="btn-link" onClick={() => setPick(null)}>
                  {t('Choose another')}
                </button>
              </div>
            </div>
          </div>
        ) : (
          <>
            <input type="search" value={q} onChange={(e) => setQ(e.target.value)} placeholder={t('A film or show')} aria-label={t('A film or show')} autoFocus />
            <ul className="diary-found">
              {found.map((r) => (
                <li key={`${r.kind}:${r.tmdbId}`}>
                  <button type="button" onClick={() => setPick(r)}>
                    {r.posterUrl ? <img src={r.posterUrl} alt="" /> : <Icon name={r.kind === 'tv' ? 'tv' : 'film'} size={16} />}
                    <span>
                      {r.title} {r.year ? <span className="hint">{r.year}</span> : null}
                    </span>
                    <span className="hint">{r.kind === 'tv' ? t('Show') : t('Film')}</span>
                  </button>
                </li>
              ))}
            </ul>
          </>
        )}
        <label className="diary-field">
          <span>{t('Watched on')}</span>
          <input type="date" value={day} max={today()} onChange={(e) => setDay(e.target.value)} />
        </label>
        <div className="diary-field">
          <span>{t('Stars')}</span>
          <Stars value={stars} onChange={setStars} size={22} />
        </div>
        <textarea value={note} onChange={(e) => setNote(e.target.value)} rows={3} maxLength={2000} placeholder={t('What did you think? (optional)')} />
        {error && <p className="error-text">{error}</p>}
        <div className="roulette-nav">
          <button className="primary" onClick={log} disabled={!pick || !day || busy}>
            {busy ? t('Saving…') : t('Add to the Diary')}
          </button>
        </div>
      </div>
    </div>,
    document.body,
  )
}

function ImportDialog({ onClose, onStarted }: { onClose: () => void; onStarted: () => void }) {
  const [user, setUser] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const file = useRef<HTMLInputElement>(null)
  const start = (p: Promise<unknown>) => {
    setBusy(true)
    setError('')
    p.then(onStarted)
      .catch((e: Error) => setError(e.message))
      .finally(() => setBusy(false))
  }
  const letterboxd = () => {
    const f = file.current?.files?.[0]
    if (!f) return
    const body = new FormData()
    body.append('file', f)
    start(send('/api/home/diary/import/letterboxd', { method: 'POST', body }))
  }
  return createPortal(
    <div className="modal-backdrop" onMouseDown={(e) => e.target === e.currentTarget && onClose()} onKeyDown={(e) => e.key === 'Escape' && onClose()}>
      <div className="modal roulette" role="dialog" aria-modal="true" aria-label={t('Bring your history in')}>
        <div className="roulette-head">
          <h2>
            <Icon name="download" size={18} /> {t('Bring your history in')}
          </h2>
          <button className="icon-btn" onClick={onClose} aria-label={t('Close')}>
            <Icon name="x" size={16} />
          </button>
        </div>
        <section className="diary-import">
          <h3>Letterboxd</h3>
          <p className="hint">
            {t(
              'On letterboxd.com: Settings → Import & Export → Export your data. Choose the zip it gives you (or its diary.csv). Your diary, stars, reviews and liked films come in; each film is found on TMDB by its name and year.',
            )}
          </p>
          <div className="diary-import-row">
            <input ref={file} type="file" accept=".zip,.csv,application/zip,text/csv" aria-label={t('The Letterboxd export')} />
            <button className="primary" onClick={letterboxd} disabled={busy}>
              {t('Import')}
            </button>
          </div>
        </section>
        <section className="diary-import">
          <h3>Trakt</h3>
          <p className="hint">
            {t('Your Trakt user name. Your profile has to be public while it imports (Trakt → Settings → Account); you can make it private again afterwards. Your watch history and ratings come in.')}
          </p>
          <div className="diary-import-row">
            <input type="text" value={user} onChange={(e) => setUser(e.target.value)} placeholder={t('Trakt user name')} aria-label={t('Trakt user name')} />
            <button className="primary" onClick={() => start(send('/api/home/diary/import/trakt', json('POST', { user })))} disabled={busy || !user.trim()}>
              {t('Import')}
            </button>
          </div>
        </section>
        {error && <p className="error-text">{error}</p>}
      </div>
    </div>,
    document.body,
  )
}

export default function Diary() {
  const [entries, setEntries] = useState<Entry[] | null>(null)
  const [years, setYears] = useState<string[]>([])
  const [media, setMedia] = useState(true)
  const [year, setYear] = useState('')
  const [kind, setKind] = useState<'' | 'movie' | 'tv'>('')
  const [error, setError] = useState('')
  const [logging, setLogging] = useState(false)
  const [importing, setImporting] = useState(false)
  const [imp, setImp] = useState<ImportState | null>(null)

  const load = useCallback(() => {
    const q = new URLSearchParams()
    if (year) q.set('year', year)
    if (kind) q.set('kind', kind)
    send<{ entries: Entry[]; years: string[]; media: boolean }>(`/api/home/diary?${q}`)
      .then((d) => {
        setEntries(d.entries)
        setYears(d.years)
        setMedia(d.media)
      })
      .catch((e: Error) => setError(e.message))
  }, [year, kind])
  useEffect(load, [load])

  // An import's progress, while one runs.
  const watchImport = useCallback(() => {
    let stop = false
    const tick = () =>
      send<ImportState>('/api/home/diary/import')
        .then((s) => {
          // A finished import is shown for half an hour, until closed.
          const recent = s.running || (s.finished && Date.now() - Date.parse(s.finished) < 30 * 60_000)
          setImp(s.source && recent ? s : null)
          if (s.running && !stop) window.setTimeout(tick, 1500)
          else if (s.finished) load()
        })
        .catch(() => undefined)
    void tick()
    return () => {
      stop = true
    }
  }, [load])
  useEffect(() => watchImport(), [watchImport])

  const months = useMemo(() => {
    const out: { key: string; list: Entry[] }[] = []
    for (const e of entries ?? []) {
      const key = e.watchedOn.slice(0, 7)
      if (out[out.length - 1]?.key !== key) out.push({ key, list: [] })
      out[out.length - 1].list.push(e)
    }
    return out
  }, [entries])

  const change = (e: Entry) => setEntries((l) => (l ?? []).map((x) => (x.id === e.id ? e : x)))
  const remove = (e: Entry) => {
    setEntries((l) => (l ?? []).filter((x) => x.id !== e.id))
    void send(`/api/home/diary/${e.id}`, { method: 'DELETE' }).catch(load)
  }

  return (
    <section className="card you-card diary">
      <div className="diary-bar">
        {/* Usherr: one slim row: the kind and the year as drop-downs */}
        <Dropdown icon="grid" label={t('Kind')} value={kind} onChange={(v) => setKind(v as typeof kind)} options={[{ value: '', label: t('Everything') }, { value: 'movie', label: t('Films') }, { value: 'tv', label: t('Shows') }]} />
        {years.length > 0 && (
          <Dropdown icon="calendar" label={t('Year')} value={year} onChange={setYear} options={[{ value: '', label: t('Every year') }, ...years.map((y) => ({ value: String(y), label: String(y) }))]} />
        )}
        <span className="spacer" />
        <div className="diary-buttons" data-walk="diary-buttons">
          <button className="btn-sm btn-with-icon" onClick={() => setImporting(true)} aria-label={t('Import')} title={t('Import')}>
            <Icon name="download" size={14} /> <span className="cbar-add-label">{t('Import')}</span>
          </button>
          <button className="btn-sm primary btn-with-icon" onClick={() => setLogging(true)} aria-label={t('Log something')} title={t('Log something')}>
            <Icon name="plus" size={14} /> <span className="cbar-add-label">{t('Log something')}</span>
          </button>
        </div>
      </div>
      {imp && (
        <div className={`diary-import-state${imp.error ? ' failed' : ''}`} role="status">
          {!imp.running && (
            <button className="icon-btn diary-import-close" onClick={() => setImp(null)} aria-label={t('Close')}>
              <Icon name="x" size={14} />
            </button>
          )}
          {imp.error ? (
            <p>{imp.error}</p>
          ) : imp.running ? (
            <p>
              {imp.source === 'trakt' ? t('Importing from Trakt…') : t('Importing from Letterboxd…')} {imp.total > 0 ? t('{done} of {total}', { done: imp.done, total: imp.total }) : ''}
            </p>
          ) : (
            <p>{plural(imp.added, 'Import finished: one entry added.', 'Import finished: {n} entries added.')}</p>
          )}
          {!imp.running && imp.missed ? (
            <details>
              <summary>{plural(imp.missed, 'One film couldn’t be found on TMDB', '{n} films couldn’t be found on TMDB')}</summary>
              <p className="hint">{(imp.unmatched ?? []).join(' · ')}</p>
            </details>
          ) : null}
        </div>
      )}
      {error && <p className="error-text">{error}</p>}
      {entries === null ? (
        <p className="hint">{t('Reading what you’ve watched…')}</p>
      ) : entries.length === 0 ? (
        <div className="diary-empty">
          <Icon name="calendar" size={32} />
          <p>{media ? t('Nothing yet. What you finish in Jellyfin shows up here by itself.') : t('Nothing yet. Log what you watch, or bring your history in from Letterboxd or Trakt.')}</p>
        </div>
      ) : (
        months.map((m) => (
          <div key={m.key} className="diary-month">
            <h3>{monthTitle(m.key + '-01')}</h3>
            <ul>
              {m.list.map((e) => (
                <Row key={e.id} e={e} onChange={change} onRemove={remove} />
              ))}
            </ul>
          </div>
        ))
      )}
      {logging && (
        <LogDialog
          onClose={() => setLogging(false)}
          onLogged={() => {
            setLogging(false)
            load()
          }}
        />
      )}
      {importing && (
        <ImportDialog
          onClose={() => setImporting(false)}
          onStarted={() => {
            setImporting(false)
            watchImport()
          }}
        />
      )}
    </section>
  )
}
