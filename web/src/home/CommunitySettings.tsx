// Usherr: Community (preview) (Settings → Movie night → Community,
// /settings/community). The household's own Discord bot: its token, whether
// it's connected, the link that invites it to a server, and the channel
// Usherr posts in. Watch parties and reviews build on this.
import { useCallback, useEffect, useState } from 'react'
import Switch from '../components/Switch'
import { useToast } from '../components/Toast'
import { useDocumentTitle } from '../documentTitle'
import { t } from '../i18n'
import { startSmh } from '../smh/engine'
import './community.css'

interface CommunitySettingsData {
  on: boolean
  server: string
  channel: string
  reviewsChannel: string // '' for the watch parties' channel
  reviewsKind: 'text' | 'forum'
}
interface Server {
  id: string
  name: string
  icon?: string
}
interface Channel {
  id: string
  name: string
  kind: 'text' | 'announcement' | 'forum' | 'category'
  parent?: string
  position: number
}
interface Linked {
  username: string
  name: string
  avatar?: string
}
interface Answer {
  settings: CommunitySettingsData
  tokenSet: boolean
  inviteUrl: string
  linked: Linked[] | null
  status: {
    state: 'off' | 'connecting' | 'connected' | 'error'
    error?: string
    name?: string
    servers: Server[] | null
    warnings?: string[] | null
  }
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
  let data: { error?: string } | null = null
  try {
    data = text ? (JSON.parse(text) as { error?: string }) : null
  } catch {
    data = null
  }
  if (!res.ok) throw new Error(data?.error ?? t('Something went wrong (error {status}).', { status: res.status }))
  return data as T
}

const STATE_TEXT: Record<Answer['status']['state'], string> = {
  off: t('Off'),
  connecting: t('Connecting…'),
  connected: t('Connected'),
  error: t('Not connected'),
}

