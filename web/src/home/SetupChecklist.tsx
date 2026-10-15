// Home fork: Settings → General → Setup checklist. Every part Mediarium (and
// the fork) can use, in one place: whether it's ready, what's missing, a test
// where there is one, the page that sets it up, and its card in the guide.
// The states come from the same answers the Extras page and the Dashboard use.
import HelpLink from '../components/HelpLink'
import { useCallback, useEffect, useState, type ReactNode } from 'react'
import { Link } from 'react-router-dom'
import { api, type DashboardData, type TwoStepStatus } from '../api'
import Icon from '../components/Icon'
import { factsFromDashboard } from '../setupStatus'
import { t } from '../i18n'
import './news.css'

type State = 'ready' | 'setup' | 'off'

interface Item {
  id: string
  title: string
  gives: string
  required?: boolean
  state: State
  missing?: string // what's missing, in a few words
  to: string // the page that sets it up
  guide: string // its card in the guide
  test?: () => Promise<string>
}

interface Extras {
  server: { kind: string; name: string }
  address: { value: string }
  streaming: { ready: boolean; problem?: string }
  streamed: { folderOk: boolean; serverFolder: string; titles: number }
  liveTv: { channels: number }
  playOnTv: { address: string; ready: boolean }
  comics: { enabled: boolean; keySet: boolean }
}

async function call<T>(method: string, path: string, body?: unknown): Promise<T> {
  const r = await fetch(path, { method, credentials: 'include', headers: body ? { 'Content-Type': 'application/json' } : undefined, body: body ? JSON.stringify(body) : undefined })
  const out = await r.json().catch(() => ({}))
  if (!r.ok) throw new Error((out as { error?: string }).error || r.statusText)
  return out as T
}
// A test answers with a message; some say ok: false (with a 200) when it didn't work.
const tested = (p: Promise<{ ok?: boolean; message?: string }>) =>
  p.then((r) => {
    if (r.ok === false) throw new Error(r.message || t('It didn’t work.'))
    return r.message || t('It works.')
  })
const get = <T,>(path: string) => call<T>('GET', path).catch(() => null)

const GUIDE = '/help/home-setup#'

