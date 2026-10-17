// Usherr: the guide's other looks, and its options. Each person picks one:
//   modern   – today's grid (TV.tsx)
//   cable    – the same grid as a '90s cable box: blocky, navy and cyan
//   prevue   – the Prevue Channel: a preview on top, the line-up scrolling below
//   listings – the day as a newspaper prints it
//   nowon    – big tiles, one per channel, with what's on now
import { useEffect, useState } from 'react'
import Icon from '../components/Icon'
import { locale, t } from '../i18n'
import { ChannelPreview, playChannel } from './Player'
import { setTVPrefs, useTVPrefs, type GuideStyle } from './tvPrefs'

interface GChannel {
  id: number
  number: number
  name: string
  logoUrl?: string
}
interface GProgram {
  start: string
  end: string
  title: string
  subtitle?: string
  kind: string
}
export interface GRow {
  channel: GChannel
  programs: GProgram[]
}

const clock = (iso: string) => new Date(iso).toLocaleTimeString(locale(), { hour: 'numeric', minute: '2-digit' })
const onNow = (r: GRow, now: number) => r.programs.find((p) => new Date(p.start).getTime() <= now && new Date(p.end).getTime() > now)
const after = (r: GRow, now: number) => r.programs.filter((p) => new Date(p.start).getTime() > now)

export const STYLES: { key: GuideStyle; label: () => string }[] = [
  { key: 'modern', label: () => t('Modern grid') },
  { key: 'cable', label: () => t('’90s cable box') },
  { key: 'prevue', label: () => t('Prevue Channel') },
  { key: 'listings', label: () => t('Newspaper listings') },
  { key: 'nowon', label: () => t('Now on (tiles)') },
]

// The options above the guide.
export function GuideOptions() {
  const p = useTVPrefs()
  const grid = p.style === 'modern' || p.style === 'cable'
  return (
    <div className="tvo">
      <label className="tvo-style">
        <Icon name="grid" size={14} />
        <select value={p.style} onChange={(e) => setTVPrefs({ style: e.target.value as GuideStyle })} aria-label={t('Guide style')}>
          {STYLES.map((s) => (
            <option key={s.key} value={s.key}>
              {s.label()}
            </option>
          ))}
        </select>
      </label>
      {grid && (
        <div className="seg" role="group" aria-label={t('Each column')}>
          {([15, 30, 60] as const).map((m) => (
            <button key={m} type="button" className={p.scale === m ? 'active' : ''} onClick={() => setTVPrefs({ scale: m })}>
              {m === 60 ? t('1 hr') : t('{n} min', { n: m })}
            </button>
          ))}
        </div>
      )}
      {(grid || p.style === 'listings') && (
        <div className="seg" role="group" aria-label={t('How far ahead')}>
          {([6, 12, 24] as const).map((h) => (
            <button key={h} type="button" className={p.hours === h ? 'active' : ''} onClick={() => setTVPrefs({ hours: h })}>
              {t('{n} h', { n: h })}
            </button>
          ))}
        </div>
      )}
      <button type="button" className={`btn-sm btn-with-icon${p.favsOnly ? ' primary' : ''}`} aria-pressed={p.favsOnly} onClick={() => setTVPrefs({ favsOnly: !p.favsOnly })} title={t('Only your favourite channels')}>
        <Icon name="star" size={14} /> {t('Favourites')}
      </button>
      <span className="spacer" />
      <label className="tv-check" title={t('A burst of snow when you change channel')}>
        <input type="checkbox" checked={p.staticOn} onChange={(e) => setTVPrefs({ staticOn: e.target.checked })} /> {t('Static')}
      </label>
      {p.staticOn && (
        <label className="tv-check">
          <input type="checkbox" checked={p.staticSound} onChange={(e) => setTVPrefs({ staticSound: e.target.checked })} /> {t('with sound')}
        </label>
      )}
      <label className="tv-check" title={t('Scan lines and a rounded glow, like an old TV')}>
        <input type="checkbox" checked={p.crt} onChange={(e) => setTVPrefs({ crt: e.target.checked })} /> {t('CRT look')}
      </label>
    </div>
  )
}

