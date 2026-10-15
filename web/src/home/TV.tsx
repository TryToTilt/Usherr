// Home fork: Mediarium TV. Channels built from the library play on a
// schedule, like broadcast TV: tap a channel to watch whatever is on, from
// the right second. Admins make and edit channels here.
import HelpLink from '../components/HelpLink'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import Icon from '../components/Icon'
import { api, isAdmin, type Series, type TitleResult } from '../api'
import { useAuth } from '../AuthContext'
import { ChannelPreview, playChannel, playTitle } from './Player'
import './tv.css'
import { locale, t } from '../i18n'

interface Rules {
  series?: number[]
  movies?: number[]
  genres?: string[]
  decades?: number[]
  allMovies?: boolean
  order?: 'rotate' | 'inorder' | 'shuffle'
  maxMoviesPerDay?: number
  streamUrl?: string
  guideUrl?: string
  guideId?: string
  stream?: StreamShow[]
}
// A show the channel streams through StreamNZB rather than plays from the library.
interface StreamShow {
  tmdbId: number
  title: string
  year?: number
}
interface Channel {
  id: number
  number: number
  name: string
  logoUrl?: string
  kind: string
  rules: Rules
  enabled: boolean
}
interface Program {
  channelId: number
  start: string
  end: string
  kind: 'episode' | 'movie' | 'live' | 'stream'
  itemId: number
  title: string
  subtitle?: string
  rating?: string // the guide grid's badges
  quality?: string
}
interface Guide {
  now: string
  from?: string
  hours?: number
  channels: { channel: Channel; programs: Program[] }[]
}

async function call<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(`/api/tv${path}`, {
    method,
    credentials: 'include',
    headers: body ? { 'Content-Type': 'application/json' } : undefined,
    body: body ? JSON.stringify(body) : undefined,
  })
  const data = await res.json().catch(() => null)
  if (!res.ok) throw new Error(data?.error ?? t('Something went wrong (error {status}).', { status: res.status }))
  return data as T
}

const clock = (iso: string) => new Date(iso).toLocaleTimeString(locale(), { hour: 'numeric', minute: '2-digit' })
const minutesLeft = (iso: string) => Math.max(0, Math.round((new Date(iso).getTime() - Date.now()) / 60000))