export default function SetupChecklist() {
  const [items, setItems] = useState<Item[] | null>(null)
  const load = useCallback(async () => {
    const [dash, mods, settings, x, ai, mdb, news, notes, two] = await Promise.all([
      api.dashboard().catch(() => null) as Promise<DashboardData | null>,
      api.modules().catch(() => null),
      api.getSettings().catch(() => null),
      get<Extras>('/api/home/extras'),
      get<{ provider: string; model: string }>('/api/home/ai'),
      get<{ keySet: boolean }>('/api/home/mdblist'),
      get<{ on: boolean }>('/api/home/news/settings'),
      get<unknown[]>('/api/notifications'),
      api.twoStep().catch(() => null) as Promise<TwoStepStatus | null>,
    ])
    const on = (k: 'movies' | 'tv') => !!(mods as Record<string, { enabled?: boolean }> | null)?.[k]?.enabled
    const f = dash ? factsFromDashboard(dash, { movies: on('movies'), tv: on('tv') }) : null
    const list: Item[] = []
    const add = (it: Item) => list.push(it)

    // The basics
    const tmdb = !!(settings as { hasTmdbApiKey?: boolean } | null)?.hasTmdbApiKey
    add({ id: 'tmdb', title: 'TMDB', gives: t('Titles, posters and episode lists'), required: true, state: tmdb ? 'ready' : 'setup', missing: tmdb ? undefined : t('a TMDB API key'), to: '/settings/metadata', guide: 'tmdb', test: () => tested(call('POST', '/api/settings/test-service', { service: 'tmdb' })) })
    if (f) {
      add({ id: 'indexers', title: t('Indexers'), gives: t('Where releases are found'), required: true, state: f.indexers > 0 ? 'ready' : 'setup', missing: f.indexers > 0 ? undefined : t('an indexer'), to: '/settings/indexers', guide: 'usenet-and-indexers' })
      const dl = f.usenetServers > 0 || f.torrentsReady
      add({ id: 'download', title: t('Usenet provider or torrents'), gives: t('Where releases are downloaded from'), required: true, state: dl ? 'ready' : 'setup', missing: dl ? undefined : t('a Usenet provider, or torrents turned on'), to: '/settings/usenet', guide: 'usenet-and-indexers' })
      const broken = f.foldersBroken
      add({ id: 'folders', title: t('Media folders'), gives: t('Where your library is filed'), required: true, state: broken.length ? 'setup' : 'ready', missing: broken.length ? t('folders Mediarium can write to: {list}', { list: broken.join(', ') }) : undefined, to: '/settings/media', guide: '' })
    }

    // Media server and streaming
    if (x) {
      const server = x.server.kind ? x.server.name || x.server.kind : ''
      add({ id: 'jellyfin', title: t('Jellyfin or Emby'), gives: t('Playing on your TVs, Live TV in its guide, the streamed library'), state: server ? 'ready' : 'setup', missing: server ? undefined : t('a Jellyfin or Emby server'), to: '/settings/media-servers', guide: 'jellyfin-or-emby' })
      add({ id: 'address', title: t("Mediarium's address on your network"), gives: t('Reached by your TV and media server'), state: x.address.value ? 'ready' : 'setup', missing: x.address.value ? undefined : t('the address, such as http://192.168.1.20:8264'), to: '/settings/extras', guide: 'mediariums-address-on-your-network', test: x.address.value ? () => tested(call('POST', '/api/home/extras/test/address')) : undefined })
      add({ id: 'streaming', title: t('Streaming (StreamNZB)'), gives: t('Stream now, without downloading first'), state: x.streaming.ready ? 'ready' : 'setup', missing: x.streaming.ready ? undefined : x.streaming.problem || t("StreamNZB's address and admin token"), to: '/settings/extras', guide: 'streaming-streamnzb', test: () => tested(call('POST', '/api/home/extras/test/streaming')) })
      const sNeeds = [!x.streaming.ready && t('Streaming'), !x.address.value && t('the address'), !server && t('Jellyfin or Emby'), !x.streamed.folderOk && t('a folder mapped to /data/streamed'), x.streamed.serverFolder === '' && t('where the media server sees that folder')].filter(Boolean)
      add({ id: 'streamed', title: t('Streamed library'), gives: t('Streamed titles in the Library and the media server'), state: sNeeds.length ? 'setup' : 'ready', missing: sNeeds.length ? sNeeds.join(', ') : undefined, to: '/settings/extras', guide: 'streamed-library' })
      add({ id: 'livetv', title: t('Live TV'), gives: t('Scheduled channels with a guide'), state: x.liveTv.channels > 0 ? 'ready' : 'off', missing: x.liveTv.channels > 0 ? undefined : t('a channel'), to: '/tv', guide: 'live-tv' })
      add({ id: 'playontv', title: t('Play on TV'), gives: t('Send a stream to an Android TV box'), state: x.playOnTv.ready ? 'ready' : x.playOnTv.address ? 'setup' : 'off', missing: x.playOnTv.ready ? undefined : x.playOnTv.address ? t('pairing with the TV') : t("the TV's address"), to: '/settings/extras', guide: 'play-on-tv', test: x.playOnTv.address ? () => tested(call('POST', '/api/home/extras/test/tv')) : undefined })
    }

    // Extras
    if (ai) {
      const ready = ai.provider !== '' && !!ai.model
      add({ id: 'ai', title: t('AI'), gives: t('AI Search, Tonight, Catch me up, the family filter, news summaries'), state: ready ? 'ready' : 'off', missing: ready ? undefined : t('Ollama or a Claude key'), to: '/settings/extras', guide: 'ai', test: ready ? () => tested(call('POST', '/api/home/ai/test')) : undefined })
    }
    if (mdb) add({ id: 'mdblist', title: t('Scores (MDBList)'), gives: t('IMDb, Rotten Tomatoes and Metacritic scores, Common Sense ages'), state: mdb.keySet ? 'ready' : 'off', missing: mdb.keySet ? undefined : t('a free MDBList key'), to: '/settings/extras', guide: 'scores-mdblist', test: mdb.keySet ? () => tested(call('POST', '/api/home/mdblist/test')) : undefined })
    if (news) add({ id: 'news', title: t('Entertainment news'), gives: t('Short news on the Dashboard'), state: news.on ? (ai && ai.provider ? 'ready' : 'setup') : 'off', missing: news.on && !(ai && ai.provider) ? t('the AI, to summarise the stories') : news.on ? undefined : t('turning on'), to: '/settings/extras', guide: 'entertainment-news' })
    if (x) add({ id: 'comics', title: t('Comics'), gives: t('Comic series, new issues and a reader'), state: x.comics.enabled && x.comics.keySet ? 'ready' : x.comics.enabled ? 'setup' : 'off', missing: x.comics.enabled && !x.comics.keySet ? t('a ComicVine API key, on the Comics page') : x.comics.enabled ? undefined : t('turning on'), to: x.comics.enabled ? '/comics' : '/settings/extras', guide: 'comics' })

    // Looking after it
    if (notes) add({ id: 'notify', title: t('Notifications'), gives: t('Messages when something arrives or fails'), state: notes.length > 0 ? 'ready' : 'off', missing: notes.length > 0 ? undefined : t('a way to send them, such as ntfy'), to: '/settings/notifications', guide: 'notifications' })
    if (two) add({ id: 'twostep', title: t('Two-step sign-in'), gives: t('Your account stays safe if Mediarium is reachable from outside'), state: two.enabled ? 'ready' : 'off', missing: two.enabled ? undefined : t('turning it on for your account'), to: '/settings/security', guide: 'reaching-mediarium-away-from-home' })
    setItems(list)
  }, [])
  useEffect(() => {
    void load()
  }, [load])

  if (!items) return <div className="skeleton" style={{ height: 320 }} />
  const ready = items.filter((i) => i.state === 'ready').length
  const missingRequired = items.filter((i) => i.required && i.state !== 'ready')
  const groups: [string, string[]][] = [
    [t('The basics'), ['tmdb', 'indexers', 'download', 'folders']],
    [t('Media server and streaming'), ['jellyfin', 'address', 'streaming', 'streamed', 'livetv', 'playontv']],
    [t('Extras'), ['ai', 'mdblist', 'news', 'comics']],
    [t('Looking after it'), ['notify', 'twostep']],
  ]
  return (
    <div className="setup-check">
      <div className="setup-check-head card">
        <span className="gap-ring" style={{ ['--p' as string]: Math.round((ready / Math.max(1, items.length)) * 100) }}>
          <span>{Math.round((ready / Math.max(1, items.length)) * 100)}%</span>
        </span>
        <div>
          <h2>
            {t('{n} of {total} parts ready', { n: ready, total: items.length })} <HelpLink topic="home-setup" />
          </h2>
          <p className="hint">
            {missingRequired.length
              ? t('Still needed to download anything: {list}.', { list: missingRequired.map((i) => i.title).join(', ') })
              : t('The basics are done. Everything else is optional: add what you want, one at a time.')}
          </p>
        </div>
        <button className="btn-sm" onClick={() => void load()}>
          <Icon name="refresh" size={14} /> {t('Check again')}
        </button>
      </div>
      {groups.map(([title, ids]) => {
        const rows = ids.map((id) => items.find((i) => i.id === id)).filter((i): i is Item => !!i)
        if (!rows.length) return null
        return (
          <section key={title} className="setup-check-group">
            <h3>{title}</h3>
            <ul>
              {rows.map((i) => (
                <Row key={i.id} item={i} />
              ))}
            </ul>
          </section>
        )
      })}
    </div>
  )
}

