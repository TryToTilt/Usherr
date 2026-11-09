// Usherr Broadcast: the channel editor made for a remote, in the Broadcast
// look (TV.tsx keeps its own editor for Classic and Neu). The name is the
// only typing (the TV's mic or its keyboard), the number has − and +,
// the shows are chips to switch on and off,
// order and start times are chips too, and Save, Watch it now and Fill it
// with AI sit at the bottom. Genres, decades, movies, streamed shows, parts
// of the day, set times, filler, the logo and Delete are under Advanced.
import { useEffect, useMemo, useState } from 'react'
import { api, type Series } from '../api'
import Icon from '../components/Icon'
import { useConfirm } from '../components/ConfirmProvider'
import { t } from '../i18n'
import { playChannel } from './Player'
import { AIBuildButton, type Draft } from './TVPacks'
import TVScheduling from './TVScheduling'
import { StreamChips, StreamFind, type StreamPick } from './TVStreamFind'
import type { Channel, Rules } from './TV'
import './tvBroadcast.css'

async function call<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(`/api/tv${path}`, { method, credentials: 'include', headers: body ? { 'Content-Type': 'application/json' } : undefined, body: body ? JSON.stringify(body) : undefined })
  const data = await res.json().catch(() => null)
  if (!res.ok) throw new Error(data?.error ?? t('Something went wrong (error {status}).', { status: res.status }))
  return data as T
}

const DECADES = [1950, 1960, 1970, 1980, 1990, 2000, 2010, 2020]
const SHOWS_FIRST = 24 // chips shown before "More shows"
const toggle = <T,>(list: T[] | undefined, v: T) => ((list ?? []).includes(v) ? (list ?? []).filter((x) => x !== v) : [...(list ?? []), v])

const ORDERS: { v: NonNullable<Rules['order']>; label: () => string }[] = [
  { v: 'inorder', label: () => t('In order') },
  { v: 'rotate', label: () => t('Take turns') },
  { v: 'shuffle', label: () => t('Shuffle') },
]
const GRIDS: { v: number; label: () => string }[] = [
  { v: 0, label: () => t('Back to back') },
  { v: 15, label: () => t('Every 15 min') },
  { v: 30, label: () => t('Every 30 min') },
  { v: 60, label: () => t('On the hour') },
]

