import { useEffect, useRef, useState } from 'react'
import { api, type SimklStatus } from '../api'
import { t, tx } from '../i18n'
import { pollPin } from '../pinPoll'
import { useConfirm } from './ConfirmProvider'
import { useToast } from './Toast'

// Settings > Info, lists and subtitles > Import lists: connect the Simkl
// account whose lists a Simkl import list reads. The person enters a short
// code at simkl.com/pin; the sign-in itself is kept on the server and never
// sent to the browser.
export default function SimklConnect({ hasClientId, onChanged }: { hasClientId: boolean; onChanged?: () => void }) {
  const toast = useToast()
  const confirm = useConfirm()
  const [st, setSt] = useState<SimklStatus | null>(null)
  const [pin, setPin] = useState<{ code: string; url: string } | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const stopPoll = useRef<() => void>(() => undefined)

  const gone = useRef(false)

  useEffect(() => {
    api.simklStatus().then(setSt).catch(() => setSt({ connected: false, unreadable: false, hasClientId }))
  }, [hasClientId])

  // The code is only waited for while this card is on screen. (Not tied to
  // hasClientId: changing that must not stop the wait and leave the code up.)
  useEffect(() => {
    gone.current = false
    return () => {
      gone.current = true
      stopPoll.current()
    }
  }, [])

  async function connect() {
    setError('')
    setBusy(true)
    try {
      const p = await api.simklStartPin()
      if (gone.current) return
      setPin({ code: p.code, url: p.url })
      stopPoll.current()
      stopPoll.current = pollPin({
        check: async () => {
          const r = await api.simklPinStatus(p.code)
          return r.done && r.status ? r.status : undefined
        },
        lifeSeconds: p.expiresIn,
        everyMs: Math.max(3, p.interval || 5) * 1000,
        onDone: (status) => {
          setPin(null)
          setSt(status)
          toast.success(t('Simkl account connected.'))
          onChanged?.()
        },
        onExpired: () => {
          setPin(null)
          setError(t('The code expired. Start again.'))
        },
        onError: (e) => {
          setPin(null)
          setError(e instanceof Error ? e.message : String(e))
        },
      })
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  function cancel() {
    stopPoll.current()
    setPin(null)
  }

  async function disconnect() {
    const ok = await confirm({
      title: t('Disconnect your Simkl account?'),
      body: <p>{t('Your Simkl lists in Mediarium stop being read until you connect again. Titles they already added stay in your library.')}</p>,
      confirmLabel: t('Disconnect'),
      danger: true,
    })
    if (!ok) return
    setBusy(true)
    setError('')
    try {
      await api.simklDisconnect()
      setSt({ connected: false, unreadable: false, hasClientId })
      toast.success(t('Simkl account disconnected.'))
      onChanged?.()
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  if (!st) return null
  return (
    <div className="simkl-connect" style={{ marginTop: 14 }}>
      <h3 style={{ margin: '0 0 6px' }}>{t('Your Simkl account')}</h3>
      {st.connected ? (
        <>
          <p style={{ margin: '0 0 10px' }}>{t('Your Simkl account is connected.')}</p>
          <button disabled={busy} onClick={() => void disconnect()}>
            {t('Disconnect')}
          </button>
        </>
      ) : pin ? (
        <>
          <p style={{ margin: '0 0 8px' }}>
            {tx('Open {link} and enter this code:', {
              link: (
                <a href={pin.url} target="_blank" rel="noreferrer">
                  {pin.url.replace(/^https:\/\//, '')}
                </a>
              ),
            })}
          </p>
          <p aria-live="polite" style={{ margin: '0 0 10px', fontSize: '1.8rem', fontWeight: 700, letterSpacing: '0.2em' }}>
            <code>{pin.code}</code>
          </p>
          <p style={{ color: 'var(--text-dim)', margin: '0 0 10px' }}>{t('Waiting for you to enter the code…')}</p>
          <button onClick={cancel}>{t('Cancel')}</button>
        </>
      ) : (
        <>
          {st.unreadable && <div className="notice notice-warn">{t("The saved Simkl sign-in can't be read. Connect your account again.")}</div>}
          <p style={{ margin: '0 0 10px', color: 'var(--text-dim)' }}>
            {hasClientId ? t('Not connected yet. Connect your account so Mediarium can read your Simkl lists.') : t('Add your Simkl client ID above first.')}
          </p>
          <button className="primary" disabled={busy || !hasClientId} onClick={() => void connect()}>
            {t('Connect my Simkl account')}
          </button>
        </>
      )}
      {error && <p className="error-text">{error}</p>}
    </div>
  )
}
