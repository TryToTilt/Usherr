import { useCallback, useEffect, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { api, isAdmin, type WatchCount, type WatchHistoryPage, type WatchHistoryQuery, type WatchSession, type WatchSettings, type WatchStatsData } from '../api'
import { useAuth } from '../AuthContext'
import { useConfirm } from '../components/ConfirmProvider'
import Icon from '../components/Icon'
import Loading from '../components/Loading'
import TimeChart from '../components/TimeChart'
import { useToast } from '../components/Toast'
import { timeAgo } from '../format'
import { locale, plural, t } from '../i18n'
import { useLive } from '../useLive'
import '../stats.css'
import './watchActivity.css'

// Usherr: Settings > System > Watching. What Plex, Jellyfin and Emby are
// playing now, the history of every playback, and the numbers for a period.
// An administrator sees everyone; a member sees their own unless the
// administrator lets members see everyone's. It does the job of Streamystats.

type Tab = 'now' | 'history' | 'stats' | 'settings'

const errText = (e: unknown) => (e instanceof Error ? e.message : String(e))

const METHOD_LABEL: Record<string, string> = {
  DirectPlay: t('Direct play'),
  DirectStream: t('Direct stream'),
  Transcode: t('Transcode'),
}

function methodClass(m?: string): string {
  return m === 'Transcode' ? 'transcode' : m === 'DirectStream' ? 'stream' : 'direct'
}

// "Severance · S1E3 · In Perpetuity" or "Arrival (2016)".
function watchTitle(s: Pick<WatchSession, 'kind' | 'title' | 'series' | 'season' | 'episode' | 'year'>): string {
  if (s.kind === 'episode') return `${s.series || t('A show')} · S${s.season ?? 0}E${s.episode ?? 0} · ${s.title}`
  return s.year ? `${s.title} (${s.year})` : s.title
}

function titleLink(s: Pick<WatchSession, 'kind' | 'libraryId' | 'tmdbId'>): string {
  if (s.kind === 'episode' && s.libraryId) return `/series/${s.libraryId}`
  if (s.kind === 'movie' && s.tmdbId) return `/title/${s.tmdbId}`
  return ''
}

function duration(sec: number): string {
  const h = Math.floor(sec / 3600)
  const m = Math.round((sec % 3600) / 60)
  if (h === 0) return plural(m, '{n} min', '{n} min')
  return t('{h} h {m} min', { h, m })
}

const hours = (h: number) => t('{n} h', { n: h.toLocaleString(locale(), { maximumFractionDigits: 1 }) })

function streamLine(label: string, from?: string, to?: string): string {
  if (!from && !to) return ''
  if (!to || from === to) return `${label} ${from}`
  return `${label} ${from || '?'} → ${to}`
}

function MethodBadge({ s }: { s: WatchSession }) {
  if (!s.playMethod) return null
  return <span className={`watch-method ${methodClass(s.playMethod)}`}>{METHOD_LABEL[s.playMethod] ?? s.playMethod}</span>
}

function NowCard({ s, everyone }: { s: WatchSession; everyone: boolean }) {
  const link = titleLink(s)
  const pct = Math.round(s.progress * 100)
  const details = [
    streamLine(t('Video'), s.videoIn, s.videoOut),
    s.resolutionIn ? (s.resolutionOut && s.resolutionOut !== s.resolutionIn ? `${s.resolutionIn} → ${s.resolutionOut}` : s.resolutionIn) : '',
    streamLine(t('Audio'), s.audioIn, s.audioOut),
    s.bandwidthKbps ? t('{n} Mbps', { n: (s.bandwidthKbps / 1000).toLocaleString(locale(), { maximumFractionDigits: 1 }) }) : '',
  ].filter(Boolean)
  return (
    <li className="watch-now-card">
      <div className="watch-now-head">
        <strong>{link ? <Link to={link}>{watchTitle(s)}</Link> : watchTitle(s)}</strong>
        <MethodBadge s={s} />
        {s.paused && <span className="badge">{t('Paused')}</span>}
      </div>
      <div className="watch-now-who">
        {everyone && s.user && (
          <span>
            <Icon name="user" size={13} /> {s.user}
          </span>
        )}
        <span>
          <Icon name="monitor" size={13} /> {[s.device, s.client].filter(Boolean).join(' · ') || t('Unknown player')}
        </span>
        <span>{s.server}</span>
        {s.location && <span>{s.location === 'lan' ? t('At home') : t('Away from home')}</span>}
        {s.address && <span className="watch-dim">{s.address}</span>}
      </div>
      <div className="watch-progress" aria-label={t('{pct}% through', { pct })}>
        <span style={{ width: `${pct}%` }} />
      </div>
      <div className="watch-now-meta">
        <span>{t('{pct}% through', { pct })}</span>
        <span>{t('Started {when}', { when: timeAgo(s.startedAt) })}</span>
        <span>{t('Watched {time}', { time: duration(s.watchedSec) })}</span>
      </div>
      {details.length > 0 && <div className="watch-now-meta watch-dim">{details.join(' · ')}</div>}
      {s.transcodeReasons && s.transcodeReasons.length > 0 && (
        <div className="watch-now-meta watch-reasons">
          {t('Why it is transcoded:')} {s.transcodeReasons.map((r) => t(r)).join(', ')}
        </div>
      )}
    </li>
  )
}

function NowPlaying() {
  const [data, setData] = useState<{ playing: WatchSession[]; everyone: boolean; recording: boolean } | null>(null)
  const [error, setError] = useState('')
  const load = useCallback(() => {
    api
      .watchNow()
      .then((d) => {
        setData(d)
        setError('')
      })
      .catch((e) => setError(errText(e)))
  }, [])
  useEffect(load, [load])
  useLive(load, 15_000)
  if (error && !data) return <p className="error-text">{error}</p>
  if (!data) return <Loading height={200} />
  return (
    <section className="card wide">
      <h2>{t('Now playing')}</h2>
      {!data.recording && <p className="field-hint">{t('Keeping the watch history is switched off, so Usherr is not asking the media servers.')}</p>}
      {data.playing.length === 0 ? (
        <p style={{ color: 'var(--text-dim)' }}>{data.everyone ? t('Nothing is playing on Plex, Jellyfin or Emby right now.') : t('You are not watching anything right now.')}</p>
      ) : (
        <ul className="watch-now">
          {data.playing.map((s) => (
            <NowCard key={s.id} s={s} everyone={data.everyone} />
          ))}
        </ul>
      )}
    </section>
  )
}

const PAGE_SIZE = 25

function History() {
  const [q, setQ] = useState<WatchHistoryQuery>({ page: 1, size: PAGE_SIZE, method: '' })
  const [data, setData] = useState<WatchHistoryPage | null>(null)
  const [error, setError] = useState('')
  useEffect(() => {
    let live = true
    const timer = setTimeout(() => {
      api
        .watchHistory(q)
        .then((d) => {
          if (!live) return
          setData(d)
          setError('')
        })
        .catch((e) => {
          if (live) setError(errText(e))
        })
    }, 250)
    return () => {
      live = false
      clearTimeout(timer)
    }
  }, [q])
  const set = (patch: Partial<WatchHistoryQuery>) => setQ((old) => ({ ...old, ...patch, page: patch.page ?? 1 }))
  const pages = data ? Math.max(1, Math.ceil(data.total / data.size)) : 1
  const everyone = data?.everyone ?? false
  return (
    <section className="card wide">
      <h2>{t('History')}</h2>
      <div className="watch-filters">
        <label>
          {t('Title')}
          <input value={q.q ?? ''} maxLength={100} placeholder={t('Any title')} onChange={(e) => set({ q: e.target.value })} />
        </label>
        {everyone && (
          <label>
            {t('Person')}
            <select value={q.user ?? ''} onChange={(e) => set({ user: e.target.value })}>
              <option value="">{t('Everyone')}</option>
              {(data?.choices.users ?? []).map((u) => (
                <option key={u} value={u}>
                  {u}
                </option>
              ))}
            </select>
          </label>
        )}
        {(data?.choices.servers ?? []).length > 1 && (
          <label>
            {t('Server')}
            <select value={q.server ?? 0} onChange={(e) => set({ server: Number(e.target.value) })}>
              <option value={0}>{t('Any server')}</option>
              {(data?.choices.servers ?? []).map((s) => (
                <option key={s.id} value={s.id}>
                  {s.name}
                </option>
              ))}
            </select>
          </label>
        )}
        <label>
          {t('Device or app')}
          <input value={q.device ?? ''} maxLength={100} placeholder={t('Any device')} onChange={(e) => set({ device: e.target.value })} />
        </label>
        <label>
          {t('How it played')}
          <select value={q.method ?? ''} onChange={(e) => set({ method: e.target.value as WatchHistoryQuery['method'] })}>
            <option value="">{t('Any way')}</option>
            <option value="direct">{t('Direct play')}</option>
            <option value="stream">{t('Direct stream')}</option>
            <option value="transcode">{t('Transcode')}</option>
          </select>
        </label>
        <label>
          {t('From')}
          <input type="date" value={q.from ?? ''} onChange={(e) => set({ from: e.target.value })} />
        </label>
        <label>
          {t('To')}
          <input type="date" value={q.to ?? ''} onChange={(e) => set({ to: e.target.value })} />
        </label>
      </div>
      {error && <p className="error-text">{error}</p>}
      {!data ? (
        <Loading height={200} />
      ) : data.items.length === 0 ? (
        <p style={{ color: 'var(--text-dim)' }}>{data.total === 0 && !q.q && !q.user && !q.device && !q.method && !q.from && !q.to ? t('Nothing watched yet. Playbacks on Plex, Jellyfin and Emby show up here.') : t('Nothing matches.')}</p>
      ) : (
        <>
          <div className="watch-table-wrap">
            <table className="watch-table">
              <thead>
                <tr>
                  <th>{t('When')}</th>
                  {everyone && <th>{t('Person')}</th>}
                  <th>{t('Title')}</th>
                  <th>{t('Device')}</th>
                  <th>{t('How')}</th>
                  <th>{t('Watched')}</th>
                </tr>
              </thead>
              <tbody>
                {data.items.map((s) => {
                  const link = titleLink(s)
                  return (
                    <tr key={s.id}>
                      <td title={new Date(s.startedAt).toLocaleString(locale())}>{new Date(s.startedAt).toLocaleString(locale(), { day: 'numeric', month: 'short', hour: 'numeric', minute: '2-digit' })}</td>
                      {everyone && <td>{s.user || '–'}</td>}
                      <td>
                        {link ? <Link to={link}>{watchTitle(s)}</Link> : watchTitle(s)}
                        {!s.endedAt && <span className="badge watch-live">{t('Playing')}</span>}
                      </td>
                      <td>
                        {s.device || '–'}
                        <div className="watch-dim">{[s.client, s.server, s.location === 'lan' ? t('At home') : s.location === 'remote' ? t('Away from home') : ''].filter(Boolean).join(' · ')}</div>
                      </td>
                      <td>
                        <MethodBadge s={s} />
                        {s.transcodeReasons && s.transcodeReasons.length > 0 && <div className="watch-dim">{s.transcodeReasons.map((r) => t(r)).join(', ')}</div>}
                      </td>
                      <td>
                        {duration(s.watchedSec)}
                        <div className="watch-dim">{s.completed ? t('Finished') : t('{pct}% through', { pct: Math.round(s.progress * 100) })}</div>
                      </td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>
          <div className="watch-pager">
            <button type="button" className="btn-sm" disabled={(q.page ?? 1) <= 1} onClick={() => set({ page: (q.page ?? 1) - 1 })}>
              {t('Newer')}
            </button>
            <span>{t('Page {page} of {pages} · {total}', { page: q.page ?? 1, pages, total: plural(data.total, '{n} playback', '{n} playbacks') })}</span>
            <button type="button" className="btn-sm" disabled={(q.page ?? 1) >= pages} onClick={() => set({ page: (q.page ?? 1) + 1 })}>
              {t('Older')}
            </button>
          </div>
        </>
      )}
    </section>
  )
}

function BarList({ items, empty, link }: { items: WatchCount[]; empty: string; link?: (c: WatchCount) => string }) {
  const most = Math.max(0.1, ...items.map((c) => c.hours))
  if (items.length === 0) return <p style={{ color: 'var(--text-dim)' }}>{empty}</p>
  return (
    <div className="bar-list">
      {items.map((c) => {
        const to = link?.(c) ?? ''
        return (
          <div key={c.name} className="bar-row">
            {to ? (
              <Link className="bar-label" to={to} title={c.name}>
                {c.name}
              </Link>
            ) : (
              <span className="bar-label" title={c.name}>
                {c.name || t('Unknown')}
              </span>
            )}
            <span className="bar-track">
              <span className="bar-fill" style={{ width: `${(c.hours * 100) / most}%` }} />
            </span>
            <span className="bar-value">
              {hours(c.hours)} · {plural(c.plays, '{n} play', '{n} plays')}
              {c.episodes ? ` · ${plural(c.episodes, '{n} episode', '{n} episodes')}` : ''}
            </span>
          </div>
        )
      })}
    </div>
  )
}

// A row of small columns: hours by the hour of the day, or by the day of the week.
function Columns({ values, labels, label, every = 1 }: { values: number[]; labels: string[]; label: string; every?: number }) {
  const most = Math.max(0.1, ...values)
  return (
    <div className="watch-cols" role="img" aria-label={label}>
      {values.map((v, i) => (
        <div key={labels[i]} className="watch-col" title={`${labels[i]}: ${hours(v)}`}>
          <span className="watch-col-bar" style={{ height: `${Math.max(v > 0 ? 4 : 0, (v * 100) / most)}%` }} />
          <small>{i % every === 0 ? labels[i] : ' '}</small>
        </div>
      ))}
    </div>
  )
}

const PERIODS = [7, 30, 90, 365]

function Stats() {
  const [days, setDays] = useState(30)
  const [user, setUser] = useState('')
  const [data, setData] = useState<{ stats: WatchStatsData; everyone: boolean } | null>(null)
  const [people, setPeople] = useState<string[]>([])
  const [error, setError] = useState('')
  useEffect(() => {
    let live = true
    api
      .watchStats(days, user)
      .then((d) => {
        if (!live) return
        setData(d)
        setError('')
        if (!user) setPeople(d.stats.users.map((u) => u.name).filter(Boolean))
      })
      .catch((e) => live && setError(errText(e)))
    return () => {
      live = false
    }
  }, [days, user])
  if (error && !data) return <p className="error-text">{error}</p>
  if (!data) return <Loading height={300} />
  const s = data.stats
  const m = s.methods
  const methodTotal = m.direct + m.stream + m.transcode
  const share = (n: number) => (methodTotal > 0 ? Math.round((n * 100) / methodTotal) : 0)
  const weekdays = Array.from({ length: 7 }, (_, i) => new Date(Date.UTC(2024, 0, 1 + i)).toLocaleDateString(locale(), { weekday: 'short', timeZone: 'UTC' }))
  const hourLabels = Array.from({ length: 24 }, (_, i) => `${i}:00`)
  return (
    <>
      <div className="watch-period span-all">
        <div className="seg" role="tablist" aria-label={t('Period')}>
          {PERIODS.map((d) => (
            <button key={d} type="button" role="tab" aria-selected={days === d} className={days === d ? 'active' : ''} onClick={() => setDays(d)}>
              {d === 365 ? t('Year') : t('{n} days', { n: d })}
            </button>
          ))}
        </div>
        {data.everyone && people.length > 1 && (
          <select value={user} aria-label={t('Person')} onChange={(e) => setUser(e.target.value)}>
            <option value="">{t('Everyone')}</option>
            {people.map((u) => (
              <option key={u} value={u}>
                {u}
              </option>
            ))}
          </select>
        )}
      </div>
      <div className="stat-cards span-all">
        <div className="card stat-card">
          <small>{t('Hours watched')}</small>
          <strong>{s.hours.toLocaleString(locale(), { maximumFractionDigits: 1 })}</strong>
          <span>{plural(s.plays, '{n} playback', '{n} playbacks')}</span>
        </div>
        <div className="card stat-card">
          <small>{t('Titles')}</small>
          <strong>{s.titles}</strong>
          <span>{plural(s.finished, '{n} watched to the end', '{n} watched to the end')}</span>
        </div>
        {data.everyone && (
          <div className="card stat-card">
            <small>{t('People')}</small>
            <strong>{s.people}</strong>
            <span>{s.users[0] ? t('Most: {name}', { name: s.users[0].name }) : t('Nobody yet')}</span>
          </div>
        )}
        <div className="card stat-card">
          <small>{t('Played directly')}</small>
          <strong>{methodTotal > 0 ? `${share(m.direct + m.stream)}%` : '–'}</strong>
          <span>{plural(m.transcode, '{n} transcoded', '{n} transcoded')}</span>
        </div>
      </div>
      <section className="card span-all">
        <h2>{t('Hours watched per day')}</h2>
        <TimeChart points={s.perDay.map((d) => ({ x: d.day, y: d.hours }))} label={t('Hours watched')} format={(v) => hours(v)} emptyText={t('Nothing watched in this period.')} />
      </section>
      <div className="half-cols">
        {data.everyone && (
          <section className="card">
            <h2>{t('By person')}</h2>
            <BarList items={s.users} empty={t('Nothing watched in this period.')} />
          </section>
        )}
        <section className="card">
          <h2>{t('How it played')}</h2>
          {methodTotal === 0 ? (
            <p style={{ color: 'var(--text-dim)' }}>{t('Nothing watched in this period.')}</p>
          ) : (
            <div className="bar-list">
              {(
                [
                  ['direct', t('Direct play'), m.direct],
                  ['stream', t('Direct stream'), m.stream],
                  ['transcode', t('Transcode'), m.transcode],
                ] as const
              ).map(([k, label, n]) => (
                <div key={k} className="bar-row">
                  <span className="bar-label">{label}</span>
                  <span className="bar-track">
                    <span className={`bar-fill watch-fill-${k}`} style={{ width: `${share(n)}%` }} />
                  </span>
                  <span className="bar-value">
                    {share(n)}% · {plural(n, '{n} play', '{n} plays')}
                  </span>
                </div>
              ))}
            </div>
          )}
          {s.reasons.length > 0 && (
            <>
              <h3 className="watch-sub">{t('Why things were transcoded')}</h3>
              <ul className="watch-reason-list">
                {s.reasons.map((r) => (
                  <li key={r.name}>
                    {t(r.name)} <small>{plural(r.plays, '{n} play', '{n} plays')}</small>
                  </li>
                ))}
              </ul>
            </>
          )}
        </section>
        <section className="card">
          <h2>{t('Top movies')}</h2>
          <BarList items={s.movies} empty={t('No movies watched in this period.')} link={(c) => (c.tmdbId ? `/title/${c.tmdbId}` : '')} />
        </section>
        <section className="card">
          <h2>{t('Top shows')}</h2>
          <BarList items={s.shows} empty={t('No shows watched in this period.')} link={(c) => (c.libraryId ? `/series/${c.libraryId}` : '')} />
        </section>
        <section className="card">
          <h2>{t('Apps')}</h2>
          <BarList items={s.clients} empty={t('Nothing watched in this period.')} />
        </section>
        <section className="card">
          <h2>{t('Devices')}</h2>
          <BarList items={s.devices} empty={t('Nothing watched in this period.')} />
        </section>
        <section className="card">
          <h2>{t('Busiest hours')}</h2>
          <Columns values={s.hourOfDay} labels={hourLabels} every={3} label={t('Hours watched by the hour they started')} />
        </section>
        <section className="card">
          <h2>{t('Busiest days')}</h2>
          <Columns values={s.dayOfWeek} labels={weekdays} label={t('Hours watched by the day of the week')} />
        </section>
      </div>
    </>
  )
}

const RETENTION = [30, 90, 180, 365, 730, 0]

function WatchSettingsCard() {
  const toast = useToast()
  const confirm = useConfirm()
  const [w, setW] = useState<WatchSettings | null>(null)
  useEffect(() => {
    api
      .watchSettings()
      .then(setW)
      .catch((e) => toast.error(errText(e)))
  }, [toast])
  async function save(patch: Partial<Omit<WatchSettings, 'addresses'>>) {
    try {
      setW(await api.saveWatchSettings(patch))
      toast.success(t('Saved.'))
    } catch (e) {
      toast.error(errText(e))
    }
  }
  async function clear() {
    const ok = await confirm({
      title: t('Clear the players’ addresses?'),
      body: <p>{t('Every IP address kept in the watch history is removed. Whether a playback was at home or away stays.')}</p>,
      confirmLabel: t('Clear addresses'),
      danger: true,
    })
    if (!ok) return
    try {
      const r = await api.clearWatchAddresses()
      toast.success(plural(r.cleared, 'Cleared {n} address.', 'Cleared {n} addresses.'))
      setW(await api.watchSettings())
    } catch (e) {
      toast.error(errText(e))
    }
  }
  if (!w) return <Loading height={160} />
  return (
    <section className="card wide">
      <h2>{t('Watch history settings')}</h2>
      <label className="check-row">
        <input type="checkbox" checked={w.record} onChange={(e) => void save({ record: e.target.checked })} />
        <span>
          {t('Keep a watch history')}
          <small className="field-hint" style={{ display: 'block' }}>
            {t('Usherr asks Plex, Jellyfin and Emby what is playing every half minute (every minute when nothing is) and keeps who watched what, on which device, and how it played.')}
          </small>
        </span>
      </label>
      <label style={{ display: 'block', marginTop: 12 }}>
        {t('Keep the history for')}{' '}
        <select value={w.retentionDays} onChange={(e) => void save({ retentionDays: Number(e.target.value) })}>
          {(RETENTION.includes(w.retentionDays) ? RETENTION : [...RETENTION, w.retentionDays]).map((d) => (
            <option key={d} value={d}>
              {d === 0 ? t('Ever') : d === 365 ? t('A year') : d === 730 ? t('Two years') : t('{n} days', { n: d })}
            </option>
          ))}
        </select>
      </label>
      <p className="field-hint">{t('Older playbacks are removed once a day.')}</p>
      <label className="check-row" style={{ marginTop: 12 }}>
        <input type="checkbox" checked={w.membersSeeAll} onChange={(e) => void save({ membersSeeAll: e.target.checked })} />
        <span>
          {t('Members see everyone’s history and stats')}
          <small className="field-hint" style={{ display: 'block' }}>{t('Off: each person sees only their own. Administrators always see everyone’s.')}</small>
        </span>
      </label>
      <label className="check-row" style={{ marginTop: 12 }}>
        <input type="checkbox" checked={w.keepAddresses} onChange={(e) => void save({ keepAddresses: e.target.checked })} />
        <span>
          {t('Keep each player’s IP address')}
          <small className="field-hint" style={{ display: 'block' }}>{t('Off: only whether it played at home or away is kept. Only administrators see addresses.')}</small>
        </span>
      </label>
      <div className="row-actions" style={{ marginTop: 12 }}>
        <button type="button" className="btn-sm" disabled={w.addresses === 0} onClick={() => void clear()}>
          {w.addresses > 0 ? plural(w.addresses, 'Clear {n} kept address', 'Clear {n} kept addresses') : t('No addresses kept')}
        </button>
      </div>
    </section>
  )
}

export default function WatchActivity() {
  const admin = isAdmin(useAuth().user)
  const [params, setParams] = useSearchParams()
  const want = params.get('tab') as Tab | null
  const tab: Tab = want === 'history' || want === 'stats' || (want === 'settings' && admin) ? want : 'now'
  const tabs: [Tab, string][] = [
    ['now', t('Now playing')],
    ['history', t('History')],
    ['stats', t('Stats')],
  ]
  if (admin) tabs.push(['settings', t('Settings')])
  return (
    <div className="settings-stack stats-page watch-page">
      <div className="seg" role="tablist" aria-label={t('Watching')}>
        {tabs.map(([k, label]) => (
          <button key={k} type="button" role="tab" aria-selected={tab === k} className={tab === k ? 'active' : ''} onClick={() => setParams(k === 'now' ? {} : { tab: k }, { replace: true })}>
            {label}
          </button>
        ))}
      </div>
      {tab === 'now' && <NowPlaying />}
      {tab === 'history' && <History />}
      {tab === 'stats' && <Stats />}
      {tab === 'settings' && <WatchSettingsCard />}
    </div>
  )
}
