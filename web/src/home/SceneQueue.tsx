// Home fork: the Scenes page's own Queue. Scene downloads, their history,
// activity and blocklist live only here (shared pages never show them).
import { useCallback, useEffect, useState } from 'react'
import Countdown, { stepLabel } from '../components/Elapsed'
import Icon from '../components/Icon'
import { locale, t } from '../i18n'

interface QItem {
  id: number
  title: string
  subtitle?: string
  posterUrl?: string
  releaseTitle: string
  status: string
  progressPct: number
  stage?: string
  stageSince?: string
  stageEnds?: string
  sizeBytes: number
  error?: string
  addedAt: string
  completedAt?: string
  queuePosition?: number
}

interface Act {
  id: number
  eventType: string
  message: string
  createdAt: string
}

interface Block {
  id: number
  releaseTitle: string
  reason: string
  createdAt?: string
}

async function api<T>(method: string, path: string): Promise<T> {
  const res = await fetch('/api' + path, { method, credentials: 'include', cache: 'no-store' })
  const text = await res.text()
  const data = text ? JSON.parse(text) : null
  if (!res.ok) throw new Error(data?.error ?? t('Something went wrong (error {status}).', { status: res.status }))
  return data as T
}

// A download's status in words.
const statusText = (s: string): string =>
  ({ queued: t('Waiting'), downloading: t('Downloading'), importing: t('Importing'), paused: t('Paused'), conflict: t('Needs a decision'), completed: t('Downloaded'), failed: t('Failed') })[s] ?? s

const active = (s: string) => ['queued', 'downloading', 'importing', 'paused', 'conflict'].includes(s)

function size(b: number) {
  if (!b) return ''
  return b >= 1e9 ? `${(b / 1e9).toFixed(1)} GB` : `${Math.round(b / 1e6)} MB`
}

function when(at?: string) {
  if (!at) return ''
  const d = new Date(at)
  return isNaN(d.getTime()) ? '' : d.toLocaleString(locale(), { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })
}

