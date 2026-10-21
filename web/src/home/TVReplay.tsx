// Usherr: Real TV. A real network's day, rebuilt as Live TV channels: the
// guide for a zip code and a day, each program as it aired when it can be
// played, and the AI's choice in its spirit when it can't.
import { useEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import Icon from '../components/Icon'
import { t } from '../i18n'
import { useFocusTrap } from '../useFocusTrap'

interface Slot {
  at: string
  minutes: number
  was?: string
  exact?: boolean
  title: string
  episodeTitle?: string
  kind: string
  new?: boolean
  plays?: string
  from?: string
  checked?: string
  why?: string
  backupName?: string
  [k: string]: unknown
}
interface Network {
  network: string
  callSign: string
  number: string
  slots: Slot[]
}
interface Job {
  step: string
  done: boolean
  error?: string
  date: string
  zip: string
  lineup?: string
  networks?: Network[]
  searches: number
}

interface Provider {
  id: string
  type: string
  name: string
}
interface LineupChannel {
  id: string
  callSign: string
  name: string
  number: string
  category: string
  network?: string
  programs: number
}
const MAIN_NETWORKS = ['NBC', 'ABC', 'CBS', 'FOX']
const MAX_CHANNELS = 40
const CATEGORY_LABEL: Record<string, string> = { Local: t('Local'), Entertainment: t('Entertainment'), News: t('News'), Sports: t('Sports'), Movies: t('Movies'), 'Kids & Family': t('Kids & Family') }
const ZIP_KEY = 'usherr.realtv.zip'

async function call<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(`/api/tv${path}`, { method, credentials: 'include', headers: body ? { 'Content-Type': 'application/json' } : undefined, body: body ? JSON.stringify(body) : undefined })
  const data = await res.json().catch(() => null)
  if (!res.ok) throw new Error(data?.error ?? t('Something went wrong (error {status}).', { status: res.status }))
  return data as T
}

const yesterday = () => {
  const d = new Date(Date.now() - 86400000)
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
}
const clock = (at: string) => {
  const [h, m] = at.split(':').map(Number)
  return new Date(2000, 0, 1, h, m).toLocaleTimeString(undefined, { hour: 'numeric', minute: '2-digit' })
}

export function RealTVButton({ folder, onMade }: { folder?: string; onMade: () => void }) {
  const [open, setOpen] = useState(false)
  return (
    <>
      <button type="button" className="btn-with-icon" onClick={() => setOpen(true)} title={t('A real network’s day, rebuilt as channels')}>
        <Icon name="tv" size={16} /> {t('Real TV')}
      </button>
      {open && createPortal(<RealTVDialog folder={folder} onClose={() => setOpen(false)} onMade={onMade} />, document.body)}
    </>
  )
}

