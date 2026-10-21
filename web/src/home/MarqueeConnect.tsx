// Usherr: the Marquee in Home Assistant (MQTT) and from scripts (the API).
import { useEffect, useState } from 'react'
import { useToast } from '../components/Toast'
import { t } from '../i18n'

interface MQTTSettings {
  on: boolean
  host: string
  port: number
  tls: boolean
  username: string
  prefix: string
  discovery: string
}
interface Answer {
  settings: MQTTSettings
  passwordSet: boolean
  status: { state: 'off' | 'connecting' | 'connected' | 'error'; error?: string; sinceMs?: number }
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

export default function MarqueeConnect() {
  const toast = useToast()
  const [a, setA] = useState<Answer | null>(null)
  const [m, setM] = useState<MQTTSettings | null>(null)
  const [password, setPassword] = useState<string | null>(null) // null keeps the saved one
  const [busy, setBusy] = useState(false)
  const [tested, setTested] = useState<{ ok: boolean; message: string } | null>(null)

  useEffect(() => {
    call<Answer>('GET', '/api/home/marquee/mqtt')
      .then((d) => {
        setA(d)
        setM(d.settings)
      })
      .catch(() => undefined)
  }, [])
  // While connecting, look again shortly.
  const state = a?.status.state
  useEffect(() => {
    if (state !== 'connecting') return
    const id = window.setTimeout(() => {
      call<Answer>('GET', '/api/home/marquee/mqtt')
        .then(setA)
        .catch(() => undefined)
    }, 2500)
    return () => window.clearTimeout(id)
  }, [state, a])
  if (!a || !m) return null

  const change = (patch: Partial<MQTTSettings>) => setM({ ...m, ...patch })
  const body = () => ({ ...m, password: password ?? undefined })
  const save = async () => {
    setBusy(true)
    try {
      const d = await call<Answer>('PUT', '/api/home/marquee/mqtt', body())
      setA(d)
      setM(d.settings)
      setPassword(null)
      toast.success(m.on ? t('Saved. Usherr is connecting to the broker.') : t('Saved.'))
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }
  const test = async () => {
    setBusy(true)
    try {
      setTested(await call<{ ok: boolean; message: string }>('POST', '/api/home/marquee/mqtt/test', body()))
    } catch (e) {
      setTested({ ok: false, message: e instanceof Error ? e.message : String(e) })
    } finally {
      setBusy(false)
    }
  }
  const origin = window.location.origin

  return (
    <div className="mqm-media">
      <section className="mqm-place">
        <h3>{t('Home Assistant')}</h3>
        <p className="hint">{t('Through an MQTT broker (Home Assistant’s Mosquitto add-on, or your own). Each screen then appears in Home Assistant by itself, with buttons, switches, its look, what it shows and the poster, for your dashboards and automations.')}</p>
        <label className="cx-check">
          <input type="checkbox" checked={m.on} onChange={(e) => change({ on: e.target.checked })} /> {t('Connect to Home Assistant')}
        </label>
        {m.on && (
          <div className="grid-form mqm-form">
            <label>
              {t('Broker address')}
              <input value={m.host} onChange={(e) => change({ host: e.target.value })} placeholder="192.168.1.20" />
            </label>
            <label>
              {t('Port')}
              <input type="number" min={1} max={65535} value={m.port} onChange={(e) => change({ port: Number(e.target.value) || 1883 })} />
            </label>
            <label className="cx-check">
              <input type="checkbox" checked={m.tls} onChange={(e) => change({ tls: e.target.checked, port: e.target.checked && m.port === 1883 ? 8883 : !e.target.checked && m.port === 8883 ? 1883 : m.port })} /> {t('Encrypted (TLS)')}
            </label>
            <label>
              {t('User name')}
              <input value={m.username} autoComplete="off" onChange={(e) => change({ username: e.target.value })} />
            </label>
            <label>
              {t('Password')}
              <input type="password" autoComplete="new-password" value={password ?? ''} placeholder={a.passwordSet ? t('(saved, unchanged)') : ''} onChange={(e) => setPassword(e.target.value)} />
            </label>
            <details>
              <summary>{t('Topics')}</summary>
              <label>
                {t('Usherr’s topics start with')}
                <input value={m.prefix} onChange={(e) => change({ prefix: e.target.value })} />
              </label>
              <label>
                {t('Home Assistant’s discovery prefix')}
                <input value={m.discovery} onChange={(e) => change({ discovery: e.target.value })} />
              </label>
            </details>
          </div>
        )}
        <div className="mqm-add">
          <button className="primary btn-sm" disabled={busy} onClick={() => void save()}>
            {t('Save')}
          </button>
          {m.on && (
            <button className="btn-sm" disabled={busy || !m.host.trim()} onClick={() => void test()}>
              {t('Test the connection')}
            </button>
          )}
          <span className={`mqm-state s-${a.status.state}`}>{STATE_TEXT[a.status.state]}</span>
        </div>
        {tested && <p className={tested.ok ? 'hint' : 'notice notice-warn'}>{tested.message}</p>}
        {a.status.state === 'error' && a.status.error && <p className="notice notice-warn">{a.status.error}</p>}
      </section>

      <section className="mqm-place">
        <h3>{t('From scripts and buttons (the API)')}</h3>
        <p className="hint">{t('Anything that can call a web address can steer the screens: a Stream Deck, iOS Shortcuts, Tasker, a script. Make an API key in Settings › Security (as an administrator) and send it as X-Api-Key. A screen is named by its name (“tablet”), its id, or “all”.')}</p>
        <pre className="mqm-code">
          {`curl -X POST ${origin}/api/home/marquee/screens/tablet/command \\
  -H "X-Api-Key: YOUR_KEY" -H "Content-Type: application/json" \\
  -d '{"cmd":"next"}'

# Buttons: next, prev, preshow, reload, scene (arg: a scene),
# pause / pin / dim / blank / info (arg: on or off)

curl -X POST ${origin}/api/home/marquee/screens/all/look \\
  -H "X-Api-Key: YOUR_KEY" -H "Content-Type: application/json" \\
  -d '{"style":"cinema"}'

# Looks: usherr, cinema, onesheet, posterr, wall, screensaver, board
# What each screen shows: GET /api/home/marquee/screens
# The poster on a screen now: GET /api/home/marquee/screens/tablet/poster`}
        </pre>
      </section>
    </div>
  )
}
