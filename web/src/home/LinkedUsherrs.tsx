// Usherr: linked screenings, step 1 (Settings → Movie night → Linked
// Usherrs, /settings/linked). This Usherr's identity, codes that link it with
// a friend's Usherr, and the Usherrs it's linked with, all through the Usherr
// Link Relay. Off until the switch is on (internal/api/home_link.go).
import { copyText } from '../components/CopyBox'
import { useCallback, useEffect, useState } from 'react'
import ConfirmDialog from '../components/ConfirmDialog'
import Switch from '../components/Switch'
import { useToast } from '../components/Toast'
import { useDocumentTitle } from '../documentTitle'
import { timeAgo } from '../format'
import { t } from '../i18n'
import './linked.css'

interface LinkSettings {
  on: boolean
  relay: string // '' for the default relay
  name: string // '' for the default name
}
interface Status {
  state: 'off' | 'connecting' | 'connected' | 'error'
  error?: string
  skewMs: number
  clockWarning: boolean
}
interface Code {
  id: number
  code?: string
  createdAt: number
  expiresAt: number
}
interface Answer {
  settings: LinkSettings
  name: string
  defaultName: string
  relay: string
  defaultRelay: string
  fingerprint: string
  status: Status
  invites: Code[] | null
  joins: Code[] | null
}
interface Peer {
  id: number
  name: string
  theirName: string
  nickname: string
  fingerprint?: string
  relay: string
  status: 'linked' | 'unlinked'
  unlinkedBy?: 'me' | 'them'
  version?: string
  createdAt: number
  lastSeen: number
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

const errText = (e: unknown) => (e instanceof Error ? e.message : String(e))

const STATE_TEXT: Record<Status['state'], string> = {
  off: t('Off'),
  connecting: t('Connecting…'),
  connected: t('Connected'),
  error: t('Can’t reach the relay'),
}

// "23 h 5 min left"
function timeLeft(ms: number): string {
  const min = Math.max(0, Math.floor(ms / 60000))
  if (min < 1) return t('less than a minute left')
  const h = Math.floor(min / 60)
  return h > 0 ? t('{h} h {m} min left', { h, m: min % 60 }) : t('{m} min left', { m: min })
}

export default function LinkedUsherrs() {
  useDocumentTitle(t('Linked Usherrs'))
  const toast = useToast()
  const [a, setA] = useState<Answer | null>(null)
  const [peers, setPeers] = useState<Peer[]>([])
  const [busy, setBusy] = useState(false)
  const [name, setName] = useState<string | null>(null) // being edited
  const [relayKind, setRelayKind] = useState<'default' | 'own' | null>(null)
  const [relay, setRelay] = useState<string | null>(null) // being edited
  const [code, setCode] = useState('')
  const [joined, setJoined] = useState<{ ok: boolean; message: string } | null>(null)
  const [unlink, setUnlink] = useState<Peer | null>(null)
  const [renaming, setRenaming] = useState<{ id: number; name: string } | null>(null)
  const [now, setNow] = useState(() => Date.now()) // for the codes' countdowns

  const load = useCallback(() => {
    call<Answer>('GET', '/api/link')
      .then(setA)
      .catch(() => undefined)
    call<{ peers: Peer[] | null }>('GET', '/api/link/peers')
      .then((d) => setPeers(d.peers ?? []))
      .catch(() => undefined)
  }, [])
  useEffect(load, [load])

  // Look again now and then: the connection, codes being used, links made
  // or ended on the other side. Quicker while connecting.
  const state = a?.status.state
  const on = a?.settings.on
  useEffect(() => {
    if (!on) return
    const id = window.setTimeout(load, state === 'connected' ? 10000 : 3000)
    return () => window.clearTimeout(id)
  }, [on, state, a, load])
  // The codes' countdowns.
  useEffect(() => {
    const id = window.setInterval(() => setNow(Date.now()), 30000)
    return () => window.clearInterval(id)
  }, [])

  if (!a) return <div>{t('Loading…')}</div>

  const set = a.settings
  const kind = relayKind ?? (set.relay ? 'own' : 'default')
  const save = async (patch: Partial<LinkSettings>, ok?: string) => {
    setBusy(true)
    try {
      setA(await call<Answer>('PUT', '/api/link/settings', { ...set, ...patch }))
      if (ok) toast.success(ok)
      return true
    } catch (e) {
      toast.error(errText(e))
      return false
    } finally {
      setBusy(false)
    }
  }
  const makeCode = async () => {
    setBusy(true)
    try {
      await call<Code>('POST', '/api/link/invites', {})
      load()
    } catch (e) {
      toast.error(errText(e))
    } finally {
      setBusy(false)
    }
  }
  const cancelCode = async (c: Code, ok: string) => {
    try {
      await call('DELETE', `/api/link/invites/${c.id}`)
      toast.success(ok)
      load()
    } catch (e) {
      toast.error(errText(e))
    }
  }
  const join = async () => {
    setBusy(true)
    setJoined(null)
    try {
      const d = await call<{ state: 'linked' | 'waiting'; peer?: Peer }>('POST', '/api/link/join', { code })
      setCode('')
      setJoined(
        d.state === 'linked'
          ? { ok: true, message: t('Linked with {name}.', { name: d.peer?.name ?? t('their Usherr') }) }
          : { ok: true, message: t('Sent. Their Usherr isn’t online right now; you’re linked as soon as it is (within 24 hours).') },
      )
      load()
    } catch (e) {
      setJoined({ ok: false, message: errText(e) })
    } finally {
      setBusy(false)
    }
  }
  const rename = async (p: Peer, nickname: string) => {
    try {
      const d = await call<{ peers: Peer[] | null }>('PUT', `/api/link/peers/${p.id}`, { name: nickname })
      setPeers(d.peers ?? [])
      setRenaming(null)
    } catch (e) {
      toast.error(errText(e))
    }
  }
  const remove = async (p: Peer) => {
    setUnlink(null)
    try {
      const d = await call<{ peers: Peer[] | null; told: boolean }>('DELETE', `/api/link/peers/${p.id}`)
      setPeers(d.peers ?? [])
      if (p.status === 'linked') toast.success(d.told ? t('Unlinked. {name} was told.', { name: p.name }) : t('Unlinked here. Nothing from {name} is taken in anymore.', { name: p.name }))
    } catch (e) {
      toast.error(errText(e))
    }
  }

  const st = a.status
  const invites = a.invites ?? []
  const joins = a.joins ?? []
  const linked = peers.filter((p) => p.status === 'linked')
  const ended = peers.filter((p) => p.status === 'unlinked')

  return (
    <div className="lk-page">
      <div className="page-header">
        <h1>{t('Linked Usherrs')}</h1>
      </div>

      <section className="card" data-walk="linked-switch">
        <p>{t('Link this Usherr with a friend’s Usherr, so you can watch together later: each house plays its own copy, and the link keeps everyone in step. No video crosses between houses.')}</p>
        <p className="hint">{t('Linked Usherrs talk through the Usherr Link Relay: a postman that only passes on sealed envelopes. It can’t read them, and nobody opens a port or sees your home address.')}</p>
        <Switch checked={set.on} disabled={busy} showState label={t('Link with other Usherrs')} onChange={(next) => void save({ on: next }, next ? undefined : t('Disconnected from the relay.'))} />
        {set.on && (
          <p className="lk-status">
            <span className={`lk-dot s-${st.state}`} aria-hidden="true" />
            {t('Relay:')} {STATE_TEXT[st.state]}
          </p>
        )}
        {set.on && st.clockWarning && (
          <p className="notice notice-warn">{t('This server’s clock is {s} seconds off. Set the time right (or turn on network time), or screenings won’t start together.', { s: Math.round(Math.abs(st.skewMs) / 1000) })}</p>
        )}
      </section>

      <section className="card">
        <h2>{t('This Usherr')}</h2>
        <div className="lk-row">
          <label>
            {t('Name linked Usherrs see')}
            <input value={name ?? set.name} placeholder={a.defaultName} maxLength={60} onChange={(e) => setName(e.target.value)} />
          </label>
          <button className="btn-sm primary" disabled={busy || name === null || name === set.name} onClick={() => void save({ name: name ?? '' }, t('Saved.')).then((ok) => ok && setName(null))}>
            {t('Save')}
          </button>
        </div>
        <p className="lk-fp">
          {t('Fingerprint')} <code>{a.fingerprint}</code>
        </p>
        <p className="hint">{t('Your friend sees the same fingerprint next to this Usherr. Compare them if you want to be sure the link is really yours.')}</p>

        <h3>{t('Relay')}</h3>
        <div className="seg" role="radiogroup" aria-label={t('Relay')}>
          <button type="button" role="radio" aria-checked={kind === 'default'} className={kind === 'default' ? 'active' : ''} disabled={busy} onClick={() => {
            setRelayKind('default')
            if (set.relay) void save({ relay: '' }, t('Saved.'))
          }}>
            {t('Usherr’s relay')}
          </button>
          <button type="button" role="radio" aria-checked={kind === 'own'} className={kind === 'own' ? 'active' : ''} disabled={busy} onClick={() => {
            setRelayKind('own')
          }}>
            {t('My own relay')}
          </button>
        </div>
        {kind === 'default' ? (
          <p className="hint">{t('{relay}, run by the Usherr project for everyone.', { relay: a.defaultRelay })}</p>
        ) : (
          <>
            <div className="lk-row">
              <label>
                {t('Relay address')}
                <input value={relay ?? set.relay} placeholder="wss://relay.example.com" spellCheck={false} onChange={(e) => setRelay(e.target.value)} />
              </label>
              <button className="btn-sm primary" disabled={busy || relay === null || !relay.trim() || relay === set.relay} onClick={() => void save({ relay: relay ?? '' }, t('Saved.')).then((ok) => ok && setRelay(null))}>
                {t('Save')}
              </button>
            </div>
            <p className="hint">{t('A relay you run yourself (usherr-relay, see the guide). Codes made here carry its address, so your friend’s Usherr uses it too. Links made before keep the relay they were made on.')}</p>
          </>
        )}
      </section>

      {set.on && (
        <section className="card" data-walk="linked-invite">
          <h2>{t('Invite a friend’s Usherr')}</h2>
          <p>{t('Make a code and send it to your friend. It works once, for 24 hours.')}</p>
          <button className="btn-sm primary" disabled={busy || st.state !== 'connected'} onClick={() => void makeCode()}>
            {t('Make a code')}
          </button>
          {invites.length > 0 && (
            <ul className="lk-codes">
              {invites.map((c) => (
                <li key={c.id}>
                  <code className="lk-code">{c.code}</code>
                  <span className="lk-code-tools">
                    <button className="btn-sm" onClick={() => void copyText(c.code ?? '').then((ok) => ok && toast.success(t('Copied.')))}>
                      {t('Copy')}
                    </button>
                    <span className="hint">{timeLeft(c.expiresAt - now)}</span>
                    <button className="btn-sm" onClick={() => void cancelCode(c, t('The code was cancelled.'))}>
                      {t('Cancel')}
                    </button>
                  </span>
                </li>
              ))}
            </ul>
          )}
        </section>
      )}

      {set.on && (
        <section className="card" data-walk="linked-join">
          <h2>{t('Join with a code')}</h2>
          <p>{t('Got a code from a friend? Paste it here.')}</p>
          <div className="lk-row">
            <label>
              {t('Code')}
              <input value={code} placeholder="USH-XXXX-XXXX-XXXX-XXXX-XXXX-X" spellCheck={false} autoComplete="off" onChange={(e) => setCode(e.target.value)} onKeyDown={(e) => e.key === 'Enter' && code.trim() && !busy && void join()} />
            </label>
            <button className="btn-sm primary" disabled={busy || !code.trim()} onClick={() => void join()}>
              {busy ? t('Linking…') : t('Link')}
            </button>
          </div>
          {joined && <p className={joined.ok ? 'hint' : 'notice notice-warn'}>{joined.message}</p>}
          {joins.map((j) => (
            <p key={j.id} className="lk-waiting">
              <span className="hint">{t('A code sent {when} is waiting for the other Usherr to answer.', { when: timeAgo(new Date(j.createdAt).toISOString()) })}</span>{' '}
              <button className="btn-sm" onClick={() => void cancelCode(j, t('Stopped waiting.'))}>
                {t('Stop waiting')}
              </button>
            </p>
          ))}
        </section>
      )}

      <section className="card" data-walk="linked-list">
        <h2>{t('Linked Usherrs')}</h2>
        {linked.length === 0 ? (
          <p className="hint">{t('None yet.')}</p>
        ) : (
          <ul className="lk-peers">
            {linked.map((p) => (
              <li key={p.id}>
                <div className="lk-peer-main">
                  {renaming?.id === p.id ? (
                    <span className="lk-rename">
                      <input autoFocus value={renaming.name} maxLength={60} placeholder={p.theirName} aria-label={t('Name here')} onChange={(e) => setRenaming({ id: p.id, name: e.target.value })} onKeyDown={(e) => {
                        if (e.key === 'Enter') void rename(p, renaming.name)
                        if (e.key === 'Escape') setRenaming(null)
                      }} />
                      <button className="btn-sm primary" onClick={() => void rename(p, renaming.name)}>
                        {t('Save')}
                      </button>
                    </span>
                  ) : (
                    <strong>{p.name}</strong>
                  )}
                  {p.nickname && p.nickname !== p.theirName && <span className="hint"> {t('(calls itself {name})', { name: p.theirName })}</span>}
                  <div className="hint">
                    <code>{p.fingerprint}</code> · {p.lastSeen ? t('heard from {when}', { when: timeAgo(new Date(p.lastSeen).toISOString()) }) : t('not heard from yet')}
                    {p.relay !== a.relay && ` · ${p.relay}`}
                  </div>
                </div>
                <span className="lk-peer-tools">
                  {renaming?.id !== p.id && (
                    <button className="btn-sm" onClick={() => setRenaming({ id: p.id, name: p.nickname })}>
                      {t('Rename')}
                    </button>
                  )}
                  <button className="btn-sm danger-ghost" onClick={() => setUnlink(p)}>
                    {t('Unlink')}
                  </button>
                </span>
              </li>
            ))}
          </ul>
        )}
        {ended.length > 0 && (
          <ul className="lk-peers lk-ended">
            {ended.map((p) => (
              <li key={p.id}>
                <div className="lk-peer-main">
                  <strong>{p.name}</strong>
                  <div className="hint">{p.unlinkedBy === 'them' ? t('Unlinked by them') : t('Unlinked')}</div>
                </div>
                <button className="btn-sm" onClick={() => void remove(p)}>
                  {t('Remove from the list')}
                </button>
              </li>
            ))}
          </ul>
        )}
      </section>

      {unlink && (
        <ConfirmDialog title={t('Unlink {name}?', { name: unlink.name })} confirmLabel={t('Unlink')} danger onConfirm={() => void remove(unlink)} onCancel={() => setUnlink(null)}>
          <p>{t('Their Usherr is told, and the two Usherrs stop talking at once. To link again, one of you makes a new code.')}</p>
        </ConfirmDialog>
      )}
    </div>
  )
}