// Prevue: a preview of the channel at the top of the list, the line-up
// scrolling slowly below (it holds still while the pointer is over it).
export function PrevueView({ rows }: { rows: GRow[] }) {
  const [now, setNow] = useState(Date.now())
  useEffect(() => {
    const tm = window.setInterval(() => setNow(Date.now()), 30000)
    return () => window.clearInterval(tm)
  }, [])
  const top = rows[0]
  const list = rows.map((r) => (
    <li key={r.channel.id}>
      <button type="button" onClick={() => playChannel(r.channel.id, r.channel.name)}>
        <span className="pv-ch">
          <b>{r.channel.number}</b>
          <span>{r.channel.name}</span>
        </span>
        {[onNow(r, now), ...after(r, now)].filter(Boolean).slice(0, 3).map((p) => (
          <span key={p!.start} className="pv-prog">
            <i>{clock(p!.start)}</i> {p!.title}
          </span>
        ))}
      </button>
    </li>
  ))
  const duration = Math.max(20, rows.length * 5)
  return (
    <div className="pv">
      <div className="pv-top">
        <div className="pv-preview">{top ? <ChannelPreview channelId={top.channel.id} name={top.channel.name} /> : null}</div>
        <div className="pv-promo">
          <div className="pv-brand">{t('PREVUE GUIDE')}</div>
          <div className="pv-time">{new Date(now).toLocaleTimeString(locale(), { hour: 'numeric', minute: '2-digit' })}</div>
          {top && onNow(top, now) && (
            <p>
              {t('Now on')} <b>{top.channel.name}</b>: {onNow(top, now)!.title}
            </p>
          )}
        </div>
      </div>
      <div className="pv-roll">
        <ul className="pv-list" style={{ animationDuration: `${duration}s` }}>
          {list}
          {list.map((li) => ({ ...li, key: `again-${li.key}` }))}
        </ul>
      </div>
    </div>
  )
}

// Listings: the day by the hour, as a newspaper prints it.
export function ListingsView({ rows, hours }: { rows: GRow[]; hours: number }) {
  const now = Date.now()
  const until = now + hours * 3600_000
  const byHour = new Map<number, { row: GRow; p: GProgram }[]>()
  for (const row of rows) {
    for (const p of row.programs) {
      const s = new Date(p.start).getTime()
      if (s < now - 3600_000 || s > until) continue
      const h = new Date(s)
      h.setMinutes(0, 0, 0)
      const k = h.getTime()
      if (!byHour.has(k)) byHour.set(k, [])
      byHour.get(k)!.push({ row, p })
    }
  }
  const hoursList = [...byHour.keys()].sort((a, b) => a - b)
  return (
    <div className="lst">
      <h2 className="lst-head">
        {t('Television')} <small>{new Date().toLocaleDateString(locale(), { weekday: 'long', month: 'long', day: 'numeric' })}</small>
      </h2>
      <div className="lst-cols">
        {hoursList.map((h) => (
          <section key={h} className="lst-hour">
            <h3>{new Date(h).toLocaleTimeString(locale(), { hour: 'numeric' })}</h3>
            {byHour
              .get(h)!
              .sort((a, b) => a.p.start.localeCompare(b.p.start) || a.row.channel.number - b.row.channel.number)
              .map(({ row, p }) => (
                <p key={`${row.channel.id}-${p.start}`}>
                  <span className="lst-ch">{row.channel.number}</span> <b>{p.title}</b>
                  {p.subtitle ? <> — {p.subtitle}</> : null} <span className="lst-at">{clock(p.start)}</span>
                </p>
              ))}
          </section>
        ))}
      </div>
    </div>
  )
}

// Now on: one big tile a channel.
export function NowOnView({ rows }: { rows: GRow[] }) {
  const now = Date.now()
  return (
    <div className="nowon">
      {rows.map((r) => {
        const p = onNow(r, now)
        const next = after(r, now)[0]
        const progress = p ? (now - new Date(p.start).getTime()) / (new Date(p.end).getTime() - new Date(p.start).getTime()) : 0
        return (
          <button key={r.channel.id} type="button" className="nowon-tile" onClick={() => playChannel(r.channel.id, r.channel.name)}>
            <span className="nowon-ch">
              {r.channel.logoUrl ? <img src={r.channel.logoUrl} alt="" /> : <b>{r.channel.number}</b>}
              <span>{r.channel.name}</span>
            </span>
            <strong>{p ? p.title : next ? `${t('Up next')}: ${next.title}` : '—'}</strong>
            {p?.subtitle && <span className="nowon-sub">{p.subtitle}</span>}
            {p && (
              <span className="nowon-bar">
                <span style={{ width: `${Math.min(100, progress * 100)}%` }} />
              </span>
            )}
            {next && p && (
              <span className="nowon-next">
                {clock(next.start)} {next.title}
              </span>
            )}
          </button>
        )
      })}
    </div>
  )
}