const STATE_TEXT: Record<State, string> = { ready: t('ready'), setup: t('needs setting up'), off: t('off') }

function Row({ item: i }: { item: Item }) {
  const [result, setResult] = useState<{ ok: boolean; text: string } | null>(null)
  const [busy, setBusy] = useState(false)
  const test = () => {
    if (!i.test) return
    setBusy(true)
    setResult(null)
    i.test()
      .then((text) => setResult({ ok: true, text }))
      .catch((e: Error) => setResult({ ok: false, text: e.message }))
      .finally(() => setBusy(false))
  }
  let missing: ReactNode = null
  if (i.missing) missing = <span className="setup-check-missing"><strong>{t('Needs:')}</strong> {i.missing}</span>
  return (
    <li className={`setup-check-row is-${i.state}`}>
      <span className="setup-check-dot" aria-hidden="true">
        <Icon name={i.state === 'ready' ? 'check' : i.state === 'setup' ? 'warning' : 'info'} size={15} />
      </span>
      <div className="setup-check-text">
        <strong>
          {i.title}
          {i.required ? <span className="req-badge required">{t('Required')}</span> : <span className="req-badge optional">{t('Optional')}</span>}
          <span className={`badge ${i.state === 'ready' ? 'downloaded' : 'missing'}`}>{STATE_TEXT[i.state]}</span>
        </strong>
        <span className="hint">{i.gives}</span>
        {missing}
        {result && <span className={result.ok ? 'setup-check-ok' : 'error-text'}>{result.text}</span>}
      </div>
      <div className="setup-check-actions">
        {i.test && (
          <button className="btn-sm" onClick={test} disabled={busy}>
            {busy ? t('Testing…') : t('Test it')}
          </button>
        )}
        <Link className={`btn-link${i.state === 'ready' ? '' : ' primary-look'}`} to={i.to}>
          {i.state === 'ready' ? t('Open') : t('Set up')}
        </Link>
        {i.guide !== '' && (
          <Link className="btn-link" to={GUIDE + i.guide} title={t('How to set it up, in the guide')}>
            <Icon name="book" size={14} /> {t('Guide')}
          </Link>
        )}
      </div>
    </li>
  )
}
