// Usherr: programming a channel like a network (internal/tv/blocks.go):
// start times on a grid, parts of the day with line-ups of their own,
// shows pinned to times, and what plays between shows.
import { useState } from 'react'
import type { Series } from '../api'
import Icon from '../components/Icon'
import { t } from '../i18n'
import { StreamChips, StreamFind, type StreamPick } from './TVStreamFind' // Usherr

export interface Daypart {
  part: string
  series?: number[]
  genres?: string[]
  decades?: number[]
  allMovies?: boolean
  stream?: StreamPick[]
}
export interface Slot {
  days?: number[]
  at: string
  seriesId?: number
  streamTmdb?: number
  streamTitle?: string
  movie?: boolean
  count?: number
  name?: string
}
export interface SchedulingRules {
  grid?: number
  dayparts?: Daypart[]
  slots?: Slot[]
  filler?: { noCards?: boolean; folder?: string }
  holidays?: boolean
  premieres?: boolean
  breaks?: number
  breakMinutes?: number
  familyHours?: boolean
  look?: '' | 'vintage' | 'hd' | 'age'
  stream?: StreamPick[]
}

const PARTS: { part: string; label: () => string; hours: string }[] = [
  { part: 'morning', label: () => t('Morning'), hours: '6–9 AM' },
  { part: 'midmorning', label: () => t('Mid-morning'), hours: '9 AM–12' },
  { part: 'afternoon', label: () => t('Afternoon'), hours: '12–5 PM' },
  { part: 'evening', label: () => t('Evening'), hours: '5–8 PM' },
  { part: 'prime', label: () => t('Prime time'), hours: '8–11 PM' },
  { part: 'latenight', label: () => t('Late night'), hours: '11 PM–2 AM' },
  { part: 'overnight', label: () => t('Overnight'), hours: '2–6 AM' },
]
const DAY = () => [t('Su'), t('Mo'), t('Tu'), t('We'), t('Th'), t('Fr'), t('Sa')]
const DECADES = [1950, 1960, 1970, 1980, 1990, 2000, 2010, 2020]
const toggle = <T,>(list: T[] | undefined, v: T) => ((list ?? []).includes(v) ? (list ?? []).filter((x) => x !== v) : [...(list ?? []), v])

