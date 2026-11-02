// Usherr: Community, step 2. Your profile: link your Discord account. /link
// in Discord answers with a code only you see; type it here.
import { useEffect, useState } from 'react'
import Icon from '../components/Icon'
import { useToast } from '../components/Toast'
import { t } from '../i18n'

interface Link {
  name: string
  avatar?: string
  linkedAt: string
}
interface Me {
  on: boolean
  server: string
  link: Link | null
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

export default function DiscordLinkCard() {
  const toast = useToast()
  const [me, setMe] = useState<Me | null>(null)
  const [code, setCode] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  useEffect(() => {
    call<Me>('GET', '/api/home/community/me')
      .then(setMe)
      .catch(() => undefined)
  }, [])

  // Shown once Community is connected, or while you're still linked.
  if (!me || (!me.on && !me.link)) return null

  const link = async () => {
    setBusy(true)
    setError('')
    try {
      const d = await call<{ link: Link }>('POST', '/api/home/community/link', { code })
      setMe({ ...me, link: d.link })
      setCode('')
      toast.success(t('Your Discord account is linked.'))
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }
  const unlink = async () => {
    setBusy(true)
    try {
      await call('DELETE', '/api/home/community/link')
      setMe({ ...me, link: null })
      toast.success(t('Your Discord account is unlinked.'))
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  return (
    <fieldset className="group" id="discord" data-walk="discord-link">
      <legend>
        <Icon name="chat" size={16} /> Discord
      </legend>
      {me.link ? (
        <>
          <p className="cm-linked">
            {me.link.avatar ? <img src={me.link.avatar} alt="" /> : <span className="cm-noicon" aria-hidden="true">{me.link.name.slice(0, 1)}</span>}
            {t('Linked to {name}', { name: me.link.name })}
          </p>
          <p className="hint">{t('Your watch parties and the reviews you choose to share carry your Discord name. Nothing is posted unless you share it.')}</p>
          <button className="btn-sm" disabled={busy} onClick={() => void unlink()}>
            {t('Unlink')}
          </button>
        </>
      ) : (
        <form
          onSubmit={(e) => {
            e.preventDefault()
            void link()
          }}
        >
          <p>{me.server ? t('Link your Discord account, so Usherr knows who you are in {server}.', { server: me.server }) : t('Link your Discord account, so Usherr knows who you are in your Discord server.')}</p>
          <p className="hint">{t('In the server, type /link in any channel and press Enter. The bot answers with six numbers only you can see. Type them here within 10 minutes.')}</p>
          <div className="cm-row">
            <label>
              {t('Code from /link')}
              <input inputMode="numeric" autoComplete="one-time-code" maxLength={7} placeholder="123 456" value={code} onChange={(e) => setCode(e.target.value)} />
            </label>
            <button className="primary btn-sm" type="submit" disabled={busy || code.replace(/\D/g, '').length !== 6}>
              {t('Link')}
            </button>
          </div>
          {error && <p className="notice notice-warn">{error}</p>}
        </form>
      )}
    </fieldset>
  )
}