export default function TV() {
  const me = useAuth().user
  const admin = isAdmin(me)
  const [guide, setGuide] = useState<Guide | null>(null)
  const [error, setError] = useState('')
  const [editing, setEditing] = useState<Channel | 'new' | null>(null)
  const [, setTick] = useState(0)
  const [view, setView] = useState<'list' | 'guide'>(() => {
    try {
      return localStorage.getItem('tv-view') === 'guide' ? 'guide' : 'list'
    } catch {
      return 'list'
    }
  })
  const [picked, setPicked] = useState<{ program: Program; channel: Channel } | null>(null)
  const [jfNote, setJfNote] = useState('')
  const switchView = (v: 'list' | 'guide') => {
    setView(v)
    try {
      localStorage.setItem('tv-view', v)
    } catch {
      /* private mode */
    }
  }
  const addToJellyfin = () => {
    setJfNote(t('Adding Mediarium TV to Jellyfin…'))
    call<{ message: string }>('POST', '/jellyfin')
      .then((r) => setJfNote(r.message))
      .catch((e: Error) => setJfNote(e.message))
  }

  const load = useCallback(() => {
    call<Guide>('GET', '/guide?hours=24')
      .then((g) => {
        setGuide(g)
        setError('')
      })
      // A blip (phone waking up, network switching) clears on the next refresh.
      .catch((e: Error) => setError(e.message === 'Failed to fetch' ? t('Couldn’t reach Mediarium just now. Trying again shortly…') : e.message))
  }, [])
  useEffect(() => {
    load()
    const tm = window.setInterval(() => {
      setTick((n) => n + 1)
      load()
    }, 60000)
    const onShow = () => {
      if (document.visibilityState === 'visible') {
        setTick((n) => n + 1)
        load()
      }
    }
    document.addEventListener('visibilitychange', onShow)
    return () => {
      window.clearInterval(tm)
      document.removeEventListener('visibilitychange', onShow)
    }
  }, [load])

  const now = Date.now()
  // Channels switched off stay out of the guide; admins still see them, dimmed, in the list.
  const onAir = guide ? { ...guide, channels: guide.channels.filter((c) => c.channel.enabled) } : null
  return (
    <div className="tv-page">
      <div className="tv-head page-header">
        <h1>{t('Live TV')}</h1>
        <div className="tv-seg" role="tablist" aria-label={t('View')}>
          <button role="tab" aria-selected={view === 'list'} className={`btn-sm${view === 'list' ? ' primary' : ''}`} onClick={() => switchView('list')}>
            {t('Channels')}
          </button>
          <button role="tab" aria-selected={view === 'guide'} className={`btn-sm${view === 'guide' ? ' primary' : ''}`} onClick={() => switchView('guide')}>
            {t('Guide')}
          </button>
          <HelpLink topic="home-watching#live-tv" />
        </div>
        {admin && (
          <div className="tv-actions">
            <button className="btn-with-icon" onClick={addToJellyfin} title={t("Show these channels, with their guide, in Jellyfin's Live TV")}>
              <Icon name="tv" size={16} /> {t('Add to Jellyfin')}
            </button>
            <button className="primary btn-with-icon" onClick={() => setEditing('new')}>
              <Icon name="plus" size={16} /> {t('New channel')}
            </button>
          </div>
        )}
      </div>
      {jfNote && <p className="hint">{jfNote}</p>}
      {error && <p className="error-text">{error}</p>}
      {guide && guide.channels.length === 0 && (
        <div className="tv-empty">
          <p>{t('No channels yet.')}</p>
          {admin && <p className="hint">{t('Make one from the shows and movies in your library: a channel of favourite sitcoms, Saturday-morning cartoons, a movie channel…')}</p>}
        </div>
      )}
      {view === 'guide' && onAir && onAir.channels.length > 0 && <GuideView guide={onAir} onOpen={(program, channel) => setPicked({ program, channel })} />}
      {picked && createPortal(<ProgramSheet program={picked.program} channel={picked.channel} onClose={() => setPicked(null)} />, document.body)}
      <ul className="tv-list" hidden={view !== 'list'}>
        {guide?.channels.filter(({ channel }) => admin || channel.enabled).map(({ channel, programs }) => {
          const onNow = programs.find((p) => new Date(p.start).getTime() <= now && new Date(p.end).getTime() > now)
          const later = programs.filter((p) => new Date(p.start).getTime() > now).slice(0, 3)
          const progress = onNow ? (now - new Date(onNow.start).getTime()) / (new Date(onNow.end).getTime() - new Date(onNow.start).getTime()) : 0
          return (
            <li key={channel.id} className={channel.enabled ? '' : 'off'}>
              <button className="tv-tune" onClick={() => playChannel(channel.id, channel.name)} disabled={!onNow} title={onNow ? t('Watch {name}', { name: channel.name }) : t('Its schedule is still being made')}>
                <span className="tv-num">{channel.logoUrl ? <img className="tv-list-logo" src={channel.logoUrl} alt={String(channel.number)} /> : channel.number}</span>
                <span className="tv-name">
                  {channel.logoUrl && <small className="hint">{channel.number} </small>}
                  {channel.name}
                </span>
                <span className="tv-now">
                  {onNow ? (
                    <>
                      <strong>{onNow.title}</strong>
                      {onNow.subtitle && <span className="hint"> · {onNow.subtitle}</span>}
                      <span className="tv-bar">
                        <span style={{ width: `${Math.min(100, progress * 100)}%` }} />
                      </span>
                      <span className="hint">{t('{n} min left', { n: minutesLeft(onNow.end) })}</span>
                    </>
                  ) : (
                    <span className="hint">{channel.enabled ? t('Getting the schedule ready…') : t('Switched off')}</span>
                  )}
                </span>
                <Icon name="play" size={20} />
              </button>
              {later.length > 0 && (
                <ol className="tv-later">
                  {later.map((p) => (
                    <li key={p.start}>
                      <span className="tv-time">{clock(p.start)}</span> {p.title}
                      {p.subtitle && <span className="hint"> · {p.subtitle}</span>}
                    </li>
                  ))}
                </ol>
              )}
              {admin && (
                <button className="btn-sm tv-edit" onClick={() => setEditing(channel)} aria-label={t('Edit {name}', { name: channel.name })}>
                  <Icon name="sliders" size={14} />
                </button>
              )}
            </li>
          )
        })}
      </ul>
      {editing &&
        createPortal(
          <ChannelEditor
            channel={editing === 'new' ? null : editing}
            onClose={() => setEditing(null)}
            onSaved={() => {
              setEditing(null)
              load()
              window.setTimeout(load, 8000) // the first programs take a moment to measure
            }}
          />,
          document.body,
        )}
    </div>
  )
}

