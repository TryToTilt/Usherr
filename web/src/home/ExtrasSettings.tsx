// Home fork: Settings > Extras. One card for each thing the fork adds that
// needs setting up, in the style of the setup wizard's service cards: what it
// is, whether it's ready, what's missing, and a Test button.
import HelpLink from '../components/HelpLink'
import { useCallback, useEffect, useRef, useState, type ReactNode } from 'react'
import { Link } from 'react-router-dom'
import Icon from '../components/Icon'
import TestButton from '../components/TestButton'
import { useToast } from '../components/Toast'
import { copyText } from '../components/CopyBox'
import { QRCode } from '../components/TwoStepCard'
import { plural, t, tx } from '../i18n'
import './news.css'
import './cinema.css' // Usherr: the Tickets button
import { TICKET_SITES, type TicketService } from './cinema' // Usherr
import { TicketsButton } from './InTheaters' // Usherr
import { sportsApi, type SportsSettings } from './sportsApi' // Usherr
import { readToMeApi, type RtvSettings } from './readToMeApi'

interface Extras {
  server: { kind: '' | 'jellyfin' | 'emby'; name: string; plex: boolean }
  address: { value: string; suggested: string }
  streaming: { url: string; defaultUrl: string; tokenSet: boolean; tokenFile: boolean; ready: boolean; problem?: string }
  streamed: { folder: string; folderOk: boolean; folderNote?: string; serverFolder: string; serverFolderBy: '' | 'set' | 'mapping'; titles: number }
  liveTv: { channels: number; feed?: string }
  playOnTv: { address: string; name: string; adb: boolean; keyMade: boolean; ready: boolean }
  comics: { enabled: boolean; keySet: boolean }
  nsfw: { enabled: boolean; allowed?: boolean; keySet: boolean } // allowed: Usherr (USHERR_ALLOW_ADULT)
  direct: { enabled: boolean; flareUrl: string }
}