function RealTVDialog({ folder: folderIn, onClose, onMade }: { folder?: string; onClose: () => void; onMade: () => void }) {
  const box = useRef<HTMLDivElement>(null)
  useFocusTrap(box)
  const [zip, setZip] = useState(() => {
    try {
      return localStorage.getItem(ZIP_KEY) ?? ''
    } catch {
      return ''
    }
  })
  const [date, setDate] = useState(yesterday)
  // The lineup to pick from (over the air, or a cable or satellite provider), its channels, and the ones ticked.
  const [lineup, setLineup] = useState('')
  const [providers, setProviders] = useState<Provider[]>([])
  const [channels, setChannels] = useState<LineupChannel[] | null>(null)
  const [loadingLineup, setLoadingLineup] = useState(false)
  const [picked, setPicked] = useState<string[]>([])
  const [find, setFind] = useState('')
  const [cat, setCat] = useState('')
  const [check, setCheck] = useState(false)
  const [folder, setFolder] = useState(folderIn ?? '')
  const [jobId, setJobId] = useState('')
  const [job, setJob] = useState<Job | null>(null)
  const [error, setError] = useState('')
  const [making, setMaking] = useState(false)
  // Usherr: the nightly refresh, now.
  const [refreshNote, setRefreshNote] = useState('')
  const [refreshing, setRefreshing] = useState(false)
  const refreshNow = () => {
    setRefreshing(true)
    call<{ message: string }>('POST', '/replay/refresh')
      .then((r) => setRefreshNote(r.message))
      .catch((e: Error) => setRefreshNote(e.message))
      .finally(() => setRefreshing(false))
  }
  useEffect(() => {
    if (!jobId) return
    const tm = window.setInterval(() => {
      call<Job>('GET', `/replay/${jobId}`)
        .then((j) => {
          setJob(j)
          if (j.done) window.clearInterval(tm)
        })
        .catch((e: Error) => {
          setError(e.message)
          window.clearInterval(tm)
        })
    }, 2000)
    return () => window.clearInterval(tm)
  }, [jobId])
  // The providers at a zip code, once it is complete.
  useEffect(() => {
    if (zip.length !== 5) return
    let live = true
    call<{ providers: Provider[] }>('GET', `/replay/providers?zip=${zip}`)
      .then((r) => live && setProviders(r.providers))
      .catch(() => live && setProviders([]))
    return () => {
      live = false
    }
  }, [zip])
  // The channels of the lineup that have programs that day. Over the air, the main networks start ticked.
  useEffect(() => {
    if (zip.length !== 5 || !date) {
      setChannels(null)
      return
    }
    let live = true
    setLoadingLineup(true)
    setError('')
    const timer = window.setTimeout(() => {
      call<{ channels: LineupChannel[] }>('GET', `/replay/lineup?zip=${zip}&date=${date}&lineup=${encodeURIComponent(lineup)}`)
        .then((r) => {
          if (!live) return
          setChannels(r.channels)
          setPicked(r.channels.filter((c) => c.network && MAIN_NETWORKS.includes(c.network)).map((c) => c.id))
        })
        .catch((e: Error) => {
          if (!live) return
          setChannels(null)
          setError(e.message)
        })
        .finally(() => live && setLoadingLineup(false))
    }, 500)
    return () => {
      live = false
      window.clearTimeout(timer)
    }
  }, [zip, date, lineup])
  const shown = (channels ?? []).filter((c) => (!cat || c.category === cat) && (!find.trim() || `${c.name} ${c.callSign} ${c.number}`.toLowerCase().includes(find.trim().toLowerCase())))
  const counts = (channels ?? []).reduce<Record<string, number>>((m, c) => ((m[c.category] = (m[c.category] ?? 0) + 1), m), {})
  const toggle = (id: string) => setPicked((l) => (l.includes(id) ? l.filter((x) => x !== id) : l.length >= MAX_CHANNELS ? l : [...l, id]))
  const start = () => {
    setError('')
    setJob(null)
    try {
      localStorage.setItem(ZIP_KEY, zip)
    } catch {
      // not kept
    }
    call<{ id: string }>('POST', '/replay', { zip, date, lineup, channels: picked, check })
      .then((r) => setJobId(r.id))
      .catch((e: Error) => setError(e.message))
  }
  const make = () => {
    if (!job?.networks) return
    setMaking(true)
    call('POST', '/replay/create', { date: job.date, zip: job.zip, lineup: job.lineup ?? '', folder, networks: job.networks })
      .then(() => {
        onMade()
        onClose()
      })
      .catch((e: Error) => setError(e.message))
      .finally(() => setMaking(false))
  }
  const busy = !!jobId && !job?.done && !error
  return (
    <div className="modal-backdrop" onMouseDown={(e) => e.target === e.currentTarget && !busy && onClose()}>
      <div ref={box} tabIndex={-1} className="modal rtv" role="dialog" aria-modal="true" aria-label={t('Real TV')}>
        <div className="modal-head">
          <h2>{t('Real TV')}</h2>
          <button className="icon-btn" onClick={onClose} aria-label={t('Close')}>
            <Icon name="x" />
          </button>
        </div>
        {!job?.networks && (
          <>
            <p className="hint">{t('A real network’s day, as it aired where you live: each show as it was when it can be played (from your library or streamed, so about a day behind), and something in its spirit for the news, sports and the rest.')}</p>
            <div className="rtv-refresh">
              <button type="button" className="btn-with-icon" onClick={refreshNow} disabled={refreshing}>
                <Icon name="refresh" size={16} /> {refreshing ? t('Asking…') : t('Refresh now')}
              </button>
              <small className="hint">{refreshNote || t('Your Real TV channels get yesterday each night on their own; this does it now. Choices made before are used again, so it rarely needs the AI.')}</small>
            </div>
            <div className="tv-field-row">
              <label className="tv-field">
                <span>{t('Zip code')}</span>
                <input inputMode="numeric" maxLength={5} value={zip} onChange={(e) => setZip(e.target.value.replace(/\D/g, ''))} placeholder="60611" />
              </label>
              <label className="tv-field">
                <span>{t('Day')}</span>
                <input type="date" value={date} onChange={(e) => setDate(e.target.value)} />
              </label>
            </div>
            {providers.length > 1 && (
              <label className="tv-field">
                <span>{t('Channels from')}</span>
                <select value={lineup} onChange={(e) => (setLineup(e.target.value), setFind(''), setCat(''))}>
                  {providers.map((p) => (
                    <option key={p.id} value={p.type === 'OTA' ? '' : p.id}>
                      {p.type === 'OTA' ? t('Local channels on the air') : p.name}
                    </option>
                  ))}
                </select>
              </label>
            )}
            {zip.length === 5 && loadingLineup && <p className="hint">{t('Getting the channels…')}</p>}
            {channels && !loadingLineup && (
              <div className="rtv-pick" role="group" aria-label={t('Channels')}>
                <div className="rtv-pick-bar">
                  <input type="search" value={find} onChange={(e) => setFind(e.target.value)} placeholder={t('Find a channel')} aria-label={t('Find a channel')} />
                  <span className="hint">{t('{n} picked (up to {max})', { n: picked.length, max: MAX_CHANNELS })}</span>
                  {channels.some((c) => c.network) && (
                    <button type="button" className="btn-sm" onClick={() => setPicked(channels.filter((c) => c.network).map((c) => c.id))}>
                      {t('Main networks')}
                    </button>
                  )}
                  <button type="button" className="btn-sm" onClick={() => setPicked((l) => Array.from(new Set([...l, ...shown.map((c) => c.id)])).slice(0, MAX_CHANNELS))}>
                    {t('Pick the ones shown')}
                  </button>
                  <button type="button" className="btn-sm" onClick={() => setPicked([])}>
                    {t('Clear')}
                  </button>
                </div>
                <div className="tv-chips" role="group" aria-label={t('Kinds of channel')}>
                  <button type="button" className={`btn-sm${cat === '' ? ' primary' : ''}`} aria-pressed={cat === ''} onClick={() => setCat('')}>
                    {t('All')} {channels.length}
                  </button>
                  {Object.keys(CATEGORY_LABEL)
                    .filter((k) => counts[k])
                    .map((k) => (
                      <button key={k} type="button" className={`btn-sm${cat === k ? ' primary' : ''}`} aria-pressed={cat === k} onClick={() => setCat(cat === k ? '' : k)}>
                        {CATEGORY_LABEL[k]} {counts[k]}
                      </button>
                    ))}
                </div>
                <ul className="rtv-list">
                  {shown.map((c) => (
                    <li key={c.id}>
                      <label>
                        <input type="checkbox" checked={picked.includes(c.id)} disabled={!picked.includes(c.id) && picked.length >= MAX_CHANNELS} onChange={() => toggle(c.id)} />
                        <span className="rtv-no">{c.number}</span>
                        <span className="rtv-name">
                          {c.name} {c.callSign.toLowerCase() !== c.name.toLowerCase() && <small className="hint">{c.callSign}</small>}
                        </span>
                        <span className="tv-tag">{CATEGORY_LABEL[c.category] ?? c.category}</span>
                      </label>
                    </li>
                  ))}
                  {shown.length === 0 && <li className="hint">{t('No channel matches.')}</li>}
                </ul>
              </div>
            )}
            <label className="tv-check">
              <input type="checkbox" checked={check} onChange={(e) => setCheck(e.target.checked)} />
              {t('Also check now which shows can stream (slower: a search per show). Without it, each show is looked for as it comes on, and its backup plays if it isn’t there yet.')}
            </label>
          </>
        )}
        {job && !job.networks && !job.error && (
          <p className="rtv-step">
            <span className="rtv-dot" aria-hidden="true" /> {job.step}…
          </p>
        )}
        {(error || job?.error) && <p className="error-text">{error || job?.error}</p>}
        {job?.networks && (
          <>
            <p className="hint">
              {t('{date} at {zip}.', { date: job.date, zip: job.zip })} {job.searches > 0 && t('{n} indexer searches used.', { n: job.searches })}
            </p>
            <div className="rtv-nets">
              {job.networks.map((n) => {
                const exact = n.slots.filter((s) => s.exact).length
                return (
                  <details key={n.network} className="rtv-net" open={job.networks!.length === 1}>
                    <summary>
                      <strong>
                        {n.network} · {n.callSign} {n.number}
                      </strong>{' '}
                      <span className="hint">{t('{exact} of {n} as aired', { exact, n: n.slots.length })}</span>
                    </summary>
                    <ul>
                      {n.slots.map((s, i) => (
                        <li key={i} className={s.exact ? 'exact' : s.plays ? 'stand' : 'none'}>
                          <span className="rtv-time">{clock(s.at)}</span>
                          <span className="rtv-was">
                            {s.title}
                            {s.episodeTitle && <small> · {s.episodeTitle}</small>}
                          </span>
                          <span className="rtv-plays">
                            {s.exact ? (
                              <>
                                <Icon name="check" size={12} /> {s.plays} <span className="tv-tag">{s.from === 'library' ? t('library') : t('stream')}</span>
                                {s.backupName && <small className="hint"> {t('backup: {name}', { name: s.backupName })}</small>}
                              </>
                            ) : s.plays ? (
                              <>
                                <Icon name="sparkles" size={12} /> {s.plays} <span className="tv-tag">{s.from === 'library' ? t('library') : t('stream')}</span>
                                {s.why && <small className="hint"> {s.why}</small>}
                              </>
                            ) : (
                              <span className="hint">{t('commercials and “Up next”')}</span>
                            )}
                            {s.checked === 'missing' && <small className="hint"> {t('(the aired show isn’t on Usenet yet)')}</small>}
                          </span>
                        </li>
                      ))}
                    </ul>
                  </details>
                )
              })}
            </div>
            <label className="tv-field">
              <span>{t('Commercials folder (fills each slot’s spare minutes)')}</span>
              <input type="text" value={folder} onChange={(e) => setFolder(e.target.value)} placeholder="/data/commercials" />
            </label>
          </>
        )}
        <div className="modal-foot">
          {job?.networks ? (
            <>
              <button className="btn-sm" onClick={() => (setJob(null), setJobId(''))}>
                {t('Start over')}
              </button>
              <button className="primary btn-with-icon" onClick={make} disabled={making}>
                <Icon name="plus" size={16} /> {making ? t('Making the channels…') : t('Make {n} channels', { n: job.networks.length })}
              </button>
            </>
          ) : (
            <button className="primary btn-with-icon" onClick={start} disabled={busy || zip.length !== 5 || !date || picked.length === 0 || loadingLineup}>
              <Icon name="sparkles" size={16} /> {busy ? t('Building…') : picked.length > 1 ? t('Build {n} channels', { n: picked.length }) : t('Build the day')}
            </button>
          )}
        </div>
      </div>
    </div>
  )
}