export default function SceneQueue() {
  const [items, setItems] = useState<QItem[] | null>(null)
  const [acts, setActs] = useState<Act[]>([])
  const [blocks, setBlocks] = useState<Block[]>([])
  const [error, setError] = useState('')
  const [view, setView] = useState<'queue' | 'activity' | 'blocklist'>('queue')

  const load = useCallback(() => {
    api<QItem[]>('GET', '/queue?scenes=only')
      .then(setItems)
      .catch((e: Error) => setError(e.message))
  }, [])
  useEffect(() => {
    load()
    const timer = window.setInterval(load, 3000)
    return () => window.clearInterval(timer)
  }, [load])
  useEffect(() => {
    if (view === 'activity') api<Act[] | { entries: Act[] }>('GET', '/activity?scenes=only&limit=500').then((r) => setActs(Array.isArray(r) ? r : r.entries ?? [])).catch((e: Error) => setError(e.message))
    if (view === 'blocklist') api<Block[] | { entries: Block[] }>('GET', '/blocklist?scenes=only').then((r) => setBlocks(Array.isArray(r) ? r : r.entries ?? [])).catch((e: Error) => setError(e.message))
  }, [view])

  const act = (method: string, path: string) =>
    api(method, path)
      .then(load)
      .catch((e: Error) => setError(e.message))

  const running = (items ?? []).filter((i) => active(i.status))
  const done = (items ?? []).filter((i) => !active(i.status)).slice(0, 50)

  return (
    <section className="sc-queue">
      <div className="seg sc-subtabs" role="tablist" aria-label={t('Scene downloads')}>
        {(['queue', 'activity', 'blocklist'] as const).map((v) => (
          <button key={v} role="tab" aria-selected={view === v} className={view === v ? 'active' : ''} onClick={() => setView(v)}>
            {v === 'queue' ? t('Downloads') : v === 'activity' ? t('Activity') : t('Blocklist')}
          </button>
        ))}
      </div>
      {error && <p className="error-text">{error}</p>}

      {view === 'queue' && (
        <>
          <h2 className="rail-head">{t('In progress ({n})', { n: running.length })}</h2>
          {items === null ? (
            <div className="skeleton" style={{ height: 80 }} />
          ) : running.length === 0 ? (
            <p className="hint">{t('Nothing downloading right now.')}</p>
          ) : (
            <ul className="sc-qlist">
              {running.map((i) => (
                <li key={i.id}>
                  <div className="sc-qmain">
                    <strong>{i.title}</strong>
                    <span className="hint">
                      {i.stage
                        ? stepLabel(i, i.sizeBytes ? size(i.sizeBytes) : '')
                        : `${i.status === 'queued' && i.queuePosition ? t('Waiting in line (#{n})', { n: i.queuePosition }) : statusText(i.status)}${i.sizeBytes && i.status !== 'queued' ? ` · ${t('{pct}% of {size}', { pct: Math.round(i.progressPct), size: size(i.sizeBytes) })}` : ''}`}
                      {i.stage && <Countdown ends={i.stageEnds} prefix=" · " />}
                    </span>
                    <div className="bar">
                      <span style={{ width: `${Math.min(100, i.progressPct)}%` }} />
                    </div>
                    <span className="sc-release">{i.releaseTitle}</span>
                  </div>
                  <div className="sc-actions">
                    {i.status === 'paused' ? (
                      <button className="btn-sm" onClick={() => act('POST', `/queue/${i.id}/resume`)}>
                        <Icon name="play" size={14} /> {t('Resume')}
                      </button>
                    ) : (
                      <button className="btn-sm" onClick={() => act('POST', `/queue/${i.id}/pause`)}>
                        <Icon name="pause" size={14} /> {t('Pause')}
                      </button>
                    )}
                    <button className="btn-sm" onClick={() => act('POST', `/queue/${i.id}/stop`)}>
                      <Icon name="stop" size={14} /> {t('Stop')}
                    </button>
                  </div>
                </li>
              ))}
            </ul>
          )}
          <h2 className="rail-head">{t('Recently finished')}</h2>
          {done.length === 0 ? (
            <p className="hint">{t('No finished scene downloads yet.')}</p>
          ) : (
            <ul className="sc-qlist">
              {done.map((i) => (
                <li key={i.id}>
                  <div className="sc-qmain">
                    <strong>{i.title}</strong>
                    <span className={i.status === 'failed' ? 'error-text' : 'hint'}>
                      {statusText(i.status)} · {when(i.completedAt || i.addedAt)}
                      {i.error ? ` · ${i.error}` : ''}
                    </span>
                    <span className="sc-release">{i.releaseTitle}</span>
                  </div>
                  <div className="sc-actions">
                    {i.status === 'failed' && (
                      <button className="btn-sm" onClick={() => act('POST', `/queue/${i.id}/retry`)}>
                        <Icon name="refresh" size={14} /> {t('Retry')}
                      </button>
                    )}
                    <button className="btn-sm" onClick={() => act('DELETE', `/queue/${i.id}`)} title={t('Remove from this list (files in your library are never touched)')}>
                      <Icon name="x" size={14} /> {t('Remove')}
                    </button>
                  </div>
                </li>
              ))}
            </ul>
          )}
        </>
      )}

      {view === 'activity' &&
        (acts.length === 0 ? (
          <p className="hint">{t('No scene activity yet.')}</p>
        ) : (
          <ul className="sc-qlist sc-acts">
            {acts.map((a) => (
              <li key={a.id}>
                <span className="hint">{when(a.createdAt)}</span> <span>{a.message}</span>
              </li>
            ))}
          </ul>
        ))}

      {view === 'blocklist' &&
        (blocks.length === 0 ? (
          <p className="hint">{t('No scene releases are blocklisted.')}</p>
        ) : (
          <ul className="sc-qlist">
            {blocks.map((b) => (
              <li key={b.id}>
                <div className="sc-qmain">
                  <span className="sc-release">{b.releaseTitle}</span>
                  <span className="hint">
                    {b.reason} · {when(b.createdAt)}
                  </span>
                </div>
                <button className="btn-sm" onClick={() => api('DELETE', `/blocklist/${b.id}`).then(() => setBlocks((l) => l.filter((x) => x.id !== b.id))).catch((e: Error) => setError(e.message))}>
                  {t('Allow again')}
                </button>
              </li>
            ))}
          </ul>
        ))}
    </section>
  )
}
