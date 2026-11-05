// Usherr: Settings > Re-Posterr > Collections (/settings/collections). Collections Usherr makes
// and keeps up to date in Jellyfin and Plex, from an import list, a TMDB collection, TMDB's
// trending titles, a rule over the library or titles picked by hand (home_collections.go). It
// does Kometa's collections job.
import { useCallback, useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import Icon from '../components/Icon'
import Switch from '../components/Switch'
import { useConfirm } from '../components/ConfirmProvider'
import { useToast } from '../components/Toast'
import { timeAgo } from '../format'
import { plural, t } from '../i18n'
import { useDocumentTitle } from '../documentTitle'
import { collApi, type CollAll, type CollInput, type CollPick, type CollPreview, type CollSource, type PushCollection } from './collectionsApi'
import '../pages/settings/serviceChecks.css' // the rows look like Service checks'
import './collections.css'

const errText = (e: unknown) => (e instanceof Error ? e.message : String(e))

const SOURCES: { value: CollSource; label: string; help: string }[] = [
  { value: 'list', label: t('One of your import lists'), help: t('Trakt, MDBList, a TMDB list, IMDb or Letterboxd: any list under Import lists. Only reading it, nothing is downloaded unless you say so below.') },
  { value: 'tmdb', label: t('A TMDB collection'), help: t('A film series, like "Alien Collection". Type its name or its number on themoviedb.org.') },
  { value: 'trending', label: t('Trending on TMDB'), help: t('This week’s trending films or shows that are in your library.') },
  { value: 'rule', label: t('A rule over your library'), help: t('Titles in your library that match: a genre, years, a rating, how recently added, or just the best rated few.') },
  { value: 'picked', label: t('Titles you pick'), help: t('Search your library and pick the titles yourself.') },
]

const HOURS = [3, 6, 12, 24, 48]

const BLANK: CollInput = { name: '', source: 'rule', config: {}, media: 'movie', servers: ['jellyfin', 'plex'], sortTitle: '', poster: '', visibility: [], onlyLibrary: true, removeMissing: true, enabled: true }

const num = (v: string) => (v.trim() === '' ? undefined : Number(v))

function sourceLine(c: PushCollection, lists: CollAll['lists']): string {
  const cfg = c.config
  switch (c.source) {
    case 'list':
      return t('Import list: {name}', { name: lists.find((l) => l.id === cfg.listId)?.name ?? '?' })
    case 'tmdb':
      return t('TMDB collection {n}', { n: cfg.tmdbName || String(cfg.tmdbCollection ?? '') })
    case 'trending':
      return t('Trending on TMDB, the first {n}', { n: cfg.count ?? 20 })
    case 'picked':
      return plural(cfg.picked?.length ?? 0, '{n} title picked by hand', '{n} titles picked by hand')
  }
  const parts: string[] = []
  if (cfg.genres?.length) parts.push(cfg.genres.join(', '))
  if (cfg.yearFrom || cfg.yearTo) parts.push(`${cfg.yearFrom ?? '…'}–${cfg.yearTo ?? '…'}`)
  if (cfg.minRating) parts.push(t('rated {n}+', { n: cfg.minRating }))
  if (cfg.addedDays) parts.push(plural(cfg.addedDays, 'added in the last day', 'added in the last {n} days'))
  if (cfg.count) parts.push(t('the best rated {n}', { n: cfg.count }))
  return t('Rule: {what}', { what: parts.join(' · ') })
}

function PickTitles({ picked, onChange }: { picked: CollPick[]; onChange: (p: CollPick[]) => void }) {
  const [q, setQ] = useState('')
  const [found, setFound] = useState<CollPick[]>([])
  useEffect(() => {
    if (q.trim().length < 2) return
    const id = window.setTimeout(() => {
      collApi
        .titles(q)
        .then(setFound)
        .catch(() => setFound([]))
    }, 300)
    return () => window.clearTimeout(id)
  }, [q])
  const key = (p: CollPick) => `${p.kind}:${p.tmdbId}:${p.imdbId ?? ''}`
  const has = new Set(picked.map(key))
  const shown = q.trim().length < 2 ? [] : found
  return (
    <div className="pcol-pick">
      <label>
        {t('Find a title in your library')}
        <input value={q} placeholder={t('Type a few letters')} onChange={(e) => setQ(e.target.value)} />
      </label>
      {shown.length > 0 && (
        <ul className="pcol-found">
          {shown.map((p) => (
            <li key={key(p)}>
              <span>
                {p.title} {p.year ? `(${p.year})` : ''} <small>{p.kind === 'tv' ? t('Show') : t('Film')}</small>
              </span>
              <button type="button" className="btn-sm" disabled={has.has(key(p))} onClick={() => onChange([...picked, p])}>
                {has.has(key(p)) ? t('Added') : t('Add')}
              </button>
            </li>
          ))}
        </ul>
      )}
      {picked.length > 0 && (
        <div className="pcol-chips">
          {picked.map((p) => (
            <span key={key(p)} className="pcol-chip">
              {p.title}
              <button type="button" aria-label={t('Take out {name}', { name: p.title })} onClick={() => onChange(picked.filter((x) => key(x) !== key(p)))}>
                <Icon name="x" size={12} />
              </button>
            </span>
          ))}
        </div>
      )}
    </div>
  )
}

function CollectionForm({ initial, id, all, onSaved, onCancel }: { initial: CollInput; id?: number; all: CollAll; onSaved: (c: PushCollection) => void; onCancel: () => void }) {
  const toast = useToast()
  const [form, setForm] = useState<CollInput>(initial)
  const [busy, setBusy] = useState(false)
  const [preview, setPreview] = useState<CollPreview | null>(null)
  const [madeAt, setMadeAt] = useState(0)
  const set = (patch: Partial<CollInput>) => {
    setForm({ ...form, ...patch })
    setPreview(null)
  }
  const cfg = (patch: Partial<CollInput['config']>) => set({ config: { ...form.config, ...patch } })
  const toggle = <T extends string>(list: T[], v: T, on: boolean) => (on ? [...list.filter((x) => x !== v), v] : list.filter((x) => x !== v))
  const src = SOURCES.find((s) => s.value === form.source)

  async function save() {
    setBusy(true)
    try {
      const out = id ? await collApi.update(id, form) : await collApi.add(form)
      toast.success(form.enabled ? t('Saved. Usherr sends it to your media servers now.') : t('Saved.'))
      onSaved(out)
    } catch (e) {
      toast.error(errText(e))
    } finally {
      setBusy(false)
    }
  }

  async function look() {
    setBusy(true)
    try {
      setPreview(await collApi.preview(form))
    } catch (e) {
      toast.error(errText(e))
    } finally {
      setBusy(false)
    }
  }

  async function upload(f: File | undefined) {
    if (!f || !id) return
    try {
      await collApi.uploadPoster(id, f)
      setForm({ ...form, poster: 'upload' })
      setMadeAt(Date.now())
      toast.success(t('Poster saved. It goes to the servers on the next sync.'))
    } catch (e) {
      toast.error(errText(e))
    }
  }

  return (
    <div className="grid-form pcol-form">
      <label>
        {t('Name')}
        <input value={form.name} maxLength={120} placeholder={t('Best of the 80s')} onChange={(e) => set({ name: e.target.value })} />
      </label>
      <label>
        {t('Titles come from')}
        <select value={form.source} onChange={(e) => set({ source: e.target.value as CollSource, config: {} })}>
          {SOURCES.map((s) => (
            <option key={s.value} value={s.value}>
              {s.label}
            </option>
          ))}
        </select>
      </label>
      {src && <p className="field-hint">{src.help}</p>}

      {form.source === 'list' &&
        (all.lists.length === 0 ? (
          <p className="field-hint">
            {t('You have no import lists yet.')} <Link to="/settings/lists">{t('Import lists')}</Link>
          </p>
        ) : (
          <label>
            {t('Import list')}
            <select value={form.config.listId ?? ''} onChange={(e) => cfg({ listId: num(e.target.value) })}>
              <option value="">{t('Choose…')}</option>
              {all.lists.map((l) => (
                <option key={l.id} value={l.id}>
                  {l.name}
                </option>
              ))}
            </select>
          </label>
        ))}
      {form.source === 'tmdb' && (
        <label>
          {t('TMDB collection (name or number)')}
          <input value={form.config.tmdbName ?? (form.config.tmdbCollection ? String(form.config.tmdbCollection) : '')} placeholder="Alien Collection" onChange={(e) => cfg({ tmdbName: e.target.value, tmdbCollection: undefined })} />
        </label>
      )}
      {form.source === 'trending' && (
        <label>
          {t('How many (at most 20)')}
          <input type="number" min={1} max={20} value={form.config.count ?? 20} onChange={(e) => cfg({ count: num(e.target.value) })} />
        </label>
      )}
      {form.source === 'rule' && (
        <>
          <label>
            {t('Genres (any of them, comma between)')}
            <input value={(form.config.genres ?? []).join(',')} placeholder="Horror, Thriller" onChange={(e) => cfg({ genres: e.target.value.split(',') })} />
          </label>
          <label>
            {t('From year')}
            <input type="number" min={1880} max={2200} value={form.config.yearFrom ?? ''} onChange={(e) => cfg({ yearFrom: num(e.target.value) })} />
          </label>
          <label>
            {t('To year')}
            <input type="number" min={1880} max={2200} value={form.config.yearTo ?? ''} onChange={(e) => cfg({ yearTo: num(e.target.value) })} />
          </label>
          <label>
            {t('Rated at least (out of 10)')}
            <input type="number" min={0} max={10} step={0.1} value={form.config.minRating ?? ''} onChange={(e) => cfg({ minRating: num(e.target.value) })} />
          </label>
          <label>
            {t('Added in the last (days)')}
            <input type="number" min={1} max={3650} value={form.config.addedDays ?? ''} onChange={(e) => cfg({ addedDays: num(e.target.value) })} />
          </label>
          <label>
            {t('Only the best rated (how many)')}
            <input type="number" min={1} max={500} value={form.config.count ?? ''} onChange={(e) => cfg({ count: num(e.target.value) })} />
          </label>
        </>
      )}
      {form.source === 'picked' && <PickTitles picked={form.config.picked ?? []} onChange={(picked) => cfg({ picked })} />}

      {(form.source === 'list' || form.source === 'trending' || form.source === 'rule') && (
        <label>
          {t('Films or shows')}
          <select value={form.media} onChange={(e) => set({ media: e.target.value as CollInput['media'] })}>
            <option value="movie">{t('Films')}</option>
            <option value="tv">{t('Shows')}</option>
            <option value="both">{t('Both')}</option>
          </select>
        </label>
      )}

      <fieldset className="pcol-checks">
        <legend>{t('Send it to')}</legend>
        <label>
          <input type="checkbox" checked={form.servers.includes('jellyfin')} onChange={(e) => set({ servers: toggle(form.servers, 'jellyfin', e.target.checked) })} />
          Jellyfin
        </label>
        <label>
          <input type="checkbox" checked={form.servers.includes('plex')} onChange={(e) => set({ servers: toggle(form.servers, 'plex', e.target.checked) })} />
          Plex
        </label>
      </fieldset>
      <label>
        {t('Sort title (optional)')}
        <input value={form.sortTitle} maxLength={120} placeholder={t('Leave empty to sort by its name')} onChange={(e) => set({ sortTitle: e.target.value })} />
      </label>
      <label>
        {t('Poster')}
        <select value={form.poster} onChange={(e) => set({ poster: e.target.value as CollInput['poster'] })}>
          <option value="">{t('The media server’s own')}</option>
          <option value="made">{t('Made in your Re-Posterr style')}</option>
          <option value="upload">{t('A picture you upload')}</option>
        </select>
      </label>
      {form.poster === 'upload' &&
        (id ? (
          <label>
            {t('Upload a poster')}
            <input type="file" accept="image/jpeg,image/png,image/webp" onChange={(e) => void upload(e.target.files?.[0])} />
          </label>
        ) : (
          <p className="field-hint">{t('Save the collection first, then upload its poster here.')}</p>
        ))}
      {id && form.poster !== '' && (initial.poster === form.poster || madeAt > 0) && (
        <img className="pcol-poster" src={collApi.posterUrl(id, form.poster === 'made') + `&m=${madeAt}`} alt={t('The poster it sends')} onError={(e) => (e.currentTarget.style.display = 'none')} />
      )}
      {form.servers.includes('plex') && (
        <fieldset className="pcol-checks">
          <legend>{t('Plex shows it on')}</legend>
          <label>
            <input type="checkbox" checked={form.visibility.includes('library')} onChange={(e) => set({ visibility: toggle(form.visibility, 'library', e.target.checked) })} />
            {t('The library’s Recommended tab')}
          </label>
          <label>
            <input type="checkbox" checked={form.visibility.includes('home')} onChange={(e) => set({ visibility: toggle(form.visibility, 'home', e.target.checked) })} />
            {t('Your Home')}
          </label>
          <label>
            <input type="checkbox" checked={form.visibility.includes('shared')} onChange={(e) => set({ visibility: toggle(form.visibility, 'shared', e.target.checked) })} />
            {t('Friends’ Home')}
          </label>
        </fieldset>
      )}
      {form.source !== 'rule' && (
        <Switch
          checked={!form.onlyLibrary}
          onChange={(v) => set({ onlyLibrary: !v })}
          label={t('Add missing titles to Usherr')}
          description={t('Off: the collection holds only what is already in your library. On: titles it names that you don’t have are added to Usherr as wanted (a few each sync) and join the collection once downloaded.')}
        />
      )}
      <Switch checked={form.removeMissing} onChange={(v) => set({ removeMissing: v })} label={t('Take out titles that no longer belong')} description={t('Off: titles are only ever added.')} />
      <Switch checked={form.enabled} onChange={(v) => set({ enabled: v })} label={t('Keep it up to date')} description={t('Off: Usherr leaves the collection as it is on the servers.')} />

      {preview && (
        <div className="pcol-preview">
          <p>
            <strong>{form.source === 'rule' ? plural(preview.inLibrary, '{n} title in your library matches.', '{n} titles in your library match.') : t('{found} titles, {have} in your library.', { found: preview.found, have: preview.inLibrary })}</strong>
          </p>
          {preview.sample.length > 0 && <p className="field-hint">{preview.sample.join(' · ')}</p>}
          {preview.missing.length > 0 && <p className="field-hint">{t('Not in your library: {list}', { list: preview.missing.join(' · ') })}</p>}
        </div>
      )}
      <div className="row-actions">
        <button type="button" className="btn-sm btn-with-icon" disabled={busy || !form.name.trim()} onClick={() => void look()}>
          <Icon name="search" size={14} /> {t('See what’s in it')}
        </button>
        <button type="button" className="btn-sm" disabled={busy} onClick={onCancel}>
          {t('Cancel')}
        </button>
        <button type="button" className="primary" disabled={busy || !form.name.trim() || form.servers.length === 0} onClick={() => void save()}>
          {id ? t('Save changes') : t('Add collection')}
        </button>
      </div>
    </div>
  )
}

function CollectionRow({ c, all, onChanged, onRemoved }: { c: PushCollection; all: CollAll; onChanged: (c: PushCollection) => void; onRemoved: () => void }) {
  const toast = useToast()
  const confirm = useConfirm()
  const [editing, setEditing] = useState(false)
  const [open, setOpen] = useState(false)

  async function sync() {
    try {
      await collApi.sync(c.id)
      onChanged({ ...c, running: true })
      toast.success(t('Syncing {name} now.', { name: c.name }))
    } catch (e) {
      toast.error(errText(e))
    }
  }

  async function remove() {
    const ok = await confirm({
      title: t('Remove {name}?', { name: c.name }),
      body: <p>{t('Usherr stops keeping it up to date. The titles themselves are never touched.')}</p>,
      confirmLabel: t('Remove'),
      danger: true,
      option: c.targets.length > 0 ? { label: t('Delete it from Jellyfin and Plex too'), hint: c.targets.join(' · '), defaultChecked: false } : undefined,
    })
    if (!ok) return
    try {
      const out = await collApi.remove(c.id, ok.checked)
      if (out.problems?.length) toast.error(out.problems.join(' '))
      onRemoved()
    } catch (e) {
      toast.error(errText(e))
    }
  }

  const state = !c.enabled ? 'paused' : c.lastStatus === 'error' ? 'down' : c.lastStatus === 'ok' ? 'up' : ''
  return (
    <li className={`svc-row pcol-row ${state}`}>
      <span className="svc-dot" aria-hidden="true" />
      <div className="svc-main">
        <div className="svc-head">
          <strong>{c.name}</strong>
          <small>{sourceLine(c, all.lists)}</small>
        </div>
        <div className="svc-numbers">
          {c.running ? (
            <span>{t('Syncing…')}</span>
          ) : c.lastSyncMs ? (
            <span>
              {t('Synced {when}', { when: timeAgo(new Date(c.lastSyncMs).toISOString()) })} · <b>{c.lastMatched}</b> {c.source === 'rule' ? t('titles') : t('of {n} in your library', { n: c.lastFound })}
            </span>
          ) : (
            <span>{c.enabled ? t('Not synced yet') : t('Paused')}</span>
          )}
          {c.targets.length > 0 && <span>{c.targets.join(' · ')}</span>}
        </div>
        {c.lastMessage && <div className={c.lastStatus === 'error' ? 'watch-why' : 'field-hint'}>{c.lastMessage}</div>}
        {c.lastMissing.length > 0 && (
          <button type="button" className="link-btn" onClick={() => setOpen(!open)}>
            {open ? t('Hide the missing titles') : t('Missing from your library ({n})', { n: c.lastFound - c.lastMatched })}
          </button>
        )}
        {open && <p className="field-hint">{c.lastMissing.join(' · ')}</p>}
        {editing && (
          <CollectionForm
            id={c.id}
            all={all}
            initial={{ name: c.name, source: c.source, config: c.config, media: c.media, servers: c.servers, sortTitle: c.sortTitle, poster: c.poster, visibility: c.visibility, onlyLibrary: c.onlyLibrary, removeMissing: c.removeMissing, enabled: c.enabled }}
            onSaved={(out) => {
              setEditing(false)
              onChanged(out)
            }}
            onCancel={() => setEditing(false)}
          />
        )}
      </div>
      {!editing && (
        <div className="svc-actions">
          <button type="button" className="btn-sm" disabled={c.running || !c.enabled} onClick={() => void sync()}>
            {t('Sync now')}
          </button>
          <button type="button" className="btn-sm" onClick={() => setEditing(true)}>
            {t('Edit')}
          </button>
          <button type="button" className="btn-sm" onClick={() => void remove()}>
            {t('Remove')}
          </button>
        </div>
      )}
    </li>
  )
}

export default function PushCollectionsSettings() {
  useDocumentTitle(t('Collections'))
  const toast = useToast()
  const [all, setAll] = useState<CollAll | null>(null)
  const [adding, setAdding] = useState(false)
  const load = useCallback(() => {
    collApi
      .all()
      .then(setAll)
      .catch((e: Error) => toast.error(e.message))
  }, [toast])
  useEffect(load, [load])
  const running = all?.collections.some((c) => c.running) ?? false
  useEffect(() => {
    if (!running) return
    const id = window.setInterval(load, 4000)
    return () => window.clearInterval(id)
  }, [running, load])
  if (!all) return <div>{t('Loading…')}</div>
  const put = (c: PushCollection) => setAll({ ...all, collections: all.collections.some((x) => x.id === c.id) ? all.collections.map((x) => (x.id === c.id ? c : x)) : [...all.collections, c] })

  return (
    <div className="settings-stack">
      <section className="card wide">
        <h2>{t('Collections')}</h2>
        <p style={{ color: 'var(--text-dim)', marginTop: 0 }}>
          {t('Usherr makes these collections in Jellyfin and Plex and keeps them up to date: new titles go in, and titles that no longer belong come out. A collection of the same name already on a server (one Kometa made, say) is taken over, not made twice.')}
        </p>
        {!all.jellyfin && !all.plex && <p className="pcol-warn">{t('Connect Jellyfin or Plex first (Settings → Media servers).')}</p>}
        {all.collections.length === 0 && !adding && <p>{t('No collections yet.')}</p>}
        {all.collections.length > 0 && (
          <ul className="svc-list">
            {all.collections.map((c) => (
              <CollectionRow key={c.id} c={c} all={all} onChanged={put} onRemoved={() => setAll({ ...all, collections: all.collections.filter((x) => x.id !== c.id) })} />
            ))}
          </ul>
        )}
        {adding ? (
          <CollectionForm
            initial={BLANK}
            all={all}
            onSaved={(out) => {
              setAdding(false)
              put(out)
            }}
            onCancel={() => setAdding(false)}
          />
        ) : (
          <div className="row-actions">
            <button type="button" className="btn-sm btn-with-icon" onClick={() => setAdding(true)}>
              <Icon name="plus" size={14} /> {t('Add a collection')}
            </button>
            {all.collections.length > 0 && (
              <button
                type="button"
                className="btn-sm"
                disabled={running}
                onClick={() =>
                  void collApi
                    .syncAll()
                    .then(() => {
                      toast.success(t('Syncing every collection now.'))
                      load()
                    })
                    .catch((e: Error) => toast.error(e.message))
                }
              >
                {t('Sync all now')}
              </button>
            )}
          </div>
        )}
      </section>
      <section className="card wide">
        <h2>{t('How often')}</h2>
        <label className="pcol-hours">
          {t('Sync every')}{' '}
          <select
            value={all.hours}
            onChange={(e) =>
              void collApi
                .setHours(Number(e.target.value))
                .then((r) => {
                  setAll({ ...all, hours: r.hours })
                  toast.success(t('Saved.'))
                })
                .catch((er: Error) => toast.error(er.message))
            }
          >
            {!HOURS.includes(all.hours) && <option value={all.hours}>{plural(all.hours, '{n} hour', '{n} hours')}</option>}
            {HOURS.map((h) => (
              <option key={h} value={h}>
                {plural(h, '{n} hour', '{n} hours')}
              </option>
            ))}
          </select>
        </label>
      </section>
    </div>
  )
}