const DECADES = [1950, 1960, 1970, 1980, 1990, 2000, 2010, 2020]

function ChannelEditor({ channel, onClose, onSaved }: { channel: Channel | null; onClose: () => void; onSaved: () => void }) {
  const [name, setName] = useState(channel?.name ?? '')
  const [number, setNumber] = useState(channel?.number ?? 0)
  const [rules, setRules] = useState<Rules>(channel?.rules ?? { order: 'rotate' })
  const [kind, setKind] = useState<'library' | 'live'>(channel?.kind === 'live' ? 'live' : 'library')
  const [enabled, setEnabled] = useState(channel?.enabled ?? true)
  const [logo, setLogo] = useState(channel?.logoUrl ?? '')
  const [ideas, setIdeas] = useState<{ title: string; url: string }[] | null>(null)
  const [logoNote, setLogoNote] = useState('')
  const uploadLogo = (file: File) => {
    if (!channel) return
    setLogoNote(t('Uploading…'))
    const fd = new FormData()
    fd.append('file', file)
    fetch(`/api/tv/channels/${channel.id}/logo`, { method: 'POST', credentials: 'include', body: fd })
      .then(async (r) => {
        const d = await r.json().catch(() => null)
        if (!r.ok) throw new Error(d?.error ?? t('Upload failed (error {status}).', { status: r.status }))
        setLogo(d.logoUrl ?? '')
        setLogoNote('')
      })
      .catch((e: Error) => setLogoNote(e.message))
  }
  const removeLogo = () => {
    if (channel && logo.startsWith('/api/')) {
      call('DELETE', `/channels/${channel.id}/logo`).catch(() => undefined)
    }
    setLogo('')
  }
  const showIdeas = () => {
    if (!channel) return
    setLogoNote(t('Looking up logos…'))
    call<{ title: string; url: string }[]>('GET', `/channels/${channel.id}/logo-ideas`)
      .then((list) => {
        setIdeas(list)
        setLogoNote(list.length ? '' : t("None of this channel's shows has a logo on TMDB."))
      })
      .catch((e: Error) => setLogoNote(e.message))
  }
  const [series, setSeries] = useState<Series[]>([])
  const [filter, setFilter] = useState('')
  const [error, setError] = useState('')
  const [saving, setSaving] = useState(false)

  useEffect(() => {
    api
      .listSeries()
      .then((list) => setSeries([...list].sort((a, b) => a.title.localeCompare(b.title))))
      .catch((e: Error) => setError(e.message))
  }, [])
  const genres = useMemo(() => {
    const all = new Set<string>()
    for (const s of series) for (const g of s.genres ?? []) all.add(g)
    return [...all].sort()
  }, [series])
  const picked = new Set(rules.series ?? [])
  const shown = series.filter((s) => !filter || s.title.toLowerCase().includes(filter.toLowerCase()))
  // Shows that aren't in the library, to stream: looked up on TMDB as you type.
  const [found, setFound] = useState<TitleResult[]>([])
  useEffect(() => {
    const q = filter.trim()
    if (q.length < 2) {
      setFound([])
      return
    }
    const ac = new AbortController()
    const tm = window.setTimeout(() => {
      api
        .discoverSearch(q, ac.signal)
        .then((list) => setFound(list.filter((r) => r.kind === 'tv' && !r.inLibrary).slice(0, 6)))
        .catch(() => undefined)
    }, 350)
    return () => {
      window.clearTimeout(tm)
      ac.abort()
    }
  }, [filter])
  const streamed = rules.stream ?? []
  const toggleStream = (r: { tmdbId: number; title: string; year?: number }) =>
    setRules((x) => {
      const list = x.stream ?? []
      return list.some((s) => s.tmdbId === r.tmdbId)
        ? { ...x, stream: list.filter((s) => s.tmdbId !== r.tmdbId) }
        : { ...x, stream: [...list, { tmdbId: r.tmdbId, title: r.title, year: r.year || undefined }] }
    })
  const toggle = <T,>(list: T[] | undefined, v: T) => ((list ?? []).includes(v) ? (list ?? []).filter((x) => x !== v) : [...(list ?? []), v])

  const save = () => {
    setSaving(true)
    setError('')
    const body = { name, number: number || undefined, kind, rules, enabled, logoUrl: logo }
    ;(channel ? call('PUT', `/channels/${channel.id}`, body) : call('POST', '/channels', body))
      .then(onSaved)
      .catch((e: Error) => setError(e.message))
      .finally(() => setSaving(false))
  }
  const remove = () => {
    if (!channel || !window.confirm(t('Delete the channel {name}? Your shows and movies stay in the library.', { name: channel.name }))) return
    call('DELETE', `/channels/${channel.id}`)
      .then(onSaved)
      .catch((e: Error) => setError(e.message))
  }

  return (
    <div className="tv-modal" role="dialog" aria-modal="true" aria-label={channel ? t('Edit {name}', { name: channel.name }) : t('New channel')} onClick={onClose}>
      <div className="card tv-modal-box" onClick={(e) => e.stopPropagation()}>
        <header>
          <strong>{channel ? t('Edit {name}', { name: channel.name }) : t('New channel')}</strong>
          <button className="btn-sm" onClick={onClose} aria-label={t('Close')}>
            <Icon name="x" size={14} />
          </button>
        </header>
        <div className="tv-field-row">
          <label className="tv-field grow">
            <span>{t('Name')}</span>
            <input value={name} onChange={(e) => setName(e.target.value)} placeholder={t('Saturday Cartoons')} />
          </label>
          <label className="tv-field">
            <span>{t('Number')}</span>
            <input type="number" min={1} value={number || ''} onChange={(e) => setNumber(Number(e.target.value))} placeholder={t('auto')} />
          </label>
        </div>
        <div className="tv-field">
          <span>{t('Logo')}</span>
          <div className="tv-logo-edit">
            <span className="tv-logo-box">{logo ? <img src={logo} alt="" /> : <span className="hint">{t('No logo')}</span>}</span>
            <div className="tv-chips">
              {channel ? (
                <>
                  <label className="btn-sm tv-upload">
                    {t('Upload a picture')}
                    <input type="file" accept="image/png,image/jpeg,image/webp,image/gif" onChange={(e) => e.target.files?.[0] && uploadLogo(e.target.files[0])} />
                  </label>
                  <button className="btn-sm" onClick={showIdeas}>
                    {t("Use a show's logo")}
                  </button>
                </>
              ) : (
                <span className="hint">{t('Save the channel first, then add its logo.')}</span>
              )}
              {logo && (
                <button className="btn-sm" onClick={removeLogo}>
                  {t('Remove')}
                </button>
              )}
            </div>
          </div>
          {channel && <input value={logo.startsWith('http') ? logo : ''} onChange={(e) => setLogo(e.target.value)} placeholder={t('…or paste a link to a picture (https://…)')} inputMode="url" />}
          {logoNote && <p className="hint">{logoNote}</p>}
          {ideas && ideas.length > 0 && (
            <div className="tv-logo-ideas">
              {ideas.map((i) => (
                <button key={i.url} className={logo === i.url ? 'on' : ''} onClick={() => setLogo(i.url)} title={i.title}>
                  <img src={i.url} alt={i.title} loading="lazy" />
                </button>
              ))}
            </div>
          )}
        </div>
        {!channel && (
          <div className="tv-chips" role="radiogroup" aria-label={t('Kind of channel')}>
            <button role="radio" aria-checked={kind === 'library'} className={`btn-sm${kind === 'library' ? ' primary' : ''}`} onClick={() => setKind('library')}>
              {t('From my library')}
            </button>
            <button role="radio" aria-checked={kind === 'live'} className={`btn-sm${kind === 'live' ? ' primary' : ''}`} onClick={() => setKind('live')}>
              {t('Live stream')}
            </button>
          </div>
        )}
        {kind === 'live' ? (
          <>
            <label className="tv-field">
              <span>{t('Stream address (.m3u8)')}</span>
              <input value={rules.streamUrl ?? ''} onChange={(e) => setRules((r) => ({ ...r, streamUrl: e.target.value }))} placeholder="https://…/playlist.m3u8" inputMode="url" />
            </label>
            <div className="tv-field-row">
              <label className="tv-field grow">
                <span>{t('Guide feed (XMLTV, optional)')}</span>
                <input value={rules.guideUrl ?? ''} onChange={(e) => setRules((r) => ({ ...r, guideUrl: e.target.value }))} placeholder="https://i.mjh.nz/PlutoTV/us.xml.gz" inputMode="url" />
              </label>
              <label className="tv-field">
                <span>{t('Its channel id')}</span>
                <input value={rules.guideId ?? ''} onChange={(e) => setRules((r) => ({ ...r, guideId: e.target.value }))} />
              </label>
            </div>
            <p className="hint">{t('Without a guide, the channel shows as “Live” in the guide.')}</p>
          </>
        ) : (
          <>
        <div className="tv-field">
          <span>
            {streamed.length
              ? t('Shows ({n} picked, {streamed} streamed)', { n: picked.size + streamed.length, streamed: streamed.length })
              : t('Shows ({n} picked)', { n: picked.size })}
          </span>
          <input type="search" value={filter} onChange={(e) => setFilter(e.target.value)} placeholder={t('Find a show')} />
          <ul className="tv-pick">
            {shown.map((s) => (
              <li key={s.id}>
                <label>
                  <input type="checkbox" checked={picked.has(s.id)} onChange={() => setRules((r) => ({ ...r, series: toggle(r.series, s.id) }))} />
                  {s.title} {s.year ? <span className="hint">({s.year})</span> : null}
                </label>
              </li>
            ))}
            {found
              .filter((r) => !streamed.some((s) => s.tmdbId === r.tmdbId))
              .map((r) => (
                <li key={`tmdb-${r.tmdbId}`}>
                  <label title={t('Not in your library: its episodes play through StreamNZB')}>
                    <input type="checkbox" checked={false} onChange={() => toggleStream(r)} />
                    {r.title} {r.year ? <span className="hint">({r.year})</span> : null} <span className="tv-tag">{t('stream')}</span>
                  </label>
                </li>
              ))}
          </ul>
          {filter.trim().length >= 2 && shown.length === 0 && found.length === 0 && <p className="hint">{t('Not in your library, and nothing found to stream.')}</p>}
          {streamed.length > 0 && (
            <div className="tv-chips">
              {streamed.map((s) => (
                <button key={s.tmdbId} className="btn-sm" onClick={() => toggleStream(s)} title={t('Stop streaming this show on the channel')}>
                  {s.title} <span className="tv-tag">{t('stream')}</span> <Icon name="x" size={12} />
                </button>
              ))}
            </div>
          )}
        </div>
        <div className="tv-field">
          <span>{t('And every show in these genres')}</span>
          <div className="tv-chips">
            {genres.map((g) => (
              <button key={g} className={`btn-sm${(rules.genres ?? []).includes(g) ? ' primary' : ''}`} onClick={() => setRules((r) => ({ ...r, genres: toggle(r.genres, g) }))}>
                {g}
              </button>
            ))}
          </div>
        </div>
        <div className="tv-field">
          <span>{t('From these decades (optional)')}</span>
          <div className="tv-chips">
            {DECADES.map((d) => (
              <button key={d} className={`btn-sm${(rules.decades ?? []).includes(d) ? ' primary' : ''}`} onClick={() => setRules((r) => ({ ...r, decades: toggle(r.decades, d) }))}>
                {t("{n}s", { n: String(d).slice(2) })}
              </button>
            ))}
          </div>
        </div>
        <div className="tv-field-row">
          <label className="tv-field">
            <span>{t('Order')}</span>
            <select value={rules.order ?? 'rotate'} onChange={(e) => setRules((r) => ({ ...r, order: e.target.value as Rules['order'] }))}>
              <option value="rotate">{t('Take turns (next episode of each show)')}</option>
              <option value="inorder">{t('In order (one show start to finish)')}</option>
              <option value="shuffle">{t('Shuffle (any episode)')}</option>
            </select>
          </label>
          <label className="tv-check">
            <input type="checkbox" checked={!!rules.allMovies} onChange={(e) => setRules((r) => ({ ...r, allMovies: e.target.checked }))} />
            {rules.genres?.length || rules.decades?.length ? t('Also movies in those genres and decades') : t('Also movies from the library')}
          </label>
          {rules.allMovies && (
            <label className="tv-field">
              <span>{t('Movies a day')}</span>
              <input type="number" min={0} value={rules.maxMoviesPerDay ?? 0} onChange={(e) => setRules((r) => ({ ...r, maxMoviesPerDay: Number(e.target.value) }))} />
            </label>
          )}
        </div>
          </>
        )}
        {channel && (
          <label className="tv-check">
            <input type="checkbox" checked={enabled} onChange={(e) => setEnabled(e.target.checked)} />
            {t('Show this channel (in the guide and in Jellyfin)')}
          </label>
        )}
        {error && <p className="error-text">{error}</p>}
        <footer>
          {channel && (
            <button className="danger-ghost" onClick={remove}>
              {t('Delete')}
            </button>
          )}
          <span style={{ flex: 1 }} />
          <button onClick={onClose}>{t('Cancel')}</button>
          <button className="primary" disabled={saving || !name.trim()} onClick={save}>
            {saving ? t('Saving…') : t('Save')}
          </button>
        </footer>
      </div>
    </div>
  )
}