export default function CommunitySettings() {
  useDocumentTitle(t('Community'))
  const toast = useToast()
  const [a, setA] = useState<Answer | null>(null)
  const [token, setToken] = useState('')
  const [busy, setBusy] = useState(false)
  const [loaded, setLoaded] = useState<{ server: string; list: Channel[] } | null>(null) // the channels of one server
  const [tested, setTested] = useState<{ ok: boolean; message: string } | null>(null)

  const load = useCallback(() => {
    call<Answer>('GET', '/api/home/community')
      .then(setA)
      .catch(() => undefined)
  }, [])
  useEffect(load, [load])

  const state = a?.status.state
  const servers = a?.status.servers ?? []
  const server = a?.settings.server ?? ''
  // While connecting, or while the bot is in no server yet (just invited),
  // look again now and then.
  useEffect(() => {
    if (state !== 'connecting' && !(state === 'connected' && servers.length === 0)) return
    const id = window.setTimeout(load, 3000)
    return () => window.clearTimeout(id)
  }, [state, servers.length, a, load])
  useEffect(() => {
    if (state !== 'connected' || !server) return
    call<{ channels: Channel[] | null }>('GET', `/api/home/community/channels?server=${server}`)
      .then((d) => setLoaded({ server, list: d.channels ?? [] }))
      .catch((e) => {
        setLoaded({ server, list: [] })
        toast.error(e instanceof Error ? e.message : String(e))
      })
  }, [state, server, toast])
  const channels = loaded && loaded.server === server ? loaded.list : null

  if (!a) return <div>{t('Loading…')}</div>

  const save = async (patch: Partial<CommunitySettingsData> & { token?: string }, ok?: string) => {
    setBusy(true)
    setTested(null)
    try {
      const d = await call<Answer>('PUT', '/api/home/community', { ...a.settings, ...patch })
      setA(d)
      if (patch.token !== undefined) setToken('')
      if (ok) toast.success(ok)
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }
  const test = async () => {
    setBusy(true)
    try {
      setTested(await call<{ ok: boolean; message: string }>('POST', '/api/home/community/test', {}))
    } catch (e) {
      setTested({ ok: false, message: e instanceof Error ? e.message : String(e) })
    } finally {
      setBusy(false)
    }
  }

  const st = a.status
  const set = a.settings
  const connected = st.state === 'connected'
  const categories = new Map((channels ?? []).filter((c) => c.kind === 'category').map((c) => [c.id, c.name]))
  const postable = (channels ?? []).filter((c) => c.kind === 'text' || c.kind === 'announcement')
  const reviewable = (channels ?? []).filter((c) => c.kind === 'text' || c.kind === 'announcement' || c.kind === 'forum')
  const serverGone = !!set.server && connected && servers.length > 0 && !servers.some((s) => s.id === set.server)

  return (
    <div className="cm-page">
      <div className="page-header">
        <h1>
          {t('Community')} <span className="cm-preview">{t('Preview')}</span>
        </h1>
      </div>

      <section className="card">
        <p>{t('Watch parties and reviews with your friends, in your own Discord server. Usherr runs your own Discord bot: you make it once in Discord’s Developer Portal, paste its token here and invite it to your server. It connects from your home to Discord, so nothing on your network is opened to the internet.')}</p>
        <p className="hint">{t('This is a preview: it connects, lets people link their Discord account with /link, and shares Showtimes as watch parties (tick Share to Discord when you book one).')}</p>
        <button className="btn-sm" onClick={() => startSmh('community-bot', 'simple')}>
          {t('Show me how')}
        </button>
      </section>

      <section className="card" data-walk="community-bot">
        <h2>{t('Your bot')}</h2>
        <Switch
          checked={set.on}
          disabled={busy}
          showState
          label={t('Connect the bot')}
          onChange={(on) => {
            if (on && !a.tokenSet && !token.trim()) {
              toast.error(t('Paste your bot’s token first.'))
              return
            }
            void save({ on, ...(token.trim() ? { token } : {}) }, on ? undefined : t('The bot is disconnected.'))
          }}
        />
        <div className="cm-row" data-walk="community-token">
          <label>
            {t('Bot token')}
            <input type="password" autoComplete="off" spellCheck={false} value={token} placeholder={a.tokenSet ? t('(saved, unchanged)') : t('Paste the token from Bot, Reset Token')} onChange={(e) => setToken(e.target.value)} />
          </label>
          <button className="primary btn-sm" disabled={busy || !token.trim()} onClick={() => void save({ on: true, token }, t('Saved.'))}>
            {t('Save and connect')}
          </button>
        </div>
        <p className="hint">{t('The token is the bot’s password: anyone with it can post as your bot. Usherr keeps it encrypted and never shows it again.')}</p>
        <p className="cm-status" data-walk="community-status">
          <span className={`cm-dot s-${st.state}`} aria-hidden="true" />
          {STATE_TEXT[st.state]}
          {connected && st.name ? ` ${t('as {name}', { name: st.name })}` : ''}
        </p>
        {st.error && <p className="notice notice-warn">{st.error}</p>}
        {(st.warnings ?? []).map((w) => (
          <p key={w} className="notice notice-warn">
            {w}
          </p>
        ))}
      </section>

      {connected && (
        <section className="card" data-walk="community-invite">
          <h2>{t('Invite it to your server')}</h2>
          <p>{t('Open the link, choose your server and press Authorize. You need Manage Server there. The bot asks only for what it uses: reading and posting in channels, cards, threads, webhooks and events.')}</p>
          {a.inviteUrl && (
            <a className="btn-sm primary cm-invite" href={a.inviteUrl} target="_blank" rel="noreferrer">
              {t('Invite the bot')}
            </a>
          )}
          {servers.length === 0 ? (
            <p className="hint">{t('The bot isn’t in a server yet. This page notices by itself once you’ve invited it.')}</p>
          ) : (
            <ul className="cm-servers">
              {servers.map((s) => (
                <li key={s.id}>
                  {s.icon ? <img src={s.icon} alt="" /> : <span className="cm-noicon" aria-hidden="true">{s.name.slice(0, 1)}</span>}
                  {s.name}
                </li>
              ))}
            </ul>
          )}
        </section>
      )}

      {connected && servers.length > 0 && (
        <section className="card" data-walk="community-where">
          <h2>{t('Where Usherr posts')}</h2>
          <p>{t('One channel for now, for example a new #usherr-showtimes. Moving to another server later is fine: choose it here, and the same bot posts there.')}</p>
          <div className="cm-row">
            <label>
              {t('Server')}
              <select value={servers.some((s) => s.id === set.server) ? set.server : ''} disabled={busy} onChange={(e) => void save({ server: e.target.value, channel: '', reviewsChannel: '' })}>
                <option value="">{t('Choose a server')}</option>
                {servers.map((s) => (
                  <option key={s.id} value={s.id}>
                    {s.name}
                  </option>
                ))}
              </select>
            </label>
            <label>
              {t('Channel')}
              <select value={set.channel} disabled={busy || !set.server || channels === null} onChange={(e) => void save({ channel: e.target.value }, e.target.value ? t('Saved.') : undefined)}>
                <option value="">{channels === null && set.server ? t('Loading…') : t('Choose a channel')}</option>
                {postable.map((c) => (
                  <option key={c.id} value={c.id}>
                    #{c.name}
                    {c.parent && categories.get(c.parent) ? ` (${categories.get(c.parent)})` : ''}
                  </option>
                ))}
              </select>
            </label>
          </div>
          <div className="cm-row" data-walk="community-reviews-channel">
            <label>
              {t('Reviews go to')}
              <select value={set.reviewsChannel} disabled={busy || !set.server || channels === null} onChange={(e) => void save({ reviewsChannel: e.target.value }, t('Saved.'))}>
                <option value="">{t('The same channel as watch parties')}</option>
                {reviewable.map((c) => (
                  <option key={c.id} value={c.id}>
                    {c.kind === 'forum' ? `🗂 ${c.name} ${t('(forum)')}` : `#${c.name}`}
                    {c.parent && categories.get(c.parent) ? ` (${categories.get(c.parent)})` : ''}
                  </option>
                ))}
              </select>
            </label>
          </div>
          <p className="hint">
            {set.reviewsChannel && set.reviewsKind === 'forum'
              ? t('A forum: each film gets its own post the first time it’s reviewed, and every review of it is a reply there. A #review typed in a watch party is copied in too, with a link back.')
              : t('Every review gets a card there, wherever it was written: a #review typed in a watch party is copied in too, with a link back. A forum channel works too: one post per film.')}
          </p>
          {serverGone && <p className="notice notice-warn">{t('The bot isn’t in the server chosen before. Choose a server again.')}</p>}
          <button className="btn-sm" data-walk="community-test" disabled={busy || !set.channel} onClick={() => void test()}>
            {t('Post a test card')}
          </button>
          {tested && <p className={tested.ok ? 'hint' : 'notice notice-warn'}>{tested.message}</p>}
        </section>
      )}

      {connected && (
        <section className="card" data-walk="community-people">
          <h2>{t('Who’s linked')}</h2>
          <p>{t('Each person links their own Discord account: /link in the server gives them a code, which they type in Settings, Linked services. Then Usherr knows who presses I’m in, and whose review is whose.')}</p>
          {(a.linked ?? []).length === 0 ? (
            <p className="hint">{t('Nobody yet. Try it yourself: type /link in your server.')}</p>
          ) : (
            <ul className="cm-people">
              {(a.linked ?? []).map((p) => (
                <li key={p.username}>
                  {p.avatar ? <img src={p.avatar} alt="" /> : <span className="cm-noicon" aria-hidden="true">{p.name.slice(0, 1)}</span>}
                  {t('{name} is {username} in Usherr', { name: p.name, username: p.username })}
                </li>
              ))}
            </ul>
          )}
        </section>
      )}
    </div>
  )
}