export default function BroadcastEditor({ channel, draft, onClose, onSaved }: { channel: Channel | null; draft?: Draft; onClose: () => void; onSaved: () => void }) {
  const confirm = useConfirm()
  const [name, setName] = useState(channel?.name ?? draft?.name ?? '')
  const [number, setNumber] = useState(channel?.number ?? 0)
  const [rules, setRules] = useState<Rules>(channel?.rules ?? (draft?.rules as Rules | undefined) ?? { order: 'rotate' })
  const [kind, setKind] = useState<'library' | 'live'>(channel?.kind === 'live' ? 'live' : 'library')
  const [enabled, setEnabled] = useState(channel?.enabled ?? true)
  const [logo, setLogo] = useState(channel?.logoUrl ?? '')
  const [note, setNote] = useState(draft?.note ?? '')
  const [series, setSeries] = useState<Series[]>([])
  const [allShows, setAllShows] = useState(false)
  const [advanced, setAdvanced] = useState(false)
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
  // the picked shows first, so they're always in reach
  const ordered = useMemo(() => [...series.filter((s) => picked.has(s.id)), ...series.filter((s) => !picked.has(s.id))], [series]) // eslint-disable-line react-hooks/exhaustive-deps
  const shows = allShows ? ordered : ordered.slice(0, Math.max(SHOWS_FIRST, picked.size))
  const streamed = rules.stream ?? []

  const save = async (): Promise<Channel | null> => {
    setSaving(true)
    setError('')
    try {
      const body = { name, number: number || undefined, kind, rules, enabled, logoUrl: logo }
      return await (channel ? call<Channel>('PUT', `/channels/${channel.id}`, body) : call<Channel>('POST', '/channels', body))
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
      return null
    } finally {
      setSaving(false)
    }
  }
  const saveAndClose = async () => {
    if (await save()) onSaved()
  }
  // Watch it now: saved first, then tuned in (its first programmes take a moment to measure).
  const watchNow = async () => {
    const saved = await save()
    if (!saved) return
    onSaved()
    const id = saved.id ?? channel?.id
    if (id) window.setTimeout(() => playChannel(id, saved.name ?? name), 1500)
  }
  const remove = async () => {
    if (!channel || !(await confirm({ title: t('Delete the channel {name}?', { name: channel.name }), body: t('Your shows and movies stay in the library.'), confirmLabel: t('Delete'), danger: true }))) return
    call('DELETE', `/channels/${channel.id}`)
      .then(onSaved)
      .catch((e: Error) => setError(e.message))
  }
  const fillWithAI = (d: Draft) => {
    if (!name.trim() && d.name) setName(d.name)
    setRules((r) => ({ ...r, ...(d.rules as Rules) }))
    setNote(d.note ?? t('Filled in. Look it over, then Save.'))
  }
  const uploadLogo = (file: File) => {
    if (!channel) return
    const fd = new FormData()
    fd.append('file', file)
    fetch(`/api/tv/channels/${channel.id}/logo`, { method: 'POST', credentials: 'include', body: fd })
      .then(async (r) => {
        const d = await r.json().catch(() => null)
        if (!r.ok) throw new Error(d?.error ?? t('Upload failed (error {status}).', { status: r.status }))
        setLogo(d.logoUrl ?? '')
      })
      .catch((e: Error) => setError(e.message))
  }

  const typing = (el: EventTarget | null) => el instanceof HTMLElement && (el.tagName === 'INPUT' || el.tagName === 'TEXTAREA' || el.tagName === 'SELECT')
  return (
    <div
      className="bed-scrim"
      data-walk="tv-editor"
      role="dialog"
      aria-modal="true"
      aria-label={channel ? t('Edit {name}', { name: channel.name }) : t('New channel')}
      onKeyDown={(e) => {
        // Back closes it (a field keeps its own Escape)
        if (e.key === 'Escape' && !typing(e.target)) {
          e.preventDefault()
          onClose()
        }
      }}
    >
      <div className="bed">
        <div className="bed-main">
          <span className="bed-where">{t('Live TV · {what}', { what: channel ? t('Edit channel') : t('New channel') })}</span>
          <h1>{name.trim() || (channel ? channel.name : t('New channel'))}</h1>
          {note && <p className="notice">{note}</p>}

          <span className="bed-label" id="bed-name">
            {t('Name')}
          </span>
          <div className="bed-name">
            <input
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder={t('Saturday Cartoons')}
              aria-labelledby="bed-name"
              enterKeyHint="done"
              autoFocus={!channel && !draft}
              data-autofocus={!channel && !draft ? '' : undefined}
            />
          </div>
          <span className="bed-hint">{t('OK, then type it or say it with the remote’s mic.')}</span>

          <span className="bed-label">{t('Channel number')}</span>
          <div className="bed-step">
            <button type="button" className="bed-btn" onClick={() => setNumber((n) => Math.max(0, n - 1))} aria-label={t('One lower')} disabled={number <= 0}>
              −
            </button>
            <output aria-live="polite">{number > 0 ? number : t('Auto')}</output>
            <button type="button" className="bed-btn" onClick={() => setNumber((n) => n + 1)} aria-label={t('One higher')}>
              +
            </button>
          </div>

          {!channel && (
            <>
              <span className="bed-label">{t('Kind of channel')}</span>
              <div className="bed-chips" role="radiogroup" aria-label={t('Kind of channel')}>
                <button type="button" role="radio" aria-checked={kind === 'library'} className="bed-chip" onClick={() => setKind('library')}>
                  {t('From my library')}
                </button>
                <button type="button" role="radio" aria-checked={kind === 'live'} className="bed-chip" onClick={() => setKind('live')}>
                  {t('Live stream')}
                </button>
              </div>
            </>
          )}

          {kind === 'live' ? (
            <>
              <span className="bed-label">{t('Stream address (.m3u8)')}</span>
              <div className="bed-name">
                <input value={rules.streamUrl ?? ''} onChange={(e) => setRules((r) => ({ ...r, streamUrl: e.target.value }))} placeholder="https://…/playlist.m3u8" inputMode="url" aria-label={t('Stream address (.m3u8)')} />
              </div>
              <span className="bed-hint">{t('The guide feed for it is under Advanced.')}</span>
            </>
          ) : (
            <>
              <span className="bed-label">
                {t('What plays')} · {t('{n} picked', { n: picked.size + streamed.length })}
              </span>
              <div className="bed-chips">
                {shows.map((s) => (
                  <button key={s.id} type="button" className="bed-chip" aria-pressed={picked.has(s.id)} onClick={() => setRules((r) => ({ ...r, series: toggle(r.series, s.id) }))}>
                    {picked.has(s.id) && <Icon name="check" size={16} />}
                    {s.title}
                  </button>
                ))}
                {streamed.map((s) => (
                  <button key={`s${s.tmdbId}`} type="button" className="bed-chip" aria-pressed={true} title={t('Stop streaming this show on the channel')} onClick={() => setRules((r) => ({ ...r, stream: (r.stream ?? []).filter((x) => x.tmdbId !== s.tmdbId) }))}>
                    <Icon name="check" size={16} />
                    {s.title} · {t('stream')}
                  </button>
                ))}
                {ordered.length > shows.length && (
                  <button type="button" className="bed-chip" onClick={() => setAllShows(true)}>
                    {t('More shows ({n})', { n: ordered.length - shows.length })}
                  </button>
                )}
                {series.length === 0 && <span className="bed-hint">{t('No shows in the library yet. Genres and streamed shows are under Advanced.')}</span>}
              </div>

              <span className="bed-label">{t('Order')}</span>
              <div className="bed-chips" role="radiogroup" aria-label={t('Order')}>
                {ORDERS.map((o) => (
                  <button key={o.v} type="button" role="radio" aria-checked={(rules.order ?? 'rotate') === o.v} className="bed-chip" onClick={() => setRules((r) => ({ ...r, order: o.v }))}>
                    {o.label()}
                  </button>
                ))}
              </div>

              <span className="bed-label">{t('Start times')}</span>
              <div className="bed-chips" role="radiogroup" aria-label={t('Start times')}>
                {GRIDS.map((g) => (
                  <button key={g.v} type="button" role="radio" aria-checked={(rules.grid ?? 0) === g.v} className="bed-chip" onClick={() => setRules((r) => ({ ...r, grid: g.v || undefined }))}>
                    {g.label()}
                  </button>
                ))}
              </div>
            </>
          )}

          {error && <p className="error-text">{error}</p>}
          <div className="bed-actions">
            <button type="button" className="bed-btn pri" data-autofocus={channel || draft ? '' : undefined} disabled={saving || !name.trim()} onClick={() => void saveAndClose()}>
              {saving ? t('Saving…') : t('Save')}
            </button>
            <button type="button" className="bed-btn" disabled={saving || !name.trim()} onClick={() => void watchNow()}>
              <Icon name="play" size={18} /> {t('Watch it now')}
            </button>
            {kind === 'library' && <AIBuildButton onPick={fillWithAI} label={t('Fill it with AI')} className="bed-btn" />}
            <button type="button" className="bed-btn" aria-expanded={advanced} onClick={() => setAdvanced((a) => !a)}>
              <Icon name="sliders" size={18} /> {t('Advanced')}
            </button>
            <button type="button" className="bed-btn" onClick={onClose}>
              {t('Cancel')}
            </button>
          </div>

          {advanced && (
            <div className="bed-adv">
              {kind === 'live' ? (
                <>
                  <label className="tv-field">
                    <span>{t('Guide feed (XMLTV, optional)')}</span>
                    <input value={rules.guideUrl ?? ''} onChange={(e) => setRules((r) => ({ ...r, guideUrl: e.target.value }))} placeholder="https://i.mjh.nz/PlutoTV/us.xml.gz" inputMode="url" />
                  </label>
                  <label className="tv-field">
                    <span>{t('Its channel id')}</span>
                    <input value={rules.guideId ?? ''} onChange={(e) => setRules((r) => ({ ...r, guideId: e.target.value }))} />
                  </label>
                </>
              ) : (
                <>
                  <span className="bed-label">{t('Shows to stream')}</span>
                  <StreamFind picked={streamed} onPick={(p: StreamPick) => setRules((r) => ({ ...r, stream: [...(r.stream ?? []), p] }))} />
                  <StreamChips list={streamed} onDrop={(p) => setRules((r) => ({ ...r, stream: (r.stream ?? []).filter((x) => x.tmdbId !== p.tmdbId) }))} />
                  <span className="bed-label">{t('And every show in these genres')}</span>
                  <div className="bed-chips">
                    {genres.map((g) => (
                      <button key={g} type="button" className="bed-chip" aria-pressed={(rules.genres ?? []).includes(g)} onClick={() => setRules((r) => ({ ...r, genres: toggle(r.genres, g) }))}>
                        {g}
                      </button>
                    ))}
                  </div>
                  <span className="bed-label">{t('From these decades (optional)')}</span>
                  <div className="bed-chips">
                    {DECADES.map((d) => (
                      <button key={d} type="button" className="bed-chip" aria-pressed={(rules.decades ?? []).includes(d)} onClick={() => setRules((r) => ({ ...r, decades: toggle(r.decades, d) }))}>
                        {t('{n}s', { n: String(d).slice(2) })}
                      </button>
                    ))}
                  </div>
                  <div className="bed-chips">
                    <button type="button" className="bed-chip" aria-pressed={!!rules.allMovies} onClick={() => setRules((r) => ({ ...r, allMovies: !r.allMovies }))}>
                      {rules.genres?.length || rules.decades?.length ? t('Also movies in those genres and decades') : t('Also movies from the library')}
                    </button>
                  </div>
                  {rules.allMovies && (
                    <>
                      <span className="bed-label">{t('Movies a day')}</span>
                      <div className="bed-step">
                        <button type="button" className="bed-btn" onClick={() => setRules((r) => ({ ...r, maxMoviesPerDay: Math.max(0, (r.maxMoviesPerDay ?? 0) - 1) }))} aria-label={t('Fewer')}>
                          −
                        </button>
                        <output>{rules.maxMoviesPerDay ? rules.maxMoviesPerDay : t('Any')}</output>
                        <button type="button" className="bed-btn" onClick={() => setRules((r) => ({ ...r, maxMoviesPerDay: (r.maxMoviesPerDay ?? 0) + 1 }))} aria-label={t('More')}>
                          +
                        </button>
                      </div>
                    </>
                  )}
                  <TVScheduling rules={rules} setRules={setRules} series={series} genres={genres} />
                </>
              )}
              {channel && (
                <>
                  <span className="bed-label">{t('Logo')}</span>
                  <div className="bed-chips">
                    {logo && <img src={logo} alt="" style={{ maxHeight: 48, maxWidth: 120, objectFit: 'contain' }} />}
                    <label className="bed-chip">
                      {t('Upload a picture')}
                      <input type="file" hidden accept="image/png,image/jpeg,image/webp,image/gif" onChange={(e) => e.target.files?.[0] && uploadLogo(e.target.files[0])} />
                    </label>
                    {logo && (
                      <button
                        type="button"
                        className="bed-chip"
                        onClick={() => {
                          if (logo.startsWith('/api/')) call('DELETE', `/channels/${channel.id}/logo`).catch(() => undefined)
                          setLogo('')
                        }}
                      >
                        {t('Remove')}
                      </button>
                    )}
                  </div>
                  <div className="bed-chips">
                    <button type="button" className="bed-chip" aria-pressed={enabled} onClick={() => setEnabled((v) => !v)}>
                      {enabled ? t('Shown in the guide and in Jellyfin') : t('Switched off')}
                    </button>
                    <button type="button" className="bed-chip" onClick={() => void remove()}>
                      <Icon name="x" size={16} /> {t('Delete the channel')}
                    </button>
                  </div>
                </>
              )}
            </div>
          )}
        </div>
        <aside className="bed-side">
          <p>{t('The name is the only typing: the remote’s mic covers it.')}</p>
          <p>{t('Shows come from your library: OK switches one on or off. Streamed shows, genres, movies, parts of the day and what plays between shows are under Advanced.')}</p>
        </aside>
      </div>
    </div>
  )
}