async function call<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(path, {
    method,
    credentials: 'include',
    cache: 'no-store',
    headers: body === undefined ? undefined : { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  const text = await res.text()
  const data = text ? JSON.parse(text) : null
  if (!res.ok) throw new Error(data?.error ?? t('Something went wrong (error {status}).', { status: res.status }))
  return data as T
}

const test = (what: string) => () =>
  call<{ message: string }>('POST', `/api/home/extras/test/${what}`).then(
    (r) => ({ ok: true, message: r.message }),
    (e: Error) => ({ ok: false, message: e.message }),
  )

type State = 'ready' | 'setup' | 'off'
const STATE_TEXT: Record<State, string> = { ready: t('ready'), setup: t('needs setting up'), off: t('off') }

function Card({ id, title, state, blurb, needs, guide, children }: { id?: string; title: string; state: State; blurb: ReactNode; needs?: ReactNode; guide?: string; children?: ReactNode }) {
  return (
    <section id={id} className={`card service-card ${state === 'ready' ? 'is-included' : 'is-optional'}`}>
      <h2>
        {title} <span className="req-badge optional">{t('Optional')}</span>{' '}
        <span className={`badge ${state === 'ready' ? 'downloaded' : 'missing'}`}>{STATE_TEXT[state]}</span>
        {guide && <HelpLink topic={`home-setup#${guide}`} />}
      </h2>
      <p style={{ color: 'var(--text-dim)' }}>{blurb}</p>
      {state !== 'ready' && needs && (
        <div className="notice notice-warn">
          <strong>{t('Needs:')}</strong> {needs}
        </div>
      )}
      {children}
    </section>
  )
}

const row = { display: 'flex', gap: 8, flexWrap: 'wrap', alignItems: 'center' } as const
const small = { color: 'var(--text-dim)', margin: 0, fontSize: 13 } as const

// `only` shows just these cards (the setup wizard's Extras step uses it).
export default function ExtrasSettings({ only }: { only?: string[] } = {}) {
  const show = (k: string) => !only || only.includes(k)
  const [x, setX] = useState<Extras | null>(null)
  const [error, setError] = useState('')
  const toast = useToast()
  const load = useCallback(() => {
    call<Extras>('GET', '/api/home/extras')
      .then((d) => {
        setX(d)
        setError('')
      })
      .catch((e: Error) => setError(e.message))
  }, [])
  useEffect(load, [load])

  const save = async (body: Record<string, unknown>, done: string) => {
    try {
      setX(await call<Extras>('PUT', '/api/home/extras', body))
      toast.success(done)
      return true
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
      return false
    }
  }
  const act = async (method: string, path: string, body?: unknown) => {
    try {
      const r = await call<{ message?: string }>(method, path, body)
      toast.success(r?.message ?? t('Done.'))
      load()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    }
  }

  if (!x) {
    return error ? (
      <div>
        <p className="error-text">{error}</p>
        <button onClick={load}>{t('Try again')}</button>
      </div>
    ) : (
      <p>{t('Loading…')}</p>
    )
  }
  const server = x.server.kind === 'emby' ? 'Emby' : 'Jellyfin'
  return (
    <div className="settings-stack">
      {show('server') && <MediaServerCard x={x} />}
      {show('address') && <AddressCard x={x} save={save} />}
      {show('streaming') && <StreamingEngineCard />}
      {show('streaming') && <StreamingCard x={x} save={save} />}
      {show('streamed') && <StreamedCard x={x} save={save} act={act} server={server} />}
      {show('livetv') && <LiveTVCard x={x} act={act} server={server} />}
      {show('addon') && <AddonCard />}
      {show('playontv') && <PlayOnTVCard x={x} save={save} act={act} />}
      {show('ai') && <AICard />}
      {show('scores') && <ScoresCard />}
      {show('news') && <NewsCard />}
      {show('cinema') && <CinemaCard />}
      {show('marquee') && <MarqueeCard inWizard={!!only} />}
      {show('preshow') && <PreshowCard inWizard={!!only} />}
      {show('comics') && <ComicsCard x={x} act={act} />}
      {show('direct') && <DirectCard x={x} save={save} />}
      {show('sports') && <SportsCard />}
      {show('readtome') && <ReadToMeCard />}
      {show('nsfw') && <NSFWCard x={x} act={act} />}
    </div>
  )
}

type Save = (body: Record<string, unknown>, done: string) => Promise<boolean>
type Act = (method: string, path: string, body?: unknown) => Promise<void>

function MediaServerCard({ x }: { x: Extras }) {
  const ok = x.server.kind !== ''
  return (
    <Card
      title={t('Jellyfin or Emby')}
      guide="jellyfin-or-emby"
      state={ok ? 'ready' : 'setup'}
      blurb={
        <>
          {t("The player, Live TV in your media server's app and the streamed library work with Jellyfin (recommended) or Emby.")}{' '}
          {x.server.plex && t("Plex is connected too: it gets new files as usual, but streamed titles and Live TV stay inside Mediarium, because Plex can't read them.")}
        </>
      }
      needs={tx('a Jellyfin or Emby server, connected under {link}. Add the folder mapping there too if it sees your folders under other names.', {
        link: <Link to="/settings/media-servers">{t('Settings > Connections > Media servers')}</Link>,
      })}
    >
      {ok && (
        <p style={{ margin: 0 }}>
          {tx('Using {name} ({kind}).', { name: <strong>{x.server.name}</strong>, kind: x.server.kind === 'emby' ? 'Emby' : 'Jellyfin' })}
        </p>
      )}
    </Card>
  )
}

function AddressCard({ x, save }: { x: Extras; save: Save }) {
  const [v, setV] = useState(x.address.value || x.address.suggested)
  return (
    <Card
      title={t("Mediarium's address on your network")}
      guide="mediariums-address-on-your-network"
      state={x.address.value ? 'ready' : 'setup'}
      blurb={t("The address your TV and media server reach Mediarium at. Live TV and the streamed library hand it out, so it has to work from another device: your server's network address, not localhost.")}
      needs={t('the address, such as http://192.168.1.20:8264.')}
    >
      <div className="grid-form">
        <label>
          {t('Address')}
          <input value={v} onChange={(e) => setV(e.target.value)} placeholder="http://192.168.1.20:8264" autoComplete="off" spellCheck={false} />
        </label>
        <div style={row}>
          <button className="primary" disabled={v.trim() === x.address.value} onClick={() => void save({ address: v }, t('Address saved.'))}>
            {t('Save')}
          </button>
          {x.address.value && <TestButton label={t('Test it')} run={test('address')} />}
        </div>
      </div>
    </Card>
  )
}

interface AddonInfo {
  key: string
  base: string
  manifestUrl: string
  https: boolean
  stremioUrl?: string
  qr?: string[]
  streamsProblem?: string
}

// Usherr: the add-on for Stremio and Nuvio. Each person has a private link; the
// apps take it once and then show Usherr's catalogs, streams and Live TV.
export function AddonCard() {
  const toast = useToast()
  const [info, setInfo] = useState<AddonInfo | null>(null)
  const [base, setBase] = useState('')
  const [error, setError] = useState('')
  const load = useCallback((b: string) => {
    call<AddonInfo>('GET', `/api/addon${b ? `?base=${encodeURIComponent(b)}` : ''}`)
      .then((x) => {
        setInfo(x)
        setError('')
        setBase((cur) => cur || x.base)
      })
      .catch((e: Error) => setError(e.message))
  }, [])
  useEffect(() => load(''), [load])
  const renew = async () => {
    if (!window.confirm(t('Make a new link? The old one stops working, and each app has to add the new one.'))) return
    try {
      setInfo(await call<AddonInfo>('POST', `/api/addon/regenerate${base ? `?base=${encodeURIComponent(base)}` : ''}`))
      toast.success(t('A new link was made.'))
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    }
  }
  return (
    <Card
      title={t('Stremio and Nuvio')}
      state={info ? 'ready' : 'setup'}
      blurb={t("One add-on for both apps: your library and streamed titles, your watchlist (thumbs up), releases from your Usenet providers for any title, subtitles from your files, and your Live TV channels. Add the link once in each app; it stays.")}
      needs={error || undefined}
    >
      {info && (
        <div className="grid-form">
          <label>
            {t('The address your devices use for Usherr')}
            <input style={{ width: '100%' }} value={base} onChange={(e) => setBase(e.target.value)} onBlur={() => load(base.trim())} placeholder="http://192.168.1.20:8264" spellCheck={false} />
          </label>
          <p style={small}>{t("Use the address your phone or TV reaches Usherr at (your home network address, or your https address from outside). The link below is made from it.")}</p>
          <label>
            {t('Your add-on link')}
            <input style={{ width: '100%' }} readOnly value={info.manifestUrl} onFocus={(e) => e.currentTarget.select()} />
          </label>
          <div style={row}>
            <button
              className="primary"
              onClick={() =>
                void copyText(info.manifestUrl).then(
                  () => toast.success(t('Copied.')),
                  () => toast.error(t("Couldn't copy it. Select the link and copy it by hand.")),
                )
              }
            >
              {t('Copy the link')}
            </button>
            {info.stremioUrl && (
              <a className="btn" href={info.stremioUrl}>
                {t('Install in Stremio')}
              </a>
            )}
            <button onClick={() => void renew()}>{t('Make a new link')}</button>
          </div>
          {info.qr && (
            <div style={row}>
              <QRCode rows={info.qr} label={t('QR code of your add-on link')} />
              <p style={{ ...small, maxWidth: 420 }}>
                {t('In Stremio: Add-ons, then Add add-on, then paste the link. In Nuvio: add it under its add-ons or plugins, with the same link. Scan the code with a phone camera to get the link onto the phone.')}
              </p>
            </div>
          )}
          {info.streamsProblem && <p style={small}>{t('Library, watchlist and Live TV work now. For releases from Usenet: {problem}', { problem: info.streamsProblem })}</p>}
          {!info.https && <p style={small}>{t('A one-tap Install in Stremio needs an https address; with a home network address, paste the link by hand.')}</p>}
        </div>
      )}
    </Card>
  )
}

interface EngineProfile {
  quality: string
  maxSizeGB: number
  exclude: string
}
interface EngineInfo {
  engine: 'streamnzb' | 'usherr'
  engines: Record<'streamnzb' | 'usherr', { ready: boolean; problem?: string }>
  profiles: EngineProfile[]
  servers: { host: string; inUse: number; allowed: number; configured: number; idle: number }[]
  sessions: { release: string; file: string; sizeMB: number; idleSec: number }[]
  speed: number
  indexers: { name: string; used: number; limit: number; note?: string }[]
}

interface ImportItem {
  name: string
  detail: string
  existing: boolean
}
interface ImportPreview {
  providers: ImportItem[]
  indexers: ImportItem[]
  profiles: ImportItem[]
}

// Usherr: copy StreamNZB's Usenet providers, indexers and size limits, after
// showing what is there. Logins are copied by the server and never shown.
function StreamNZBImport({ onDone }: { onDone: () => void }) {
  const toast = useToast()
  const [open, setOpen] = useState(false)
  const [config, setConfig] = useState('')
  const [fileName, setFileName] = useState('')
  const [pre, setPre] = useState<ImportPreview | null>(null)
  const [pick, setPick] = useState<{ providers: string[]; indexers: string[]; profiles: boolean }>({ providers: [], indexers: [], profiles: true })
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const look = async (cfg: string) => {
    setBusy(true)
    setError('')
    try {
      const p = await call<ImportPreview>('POST', '/api/home/streaming/import/preview', { config: cfg })
      setPre(p)
      setPick({ providers: p.providers.filter((x) => !x.existing).map((x) => x.name), indexers: p.indexers.filter((x) => !x.existing).map((x) => x.name), profiles: p.profiles.some((x) => !x.existing) })
    } catch (e) {
      setPre(null)
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }
  const toggle = (k: 'providers' | 'indexers', name: string) => setPick((p) => ({ ...p, [k]: p[k].includes(name) ? p[k].filter((n) => n !== name) : [...p[k], name] }))
  const run = async () => {
    setBusy(true)
    try {
      const r = await call<{ imported: string[]; skipped: string[] }>('POST', '/api/home/streaming/import', { config, ...pick })
      toast.success(r.imported.length ? t('Imported: {list}.', { list: r.imported.join(', ') }) : t('Nothing new to import.'))
      if (r.skipped.length) toast.error(t('Left out: {list}.', { list: r.skipped.join('; ') }))
      setPre(null)
      setOpen(false)
      onDone()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }
  const list = (title: string, k: 'providers' | 'indexers', items: ImportItem[]) =>
    items.length > 0 && (
      <div>
        <strong>{title}</strong>
        {items.map((it) => (
          <label key={it.name} style={row}>
            <input type="checkbox" disabled={it.existing} checked={!it.existing && pick[k].includes(it.name)} onChange={() => toggle(k, it.name)} />
            <span>
              {it.name} <span style={small}>{it.detail}</span> {it.existing && <span className="badge downloaded">{t('already in Usherr')}</span>}
            </span>
          </label>
        ))}
      </div>
    )
  if (!open)
    return (
      <div style={row}>
        <button onClick={() => setOpen(true)}>{t('Import from StreamNZB…')}</button>
      </div>
    )
  const nothing = pre && !pre.providers.some((x) => !x.existing) && !pre.indexers.some((x) => !x.existing) && !pre.profiles.some((x) => !x.existing)
  return (
    <div className="notice" style={{ display: 'grid', gap: 8 }}>
      <strong>{t("Copy StreamNZB's settings")}</strong>
      <p style={small}>
        {t("Uses StreamNZB's config.json (mapped into Usherr at /streamnzb/config.json, or chosen here). You see what is in it first, and only what you tick is copied. Logins are copied into Usherr's own settings and never shown.")}
      </p>
      <div style={row}>
        <label className="btn-like">
          <input
            type="file"
            accept=".json,application/json"
            style={{ display: 'none' }}
            onChange={(e) => {
              const f = e.target.files?.[0]
              if (!f) return
              setFileName(f.name)
              void f.text().then((txt) => {
                setConfig(txt)
                void look(txt)
              })
            }}
          />
          <span className="btn-sm">{fileName ? fileName : t('Choose config.json')}</span>
        </label>
        <button disabled={busy} onClick={() => void look(config)}>
          {busy ? t('Looking…') : config ? t('Look again') : t('Use the mapped file')}
        </button>
        <button onClick={() => (setOpen(false), setPre(null), setError(''))}>{t('Cancel')}</button>
      </div>
      {error && <p className="error-text">{error}</p>}
      {pre && (
        <>
          {list(t('Usenet providers'), 'providers', pre.providers)}
          {list(t('Indexers'), 'indexers', pre.indexers)}
          {pre.profiles.length > 0 && (
            <label style={row}>
              <input type="checkbox" disabled={!pre.profiles.some((x) => !x.existing)} checked={pick.profiles} onChange={(e) => setPick((p) => ({ ...p, profiles: e.target.checked }))} />
              <span>
                {t('Size limits for qualities')} <span style={small}>{pre.profiles.map((x) => `${x.name} ${x.detail}`).join(' · ')}</span>
              </span>
            </label>
          )}
          {nothing ? <p style={small}>{t('Usherr already has everything in this file.')}</p> : null}
          <div style={row}>
            <button className="primary" disabled={busy || nothing || (pick.providers.length === 0 && pick.indexers.length === 0 && !pick.profiles)} onClick={() => void run()}>
              {t('Import what is ticked')}
            </button>
          </div>
        </>
      )}
    </div>
  )
}

// Usherr: which engine streams (StreamNZB or Usherr's own), what each device
// quality may be, and what streaming is using right now.
function StreamingEngineCard() {
  const toast = useToast()
  const [d, setD] = useState<EngineInfo | null>(null)
  const [profiles, setProfiles] = useState<EngineProfile[]>([])
  const [dirty, setDirty] = useState(false)
  const dirtyRef = useRef(false)
  const load = useCallback(() => {
    call<EngineInfo>('GET', '/api/home/streaming')
      .then((x) => {
        setD(x)
        if (!dirtyRef.current) setProfiles(x.profiles ?? [])
      })
      .catch(() => undefined)
  }, [])
  useEffect(() => {
    load()
    const timer = setInterval(load, 5000)
    return () => clearInterval(timer)
  }, [load])
  const put = async (body: Record<string, unknown>, done: string) => {
    try {
      const x = await call<EngineInfo>('PUT', '/api/home/streaming', body)
      setD(x)
      dirtyRef.current = false
      setDirty(false)
      setProfiles(x.profiles ?? [])
      toast.success(done)
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    }
  }
  if (!d || !d.engines) return null
  // Lists the server sent as null when empty.
  d.sessions ??= []
  d.servers ??= []
  d.indexers ??= []
  const own = d.engines.usherr
  const nzb = d.engines.streamnzb
  const chosen = d.engines[d.engine] ?? own
  const edit = (i: number, patch: Partial<EngineProfile>) => {
    dirtyRef.current = true
    setDirty(true)
    setProfiles((ps) => ps.map((p, j) => (j === i ? { ...p, ...patch } : p)))
  }
  const mbps = (d.speed / 1e6).toFixed(1)
  return (
    <Card
      title={t('Streaming engine')}
      state={chosen.ready ? 'ready' : 'setup'}
      blurb={t('"Stream now", the streamed library and streamed TV channels play through one of two engines. If the chosen one finds nothing, the other is tried.')}
      needs={chosen.problem}
    >
      <div className="grid-form">
        <label style={row}>
          <input type="radio" name="engine" checked={d.engine === 'usherr'} onChange={() => void put({ engine: 'usherr' }, t('Streaming with Usherr.'))} />
          <span>
            <strong>Usherr</strong> {t('searches your indexers and reads from your Usenet servers itself, daily shows by date too.')}
            {!own.ready && own.problem && <span className="hint"> {own.problem}</span>}
          </span>
        </label>
        <label style={row}>
          <input type="radio" name="engine" checked={d.engine === 'streamnzb'} onChange={() => void put({ engine: 'streamnzb' }, t('Streaming with StreamNZB.'))} />
          <span>
            <strong>StreamNZB</strong> {t('the separate StreamNZB program set up below.')}
            {!nzb.ready && nzb.problem && <span className="hint"> {nzb.problem}</span>}
          </span>
        </label>

        <h3 style={{ margin: '12px 0 0' }}>{t('What each quality may be')}</h3>
        <p style={small}>{t("A phone takes 720p, a TV 4K. Usherr's engine only offers releases at or below the quality, within the size, and without the words listed.")}</p>
        {profiles.map((p, i) => (
          <div key={p.quality} style={row}>
            <strong style={{ minWidth: 48 }}>{p.quality}</strong>
            <label style={row}>
              {t('Up to')}
              <input type="number" min={0} max={500} step={1} style={{ width: 80 }} value={p.maxSizeGB} onChange={(e) => edit(i, { maxSizeGB: Number(e.target.value) })} /> GB
            </label>
            <label style={{ ...row, flex: 1 }}>
              {t('Leave out')}
              <input style={{ flex: 1, minWidth: 120 }} value={p.exclude} onChange={(e) => edit(i, { exclude: e.target.value })} placeholder="av1, remux" spellCheck={false} />
            </label>
          </div>
        ))}
        <div style={row}>
          <button className="primary" disabled={!dirty} onClick={() => void put({ profiles }, t('Saved.'))}>
            {t('Save')}
          </button>
        </div>

        <StreamNZBImport onDone={load} />

        <h3 style={{ margin: '12px 0 0' }}>{t('Right now')}</h3>
        <p style={small}>
          {t('Speed: {n} MB/s', { n: mbps })}
          {d.servers.map((sv) => (
            <span key={sv.host}>
              {' · '}
              {t('{host}: {used} of {allowed} connections in use', { host: sv.host, used: sv.inUse, allowed: sv.allowed })}
            </span>
          ))}
        </p>
        {d.sessions.length === 0 ? (
          <p style={small}>{t('Nothing is streaming through Usherr.')}</p>
        ) : (
          <ul style={{ margin: 0, paddingLeft: 18 }}>
            {d.sessions.slice(0, 8).map((se) => (
              <li key={se.release + se.file} style={small}>
                {se.release} {t('({mb} MB, last read {sec}s ago)', { mb: se.sizeMB, sec: se.idleSec })}
              </li>
            ))}
          </ul>
        )}
        {d.indexers.length > 0 && (
          <p style={small}>
            {t('Searches today')}:{' '}
            {d.indexers.map((ix, i) => (
              <span key={ix.name}>
                {i > 0 && ' · '}
                {ix.limit > 0 ? t('{name} {used} of {limit}', { name: ix.name, used: ix.used, limit: ix.limit }) : t('{name} {used} (no limit)', { name: ix.name, used: ix.used })}
              </span>
            ))}
          </p>
        )}
      </div>
    </Card>
  )
}

function StreamingCard({ x, save }: { x: Extras; save: Save }) {
  const s = x.streaming
  const [url, setUrl] = useState(s.url)
  const [token, setToken] = useState('')
  return (
    <Card
      title={t('Streaming (StreamNZB)')}
      guide="streaming-streamnzb"
      state={s.ready ? 'ready' : s.url || s.tokenSet || s.tokenFile ? 'setup' : 'off'}
      blurb={tx('"Stream now", the streamed library and streamed TV channels play through {streamnzb}, which streams from Usenet without downloading first. Run it next to Mediarium.', {
        streamnzb: (
          <a href="https://github.com/Gaisberg/streamnzb" target="_blank" rel="noreferrer">
            StreamNZB
          </a>
        ),
      })}
      needs={
        s.url || s.tokenSet || s.tokenFile ? (
          <>
            {t("StreamNZB's address and its admin token.")}
            {s.problem && <> {t('Now: {problem}.', { problem: s.problem })}</>}
          </>
        ) : undefined
      }
    >
      <div className="grid-form">
        <label>
          {t('StreamNZB address')}
          <input value={url} onChange={(e) => setUrl(e.target.value)} placeholder={s.defaultUrl} autoComplete="off" spellCheck={false} />
        </label>
        <label>
          {s.tokenSet ? t('Replace the admin token') : t('Admin token')}
          <input
            value={token}
            onChange={(e) => setToken(e.target.value)}
            placeholder={s.tokenFile ? t("Read from StreamNZB's config file") : t("From StreamNZB's config.json (admin_token)")}
            autoComplete="off"
            spellCheck={false}
            type="password"
          />
        </label>
        <p style={small}>
          {t("Leave the address empty for {url}. Instead of pasting the token you can map StreamNZB's config.json into Mediarium at /streamnzb/config.json (read only).", {
            url: s.defaultUrl,
          })}
        </p>
        <div style={row}>
          <button
            className="primary"
            disabled={url.trim() === s.url && token.trim() === ''}
            onClick={() => void save({ streamnzbUrl: url, ...(token.trim() ? { streamnzbToken: token } : {}) }, t('StreamNZB saved.')).then((ok) => ok && setToken(''))}
          >
            {t('Save')}
          </button>
          <TestButton label={t('Test it')} run={test('streaming')} />
          {s.tokenSet && <button onClick={() => void save({ removeStreamnzbToken: true }, t('Token removed.'))}>{t('Remove the token')}</button>}
        </div>
      </div>
    </Card>
  )
}

function StreamedCard({ x, save, act, server }: { x: Extras; save: Save; act: Act; server: string }) {
  const s = x.streamed
  const [folder, setFolder] = useState(s.serverFolderBy === 'set' ? s.serverFolder : '')
  const ready = s.folderOk && s.serverFolder !== '' && !!x.address.value && x.streaming.ready && x.server.kind !== ''
  const missing = [
    !x.streaming.ready && t('Streaming (above)'),
    !x.address.value && t("Mediarium's address (above)"),
    x.server.kind === '' && t('Jellyfin or Emby (above)'),
    !s.folderOk && t('a folder mapped to {folder}', { folder: s.folder }),
    s.serverFolder === '' && t('where {server} sees that folder', { server }),
  ].filter(Boolean)
  return (
    <Card
      title={t('Streamed library')}
      guide="streamed-library"
      state={ready ? 'ready' : 'setup'}
      blurb={t(
        'Titles you stream rather than download show in the Library next to the ones on disk, and in {server} as small pointer files that start the stream when played. A title you later download is played from disk instead.',
        { server },
      )}
      needs={missing.join(', ') + '.'}
    >
      {s.folderNote && <div className="notice notice-warn">{s.folderNote}</div>}
      <div className="grid-form">
        <label>
          {t('The streamed folder as {server} sees it', { server })}
          <input
            value={folder}
            onChange={(e) => setFolder(e.target.value)}
            placeholder={s.serverFolderBy === 'mapping' ? t('{folder} (from the folder mapping)', { folder: s.serverFolder }) : t('E.g. /media/streamed or D:\\Streamed')}
            autoComplete="off"
            spellCheck={false}
          />
        </label>
        <p style={small}>
          {t('Inside Mediarium it is {folder}. Leave this empty when the folder mapping under Media servers already covers it.', { folder: s.folder })}{' '}
          {s.titles > 0 && plural(s.titles, '{n} title so far.', '{n} titles so far.')}
        </p>
        <div style={row}>
          <button className="primary" onClick={() => void save({ streamedServerFolder: folder }, t('Saved.'))}>
            {t('Save')}
          </button>
          <button disabled={!ready} onClick={() => void act('POST', '/api/streamed/merge')}>
            {t("Put streamed titles into {server}'s libraries", { server })}
          </button>
        </div>
      </div>
    </Card>
  )
}

function LiveTVCard({ x, act, server }: { x: Extras; act: Act; server: string }) {
  const tv = x.liveTv
  const canAdd = x.server.kind !== '' && !!tv.feed
  return (
    <Card
      title={t('Live TV')}
      guide="live-tv"
      state={tv.channels > 0 ? 'ready' : 'off'}
      blurb={t("Scheduled channels from your library (and from streams), with a guide. Watch them on the Live TV page, or add Mediarium as a tuner so they show in {server}'s Live TV on your TV.", {
        server,
      })}
      needs={tx('a channel: make one on the {link}.', { link: <Link to="/tv">{t('Live TV page')}</Link> })}
    >
      <div style={row}>
        <button disabled={!canAdd} onClick={() => void act('POST', '/api/tv/jellyfin')}>
          {t("Add to {server}'s Live TV", { server })}
        </button>
        {!canAdd && (
          <span style={{ color: 'var(--text-dim)', fontSize: 13 }}>{x.server.kind === '' ? t('Needs Jellyfin or Emby first.') : t("Needs Mediarium's address first.")}</span>
        )}
      </div>
      {tv.feed && (
        <p style={{ color: 'var(--text-dim)', fontSize: 13, marginBottom: 0 }}>
          {tx('For other IPTV apps: {feed} (keep it private: it works without signing in).', { feed: <code style={{ wordBreak: 'break-all' }}>{tv.feed}</code> })}
        </p>
      )}
    </Card>
  )
}

function PlayOnTVCard({ x, save, act }: { x: Extras; save: Save; act: Act }) {
  const p = x.playOnTv
  const [addr, setAddr] = useState(p.address)
  const [name, setName] = useState(p.name)
  return (
    <Card
      title={t('Play on TV')}
      guide="play-on-tv"
      state={p.ready ? 'ready' : p.address ? 'setup' : 'off'}
      blurb={t("Sends a stream to an Android TV box (such as an NVIDIA Shield), which plays it in VLC or opens it in Stremio. Mediarium uses the TV's network debugging to do it.")}
      needs={
        p.adb
          ? t("the TV's address, then Pair. On the TV, turn on Developer options (press Build seven times in Settings > About), then Network debugging (USB debugging on some boxes).")
          : t("adb, which this Mediarium image doesn't include.")
      }
    >
      <div className="grid-form">
        <label>
          {t("The TV's address")}
          <input value={addr} onChange={(e) => setAddr(e.target.value)} placeholder="192.168.1.50" autoComplete="off" spellCheck={false} />
        </label>
        <label>
          {t('Its name')}
          <input value={name} onChange={(e) => setName(e.target.value)} placeholder={t('Living room TV')} />
        </label>
        <div style={row}>
          <button className="primary" disabled={addr.trim() === p.address && name.trim() === p.name} onClick={() => void save({ tvAddress: addr, tvName: name }, t('TV saved.'))}>
            {t('Save')}
          </button>
          <button disabled={!p.address || !p.adb} onClick={() => void act('POST', '/api/home/extras/tv-pair')}>
            {t('Pair')}
          </button>
          {p.keyMade && p.address && <TestButton label={t('Test it')} run={test('tv')} />}
        </div>
      </div>
    </Card>
  )
}

function ComicsCard({ x, act }: { x: Extras; act: Act }) {
  const c = x.comics
  return (
    <Card
      title={t('Comics')}
      guide="comics"
      state={c.enabled && c.keySet ? 'ready' : c.enabled ? 'setup' : 'off'}
      blurb={t('Follow comic series, get new issues and read them with a page-flip reader. Issue details come from ComicVine.')}
      needs={c.enabled ? tx('a free ComicVine API key, under Settings on the {link}.', { link: <Link to="/comics">{t('Comics page')}</Link> }) : t('turning on.')}
    >
      <div style={row}>
        <button className={c.enabled ? '' : 'primary'} onClick={() => void act('PUT', '/api/comics/settings', { enabled: !c.enabled })}>
          {c.enabled ? t('Turn Comics off') : t('Turn Comics on')}
        </button>
        {c.enabled && <Link to="/comics">{t('Open Comics')}</Link>}
      </div>
    </Card>
  )
}

function NSFWCard({ x, act }: { x: Extras; act: Act }) {
  const n = x.nsfw
  const [asked, setAsked] = useState(false)
  const [phrase, setPhrase] = useState('')
  const PHRASE = 'I am 18 or older' // Usherr: typed to switch it on (the server checks it too)
  const typed = phrase.trim().toLowerCase() === PHRASE.toLowerCase()
  return (
    <Card
      title={t('NSFW 18+')}
      state={n.enabled ? 'ready' : 'off'}
      blurb={t('Adult videos, with performers and studios. Only administrators ever see it: it is hidden from everyone else, keeps its own library and never sends notifications. Off unless you turn it on.')}
    >
      {n.enabled ? (
        <div style={row}>
          <button onClick={() => void act('PUT', '/api/nsfw/settings', { enabled: false })}>{t('Turn NSFW 18+ off')}</button>
          <Link to="/nsfw">{t('Open NSFW 18+')}</Link>
        </div>
      ) : n.allowed === false ? (
        <div className="notice">
          <p style={{ margin: 0 }}>
            {tx("Locked on this server. Whoever runs it can unlock it by adding {setting} to the container's environment and restarting it. Then it can be turned on here.", {
              setting: <code>USHERR_ALLOW_ADULT=1</code>,
            })}
          </p>
        </div>
      ) : asked ? (
        <div className="notice notice-warn">
          <p style={{ marginTop: 0 }}>
            <strong>{t('Adults only.')}</strong> {t('This shows adult content to administrators. Everyone else still never sees it. Only turn it on if you are 18 or older and adult content is legal where you live.')}
          </p>
          <label style={{ display: 'grid', gap: 6, margin: '10px 0' }}>
            <span>{tx('To confirm, type {phrase}', { phrase: <strong>{PHRASE}</strong> })}</span>
            <input value={phrase} onChange={(e) => setPhrase(e.target.value)} autoComplete="off" spellCheck={false} aria-label={t('Confirmation')} />
          </label>
          <div style={row}>
            <button
              className="primary"
              disabled={!typed}
              onClick={() =>
                void act('PUT', '/api/nsfw/settings', { enabled: true, confirm: phrase }).then(() => {
                  setAsked(false)
                  setPhrase('')
                })
              }
            >
              {t('Yes, turn it on')}
            </button>
            <button onClick={() => (setAsked(false), setPhrase(''))}>{t('No')}</button>
          </div>
        </div>
      ) : (
        <button onClick={() => setAsked(true)}>{t('Turn NSFW 18+ on…')}</button>
      )}
    </Card>
  )
}

function DirectCard({ x, save }: { x: Extras; save: Save }) {
  const d = x.direct
  const [flare, setFlare] = useState(d.flareUrl)
  return (
    <Card
      title={t('Direct downloads for comics (GetComics)')}
      guide="comics"
      state={d.enabled ? 'ready' : 'off'}
      blurb={t('Searches GetComics next to your indexers when looking for comics, and downloads from its own server, PixelDrain, MediaFire or MEGA, whichever works. Packs of a whole run or story arc fill every issue they hold.')}
    >
      <div className="grid-form">
        <label>
          {t("FlareSolverr's address (only needed when GetComics shows a Cloudflare check)")}
          <input value={flare} onChange={(e) => setFlare(e.target.value)} placeholder="http://host.docker.internal:8191" autoComplete="off" spellCheck={false} />
        </label>
        <div style={row}>
          <button className={d.enabled ? '' : 'primary'} onClick={() => void save({ getcomics: !d.enabled }, d.enabled ? t('GetComics is off.') : t('GetComics is on.'))}>
            {d.enabled ? t('Turn GetComics off') : t('Turn GetComics on')}
          </button>
          <button disabled={flare.trim() === d.flareUrl} onClick={() => void save({ flareUrl: flare }, t('FlareSolverr saved.'))}>
            {t('Save address')}
          </button>
          {d.flareUrl && <TestButton label={t('Test FlareSolverr')} run={test('flare')} />}
          {d.enabled && <TestButton label={t('Test GetComics')} run={test('getcomics')} />}
        </div>
      </div>
    </Card>
  )
}

// Read-to-Me (Usherr): the two voice engines on the media PC, and when
// audiobooks are made.
function ReadToMeCard() {
  const toast = useToast()
  const [s, setS] = useState<RtvSettings | null>(null)
  const [kokoro, setKokoro] = useState('')
  const [chatterbox, setChatterbox] = useState('')
  const [runner, setRunner] = useState('')
  const [runnerToken, setRunnerToken] = useState('')
  useEffect(() => {
    readToMeApi
      .settings()
      .then((d) => (setS(d), setKokoro(d.kokoroUrl), setChatterbox(d.chatterboxUrl), setRunner(d.runnerUrl)))
      .catch(() => setS(null))
  }, [])
  if (!s) return null
  const save = (body: Parameters<typeof readToMeApi.saveSettings>[0], done: string) =>
    readToMeApi
      .saveSettings(body)
      .then((d) => {
        setS(d)
        setKokoro(d.kokoroUrl)
        setChatterbox(d.chatterboxUrl)
        setRunner(d.runnerUrl)
        setRunnerToken('')
        toast.success(done)
      })
      .catch((e: Error) => toast.error(e.message))
  const engine = (name: string) => (s.engines ?? []).find((e) => e.name === name)
  const k = engine('kokoro')
  const on = !!k?.on
  const changed = kokoro.trim() !== s.kokoroUrl || chatterbox.trim() !== s.chatterboxUrl || runner.trim() !== s.runnerUrl || runnerToken.trim() !== ''
  return (
    <Card
      id="readtome"
      title={t('Read-to-Me')}
      guide="read-to-me"
      state={k?.ready ? 'ready' : on ? 'setup' : 'off'}
      blurb={t('Turns an ebook into an audiobook with a computer voice: Kokoro for everyday narration, Chatterbox (started only when wanted) for more expressive voices and copied voices. Both run on the graphics card of this computer.')}
      needs={on ? k?.problem ?? t('Kokoro to answer') : t("Kokoro's address")}
    >
      <div className="grid-form">
        <label>
          {t("Kokoro's address")}
          <input value={kokoro} onChange={(e) => setKokoro(e.target.value)} placeholder="http://host.docker.internal:8881" autoComplete="off" spellCheck={false} />
        </label>
        <label>
          {t("Chatterbox's address (optional)")}
          <input value={chatterbox} onChange={(e) => setChatterbox(e.target.value)} placeholder="http://host.docker.internal:4123" autoComplete="off" spellCheck={false} />
        </label>
        <label>
          {t("The runner's address (optional)")}
          <input value={runner} onChange={(e) => setRunner(e.target.value)} placeholder="http://host.docker.internal:4130" autoComplete="off" spellCheck={false} />
        </label>
        <label>
          {t("The runner's token")}
          <input type="password" value={runnerToken} onChange={(e) => setRunnerToken(e.target.value)} placeholder={s.runnerTokenSet ? t('Saved (type to replace it)') : 'RUNNER_TOKEN'} autoComplete="new-password" spellCheck={false} />
        </label>
        <p style={small}>
          {t('Chatterbox takes a lot of graphics memory. With the runner (a small container next to the voice engines, in the setup), Usherr starts it when a job needs it and stops it when the queue is empty. Without it, Chatterbox is started by hand: docker compose --profile chatterbox up -d.')}
        </p>
        <label>
          {t('Chatterbox model for jobs')}
          <select value={s.chatterboxModel} onChange={(e) => void save({ chatterboxModel: e.target.value as 'standard' | 'turbo' }, t('Saved.'))}>
            <option value="standard">{t('Standard (the best sound)')}</option>
            <option value="turbo">{t('Turbo (about twice as fast)')}</option>
          </select>
        </label>
        <label className="tv-check">
          <input type="checkbox" checked={s.nightOnly} onChange={(e) => void save({ nightOnly: e.target.checked }, e.target.checked ? t('Audiobooks are made at night only.') : t('Audiobooks are made at any time.'))} />
          {t('Make audiobooks at night only, so the graphics card is free for watching')}
        </label>
        {s.nightOnly && (
          <div style={row}>
            <label style={row}>
              {t('from')}
              <input type="time" value={s.nightFrom} onChange={(e) => e.target.value && void save({ nightFrom: e.target.value }, t('Saved.'))} />
            </label>
            <label style={row}>
              {t('to')}
              <input type="time" value={s.nightTo} onChange={(e) => e.target.value && void save({ nightTo: e.target.value }, t('Saved.'))} />
            </label>
          </div>
        )}
        <div style={row}>
          <button className="primary" disabled={!changed} onClick={() => void save({ kokoroUrl: kokoro, chatterboxUrl: chatterbox, runnerUrl: runner, ...(runnerToken.trim() ? { runnerToken: runnerToken.trim() } : {}) }, t('Read-to-Me saved.'))}>
            {t('Save')}
          </button>
          {s.kokoroUrl && (
            <TestButton label={t('Test Kokoro')} run={() => readToMeApi.test('kokoro').then((r) => ({ ok: true, message: r.message }), (e: Error) => ({ ok: false, message: e.message }))} />
          )}
          {s.chatterboxUrl && (
            <TestButton label={t('Test Chatterbox')} run={() => readToMeApi.test('chatterbox').then((r) => ({ ok: true, message: r.message }), (e: Error) => ({ ok: false, message: e.message }))} />
          )}
          {s.runnerUrl && s.runnerTokenSet && (
            <TestButton label={t('Test the runner')} run={() => readToMeApi.test('runner').then((r) => ({ ok: true, message: r.message }), (e: Error) => ({ ok: false, message: e.message }))} />
          )}
        </div>
        <p style={small}>{t('Then open an ebook (an EPUB) in the library: it has a Read to me button.')}</p>
      </div>
    </Card>
  )
}

// Sports (Usherr): on the household's Sportarr ("stacked"), or on Usherr's
// own engine with TheSportsDB.
function SportsCard() {
  const toast = useToast()
  const [s, setS] = useState<SportsSettings | null>(null)
  const [mode, setMode] = useState<SportsSettings['mode']>('')
  const [url, setUrl] = useState('')
  const [key, setKey] = useState('')
  const [tsdb, setTsdb] = useState('')
  useEffect(() => {
    sportsApi
      .settings()
      .then((d) => (setS(d), setUrl(d.sportarrUrl), setMode(d.mode)))
      .catch(() => setS(null))
  }, [])
  if (!s) return null
  const save = (body: Parameters<typeof sportsApi.saveSettings>[0], done: string) =>
    sportsApi
      .saveSettings(body)
      .then((d) => {
        setS(d)
        setUrl(d.sportarrUrl)
        setMode(d.mode)
        setKey('')
        setTsdb('')
        toast.success(done)
      })
      .catch((e: Error) => toast.error(e.message))
  const changed = mode !== s.mode || url.trim() !== s.sportarrUrl || key.trim() !== '' || tsdb.trim() !== ''
  const body = () => ({ mode, sportarrUrl: url, ...(key.trim() ? { sportarrKey: key } : {}), ...(tsdb.trim() ? { tsdbKey: tsdb } : {}) })
  const on = s.mode !== ''
  return (
    <Card
      id="sports"
      title={t('Sports')}
      guide="sports"
      state={on && s.ready ? 'ready' : on ? 'setup' : 'off'}
      blurb={t('A Sports page for everyone: leagues, teams you follow and the games of the coming weeks. Games stream like films and episodes; with Sportarr they can be downloaded too.')}
      needs={on ? s.problem ?? t('Sportarr’s address and API key') : t('choosing what Sports runs on')}
    >
      <div className="grid-form">
        <label>
          {t('Sports runs on')}
          <select value={mode} onChange={(e) => setMode(e.target.value as SportsSettings['mode'])}>
            <option value="">{t('Off')}</option>
            <option value="sportarr">{t('Sportarr (stacked: Sportarr downloads, Usherr shows and streams)')}</option>
            <option value="usherr">{t('Usherr’s own engine (TheSportsDB; streams, no downloads yet)')}</option>
          </select>
        </label>
        {mode === 'sportarr' && (
          <>
            <label>
              {t('Sportarr’s address')}
              <input value={url} onChange={(e) => setUrl(e.target.value)} placeholder="http://host.docker.internal:1867" autoComplete="off" spellCheck={false} />
            </label>
            <label>
              {t('Sportarr’s API key (Sportarr: Settings > General)')}
              <input type="password" value={key} onChange={(e) => setKey(e.target.value)} placeholder={s.sportarrKeySet ? t('Saved. Type a new one to change it.') : ''} autoComplete="off" spellCheck={false} />
            </label>
          </>
        )}
        {mode !== '' && (
          <>
            <label>
              {t('TheSportsDB key (optional)')}
              <input type="password" value={tsdb} onChange={(e) => setTsdb(e.target.value)} placeholder={s.tsdbKeySet ? t('Saved. Type a new one to change it.') : mode === 'usherr' ? t('Without one, the free key: search and part of each season') : t('Adds the clock and crests to live scores on the Marquee')} autoComplete="off" spellCheck={false} />
            </label>
            <p style={small}>
              {mode === 'usherr' ? t('A paid key gives full schedules and live scores.') : t('Live scores come from Sportarr; a paid key adds the clock and the teams’ crests on the Marquee.')}{' '}
              <a href="https://www.thesportsdb.com/pricing" target="_blank" rel="noreferrer">
                {t('Get a key at thesportsdb.com')}
              </a>
              {s.tsdbKeySet && (
                <>
                  {' · '}
                  <button type="button" className="link-btn" onClick={() => void save({ removeTsdbKey: true }, t('The key is removed.'))}>
                    {t('Remove the key')}
                  </button>
                </>
              )}
            </p>
          </>
        )}
        <div style={row}>
          <button className="primary" disabled={!changed} onClick={() => void save(body(), mode === '' ? t('Sports is off.') : t('Sports saved.'))}>
            {t('Save')}
          </button>
          {on && (
            <TestButton
              label={s.mode === 'usherr' ? t('Test TheSportsDB') : t('Test Sportarr')}
              run={() =>
                sportsApi.test().then(
                  (r) => ({ ok: true, message: r.message }),
                  (e: Error) => ({ ok: false, message: e.message }),
                )
              }
            />
          )}
          {on && s.ready && (
            <Link className="btn-with-icon" to="/sports">
              <Icon name="trophy" /> {t('Open Sports')}
            </Link>
          )}
        </div>
        <p style={small}>
          {mode === 'usherr'
            ? t('Usherr keeps the leagues itself and finds each game on your indexers when someone streams it.')
            : t('Sportarr runs as its own app next to Usherr, like Radarr or Sonarr. Usherr shows its leagues and games, streams them, and asks it for the ones people want downloaded.')}
        </p>
      </div>
    </Card>
  )
}

interface AISettings {
  provider: '' | 'ollama' | 'openai' | 'claude'
  url: string
  model: string
  keySet: boolean
  dailyLimit: number
  defaults: { ollamaUrl: string; ollamaModel: string; claudeModel: string }
}

// AI (home fork): the model behind AI Search and the other AI features.
function AICard() {
  const toast = useToast()
  const [a, setA] = useState<AISettings | null>(null)
  const [provider, setProvider] = useState<AISettings['provider']>('')
  const [url, setUrl] = useState('')
  const [model, setModel] = useState('')
  const [key, setKey] = useState('')
  const [limit, setLimit] = useState(20)
  const fill = (d: AISettings) => {
    setA(d)
    setProvider(d.provider)
    setUrl(d.url)
    setModel(d.model)
    setLimit(d.dailyLimit)
    setKey('')
  }
  useEffect(() => {
    call<AISettings>('GET', '/api/home/ai').then(fill).catch(() => undefined)
  }, [])
  if (!a) return null
  const save = async (body: Record<string, unknown>) => {
    try {
      fill(await call<AISettings>('PUT', '/api/home/ai', body))
      toast.success(t('AI settings saved.'))
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    }
  }
  const choose = (p: AISettings['provider']) => {
    setProvider(p)
    if (p === 'ollama') {
      setUrl(url || a.defaults.ollamaUrl)
      setModel(a.provider === 'ollama' ? model : a.defaults.ollamaModel)
    } else if (p === 'claude') {
      setModel(a.provider === 'claude' ? model : a.defaults.claudeModel)
    }
  }
  const ready = a.provider !== '' && !!a.model
  return (
    <Card
      title={t('AI')}
      guide="ai"
      state={ready ? 'ready' : 'off'}
      blurb={t(
        'AI Search on Discover finds titles from a description or a photo. The AI only suggests: every title is checked on TMDB, and nothing is added without you. A model on your own computer (Ollama) keeps it all at home; Claude knows films best.',
      )}
    >
      <div className="grid-form">
        <label>
          {t('Which AI')}
          <select value={provider} onChange={(e) => choose(e.target.value as AISettings['provider'])}>
            <option value="">{t('Off')}</option>
            <option value="ollama">{t('Ollama (a model on your own computer)')}</option>
            <option value="openai">{t('An OpenAI-compatible server (LM Studio and others)')}</option>
            <option value="claude">{t('Claude (Anthropic, needs an API key)')}</option>
          </select>
        </label>
        {(provider === 'ollama' || provider === 'openai') && (
          <label>
            {t('Address')}
            <input value={url} onChange={(e) => setUrl(e.target.value)} placeholder={a.defaults.ollamaUrl} autoComplete="off" spellCheck={false} />
          </label>
        )}
        {provider !== '' && (
          <label>
            {t('Model')}
            <input value={model} onChange={(e) => setModel(e.target.value)} placeholder={provider === 'claude' ? a.defaults.claudeModel : a.defaults.ollamaModel} autoComplete="off" spellCheck={false} />
          </label>
        )}
        {(provider === 'claude' || provider === 'openai') && (
          <label>
            {a.keySet ? t('Replace the API key') : t('API key')}
            <input type="password" value={key} onChange={(e) => setKey(e.target.value)} autoComplete="off" spellCheck={false} placeholder={a.keySet ? '••••••••' : ''} />
          </label>
        )}
        {provider !== '' && (
          <label>
            {t('Answers a day for each person (administrators aren’t limited; 0 is no limit)')}
            <input type="number" min={0} value={limit} onChange={(e) => setLimit(Number(e.target.value))} />
          </label>
        )}
        <div style={row}>
          <button className="primary" onClick={() => void save({ provider, url, model, dailyLimit: limit, ...(key.trim() ? { key } : {}) })}>
            {t('Save')}
          </button>
          {ready && (
            <TestButton
              label={t('Test it')}
              run={() =>
                call<{ message: string }>('POST', '/api/home/ai/test').then(
                  (r) => ({ ok: true, message: r.message }),
                  (e: Error) => ({ ok: false, message: e.message }),
                )
              }
            />
          )}
          {a.keySet && <button onClick={() => void save({ removeKey: true })}>{t('Remove the key')}</button>}
        </div>
      </div>
    </Card>
  )
}

interface MDBListSettings {
  keySet: boolean
  titles: number
  callsLeft: number
}

// Scores from MDBList (home fork): IMDb, Rotten Tomatoes, Metacritic,
// Letterboxd and Common Sense ages, with the household's own free key.
function ScoresCard() {
  const toast = useToast()
  const [m, setM] = useState<MDBListSettings | null>(null)
  const [key, setKey] = useState('')
  useEffect(() => {
    call<MDBListSettings>('GET', '/api/home/mdblist').then(setM).catch(() => undefined)
  }, [])
  if (!m) return null
  const save = async (body: Record<string, unknown>) => {
    try {
      setM(await call<MDBListSettings>('PUT', '/api/home/mdblist', body))
      setKey('')
      toast.success(t('Saved.'))
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    }
  }
  return (
    <Card
      title={t('Scores (MDBList)')}
      guide="scores-mdblist"
      state={m.keySet ? 'ready' : 'setup'}
      blurb={t('IMDb, Rotten Tomatoes, Metacritic and Letterboxd scores and Common Sense Media ages on Discover and title pages, and as filters in Discover recipes. Uses your own free MDBList key; your library is filled in first, a little each hour.')}
      needs={
        <>
          {t('a free API key from')}{' '}
          <a href="https://mdblist.com/preferences/" target="_blank" rel="noreferrer noopener">
            mdblist.com/preferences
          </a>
        </>
      }
    >
      <div className="grid-form">
        <label>
          {m.keySet ? t('Replace the API key') : t('API key')}
          <input type="password" value={key} onChange={(e) => setKey(e.target.value)} autoComplete="off" spellCheck={false} placeholder={m.keySet ? '••••••••' : ''} />
        </label>
        {m.keySet && (
          <p style={small}>
            {t('{n} titles have scores so far.', { n: m.titles })}
            {m.callsLeft >= 0 ? ` ${t('{n} MDBList calls left today.', { n: m.callsLeft })}` : ''}
          </p>
        )}
        <div style={row}>
          <button className="primary" disabled={!key.trim()} onClick={() => void save({ key })}>
            {t('Save')}
          </button>
          {m.keySet && (
            <TestButton
              label={t('Test it')}
              run={() =>
                call<{ message: string }>('POST', '/api/home/mdblist/test').then(
                  (r) => ({ ok: true, message: r.message }),
                  (e: Error) => ({ ok: false, message: e.message }),
                )
              }
            />
          )}
          {m.keySet && <button onClick={() => void save({ removeKey: true })}>{t('Remove the key')}</button>}
        </div>
      </div>
    </Card>
  )
}

interface NewsSource {
  name: string
  url: string
  category: string
  on: boolean
}

interface NewsSettings {
  on: boolean
  sources: NewsSource[]
  dailyBudget: number
  categories: string[]
  summarisedToday: number
  waiting: number
  stories: number
}

function newsCategoryName(c: string): string {
  switch (c) {
    case 'film-tv':
      return t('Film & TV')
    case 'home-theater':
      return t('Home theater & tech')
    case 'books':
      return t('Books')
    case 'comics':
      return t('Comics')
    default:
      return t('Music')
  }
}

// Entertainment news (home fork): the feeds behind the news on the Dashboard.
// Usherr: the Marquee has its own page (/marquee/manage); here, and in the
// setup wizard, a card says what it is and leads there.
function MarqueeCard({ inWizard }: { inWizard: boolean }) {
  // Ready once a screen shows it.
  const [screens, setScreens] = useState(0)
  useEffect(() => {
    call<{ screens: unknown[] }>('GET', '/api/home/marquee/screens')
      .then((d) => setScreens((d.screens ?? []).length))
      .catch(() => undefined)
  }, [])
  return (
    <Card
      title={t('Marquee')}
      state={screens > 0 ? 'ready' : 'off'}
      blurb={t('A lobby display for a tablet or a screen next to the TV: what’s playing, new and coming films, trailers, Live TV, your own slides and a pre-show. Open {address} on the screen, full-screen.', { address: `${window.location.origin}/marquee` })}
    >
      <div style={row}>
        {/* In the setup wizard it opens beside it, so the wizard isn't left halfway. */}
        {inWizard ? (
          <a className="primary btn-sm btn-with-icon" href="/marquee/manage" target="_blank" rel="noreferrer">
            <Icon name="monitor" size={14} /> {t('The Marquee’s page')}
          </a>
        ) : (
          <Link className="primary btn-sm btn-with-icon" to="/marquee/manage">
            <Icon name="monitor" size={14} /> {t('The Marquee’s page')}
          </Link>
        )}
        <a className="btn-sm btn-with-icon" href="/marquee" target="_blank" rel="noreferrer">
          <Icon name="external" size={14} /> {t('Open the Marquee')}
        </a>
      </div>
      <p className="hint">{t('Its looks, scenes, slides, pre-show, the phone remote and Home Assistant are all on its own page.')}</p>
    </Card>
  )
}

// Usherr: the pre-show before a film (PreshowSettings.tsx). Ready as it is;
// the card says whether it also plays in the Jellyfin app.
function PreshowCard({ inWizard }: { inWizard: boolean }) {
  const [jf, setJf] = useState<{ plugin: string; configured: boolean } | null>(null)
  useEffect(() => {
    call<{ plugin: string; configured: boolean }>('GET', '/api/home/preshow/jellyfin')
      .then(setJf)
      .catch(() => undefined)
  }, [])
  const inJellyfin = jf?.plugin === 'active' && jf.configured
  const page = '/preshow/manage'
  return (
    <Card
      title={t('Pre-show')}
      state="ready"
      blurb={t('Before a film, a cinema’s pre-show: a welcome, trailers for what’s coming (downloaded ahead), your own clips, the rating and the countdown. “Movie night” is ready as it is; add clips, themes and schedules when you like.')}
    >
      <div style={row}>
        {inWizard ? (
          <a className="primary btn-sm btn-with-icon" href={page} target="_blank" rel="noreferrer" title={t('Opens beside the setup, so it isn’t left halfway')}>
            <Icon name="film" size={14} /> {t('The pre-show’s page')}
          </a>
        ) : (
          <Link className="primary btn-sm btn-with-icon" to={page}>
            <Icon name="film" size={14} /> {t('The pre-show’s page')}
          </Link>
        )}
      </div>
      <p className="hint">
        {inJellyfin
          ? t('It also plays before films in the Jellyfin app.')
          : t('To play it in the Jellyfin app too, press Set it up under In the Jellyfin app on its page (it installs the NeXroll Intros plugin).')}
      </p>
    </Card>
  )
}

// Usherr: the household's cinema, for the "In theaters now" rows, the
// openings on the calendar and the links to showtimes.
interface CinemaSettings {
  on: boolean
  region: string
  zip: string
  theatre: string
  hasAmcKey: boolean
  logos?: Record<string, boolean> // services with a picture of the household's own on their button
  tickets: TicketService
  ticketsUrl: string
}

function CinemaCard() {
  const toast = useToast()
  const [c, setC] = useState<CinemaSettings | null>(null)
  const [zip, setZip] = useState('')
  const [theatre, setTheatre] = useState('')
  const [region, setRegion] = useState('US')
  const [amcKey, setAmcKey] = useState('')
  const [ticketsUrl, setTicketsUrl] = useState('')
  const fill = (d: CinemaSettings) => {
    setC(d)
    setTicketsUrl(d.ticketsUrl ?? '')
    setZip(d.zip)
    setTheatre(d.theatre)
    setRegion(d.region)
    setAmcKey('')
  }
  useEffect(() => {
    call<CinemaSettings>('GET', '/api/home/cinema/settings').then(fill).catch(() => undefined)
  }, [])
  if (!c) return null
  const save = async (body: Record<string, unknown>) => {
    try {
      fill(await call<CinemaSettings>('PUT', '/api/home/cinema/settings', body))
      toast.success(t('Cinema settings saved.'))
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    }
  }
  return (
    <Card
      title={t('In theaters')}
      state={c.on ? 'ready' : 'off'}
      blurb={t(
        'Films in cinemas now, as a row on the Dashboard and in the Library, with their opening days on the calendar. Each film links to its showtimes at your theater, and to everywhere else it is playing near you.',
      )}
    >
      <label className="cx-check" style={{ marginBottom: 10 }}>
        <input type="checkbox" checked={c.on} onChange={(e) => void save({ on: e.target.checked })} /> {t('Show films in theaters')}
      </label>
      <div className="grid-form">
        <label>
          {t('Your ZIP or postcode')}
          <input value={zip} onChange={(e) => setZip(e.target.value)} placeholder="73102" inputMode="text" autoComplete="postal-code" />
        </label>
        <label>
          {t('Your theater')}
          <input value={theatre} onChange={(e) => setTheatre(e.target.value)} placeholder="AMC Quail Springs Mall 24" />
        </label>
        <label>
          {t('Country')}
          <input value={region} onChange={(e) => setRegion(e.target.value.toUpperCase())} maxLength={2} style={{ width: 70 }} aria-describedby="cinema-region-hint" />
          <span id="cinema-region-hint" className="hint">
            {t('Two letters, such as US. Which country’s cinemas count.')}
          </span>
        </label>
        <label>
          {t('AMC key (optional)')}
          <input type="password" value={amcKey} onChange={(e) => setAmcKey(e.target.value)} placeholder={c.hasAmcKey ? t('A key is saved') : t('From developers.amctheatres.com')} autoComplete="off" spellCheck={false} />
          <span className="hint">{t('With your own AMC key, the row will follow what your AMC theater is showing, with its showtimes.')}</span>
        </label>
      </div>
      <div style={row}>
        <button className="primary" onClick={() => void save({ zip, theatre, region, ...(amcKey ? { amcKey } : {}) })}>
          {t('Save')}
        </button>
        {c.hasAmcKey && (
          <button className="btn-sm" onClick={() => void save({ amcKey: '' })}>
            {t('Remove the AMC key')}
          </button>
        )}
      </div>
      <div className="link-logo-set">
        <label>
          {t('Tickets from')}
          <select value={c.tickets ?? ''} onChange={(e) => void save({ tickets: e.target.value })}>
            <option value="">{t('Fandango (in the US)')}</option>
            {(Object.keys(TICKET_SITES) as (keyof typeof TICKET_SITES)[])
              .filter((k) => k !== 'fandango')
              .map((k) => (
                <option key={k} value={k}>
                  {TICKET_SITES[k].name}
                </option>
              ))}
            <option value="custom">{t('A link of my own')}</option>
            <option value="off">{t('No Tickets button')}</option>
          </select>
        </label>
        {c.tickets === 'custom' && (
          <div className="grid-form">
            <label>
              {t('Link')}
              <input value={ticketsUrl} onChange={(e) => setTicketsUrl(e.target.value)} placeholder="https://www.amctheatres.com/search?q={title}" spellCheck={false} />
              <span className="hint">{t('{title} becomes the film’s name and {zip} your ZIP code, such as an AMC, Regal or local theater’s search page.')}</span>
            </label>
            <div>
              <button className="btn-sm" onClick={() => void save({ tickets: 'custom', ticketsUrl })}>
                {t('Save the link')}
              </button>
            </div>
          </div>
        )}
      </div>
      {c.tickets !== 'off' && (
        <LinkLogo
          key={c.tickets || 'fandango'}
          service={c.tickets || 'fandango'}
          has={!!c.logos?.[c.tickets || 'fandango']}
          name={c.tickets === 'custom' ? t('Tickets') : (TICKET_SITES[(c.tickets || 'fandango') as keyof typeof TICKET_SITES]?.name ?? t('Tickets'))}
          onChange={() => void call<CinemaSettings>('GET', '/api/home/cinema/settings').then(fill)}
        />
      )}
    </Card>
  )
}

// Usherr: the picture on the Tickets button: a plain button with the
// service's name, or any picture you upload, such as the service's logo.
// Like a channel's logo.
function LinkLogo({ service, has, name, onChange }: { service: string; has: boolean; name: string; onChange: () => void }) {
  const toast = useToast()
  const file = useRef<HTMLInputElement>(null)
  const [busy, setBusy] = useState(false)
  const [v, setV] = useState(0)
  const upload = async (f: File) => {
    setBusy(true)
    try {
      const fd = new FormData()
      fd.append('file', f)
      const r = await fetch(`/api/home/link-logo/${service}`, { method: 'PUT', credentials: 'include', body: fd })
      const d = (await r.json().catch(() => ({}))) as { error?: string }
      if (!r.ok) throw new Error(d.error ?? t('Something went wrong (error {status}).', { status: r.status }))
      setV(Date.now())
      onChange()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }
  const remove = async () => {
    setBusy(true)
    try {
      await call('DELETE', `/api/home/link-logo/${service}`)
      onChange()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }
  return (
    <div className="link-logo-set">
      <span className="hint">{t('{service}’s button: a plain button, or a picture of your own, such as its logo. Each service keeps its own.', { service: name })}</span>
      <div style={row}>
        {has ? <img className="tickets-logo" src={`/api/home/link-logo/${service}?v=${v}`} alt="" /> : <TicketsButton name={name} />}
        <input
          ref={file}
          type="file"
          hidden
          accept="image/png,image/jpeg,image/webp"
          onChange={(e) => {
            const f = e.target.files?.[0]
            e.target.value = ''
            if (f) void upload(f)
          }}
        />
        <button className="btn-sm" disabled={busy} onClick={() => file.current?.click()}>
          {busy ? t('Uploading…') : t('Upload a picture')}
        </button>
        {has && (
          <button className="btn-sm" disabled={busy} onClick={() => void remove()}>
            {t('Use the plain button')}
          </button>
        )}
      </div>
    </div>
  )
}

function NewsCard() {
  const toast = useToast()
  const [n, setN] = useState<NewsSettings | null>(null)
  const [sources, setSources] = useState<NewsSource[]>([])
  const [budget, setBudget] = useState(40)
  const fill = (d: NewsSettings) => {
    setN(d)
    setSources(d.sources)
    setBudget(d.dailyBudget)
  }
  useEffect(() => {
    call<NewsSettings>('GET', '/api/home/news/settings').then(fill).catch(() => undefined)
  }, [])
  if (!n) return null
  const save = async (body: Record<string, unknown>) => {
    try {
      fill(await call<NewsSettings>('PUT', '/api/home/news/settings', body))
      toast.success(t('News settings saved.'))
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    }
  }
  const change = (i: number, patch: Partial<NewsSource>) => setSources((list) => list.map((s, j) => (j === i ? { ...s, ...patch } : s)))
  return (
    <Card
      title={t('Entertainment news')}
      guide="entertainment-news"
      state={n.on ? 'ready' : 'off'}
      blurb={t(
        'Bite-sized news under Tonight on the Dashboard: film and TV, home theater, books, comics and music. Every few hours Mediarium reads these feeds and the AI writes a short summary of each story, with a link to the full article. Films, shows and music a story names link to your library, or can be added.',
      )}
    >
      <label className="cx-check" style={{ marginBottom: 10 }}>
        <input type="checkbox" checked={n.on} onChange={(e) => void save({ on: e.target.checked })} /> {t('Show entertainment news on the Dashboard')}
      </label>
      <div className="news-sources">
        {sources.map((s, i) => (
          <div key={i} className="news-source">
            <input type="checkbox" checked={s.on} onChange={(e) => change(i, { on: e.target.checked })} aria-label={t('Use {name}', { name: s.name || t('this feed') })} />
            <input value={s.name} onChange={(e) => change(i, { name: e.target.value })} placeholder={t('Name')} aria-label={t('Name')} />
            <input value={s.url} onChange={(e) => change(i, { url: e.target.value })} placeholder="https://…/feed/" aria-label={t('Feed address')} spellCheck={false} />
            <select value={s.category} onChange={(e) => change(i, { category: e.target.value })} aria-label={t('Topic')}>
              {n.categories.map((c) => (
                <option key={c} value={c}>
                  {newsCategoryName(c)}
                </option>
              ))}
            </select>
            <TestButton
              label={t('Test')}
              run={() =>
                call<{ message: string }>('POST', '/api/home/news/test', { url: s.url }).then(
                  (r) => ({ ok: true, message: r.message }),
                  (e: Error) => ({ ok: false, message: e.message }),
                )
              }
            />
            <button className="icon-btn" onClick={() => setSources((list) => list.filter((_, j) => j !== i))} aria-label={t('Remove {name}', { name: s.name || t('this feed') })}>
              <Icon name="trash" size={15} />
            </button>
          </div>
        ))}
      </div>
      <div style={row}>
        <button className="btn-sm btn-with-icon" onClick={() => setSources((list) => [...list, { name: '', url: '', category: 'film-tv', on: true }])}>
          <Icon name="plus" size={14} /> {t('Add a feed')}
        </button>
      </div>
      <div className="grid-form" style={{ marginTop: 12 }}>
        <label>
          {t('Stories summarised a day (each costs a little AI use; 0 stops new summaries)')}
          <input type="number" min={0} max={500} value={budget} onChange={(e) => setBudget(Number(e.target.value))} />
        </label>
        <p className="hint">{t('{today} summarised in the last day · {stories} stories this week · {waiting} waiting', { today: n.summarisedToday, stories: n.stories, waiting: n.waiting })}</p>
        <div style={row}>
          <button className="primary" onClick={() => void save({ sources, dailyBudget: budget })}>
            {t('Save')}
          </button>
          <button
            onClick={() =>
              call<{ message: string }>('POST', '/api/home/news/refresh').then(
                (r) => toast.success(r.message),
                (e: Error) => toast.error(e.message),
              )
            }
          >
            <Icon name="refresh" size={14} /> {t('Read the feeds now')}
          </button>
        </div>
      </div>
    </Card>
  )
}
