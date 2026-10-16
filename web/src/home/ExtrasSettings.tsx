// Home fork: Settings > Extras. One card for each thing the fork adds that
// needs setting up, in the style of the setup wizard's service cards: what it
// is, whether it's ready, what's missing, and a Test button.
import type { MarqueeSettings, SceneKind } from './Marquee' // Usherr
import HelpLink from '../components/HelpLink'
import { useCallback, useEffect, useState, type ReactNode } from 'react'
import { Link } from 'react-router-dom'
import Icon from '../components/Icon'
import TestButton from '../components/TestButton'
import { useToast } from '../components/Toast'
import { plural, t, tx } from '../i18n'
import './news.css'

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

function Card({ title, state, blurb, needs, guide, children }: { title: string; state: State; blurb: ReactNode; needs?: ReactNode; guide?: string; children?: ReactNode }) {
  return (
    <section className={`card service-card ${state === 'ready' ? 'is-included' : 'is-optional'}`}>
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
      {show('streaming') && <StreamingCard x={x} save={save} />}
      {show('streamed') && <StreamedCard x={x} save={save} act={act} server={server} />}
      {show('livetv') && <LiveTVCard x={x} act={act} server={server} />}
      {show('playontv') && <PlayOnTVCard x={x} save={save} act={act} />}
      {show('ai') && <AICard />}
      {show('scores') && <ScoresCard />}
      {show('news') && <NewsCard />}
      {show('cinema') && <CinemaCard />}
      {show('marquee') && <MarqueeCard />}
      {show('comics') && <ComicsCard x={x} act={act} />}
      {show('direct') && <DirectCard x={x} save={save} />}
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

function StreamingCard({ x, save }: { x: Extras; save: Save }) {
  const s = x.streaming
  const [url, setUrl] = useState(s.url)
  const [token, setToken] = useState('')
  return (
    <Card
      title={t('Streaming (StreamNZB)')}
      guide="streaming-streamnzb"
      state={s.ready ? 'ready' : 'setup'}
      blurb={tx('"Stream now", the streamed library and streamed TV channels play through {streamnzb}, which streams from Usenet without downloading first. Run it next to Mediarium.', {
        streamnzb: (
          <a href="https://github.com/Gaisberg/streamnzb" target="_blank" rel="noreferrer">
            StreamNZB
          </a>
        ),
      })}
      needs={
        <>
          {t("StreamNZB's address and its admin token.")}
          {s.problem && <> {t('Now: {problem}.', { problem: s.problem })}</>}
        </>
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
      blurb={t('Adult scenes, with performers and studios. Only administrators ever see it: it is hidden from everyone else, keeps its own library and never sends notifications. Off unless you turn it on.')}
    >
      {n.enabled ? (
        <div style={row}>
          <button onClick={() => void act('PUT', '/api/scenes/settings', { enabled: false })}>{t('Turn NSFW 18+ off')}</button>
          <Link to="/scenes">{t('Open NSFW 18+')}</Link>
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
                void act('PUT', '/api/scenes/settings', { enabled: true, confirm: phrase }).then(() => {
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
// Usherr: the Marquee (/marquee), the lobby display for a tablet by the TV.
// What it shows and in what order, the sign, sizes and quiet hours. The
// tablet picks changes up within a couple of minutes.
const MQ_SCENE_LABEL: Record<SceneKind, string> = {
  playing: t('Now showing (what is playing on the TV)'),
  new: t('New on the server (one film)'),
  trailer: t('Trailer'),
  'theaters-film': t('In theaters now (one film)'),
  arrivals: t('Just added (posters)'),
  'coming-film': t('Coming soon (one film)'),
  theaters: t('In theaters now (posters)'),
  coming: t('Coming up (the week)'),
}

function MarqueeCard() {
  const toast = useToast()
  const [m, setM] = useState<MarqueeSettings | null>(null)
  const [profiles, setProfiles] = useState<string[]>([])
  const [dirty, setDirty] = useState(false)
  useEffect(() => {
    call<{ settings: MarqueeSettings; profiles: string[] }>('GET', '/api/home/marquee/settings')
      .then((d) => {
        setM(d.settings)
        setProfiles(d.profiles ?? [])
      })
      .catch(() => undefined)
  }, [])
  if (!m) return null
  const change = (patch: Partial<MarqueeSettings>) => {
    setM({ ...m, ...patch })
    setDirty(true)
  }
  const move = (i: number, by: number) => {
    const list = [...m.scenes]
    const j = i + by
    if (j < 0 || j >= list.length) return
    ;[list[i], list[j]] = [list[j], list[i]]
    change({ scenes: list })
  }
  const scene = (i: number, patch: Partial<MarqueeSettings['scenes'][number]>) => change({ scenes: m.scenes.map((s, j) => (j === i ? { ...s, ...patch } : s)) })
  const save = async () => {
    try {
      const d = await call<{ settings: MarqueeSettings }>('PUT', '/api/home/marquee/settings', m)
      setM(d.settings)
      setDirty(false)
      toast.success(t('Marquee settings saved. The tablet shows them within a couple of minutes.'))
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    }
  }
  const toggleProfile = (p: string, on: boolean) => change({ profiles: on ? [...m.profiles, p] : m.profiles.filter((x) => x !== p) })
  return (
    <Card
      title={t('Marquee')}
      state="ready"
      blurb={t('The lobby display for a tablet next to the TV: open {address} on it, full-screen. Everything here applies to it within a couple of minutes.', { address: `${window.location.origin}/marquee` })}
    >
      <div className="mq-set">
        <h3>{t('Scenes, in order')}</h3>
        <ol className="mq-set-scenes">
          {m.scenes.map((s, i) => (
            <li key={s.kind} className={s.on ? '' : 'off'}>
              <label className="cx-check">
                <input type="checkbox" checked={s.on} onChange={(e) => scene(i, { on: e.target.checked })} /> {MQ_SCENE_LABEL[s.kind]}
              </label>
              <label className="mq-set-secs">
                <input type="number" min={5} max={600} value={s.seconds} onChange={(e) => scene(i, { seconds: Number(e.target.value) || 5 })} aria-label={t('Seconds for {scene}', { scene: MQ_SCENE_LABEL[s.kind] })} /> {t('sec')}
              </label>
              <button className="icon-btn" onClick={() => move(i, -1)} disabled={i === 0} aria-label={t('Move {scene} up', { scene: MQ_SCENE_LABEL[s.kind] })}>
                <Icon name="chevron-up" size={15} />
              </button>
              <button className="icon-btn" onClick={() => move(i, 1)} disabled={i === m.scenes.length - 1} aria-label={t('Move {scene} down', { scene: MQ_SCENE_LABEL[s.kind] })}>
                <Icon name="chevron-down" size={15} />
              </button>
            </li>
          ))}
        </ol>
        <p className="hint">{t('Scenes with nothing to show are skipped. A scene taller than the screen scrolls slowly down and back, and takes the time it needs.')}</p>

        <h3>{t('The sign')}</h3>
        <div style={row}>
          <label className="cx-check">
            <input type="radio" name="mq-sign" checked={m.signMode === 'auto'} onChange={() => change({ signMode: 'auto' })} /> {t('The name of each scene')}
          </label>
          <label className="cx-check">
            <input type="radio" name="mq-sign" checked={m.signMode === 'custom'} onChange={() => change({ signMode: 'custom' })} /> {t('My own words')}
          </label>
        </div>
        {m.signMode === 'custom' && (
          <div className="grid-form">
            <label>
              {t('Line 1')}
              <input value={m.signLine1} maxLength={40} onChange={(e) => change({ signLine1: e.target.value })} placeholder={t('Now Showing')} />
            </label>
            <label>
              {t('Line 2 (optional)')}
              <input value={m.signLine2} maxLength={40} onChange={(e) => change({ signLine2: e.target.value })} placeholder={t('The Living Room')} />
            </label>
            <span className="hint">{t('Each line stays on one line and gets smaller to fit. Leave both empty to hide the sign.')}</span>
          </div>
        )}

        <h3>{t('Top bar')}</h3>
        <div style={row}>
          <label className="cx-check">
            <input type="checkbox" checked={m.showIcon} onChange={(e) => change({ showIcon: e.target.checked })} /> {t('Icon')}
          </label>
          <label className="cx-check">
            <input type="checkbox" checked={m.showName} onChange={(e) => change({ showName: e.target.checked })} /> {t('Name')}
          </label>
          <label className="cx-check">
            <input type="checkbox" checked={m.showClock} onChange={(e) => change({ showClock: e.target.checked })} /> {t('Clock')}
          </label>
        </div>

        <h3>{t('Sizes')}</h3>
        <div className="grid-form">
          <label>
            {t('Text size: {n}%', { n: m.textScale })}
            <input type="range" min={70} max={140} step={5} value={m.textScale} onChange={(e) => change({ textScale: Number(e.target.value) })} />
          </label>
          <label>
            {t('Poster and trailer size: {n}%', { n: m.posterScale })}
            <input type="range" min={60} max={120} step={5} value={m.posterScale} onChange={(e) => change({ posterScale: Number(e.target.value) })} />
          </label>
          <span className="hint">{t('Posters and the trailer also grow to fill the room left when the top bar or a sign line is switched off.')}</span>
          <label>
            {t('Scroll speed')}
            <select value={m.scrollSpeed} onChange={(e) => change({ scrollSpeed: e.target.value as MarqueeSettings['scrollSpeed'] })}>
              <option value="slow">{t('Slow')}</option>
              <option value="normal">{t('Normal')}</option>
              <option value="fast">{t('Fast')}</option>
            </select>
          </label>
          <label className="cx-check">
            <input type="checkbox" checked={m.trailerSound} onChange={(e) => change({ trailerSound: e.target.checked })} /> {t('Play trailers with sound (the tablet’s browser must allow it)')}
          </label>
        </div>

        <h3>{t('Now showing comes from')}</h3>
        <div style={row}>
          <label className="cx-check">
            <input type="checkbox" checked={m.fromJellyfin} onChange={(e) => change({ fromJellyfin: e.target.checked })} /> {t('The media server (Jellyfin or Emby)')}
          </label>
          <label className="cx-check">
            <input type="checkbox" checked={m.fromStreamNZB} onChange={(e) => change({ fromStreamNZB: e.target.checked })} /> {t('StreamNZB (Nuvio, Stremio)')}
          </label>
        </div>
        {m.fromStreamNZB && profiles.length > 0 && (
          <div style={row}>
            <span className="hint">{t('StreamNZB profiles to show (none ticked shows them all):')}</span>
            {profiles.map((p) => (
              <label key={p} className="cx-check">
                <input type="checkbox" checked={m.profiles.includes(p)} onChange={(e) => toggleProfile(p, e.target.checked)} /> {p}
              </label>
            ))}
          </div>
        )}

        <h3>{t('Quiet hours')}</h3>
        <div style={row}>
          <label className="cx-check">
            <input type="checkbox" checked={m.quietOn} onChange={(e) => change({ quietOn: e.target.checked })} /> {t('Rest between')}
          </label>
          <input type="time" value={m.quietFrom} onChange={(e) => change({ quietFrom: e.target.value })} aria-label={t('Quiet from')} />
          {t('and')}
          <input type="time" value={m.quietTo} onChange={(e) => change({ quietTo: e.target.value })} aria-label={t('Quiet until')} />
          <select value={m.quietMode} onChange={(e) => change({ quietMode: e.target.value as MarqueeSettings['quietMode'] })} aria-label={t('While quiet')}>
            <option value="clock">{t('showing a dim clock')}</option>
            <option value="black">{t('with a black screen')}</option>
          </select>
        </div>

        <h3>{t('Colours')}</h3>
        <select value={m.palette} onChange={(e) => change({ palette: e.target.value })} aria-label={t('Marquee colours')}>
          <option value="">{t('As chosen on the tablet')}</option>
          {['wine', 'cherry', 'amber', 'emerald', 'teal', 'blue', 'violet', 'rose', 'graphite'].map((p) => (
            <option key={p} value={p}>
              {p[0].toUpperCase() + p.slice(1)}
            </option>
          ))}
        </select>

        <div style={{ ...row, marginTop: 16 }}>
          <button className="primary" onClick={() => void save()} disabled={!dirty}>
            {t('Save')}
          </button>
          <a className="btn-sm btn-with-icon" href="/marquee" target="_blank" rel="noreferrer">
            <Icon name="external" size={14} /> {t('Open the marquee')}
          </a>
        </div>
      </div>
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
}

function CinemaCard() {
  const toast = useToast()
  const [c, setC] = useState<CinemaSettings | null>(null)
  const [zip, setZip] = useState('')
  const [theatre, setTheatre] = useState('')
  const [region, setRegion] = useState('US')
  const [amcKey, setAmcKey] = useState('')
  const fill = (d: CinemaSettings) => {
    setC(d)
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
    </Card>
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
