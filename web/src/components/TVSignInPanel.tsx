// Usherr: Sign in with your phone, on the sign-in page (internal/api/home_tv_signin.go).
// The TV asks for a code, shows it with a QR code, and checks every couple of
// seconds whether a signed-in phone approved it. The pairing secret stays in
// this page's memory; it is never shown and never put in the QR code.
import { useCallback, useEffect, useRef, useState } from 'react'
import { api, type TVStart } from '../api'
import { t } from '../i18n'
import { clockText, formatTVCode, secondsLeft } from '../tvSignIn'
import { QRCode } from './TwoStepCard'

type State = 'loading' | 'waiting' | 'denied' | 'expired' | 'error'

export function TVSignInPanel({ onSignedIn, onBack }: { onSignedIn: () => Promise<void> | void; onBack: () => void }) {
  const [start, setStart] = useState<TVStart | null>(null)
  const [state, setState] = useState<State>('loading')
  const [error, setError] = useState('')
  const [now, setNow] = useState(() => Date.now())
  const secret = useRef('')
  const alive = useRef(true)

  const newCode = useCallback(async () => {
    setState('loading')
    setError('')
    try {
      const s = await api.tvStart(secret.current || undefined)
      if (!alive.current) return
      secret.current = s.secret
      setStart(s)
      setState('waiting')
    } catch (e) {
      if (!alive.current) return
      setError(e instanceof Error ? e.message : String(e))
      setState('error')
    }
  }, [])

  useEffect(() => {
    alive.current = true
    void newCode()
    return () => {
      alive.current = false
    }
  }, [newCode])

  // Ask whether the phone answered, every few seconds while waiting.
  useEffect(() => {
    if (state !== 'waiting' || !start) return
    let stop = false
    const timer = setInterval(() => {
      api
        .tvPoll(start.secret)
        .then((r) => {
          if (stop || !alive.current) return
          if (r.status === 'approved') {
            stop = true
            secret.current = ''
            void onSignedIn()
          } else if (r.status === 'denied' || r.status === 'expired') {
            secret.current = ''
            setState(r.status)
          }
        })
        .catch(() => undefined) // a missed check is tried again next time
    }, Math.max(1, start.interval) * 1000)
    return () => {
      stop = true
      clearInterval(timer)
    }
  }, [state, start, onSignedIn])

  useEffect(() => {
    if (state !== 'waiting') return
    const tick = setInterval(() => setNow(Date.now()), 1000)
    return () => clearInterval(tick)
  }, [state])

  const left = start ? secondsLeft(start.expiresAt, now) : 0
  useEffect(() => {
    if (state === 'waiting' && start && left === 0) setState('expired')
  }, [state, start, left])

  // Where to type the code by hand: the link without the code in it.
  const typeAt = start ? start.url.replace(/\?.*$/, '') : ''

  return (
    <div className="auth-card tv-signin" aria-live="polite">
      <h1 className="auth-sub" style={{ margin: 0 }}>
        {t('Sign in with your phone')}
      </h1>
      {state === 'loading' && <p className="hint">{t('Getting a code…')}</p>}
      {state === 'error' && <p className="error-text">{error}</p>}
      {state === 'waiting' && start && (
        <>
          <p style={{ margin: 0 }}>{t('Scan this with your phone’s camera, then approve the TV there. Your phone needs to be signed in to Usherr.')}</p>
          <div style={{ display: 'flex', justifyContent: 'center' }}>
            <QRCode rows={start.qr} label={t('QR code that opens Usherr on your phone to approve this TV')} />
          </div>
          <p style={{ margin: 0 }}>{t('Or open {address} on your phone and type this code:', { address: typeAt })}</p>
          <p className="tv-signin-code" style={{ margin: 0, textAlign: 'center', fontSize: '2.2rem', fontWeight: 700, letterSpacing: '0.12em', fontVariantNumeric: 'tabular-nums' }}>
            {formatTVCode(start.code)}
          </p>
          <p className="hint" style={{ margin: 0, textAlign: 'center' }}>
            {t('This code works for {time}.', { time: clockText(left) })}
          </p>
        </>
      )}
      {state === 'denied' && <p className="error-text">{t('The phone said no, so this TV wasn’t signed in.')}</p>}
      {state === 'expired' && <p className="error-text">{t('That code ran out. Get a new one to try again.')}</p>}
      {(state === 'denied' || state === 'expired' || state === 'error') && (
        <button type="button" className="primary" onClick={() => void newCode()} autoFocus>
          {t('Get a new code')}
        </button>
      )}
      <button type="button" className="link-btn" onClick={onBack} autoFocus={state === 'waiting'}>
        {t('Sign in another way')}
      </button>
    </div>
  )
}