// ---- the guide grid: channels down the side, time across ----


// showColor gives each show (or movie) its own color in the guide, the same
// wherever and whenever it's on, as NostalgiaTV's guide does.
const GUIDE_COLORS = 8
function showColor(title: string) {
  let h = 0
  for (let i = 0; i < title.length; i++) h = (h * 31 + title.charCodeAt(i)) >>> 0
  return h % GUIDE_COLORS
}

type Selection = { channel: Channel; program: Program }

// GuideView is the guide as on a cable box: the selected channel playing
// small at the top with what's on described beside it, and the grid below.
// Tap a program to select it, tap it again to watch it (or, for a later one,
// see what you can do with it).
function GuideView({ guide, onOpen }: { guide: Guide; onOpen: (p: Program, c: Channel) => void }) {
  const [sel, setSel] = useState<Selection | null>(null)
  const now = Date.now()
  const onNow = (c: { channel: Channel; programs: Program[] }) =>
    c.programs.find((p) => new Date(p.start).getTime() <= now && new Date(p.end).getTime() > now)
  // Start on the channel last looked at, else the first.
  useEffect(() => {
    if (sel) return
    let last = 0
    try {
      last = Number(localStorage.getItem('tv-guide-channel')) || 0
    } catch {
      /* private mode */
    }
    const row = guide.channels.find((c) => c.channel.id === last) ?? guide.channels[0]
    const p = row && onNow(row)
    if (row && p) setSel({ channel: row.channel, program: p })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [guide])
  const select = (program: Program, channel: Channel) => {
    if (sel && sel.channel.id === channel.id && sel.program.start === program.start) {
      const s = new Date(program.start).getTime()
      if (s <= Date.now() && new Date(program.end).getTime() > Date.now()) playChannel(channel.id, channel.name)
      else onOpen(program, channel)
      return
    }
    setSel({ channel, program })
    try {
      localStorage.setItem('tv-guide-channel', String(channel.id))
    } catch {
      /* private mode */
    }
  }
  const selectChannel = (channel: Channel) => {
    const row = guide.channels.find((c) => c.channel.id === channel.id)
    const p = row && onNow(row)
    if (p) select(p, channel)
  }
  return (
    <>
      {sel && <GuidePanel sel={sel} />}
      <GuideGrid guide={guide} sel={sel} onPick={select} onChannel={selectChannel} />
    </>
  )
}

interface ProgramInfo {
  kind: string
  title: string
  episode?: string
  overview?: string
  airDate?: string
  rating?: string
  score?: number
  quality?: string
  logo?: string
  backdrop?: string
  poster?: string
}

function GuidePanel({ sel }: { sel: Selection }) {
  const { channel, program } = sel
  const [info, setInfo] = useState<ProgramInfo | null>(null)
  const [previewing, setPreviewing] = useState<Channel | null>(null)
  const [muted, setMuted] = useState(true)
  const at = new Date(program.start).getTime()
  useEffect(() => {
    let gone = false
    setInfo(null)
    call<{ info: ProgramInfo }>('GET', `/channels/${channel.id}/info?at=${at + 1000}`)
      .then((r) => !gone && setInfo(r.info))
      .catch(() => undefined)
    return () => {
      gone = true
    }
  }, [channel.id, at])
  // The preview follows the selection after a moment, so moving through the
  // guide doesn't start a stream for every channel passed over.
  useEffect(() => {
    const tm = window.setTimeout(() => setPreviewing(channel), 800)
    return () => window.clearTimeout(tm)
  }, [channel])
  const onAir = at <= Date.now() && new Date(program.end).getTime() > Date.now()
  const year = info?.airDate?.slice(0, 4)
  const mins = Math.round((new Date(program.end).getTime() - at) / 60000)
  return (
    <section className="tvp">
      <div className="tvp-info" data-has-logo={channel.logoUrl ? '' : undefined} style={info?.backdrop ? { backgroundImage: `linear-gradient(90deg, var(--tvp-bg) 30%, color-mix(in srgb, var(--tvp-bg) 55%, transparent)), url(${info.backdrop})` } : undefined}>
        {channel.logoUrl && <img className="tvp-chlogo" src={channel.logoUrl} alt={channel.name} />}
        {info?.logo ? <img className="tvp-logo" src={info.logo} alt={program.title} /> : <h2 className="tvp-title">{program.title}</h2>}
        {(info?.episode || program.subtitle) && <p className="tvp-episode">{info?.episode || program.subtitle}</p>}
        <div className="tvp-badges">
          <span className="tvp-time">
            {clock(program.start)} – {clock(program.end)}
          </span>
          {year && <span className="chip">{year}</span>}
          {info?.rating && <span className="chip">{info.rating}</span>}
          {info?.quality && <span className="chip">{info.quality}</span>}
          {mins > 0 && <span className="chip">{t('{n} MIN', { n: mins })}</span>}
          {info?.score ? <span className="chip">{t('SCORE {n}', { n: info.score.toFixed(1) })}</span> : null}
          {program.kind === 'stream' && <span className="chip">{t('STREAMED')}</span>}
        </div>
        {info?.overview && <p className="tvp-overview">{info.overview}</p>}
        <p className="tvp-when">
          {channel.number} {channel.name}
          {onAir && ` · ${t('{n} min left', { n: minutesLeft(program.end) })}`}
        </p>
        <div className="tvp-actions">
          <button className="tvp-btn" onClick={() => playChannel(channel.id, channel.name)}>
            <Icon name="play" size={14} /> {onAir ? t('Watch') : t('Watch {name}', { name: channel.name })}
          </button>
        </div>
      </div>
      <div className="tvp-video">
        {previewing ? <ChannelPreview channelId={previewing.id} name={previewing.name} muted={muted} /> : <div className="hp-inline" />}
        <button className="tvp-sound" onClick={() => setMuted((m) => !m)} aria-label={muted ? t('Turn the sound on') : t('Turn the sound off')} title={muted ? t('Sound on') : t('Sound off')}>
          <Icon name={muted ? 'volume-x' : 'volume'} size={18} />
        </button>
      </div>
    </section>
  )
}

// A cable box's clock: hours, minutes and seconds.
function GuideClock() {
  const [tm, setT] = useState(() => new Date())
  useEffect(() => {
    const i = window.setInterval(() => setT(new Date()), 1000)
    return () => window.clearInterval(i)
  }, [])
  return (
    <div className="tvg-clock">
      <strong>{tm.toLocaleTimeString(locale(), { hour: 'numeric', minute: '2-digit', second: '2-digit' })}</strong>
      <span>{t('NOW')}</span>
    </div>
  )
}

const movieTitle = (p: Program) => (p.kind === 'movie' && /^\d{4}$/.test(p.subtitle ?? '') ? `${p.title} (${p.subtitle})` : p.title)
const shortEpisode = (sub?: string) => {
  if (!sub) return ''
  const [code, name] = sub.split(' · ')
  const m = /^S(\d+)E(\d+)$/i.exec(code ?? '')
  const c = m ? `S${Number(m[1])}E${Number(m[2])}` : code
  return name ? `${c} - ${name}` : c
}

function GuideGrid({ guide, sel, onPick, onChannel }: { guide: Guide; sel: Selection | null; onPick: (p: Program, c: Channel) => void; onChannel: (c: Channel) => void }) {
  const now = Date.now()
  const pxPerMin = typeof window !== 'undefined' && window.innerWidth < 700 ? 5 : 8
  const from = new Date(guide.from ?? now).getTime()
  const hours = guide.hours ?? 24
  const to = from + hours * 3600_000
  const slots = Array.from({ length: hours * 2 }, (_, i) => from + i * 1800_000)
  const x = (tm: number) => ((Math.min(Math.max(tm, from), to) - from) / 60000) * pxPerMin
  const scroller = useRef<HTMLDivElement>(null)
  // Open on now, with a little of what just finished showing.
  useEffect(() => {
    if (scroller.current) scroller.current.scrollLeft = Math.max(0, x(Date.now()) - 20 * pxPerMin)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [from])
  const page = (minutes: number) => scroller.current?.scrollBy({ left: minutes * pxPerMin, behavior: 'smooth' })
  return (
    <div className="tvg">
      <div className="tvg-names">
        <GuideClock />
        {guide.channels.map(({ channel }) => (
          <button key={channel.id} className={`tvg-name${sel?.channel.id === channel.id ? ' sel' : ''}`} onClick={() => onChannel(channel)} title={channel.name}>
            {channel.logoUrl && <img className="tvg-logo" src={channel.logoUrl} alt="" loading="lazy" />}
            <span className="tvg-ctext">
              <span className="tv-num">{channel.number}</span>
              <span className="tvg-cname">{channel.name}</span>
            </span>
          </button>
        ))}
      </div>
      <div className="tvg-scroll" ref={scroller}>
        <div className="tvg-body" style={{ width: hours * 60 * pxPerMin }}>
          <div className="tvg-times">
            {slots.map((tm) => (
              <span key={tm} style={{ left: x(tm) }}>
                {new Date(tm).toLocaleTimeString(locale(), { hour: 'numeric', minute: '2-digit' })}
              </span>
            ))}
          </div>
          {guide.channels.map(({ channel, programs }) => (
            <div key={channel.id} className="tvg-row">
              {programs
                .filter((p) => new Date(p.end).getTime() > from && new Date(p.start).getTime() < to)
                .map((p) => {
                  const s = new Date(p.start).getTime()
                  const e = new Date(p.end).getTime()
                  const onAir = s <= now && e > now
                  const past = e <= now
                  const isSel = sel?.channel.id === channel.id && sel.program.start === p.start
                  const isMovie = p.kind === 'movie'
                  return (
                    <button
                      key={p.start}
                      className={`tvg-prog tvg-c${showColor(p.title)}${onAir ? ' on' : ''}${past ? ' past' : ''}${isSel ? ' sel' : ''}`}
                      style={{ left: x(s), width: Math.max(2, x(e) - x(s) - 2) }}
                      onClick={() => onPick(p, channel)}
                      title={p.subtitle ? `${p.title} · ${p.subtitle}` : p.title}
                    >
                      <strong>{movieTitle(p)}</strong>
                      {isMovie || !p.subtitle ? (
                        <span className="tvg-chips">
                          {p.rating && <i>{p.rating}</i>}
                          {p.quality && <i>{p.quality}</i>}
                        </span>
                      ) : (
                        <span>{shortEpisode(p.subtitle)}</span>
                      )}
                    </button>
                  )
                })}
            </div>
          ))}
          <div className="tvg-now" style={{ left: x(now) }} />
        </div>
      </div>
      <div className="tvg-paging">
        <button onClick={() => page(-60)} aria-label={t('An hour earlier')}>
          ◀
        </button>
        <button onClick={() => page(60)} aria-label={t('An hour later')}>
          ▶
        </button>
      </div>
    </div>
  )
}

function ProgramSheet({ program, channel, onClose }: { program: Program; channel: Channel; onClose: () => void }) {
  const s = new Date(program.start)
  const e = new Date(program.end)
  const now = Date.now()
  const onAir = s.getTime() <= now && e.getTime() > now
  const day = s.toDateString() === new Date().toDateString() ? t('Today') : s.toLocaleDateString(locale(), { weekday: 'long' })
  const watch = () => {
    onClose()
    if (onAir) playChannel(channel.id, channel.name)
    else if (program.kind === 'episode' || program.kind === 'movie') playTitle(program.kind, program.itemId, program.subtitle ? `${program.title} · ${program.subtitle}` : program.title)
  }
  return (
    <div className="tv-modal" role="dialog" aria-modal="true" aria-label={program.title} onClick={onClose}>
      <div className="card tv-modal-box tv-sheet" onClick={(ev) => ev.stopPropagation()}>
        <header>
          <strong>{program.title}</strong>
          <button className="btn-sm" onClick={onClose} aria-label={t('Close')}>
            <Icon name="x" size={14} />
          </button>
        </header>
        {program.subtitle && <p>{program.subtitle}</p>}
        <p className="hint">
          {channel.number} {channel.name} · {day} {clock(program.start)}–{clock(program.end)}
          {onAir ? ` · ${t('on now, {n} min left', { n: minutesLeft(program.end) })}` : ''}
        </p>
        <footer>
          <span style={{ flex: 1 }} />
          {(onAir || program.kind === 'episode' || program.kind === 'movie') && (
            <button className="primary btn-with-icon" onClick={watch} title={onAir ? t('Tune in where it is now') : t('Play it from the start, right now')}>
              <Icon name="play" size={16} /> {onAir ? t('Watch {name}', { name: channel.name }) : t('Watch it now')}
            </button>
          )}
        </footer>
      </div>
    </div>
  )
}