export default function TVScheduling<R extends SchedulingRules>({ rules, setRules, series, genres }: { rules: R; setRules: (f: (r: R) => R) => void; series: Series[]; genres: string[] }) {
  const [open, setOpen] = useState<string | null>(null)
  const parts = rules.dayparts ?? []
  const partOf = (p: string) => parts.find((d) => d.part === p)
  const setPart = (p: string, f: (d: Daypart) => Daypart) =>
    setRules((r) => {
      const list = r.dayparts ?? []
      const cur = list.find((d) => d.part === p) ?? { part: p }
      const next = f(cur)
      const empty = !next.series?.length && !next.genres?.length && !next.decades?.length && !next.allMovies && !next.stream?.length
      return { ...r, dayparts: empty ? list.filter((d) => d.part !== p) : [...list.filter((d) => d.part !== p), next] }
    })
  const slots = rules.slots ?? []
  // Usherr: a set time can stream a show: the ones already on the channel, or one found.
  const [findFor, setFindFor] = useState<number | null>(null)
  const knownStreams: StreamPick[] = []
  for (const k of [...(rules.stream ?? []), ...parts.flatMap((d) => d.stream ?? []), ...slots.filter((x) => x.streamTmdb).map((x) => ({ tmdbId: x.streamTmdb!, title: x.streamTitle ?? '' }))])
    if (k.title && !knownStreams.some((y) => y.tmdbId === k.tmdbId)) knownStreams.push(k)
  const setSlot = (i: number, f: (s: Slot) => Slot) => setRules((r) => ({ ...r, slots: (r.slots ?? []).map((s, j) => (j === i ? f(s) : s)) }))
  const titleOf = (id?: number) => series.find((s) => s.id === id)?.title ?? ''
  const summary = (d?: Daypart) => {
    if (!d) return t("The channel's line-up")
    const bits = [
      d.series?.length ? (d.series.length === 1 ? titleOf(d.series[0]) : t('{n} shows', { n: d.series.length })) : '',
      d.genres?.length ? d.genres.join(', ') : '',
      d.decades?.length ? d.decades.map((x) => `${String(x).slice(2)}s`).join(', ') : '',
      d.allMovies ? t('movies') : '',
    ].filter(Boolean)
    return bits.join(' · ')
  }

  return (
    <div className="tvs">
      <div className="tv-field">
        <span>{t('Start times')}</span>
        <div className="seg" role="group" aria-label={t('Start times')}>
          {[0, 15, 30, 60].map((g) => (
            <button key={g} type="button" className={(rules.grid ?? 0) === g ? 'active' : ''} onClick={() => setRules((r) => ({ ...r, grid: g || undefined }))}>
              {g === 0 ? t('Back to back') : g === 60 ? t('On the hour') : t(':{n}', { n: g })}
            </button>
          ))}
        </div>
        <small className="hint">{rules.grid ? t('Shows start on the hour and every {n} minutes after; the time between is filled.', { n: rules.grid }) : t('Each show starts as the last one ends.')}</small>
      </div>

      <div className="tv-field">
        <span>{t('Parts of the day')}</span>
        <ul className="tvs-parts">
          {PARTS.map((p) => {
            const d = partOf(p.part)
            const isOpen = open === p.part
            return (
              <li key={p.part} className={d ? 'own' : ''}>
                <button type="button" className="tvs-part-head" onClick={() => setOpen(isOpen ? null : p.part)} aria-expanded={isOpen}>
                  <strong>{p.label()}</strong> <span className="hint">{p.hours}</span>
                  <span className="tvs-part-sum">{summary(d)}</span>
                  <Icon name="chevron-down" size={14} />
                </button>
                {isOpen && (
                  <div className="tvs-part-body">
                    <ul className="tv-pick">
                      {series.map((s) => (
                        <li key={s.id}>
                          <label>
                            <input type="checkbox" checked={(d?.series ?? []).includes(s.id)} onChange={() => setPart(p.part, (x) => ({ ...x, series: toggle(x.series, s.id) }))} />
                            {s.title} {s.year ? <span className="hint">({s.year})</span> : null}
                          </label>
                        </li>
                      ))}
                    </ul>
                    <StreamChips list={d?.stream ?? []} onDrop={(s) => setPart(p.part, (x) => ({ ...x, stream: (x.stream ?? []).filter((y) => y.tmdbId !== s.tmdbId) }))} />
                    <StreamFind picked={d?.stream} onPick={(s) => setPart(p.part, (x) => ({ ...x, stream: [...(x.stream ?? []), s] }))} />
                    <div className="tv-chips">
                      {genres.map((g) => (
                        <button key={g} type="button" className={`btn-sm${(d?.genres ?? []).includes(g) ? ' primary' : ''}`} onClick={() => setPart(p.part, (x) => ({ ...x, genres: toggle(x.genres, g) }))}>
                          {g}
                        </button>
                      ))}
                    </div>
                    <div className="tv-chips">
                      {DECADES.map((y) => (
                        <button key={y} type="button" className={`btn-sm${(d?.decades ?? []).includes(y) ? ' primary' : ''}`} onClick={() => setPart(p.part, (x) => ({ ...x, decades: toggle(x.decades, y) }))}>
                          {t('{n}s', { n: String(y).slice(2) })}
                        </button>
                      ))}
                    </div>
                    <label className="tv-check">
                      <input type="checkbox" checked={!!d?.allMovies} onChange={(e) => setPart(p.part, (x) => ({ ...x, allMovies: e.target.checked }))} />
                      {t('Movies too')}
                    </label>
                    {d && (
                      <button type="button" className="btn-sm" onClick={() => setRules((r) => ({ ...r, dayparts: (r.dayparts ?? []).filter((x) => x.part !== p.part) }))}>
                        {t("Use the channel's line-up")}
                      </button>
                    )}
                  </div>
                )}
              </li>
            )
          })}
        </ul>
      </div>

      <div className="tv-field">
        <span>{t('Shows at set times')}</span>
        {slots.length === 0 && <small className="hint">{t('Pin a show or a movie to a time, like “Seinfeld, weeknights at 7” or a Sunday Night Movie.')}</small>}
        <ul className="tvs-slots">
          {slots.map((s, i) => (
            <li key={i}>
              <div className="tv-chips">
                {DAY().map((label, d) => (
                  <button key={d} type="button" className={`btn-sm tvs-day${(s.days ?? []).includes(d) || !(s.days ?? []).length ? ' primary' : ''}`} onClick={() => setSlot(i, (x) => ({ ...x, days: toggle(x.days?.length ? x.days : [0, 1, 2, 3, 4, 5, 6], d) }))}>
                    {label}
                  </button>
                ))}
                <button type="button" className="btn-sm" onClick={() => setSlot(i, (x) => ({ ...x, days: [1, 2, 3, 4, 5] }))}>
                  {t('Weeknights')}
                </button>
                <button type="button" className="btn-sm" onClick={() => setSlot(i, (x) => ({ ...x, days: [0, 6] }))}>
                  {t('Weekends')}
                </button>
              </div>
              <div className="tv-field-row">
                <input type="time" value={s.at} step={900} onChange={(e) => setSlot(i, (x) => ({ ...x, at: e.target.value }))} aria-label={t('Time')} />
                <select
                  value={s.movie ? 'movie' : s.streamTmdb ? `s${s.streamTmdb}` : findFor === i ? 'find' : String(s.seriesId ?? '')}
                  onChange={(e) => {
                    const v = e.target.value
                    setFindFor(v === 'find' ? i : null)
                    if (v === 'find') return
                    const st = v.startsWith('s') ? knownStreams.find((k) => `s${k.tmdbId}` === v) : undefined
                    setSlot(i, (x) =>
                      v === 'movie'
                        ? { ...x, movie: true, seriesId: undefined, streamTmdb: undefined, streamTitle: undefined }
                        : st
                          ? { ...x, movie: false, seriesId: undefined, streamTmdb: st.tmdbId, streamTitle: st.title }
                          : { ...x, movie: false, seriesId: Number(v) || undefined, streamTmdb: undefined, streamTitle: undefined },
                    )
                  }}
                  aria-label={t('What plays')}
                >
                  <option value="">{t('Pick a show…')}</option>
                  <option value="movie">{t('A movie (from the channel’s movies)')}</option>
                  {series.map((x) => (
                    <option key={x.id} value={x.id}>
                      {x.title}
                    </option>
                  ))}
                  <optgroup label={t('Streamed')}>
                    {[...knownStreams, ...(s.streamTmdb && !knownStreams.some((k) => k.tmdbId === s.streamTmdb) ? [{ tmdbId: s.streamTmdb, title: s.streamTitle ?? String(s.streamTmdb) }] : [])].map((k) => (
                      <option key={`s${k.tmdbId}`} value={`s${k.tmdbId}`}>
                        {k.title} ({t('stream')})
                      </option>
                    ))}
                    <option value="find">{t('Find a show to stream…')}</option>
                  </optgroup>
                </select>
                {!s.movie && (
                  <label className="tvs-count">
                    <input type="number" min={1} max={8} value={s.count ?? 1} onChange={(e) => setSlot(i, (x) => ({ ...x, count: Number(e.target.value) || 1 }))} /> {t('in a row')}
                  </label>
                )}
                <input type="text" value={s.name ?? ''} placeholder={t('Name (optional): Sunday Night Movie')} onChange={(e) => setSlot(i, (x) => ({ ...x, name: e.target.value }))} />
                <button type="button" className="icon-btn" aria-label={t('Remove')} onClick={() => setRules((r) => ({ ...r, slots: (r.slots ?? []).filter((_, j) => j !== i) }))}>
                  <Icon name="x" size={14} />
                </button>
              </div>
              {findFor === i && (
                <StreamFind
                  autoFocus
                  onPick={(p) => {
                    setFindFor(null)
                    setSlot(i, (x) => ({ ...x, movie: false, seriesId: undefined, streamTmdb: p.tmdbId, streamTitle: p.title }))
                  }}
                />
              )}
            </li>
          ))}
        </ul>
        <div>
          <button type="button" className="btn-sm btn-with-icon" onClick={() => setRules((r) => ({ ...r, slots: [...(r.slots ?? []), { at: '19:00', days: [1, 2, 3, 4, 5] }] }))}>
            <Icon name="plus" size={14} /> {t('Add a set time')}
          </button>
        </div>
      </div>

      <div className="tv-field">
        <span>{t('Between shows')}</span>
        <label className="tv-check">
          <input type="checkbox" checked={!rules.filler?.noCards} onChange={(e) => setRules((r) => ({ ...r, filler: { ...r.filler, noCards: !e.target.checked || undefined } }))} />
          {t('“Up next” cards')}
        </label>
        <input type="text" value={rules.filler?.folder ?? ''} placeholder={t('Folder of bumpers, idents and ads (optional), like /data/tv-filler')} onChange={(e) => setRules((r) => ({ ...r, filler: { ...r.filler, folder: e.target.value || undefined } }))} />
        <small className="hint">{t('Videos in it (MP4 or WebM) play in the gaps first; a card fills what is left.')}</small>
        <div className="tv-field-row tvs-breaks">
          <label className="tv-field">
            <span>{t('Commercial breaks in each show')}</span>
            <select value={rules.breaks ?? 0} onChange={(e) => setRules((r) => ({ ...r, breaks: Number(e.target.value) || undefined }))} disabled={!rules.filler?.folder}>
              <option value={0}>{t('None')}</option>
              <option value={1}>{t('1 break')}</option>
              <option value={2}>{t('2 breaks')}</option>
              <option value={3}>{t('3 breaks')}</option>
            </select>
          </label>
          {!!rules.breaks && (
            <label className="tv-field">
              <span>{t('Each break')}</span>
              <select value={rules.breakMinutes ?? 2} onChange={(e) => setRules((r) => ({ ...r, breakMinutes: Number(e.target.value) }))}>
                {[1, 2, 3, 4].map((m) => (
                  <option key={m} value={m}>
                    {t('{n} min', { n: m })}
                  </option>
                ))}
              </select>
            </label>
          )}
        </div>
        {!rules.filler?.folder && <small className="hint">{t('Breaks play the ads in the filler folder: choose one above first.')}</small>}
      </div>

      <label className="tv-field">
        <span>{t('Picture')}</span>
        <select value={rules.look ?? ''} onChange={(e) => setRules((r) => ({ ...r, look: (e.target.value || undefined) as SchedulingRules['look'] }))}>
          <option value="">{t('As each viewer set it')}</option>
          <option value="age">{t('By each show’s age: older than 2000 looks like old TV')}</option>
          <option value="vintage">{t('Always old TV: scan lines and static')}</option>
          <option value="hd">{t('Always clean HD: no effects')}</option>
        </select>
        <small className="hint">{t('Old TV adds scan lines, softer colour and static between channels.')}</small>
      </label>

      <div className="tv-field">
        <span>{t('Special programming')}</span>
        <label className="tv-check">
          <input type="checkbox" checked={!!rules.holidays} onChange={(e) => setRules((r) => ({ ...r, holidays: e.target.checked || undefined }))} />
          {t('Holiday episodes in season (Halloween, Thanksgiving, Christmas, New Year, Valentine’s, Easter, the Fourth of July)')}
        </label>
        <label className="tv-check">
          <input type="checkbox" checked={!!rules.premieres} onChange={(e) => setRules((r) => ({ ...r, premieres: e.target.checked || undefined }))} />
          {t('Premieres: new episodes first in prime time, marked NEW, reruns later')}
        </label>
        <label className="tv-check">
          <input type="checkbox" checked={!!rules.familyHours} onChange={(e) => setRules((r) => ({ ...r, familyHours: e.target.checked || undefined }))} />
          {t('Family hours: nothing for older than about 10 before 9 PM')}
        </label>
      </div>
    </div>
  )
}
