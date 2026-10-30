// Usherr: the pre-show's page (/preshow/manage). Simple first: what plays,
// a sample to watch, the switch for this browser. Then the themes (the
// pre-show as a list of blocks), when each plays (schedules), the clips (a
// folder per category; a holiday's folder plays in its season) and the
// holidays' dates.
import { useCallback, useEffect, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import Icon from '../components/Icon'
import Switch from '../components/Switch'
import { useConfirm } from '../components/ConfirmProvider'
import { useToast } from '../components/Toast'
import { useDocumentTitle } from '../documentTitle'
import { t } from '../i18n'
import PreshowJellyfin from './PreshowJellyfin'
import PreshowTrailers from './PreshowTrailers'
import { playWithPreshow } from './Player'
import { CARD_LABEL, preshowApi, preshowBefore, setPreshowBefore, type PreshowAll, type PreshowBlock, type PreshowCard, type PreshowRule, type PreshowSchedule, type PreshowTheme, type PreshowWhen } from './preshowApi'
import './preshowSettings.css'

const DAYS = [t('Sun'), t('Mon'), t('Tue'), t('Wed'), t('Thu'), t('Fri'), t('Sat')]
const STARTER_CATEGORIES = ['Bumpers', 'Sound', 'Intermission', 'Kids']
const mins = (secs: number) => (secs >= 60 ? `${Math.floor(secs / 60)}:${String(Math.round(secs % 60)).padStart(2, '0')}` : t('{n} s', { n: Math.round(secs) }))
const hourLabel = (h: number) => new Date(2000, 0, 1, h).toLocaleTimeString(undefined, { hour: 'numeric' })

// What a block plays, in a few words.
function blockText(b: PreshowBlock, clipName: (id: number) => string): string {
  const n = Math.max(b.count ?? 1, 1)
  switch (b.type) {
    case 'card':
      return CARD_LABEL[b.card ?? 'feature']
    case 'trailers':
      return n === 1 ? t('A coming-soon trailer') : t('{n} coming-soon trailers', { n })
    case 'seasonal':
      return n === 1 ? t('A clip for the holiday (when there is one)') : t('{n} clips for the holiday (when there is one)', { n })
    default:
      if (b.clipId) return t('The clip “{name}”', { name: clipName(b.clipId) })
      return n === 1 ? t('A clip from {cat}', { cat: b.category ?? '' }) : t('{n} clips from {cat}', { n, cat: b.category ?? '' })
  }
}

function whenText(w?: PreshowWhen): string {
  if (!w) return ''
  const parts: string[] = []
  const sound = w.sound?.length ? w.sound.map((s) => ({ atmos: 'Dolby Atmos', dtsx: 'DTS:X', truehd: 'TrueHD', dts: 'DTS' })[s] ?? s).join(t(' or ')) : ''
  const picture = w.picture?.length ? w.picture.map((p) => ({ '4k': '4K', hdr: 'HDR', dv: 'Dolby Vision', imax: 'IMAX' })[p] ?? p).join(t(' or ')) : ''
  if (sound && picture && w.eitherAv) parts.push(`${sound}${t(' or ')}${picture}`)
  else {
    if (sound) parts.push(sound)
    if (picture) parts.push(picture)
  }
  if (w.genres?.length) parts.push(w.genres.join(t(' or ')))
  if (w.notGenres?.length) parts.push(t('not {genres}', { genres: w.notGenres.join(', ') }))
  if (w.kids !== undefined) parts.push(w.kids ? t('a kids’ film') : t('not a kids’ film'))
  if (w.showtime !== undefined) parts.push(w.showtime ? t('a booked Showtime') : t('not a Showtime'))
  if (w.holidays?.length) parts.push(w.holidays.includes('any') ? t('a holiday’s season') : t('{names} season', { names: w.holidays.join(t(' or ')) }))
  if (w.fromHour !== undefined || w.toHour !== undefined) parts.push(t('{from} to {to}', { from: hourLabel(w.fromHour ?? 0), to: hourLabel(w.toHour ?? 24) }))
  return parts.length ? t('only for {what}', { what: parts.join(', ') }) : ''
}

function ruleText(r: PreshowRule): string {
  const hours = r.fromHour !== undefined || r.toHour !== undefined ? ` · ${hourLabel(r.fromHour ?? 0)}–${hourLabel(r.toHour ?? 24)}` : ''
  if (r.kind === 'holiday') return t('{name} season', { name: r.holiday ?? '' }) + hours
  if (r.kind === 'dates') return t('{from} to {to}, every year', { from: r.from ?? '', to: r.to ?? '' }) + hours
  return (r.days ?? []).map((d) => DAYS[d]).join(', ') + hours
}

function toggle<T>(list: T[] | undefined, v: T): T[] | undefined {
  const l = list ?? []
  const out = l.includes(v) ? l.filter((x) => x !== v) : [...l, v]
  return out.length ? out : undefined
}

// ---- a theme's blocks ----

function WhenEditor({ w, onChange, holidays }: { w?: PreshowWhen; onChange: (w?: PreshowWhen) => void; holidays: string[] }) {
  const set = (patch: Partial<PreshowWhen>) => {
    const next: PreshowWhen = { ...(w ?? {}), ...patch }
    for (const k of Object.keys(next) as (keyof PreshowWhen)[]) if (next[k] === undefined) delete next[k]
    onChange(Object.keys(next).length ? next : undefined)
  }
  const tri = (v: boolean | undefined) => (v === undefined ? '' : v ? 'yes' : 'no')
  const fromTri = (v: string) => (v === '' ? undefined : v === 'yes')
  return (
    <div className="psm-when">
      <div className="psm-when-row">
        <span>{t('Sound')}</span>
        {(['atmos', 'dtsx'] as const).map((s) => (
          <label key={s} className="cx-check">
            <input type="checkbox" checked={!!w?.sound?.includes(s)} onChange={() => set({ sound: toggle(w?.sound, s) })} /> {s === 'atmos' ? 'Dolby Atmos' : 'DTS:X'}
          </label>
        ))}
      </div>
      <div className="psm-when-row">
        <span>{t('Picture')}</span>
        {(['4k', 'hdr', 'dv', 'imax'] as const).map((p) => (
          <label key={p} className="cx-check">
            <input type="checkbox" checked={!!w?.picture?.includes(p)} onChange={() => set({ picture: toggle(w?.picture, p) })} /> {{ '4k': '4K', hdr: 'HDR', dv: 'Dolby Vision', imax: 'IMAX' }[p]}
          </label>
        ))}
      </div>
      {!!w?.sound?.length && !!w?.picture?.length && (
        <div className="psm-when-row">
          <span />
          <select value={w.eitherAv ? 'any' : 'all'} onChange={(e) => set({ eitherAv: e.target.value === 'any' || undefined })} aria-label={t('Sound and picture')} title={t('Any is kinder to files whose labels aren’t all there')}>
            <option value="any">{t('Any ticked sound or picture is enough')}</option>
            <option value="all">{t('A ticked sound and a ticked picture, both')}</option>
          </select>
        </div>
      )}
      <div className="psm-when-row">
        <span>{t('Film')}</span>
        <select value={tri(w?.kids)} onChange={(e) => set({ kids: fromTri(e.target.value) })} aria-label={t('Kids’ films')}>
          <option value="">{t('Any rating')}</option>
          <option value="yes">{t('Kids’ films (G, PG)')}</option>
          <option value="no">{t('Not kids’ films')}</option>
        </select>
        <select value={tri(w?.showtime)} onChange={(e) => set({ showtime: fromTri(e.target.value) })} aria-label={t('Showtime')}>
          <option value="">{t('Showtime or not')}</option>
          <option value="yes">{t('Only a booked Showtime')}</option>
          <option value="no">{t('Only when just pressing play')}</option>
        </select>
      </div>
      <div className="psm-when-row">
        <span>{t('Season')}</span>
        <label className="cx-check">
          <input type="checkbox" checked={!!w?.holidays?.includes('any')} onChange={() => set({ holidays: w?.holidays?.includes('any') ? undefined : ['any'] })} /> {t('Any holiday')}
        </label>
        {!w?.holidays?.includes('any') &&
          holidays.map((h) => (
            <label key={h} className="cx-check">
              <input type="checkbox" checked={!!w?.holidays?.includes(h)} onChange={() => set({ holidays: toggle(w?.holidays, h) })} /> {h}
            </label>
          ))}
      </div>
      <div className="psm-when-row">
        <span>{t('Genres')}</span>
        <input type="text" title={t('Plays only for films of one of these genres, separated by commas')} placeholder={t('Horror, Comedy')} defaultValue={(w?.genres ?? []).join(', ')} onBlur={(e) => set({ genres: e.target.value.split(',').map((x) => x.trim()).filter(Boolean).slice(0, 10) || undefined })} />
      </div>
      <div className="psm-when-row">
        <span>{t('Hours')}</span>
        <select value={w?.fromHour ?? ''} onChange={(e) => set({ fromHour: e.target.value === '' ? undefined : Number(e.target.value) })} aria-label={t('From')}>
          <option value="">{t('Any time')}</option>
          {Array.from({ length: 24 }, (_, h) => (
            <option key={h} value={h}>
              {t('from {hour}', { hour: hourLabel(h) })}
            </option>
          ))}
        </select>
        <select value={w?.toHour ?? ''} onChange={(e) => set({ toHour: e.target.value === '' ? undefined : Number(e.target.value) })} aria-label={t('To')}>
          <option value="">{t('till late')}</option>
          {Array.from({ length: 24 }, (_, h) => (
            <option key={h} value={h}>
              {t('to {hour}', { hour: hourLabel(h) })}
            </option>
          ))}
        </select>
      </div>
    </div>
  )
}

function ThemeEditor({ theme, all, onSaved, onCancel }: { theme: Partial<PreshowTheme>; all: PreshowAll; onSaved: () => void; onCancel: () => void }) {
  const toast = useToast()
  const [name, setName] = useState(theme.name ?? '')
  const [blocks, setBlocks] = useState<PreshowBlock[]>(theme.blocks ?? [{ type: 'card', card: 'silence' }, { type: 'trailers', count: 2 }, { type: 'card', card: 'feature' }])
  const [open, setOpen] = useState<number | null>(null)
  const cats = Array.from(new Set([...all.categories.map((c) => c.name).filter(Boolean), ...STARTER_CATEGORIES]))
  const put = (i: number, b: PreshowBlock) => setBlocks((l) => l.map((x, j) => (j === i ? b : x)))
  const move = (i: number, d: number) =>
    setBlocks((l) => {
      const n = [...l]
      const [x] = n.splice(i, 1)
      n.splice(i + d, 0, x)
      return n
    })
  const save = async () => {
    try {
      await preshowApi.saveTheme({ id: theme.id, name, blocks })
      toast.success(t('Theme saved.'))
      onSaved()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    }
  }
  return (
    <div className="psm-editor">
      <label className="psm-field">
        <span>{t('Name')}</span>
        <input type="text" value={name} onChange={(e) => setName(e.target.value)} maxLength={60} placeholder={t('IMAX night')} />
      </label>
      <ol className="psm-blocks">
        {blocks.map((b, i) => (
          <li key={i}>
            <div className="psm-block">
              <span className="psm-n">{i + 1}</span>
              <select
                value={b.type === 'clips' && b.clipId ? 'clip' : b.type}
                onChange={(e) => {
                  const v = e.target.value
                  if (v === 'clip') put(i, { type: 'clips', clipId: all.clips[0]?.id, when: b.when })
                  else put(i, { type: v as PreshowBlock['type'], card: v === 'card' ? 'silence' : undefined, category: v === 'clips' ? cats[0] : undefined, when: b.when })
                }}
                aria-label={t('What plays')}
                title={t('What this slot plays')}
              >
                <option value="card">{t('A card')}</option>
                <option value="clips">{t('Clips from a category')}</option>
                <option value="clip">{t('One clip')}</option>
                <option value="seasonal">{t('The holiday’s clip')}</option>
                <option value="trailers">{t('Coming-soon trailers')}</option>
              </select>
              {b.type === 'card' && (
                <select value={b.card} onChange={(e) => put(i, { ...b, card: e.target.value as PreshowCard, text: undefined })} aria-label={t('Card')}>
                  {all.cards.map((c) => (
                    <option key={c} value={c}>
                      {CARD_LABEL[c]}
                    </option>
                  ))}
                </select>
              )}
              {b.type === 'card' && (b.card === 'announce' || b.card === 'qr') && (
                <input
                  type={b.card === 'qr' ? 'url' : 'text'}
                  className="psm-text"
                  value={b.text ?? ''}
                  maxLength={120}
                  onChange={(e) => put(i, { ...b, text: e.target.value || undefined })}
                  placeholder={b.card === 'qr' ? t('Usherr’s address (from Settings)') : t('Pizza’s here!')}
                  aria-label={b.card === 'qr' ? t('Address') : t('What it says')}
                />
              )}
              {b.type === 'clips' && !!b.clipId && (
                <select value={b.clipId} onChange={(e) => put(i, { ...b, clipId: Number(e.target.value) })} aria-label={t('Clip')}>
                  {all.clips.map((c) => (
                    <option key={c.id} value={c.id}>
                      {c.category ? `${c.category} · ${c.title}` : c.title}
                    </option>
                  ))}
                </select>
              )}
              {b.type === 'clips' && !b.clipId && (
                <>
                  <input type="text" list="psm-cats" value={b.category ?? ''} onChange={(e) => put(i, { ...b, category: e.target.value })} aria-label={t('Category')} title={t('The category (folder) its clips come from')} placeholder={t('Category')} />
                  <label className="cx-check" title={t('In name order, instead of the ones not played lately')}>
                    <input type="checkbox" checked={!!b.inOrder} onChange={(e) => put(i, { ...b, inOrder: e.target.checked || undefined })} /> {t('In order')}
                  </label>
                </>
              )}
              {b.type !== 'card' && !b.clipId && (
                <input className="psm-count" type="number" min={1} max={10} value={b.count ?? 1} onChange={(e) => put(i, { ...b, count: Math.min(10, Math.max(1, Number(e.target.value) || 1)) })} aria-label={t('How many')} title={t('How many play from this slot')} />
              )}
              <button type="button" className={`btn-sm${b.when ? ' primary' : ''}`} onClick={() => setOpen(open === i ? null : i)} aria-expanded={open === i} title={t('Play this slot only for some films, in some seasons or at some hours')}>
                {t('Only when…')}
              </button>
              <span className="psm-spacer" />
              <button type="button" className="icon-btn" onClick={() => move(i, -1)} disabled={i === 0} aria-label={t('Move up')} title={t('Move up')}>
                <Icon name="chevron-up" size={16} />
              </button>
              <button type="button" className="icon-btn" onClick={() => move(i, 1)} disabled={i === blocks.length - 1} aria-label={t('Move down')} title={t('Move down')}>
                <Icon name="chevron-down" size={16} />
              </button>
              <button type="button" className="icon-btn" onClick={() => setBlocks((l) => l.filter((_, j) => j !== i))} aria-label={t('Remove')} title={t('Remove this slot')}>
                <Icon name="x" size={16} />
              </button>
            </div>
            {b.when && open !== i && <p className="hint psm-when-text">{whenText(b.when)}</p>}
            {open === i && <WhenEditor w={b.when} onChange={(w) => put(i, { ...b, when: w })} holidays={Array.from(new Set(all.holidays.map((h) => h.name)))} />}
          </li>
        ))}
      </ol>
      <datalist id="psm-cats">
        {cats.map((c) => (
          <option key={c} value={c} />
        ))}
      </datalist>
      <div className="psm-actions">
        <button type="button" className="btn-sm btn-with-icon" onClick={() => setBlocks((l) => [...l, { type: 'card', card: 'silence' }])} disabled={blocks.length >= 40} title={t('A new slot at the end; move it with the arrows')}>
          <Icon name="plus" size={14} /> {t('Add a block')}
        </button>
        <span className="psm-spacer" />
        <button type="button" className="btn-sm" onClick={onCancel}>
          {t('Cancel')}
        </button>
        <button type="button" className="btn-sm primary" onClick={() => void save()}>
          {t('Save theme')}
        </button>
      </div>
    </div>
  )
}

// ---- schedules ----

function ScheduleForm({ all, onSaved }: { all: PreshowAll; onSaved: () => void }) {
  const toast = useToast()
  const [kind, setKind] = useState<PreshowRule['kind']>('holiday')
  const [holiday, setHoliday] = useState(all.holidays[0]?.name ?? 'Halloween')
  const [from, setFrom] = useState('12-01')
  const [to, setTo] = useState('12-31')
  const [days, setDays] = useState<number[]>([5, 6])
  const [themeId, setThemeId] = useState(all.themes[0]?.id ?? 0)
  const [name, setName] = useState('')
  const names = Array.from(new Set(all.holidays.map((h) => h.name)))
  const save = async () => {
    const rule: PreshowRule = kind === 'holiday' ? { kind, holiday } : kind === 'dates' ? { kind, from, to } : { kind, days }
    try {
      await preshowApi.saveSchedule({ name: name || (kind === 'holiday' ? holiday : ''), themeId, rule, priority: kind === 'holiday' ? 2 : kind === 'dates' ? 1 : 0, enabled: true })
      toast.success(t('Schedule added.'))
      setName('')
      onSaved()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    }
  }
  return (
    <div className="psm-schedform">
      <select value={themeId} onChange={(e) => setThemeId(Number(e.target.value))} aria-label={t('Theme')}>
        {all.themes.map((th) => (
          <option key={th.id} value={th.id}>
            {th.name}
          </option>
        ))}
      </select>
      <select value={kind} onChange={(e) => setKind(e.target.value as PreshowRule['kind'])} aria-label={t('When')} title={t('A holiday beats dates, dates beat days of the week')}>
        <option value="holiday">{t('in a holiday’s season')}</option>
        <option value="weekly">{t('on some days of the week')}</option>
        <option value="dates">{t('between two dates')}</option>
      </select>
      {kind === 'holiday' && (
        <select value={holiday} onChange={(e) => setHoliday(e.target.value)} aria-label={t('Holiday')}>
          {names.map((n) => (
            <option key={n}>{n}</option>
          ))}
        </select>
      )}
      {kind === 'weekly' &&
        DAYS.map((d, i) => (
          <label key={d} className="cx-check">
            <input type="checkbox" checked={days.includes(i)} onChange={() => setDays((l) => (l.includes(i) ? l.filter((x) => x !== i) : [...l, i]))} /> {d}
          </label>
        ))}
      {kind === 'dates' && (
        <>
          <input type="text" value={from} onChange={(e) => setFrom(e.target.value)} placeholder="12-01" aria-label={t('From (month-day)')} title={t('Month and day, such as 12-01')} size={6} />
          <input type="text" value={to} onChange={(e) => setTo(e.target.value)} placeholder="12-31" aria-label={t('To (month-day)')} title={t('Month and day, such as 12-31')} size={6} />
        </>
      )}
      {kind !== 'holiday' && <input type="text" value={name} onChange={(e) => setName(e.target.value)} placeholder={t('Name')} aria-label={t('Name')} maxLength={60} />}
      <button type="button" className="btn-sm primary" onClick={() => void save()}>
        {t('Add')}
      </button>
    </div>
  )
}

// ---- the page ----

export default function PreshowSettings() {
  useDocumentTitle(t('Pre-show'))
  const toast = useToast()
  const confirm = useConfirm()
  const [all, setAll] = useState<PreshowAll | null>(null)
  const [editing, setEditing] = useState<Partial<PreshowTheme> | null>(null)
  const [before, setBefore] = useState(preshowBefore)
  const [category, setCategory] = useState('Bumpers')
  const [busy, setBusy] = useState(false)
  const file = useRef<HTMLInputElement>(null)
  const load = useCallback((scan = false) => {
    preshowApi
      .all(scan)
      .then(setAll)
      .catch((e: Error) => toast.error(e.message))
  }, [toast])
  useEffect(() => load(), [load])
  if (!all) return <div className="page-pad">{t('Loading…')}</div>

  const clipName = (id: number) => all.clips.find((c) => c.id === id)?.title ?? '?'
  const themeName = (id: number) => all.themes.find((th) => th.id === id)?.name ?? '?'
  const sample = (themeId?: number) => void playWithPreshow({ kind: 'movie', theme: themeId, dry: true }, () => undefined)
  const saveSettings = async (patch: Partial<typeof all.settings>) => {
    try {
      await preshowApi.saveSettings({ ...all.settings, ...patch })
      load()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    }
  }
  const upload = async (files: FileList | null) => {
    if (!files?.length) return
    setBusy(true)
    try {
      for (const f of Array.from(files)) await preshowApi.uploadClip(f, category)
      toast.success(t('Added to {cat}.', { cat: category || t('the pre-show') }))
      load()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
      if (file.current) file.current.value = ''
    }
  }
  const byCat = new Map<string, typeof all.clips>()
  for (const c of all.clips) byCat.set(c.category, [...(byCat.get(c.category) ?? []), c])
  const cats = Array.from(new Set([...all.categories.map((c) => c.name).filter(Boolean), ...STARTER_CATEGORIES, ...all.holidays.map((h) => h.name)]))

  return (
    <div className="page-pad psm">
      <div className="page-header">
        <h1>{t('Pre-show')}</h1>
      </div>

      <section className="card psm-intro">
        <p>{t('Before a film, Usherr plays a short show: a welcome, a reminder to silence phones, trailers for films coming soon, your own clips, the countdown and “Feature presentation”. It plays on the Showtime screen before a booked film and in the Jellyfin app, and the Marquee follows along.')}</p>
        <ul className="psm-how">
          <li>{t('Nothing to set up: “Movie night” is ready. Add clips when you like.')}</li>
          <li>{t('Holiday clips play by themselves in their season: put them in a category named after the holiday.')}</li>
          <li>{t('Clips and trailers take turns, so the same ones don’t come round every night.')}</li>
        </ul>
        <div className="psm-actions">
          <button type="button" className="primary btn-with-icon" onClick={() => sample(all.activeTheme)} title={t('Plays tonight’s theme here, without a film')}>
            <Icon name="play" size={16} /> {t('Watch a sample')}
          </button>
          <span className="hint">
            {t('Tonight’s theme: {name}', { name: themeName(all.activeTheme) })}
            {all.holidayNow ? ` · ${t('{name} season', { name: all.holidayNow })}` : ''}
          </span>
        </div>
        <Switch
          checked={before}
          onChange={(on) => {
            setBefore(on)
            setPreshowBefore(on)
          }}
          label={t('Also before any film I play in this browser')}
          description={t('Not just a Showtime: pressing Play on a film starts with its pre-show. Skip it any time. A film picked up part-way starts without one.')}
        />
      </section>

      <PreshowJellyfin themes={all.themes} />

      <PreshowTrailers />

      <section className="card">
        <h2>{t('Themes')}</h2>
        <p className="hint">{t('A theme is the pre-show, piece by piece. A Showtime can pick its own; otherwise the schedule below decides, then the default.')}</p>
        <ul className="psm-themes">
          {all.themes.map((th) => (
            <li key={th.id}>
              <div className="psm-theme-head">
                <strong>{th.name}</strong>
                {all.settings.themeId === th.id && <span className="psm-tag">{t('Default')}</span>}
                {all.activeTheme === th.id && <span className="psm-tag on">{t('Tonight')}</span>}
                <span className="psm-spacer" />
                <button type="button" className="btn-sm btn-with-icon" onClick={() => sample(th.id)} title={t('Plays this theme here, without a film')}>
                  <Icon name="play" size={13} /> {t('Sample')}
                </button>
                {all.settings.themeId !== th.id && (
                  <button type="button" className="btn-sm" onClick={() => void saveSettings({ themeId: th.id })} title={t('Plays when no Showtime or schedule picks a theme')}>
                    {t('Make default')}
                  </button>
                )}
                <button type="button" className="btn-sm" onClick={() => setEditing(th)}>
                  {t('Edit')}
                </button>
                <button
                  type="button"
                  className="icon-btn"
                  aria-label={t('Remove {name}', { name: th.name })}
                  title={t('Remove {name}', { name: th.name })}
                  onClick={async () => {
                    if (!(await confirm({ title: t('Remove {name}?', { name: th.name }), body: t('Its schedules go too.'), confirmLabel: t('Remove'), danger: true }))) return
                    preshowApi
                      .deleteTheme(th.id)
                      .then(() => load())
                      .catch((e: Error) => toast.error(e.message))
                  }}
                >
                  <Icon name="x" size={15} />
                </button>
              </div>
              {editing?.id === th.id ? (
                <ThemeEditor theme={th} all={all} onCancel={() => setEditing(null)} onSaved={() => (setEditing(null), load())} />
              ) : (
                <ol className="psm-summary">
                  {th.blocks.map((b, i) => (
                    <li key={i}>
                      {blockText(b, clipName)}
                      {b.when && <span className="hint"> · {whenText(b.when)}</span>}
                    </li>
                  ))}
                </ol>
              )}
            </li>
          ))}
        </ul>
        {editing && !editing.id ? (
          <ThemeEditor theme={editing} all={all} onCancel={() => setEditing(null)} onSaved={() => (setEditing(null), load())} />
        ) : (
          <button type="button" className="btn-sm btn-with-icon" onClick={() => setEditing({})}>
            <Icon name="plus" size={14} /> {t('New theme')}
          </button>
        )}
      </section>

      <section className="card">
        <h2>{t('When each theme plays')}</h2>
        <p className="hint">{t('Without a schedule, the default theme plays. A holiday beats dates, dates beat days of the week.')}</p>
        {all.schedules.length > 0 && (
          <ul className="psm-scheds">
            {all.schedules.map((sc: PreshowSchedule) => (
              <li key={sc.id} className={sc.enabled ? '' : 'off'}>
                <label className="cx-check" title={t('Off leaves the schedule here without using it')}>
                  <input type="checkbox" checked={sc.enabled} onChange={(e) => void preshowApi.saveSchedule({ ...sc, enabled: e.target.checked }).then(() => load())} />
                  <strong>{sc.name}</strong>
                </label>
                <span>{themeName(sc.themeId)}</span>
                <span className="hint">{ruleText(sc.rule)}</span>
                <button type="button" className="icon-btn" aria-label={t('Remove')} title={t('Remove this schedule')} onClick={() => void preshowApi.deleteSchedule(sc.id).then(() => load())}>
                  <Icon name="x" size={15} />
                </button>
              </li>
            ))}
          </ul>
        )}
        <ScheduleForm all={all} onSaved={() => load()} />
      </section>

      <section className="card">
        <h2>{t('Clips')}</h2>
        <p className="hint">
          {t('Your own videos: bumpers, sound logos, intermissions, holiday clips. Each category is a folder in {folder}; files you put there show up by themselves.', { folder: all.settings.folder })}
        </p>
        {all.settings.inConfig && (
          <p className="hint psm-folder-tip">
            {t('This folder is inside Usherr’s own storage, which your computer’s file browser may not show (a Docker volume). Upload clips here, or give Usherr a folder of your own: map one to /data/preshow in Usherr’s compose file, for example')} <code>- C:/Usherr/Preshow:/data/preshow</code>
            {t(', and restart Usherr. It uses that folder by itself, makes the categories in it and copies these clips over.')}
          </p>
        )}
        <div className="psm-actions">
          <input type="text" list="psm-allcats" value={category} onChange={(e) => setCategory(e.target.value)} aria-label={t('Category')} title={t('Uploads go into this category (folder); a holiday’s name plays them in its season')} placeholder={t('Category')} />
          <datalist id="psm-allcats">
            {cats.map((c) => (
              <option key={c} value={c} />
            ))}
          </datalist>
          <input ref={file} type="file" accept="video/mp4,video/webm,video/quicktime,.m4v" multiple hidden onChange={(e) => void upload(e.target.files)} />
          <button type="button" className="btn-sm btn-with-icon" disabled={busy} onClick={() => file.current?.click()} title={t('MP4 plays everywhere; several at once is fine')}>
            <Icon name="plus" size={14} /> {busy ? t('Uploading…') : t('Upload clips')}
          </button>
          <button type="button" className="btn-sm btn-with-icon" onClick={() => load(true)} title={t('Picks up files copied into the clips folder now (it also looks by itself)')}>
            <Icon name="refresh" size={14} /> {t('Look in the folder again')}
          </button>
        </div>
        {all.clips.length === 0 && <p className="psm-none">{t('No clips yet. The pre-show still plays its cards and trailers.')}</p>}
        {Array.from(byCat.entries()).map(([cat, list]) => (
          <div key={cat} className="psm-cat">
            <h3>
              {cat || t('No category')} <span className="hint">{all.holidays.some((h) => h.name === cat) ? t('plays in its season') : ''}</span>
            </h3>
            <ul className="psm-clips">
              {list.map((c) => (
                <li key={c.id} className={c.enabled ? '' : 'off'}>
                  <label className="cx-check" title={t('Plays in the pre-show; untick to leave it out')}>
                    <input type="checkbox" checked={c.enabled} onChange={(e) => void preshowApi.saveClip(c.id, { enabled: e.target.checked }).then(() => load())} />
                  </label>
                  <a href={c.fileUrl} target="_blank" rel="noreferrer">
                    {c.title}
                  </a>
                  <span className="hint">
                    {mins(c.seconds)}
                    {c.height ? ` · ${c.height >= 2000 ? '4K' : `${c.height}p`}` : ''}
                  </span>
                  <span className="psm-spacer" />
                  <input
                    type="text"
                    list="psm-allcats"
                    defaultValue={c.category}
                    aria-label={t('Category')}
                    title={t('Its category: changing it moves the file to that folder')}
                    className="psm-catinput"
                    onBlur={(e) => e.target.value !== c.category && void preshowApi.saveClip(c.id, { category: e.target.value }).then(() => load(), (er: Error) => toast.error(er.message))}
                  />
                  <button
                    type="button"
                    className="icon-btn"
                    aria-label={t('Remove {name}', { name: c.title })}
                    title={t('Moves it to the .removed folder, so it can be put back')}
                    onClick={async () => {
                      if (!(await confirm({ title: t('Remove {name}?', { name: c.title }), body: t('The file moves to the .removed folder in the pre-show folder, so it can be put back.'), confirmLabel: t('Remove'), danger: true }))) return
                      preshowApi
                        .deleteClip(c.id)
                        .then(() => load())
                        .catch((e: Error) => toast.error(e.message))
                    }}
                  >
                    <Icon name="x" size={15} />
                  </button>
                </li>
              ))}
            </ul>
          </div>
        ))}
      </section>

      <section className="card">
        <h2>{t('Settings')}</h2>
        <div className="psm-settings">
          <label className="psm-field">
            <span>{t('Your theater’s name')}</span>
            <input type="text" defaultValue={all.settings.theater} maxLength={60} placeholder={t('the theater')} onBlur={(e) => e.target.value !== all.settings.theater && void saveSettings({ theater: e.target.value })} />
            <small className="hint">{t('On the welcome card: “Welcome to …”.')}</small>
          </label>
          <label className="psm-field">
            <span>{t('A trailer plays at most')}</span>
            <select value={all.settings.trailerSeconds} onChange={(e) => void saveSettings({ trailerSeconds: Number(e.target.value) })} title={t('A longer trailer stops here and the pre-show moves on')}>
              {[60, 90, 120, 150, 180, 240].map((s) => (
                <option key={s} value={s}>
                  {mins(s)}
                </option>
              ))}
            </select>
          </label>
          <label className="psm-field">
            <span>{t('Clips folder')}</span>
            <input type="text" defaultValue={all.settings.folder} onBlur={(e) => e.target.value !== all.settings.folder && void saveSettings({ folder: e.target.value })} />
            <small className="hint">{t('As Usherr sees it. Leave it for the folder in Usherr’s config.')}</small>
          </label>
        </div>
        <h3>{t('Holidays')}</h3>
        <ul className="psm-holidays">
          {all.holidays
            .filter((h, i, list) => list.findIndex((x) => x.name === h.name) === i)
            .map((h, i) => (
            <li key={i} className={h.now || all.holidayNow === h.name ? 'on' : ''}>
              <strong>{h.name}</strong> <span className="hint">{h.from.slice(5)} – {h.to.slice(5)}</span>
            </li>
            ))}
        </ul>
        <p className="hint">
          {t('The Marquee follows the pre-show by itself. Its own look and pre-show pieces are on its page:')} <Link to="/marquee/manage">{t('Marquee')}</Link>
        </p>
      </section>
    </div>
  )
}
