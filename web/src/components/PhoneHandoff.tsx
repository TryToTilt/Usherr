// Usherr: Finish on your phone. Typing with a TV remote is slow, so a page with
// text to write (a review, a chat, a setting) can hand itself to the person's
// phone: a sheet shows a QR code (and a short code) that opens the same page on
// a phone signed in as the same person. The link works once, for ten minutes,
// and carries only the page's path, nothing secret (internal/api/home_tv_signin.go).
//
//   <PhoneHandoff path="/you/diary" what={t('your diary')} onClose={...} onDone={reload} />
//
// or, from any page, with the current page as the default:
//
//   const phone = usePhoneHandoff({ onDone: reload })
//   {phone.offer && <button onClick={() => phone.open()}>{t('Finish on your phone')}</button>}
//   {phone.sheet}
import { createPortal } from 'react-dom'
import { useCallback, useEffect, useRef, useState, type ReactNode } from 'react'
import { useLocation } from 'react-router-dom'
import { api, type Handoff } from '../api'
import { t } from '../i18n'
import { useFocusTrap } from '../useFocusTrap'
import { clockText, formatTVCode, isAppPath, secondsLeft } from '../tvSignIn'
import Icon from './Icon'
import { QRCode } from './TwoStepCard'

// The event a page can listen for to reload its data once the person says
// they finished on their phone (besides passing onDone).
export const PHONE_DONE_EVENT = 'usherr:phone-done'

export interface PhoneHandoffProps {
  /** A path of this app, like "/title/603?tab=reviews". Never a full address. */
  path: string
  /** What is being finished, for the sheet's heading ("your review"). */
  what?: string
  onClose: () => void
  /** Called when the person presses Done: reload what the phone may have changed. */
  onDone?: () => void
}

export function PhoneHandoff({ path, what, onClose, onDone }: PhoneHandoffProps) {
  const box = useRef<HTMLDivElement>(null)
  useFocusTrap(box)
  const [link, setLink] = useState<Handoff | null>(null)
  const [error, setError] = useState('')
  const [opened, setOpened] = useState(false)
  const [now, setNow] = useState(() => Date.now())
  const [round, setRound] = useState(0) // a new link after one ran out

  useEffect(() => {
    let gone = false
    setLink(null)
    setOpened(false)
    setError('')
    if (!isAppPath(path)) {
      setError(t('Only a page of this app can be opened on your phone.'))
      return
    }
    api
      .handoff(path, what)
      .then((h) => !gone && setLink(h))
      .catch((e) => !gone && setError(e instanceof Error ? e.message : String(e)))
    return () => {
      gone = true
    }
  }, [path, what, round])

  // Says when the phone opened it.
  useEffect(() => {
    if (!link || opened) return
    const timer = setInterval(() => {
      api
        .handoffStatus(link.code)
        .then((s) => s.opened && setOpened(true))
        .catch(() => undefined)
    }, 3000)
    return () => clearInterval(timer)
  }, [link, opened])

  useEffect(() => {
    const tick = setInterval(() => setNow(Date.now()), 1000)
    return () => clearInterval(tick)
  }, [])

  const done = useCallback(() => {
    onDone?.()
    window.dispatchEvent(new CustomEvent(PHONE_DONE_EVENT, { detail: { path } }))
    onClose()
  }, [onDone, onClose, path])

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose])

  const left = link ? secondsLeft(link.expiresAt, now) : 0
  const ranOut = !!link && !opened && left === 0
  const title = what ? t('Finish {what} on your phone', { what }) : t('Finish on your phone')

  return createPortal(
    <div className="modal-backdrop" onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      <div ref={box} tabIndex={-1} className="modal phone-handoff" data-walk="phone-handoff" role="dialog" aria-modal="true" aria-label={title} style={{ maxWidth: 460 }}>
        <div className="modal-head">
          <h2 style={{ margin: 0, flex: 1 }}>{title}</h2>
          <button className="icon-btn" onClick={onClose} aria-label={t('Close')}>
            <Icon name="x" />
          </button>
        </div>
        <div className="modal-body" style={{ display: 'grid', gap: 12, justifyItems: 'center', textAlign: 'center' }} aria-live="polite">
          {error && <p className="error-text">{error}</p>}
          {!error && !link && <p className="hint">{t('Making a link…')}</p>}
          {link && !ranOut && !opened && (
            <>
              <p style={{ margin: 0 }}>{t('Scan this with your phone’s camera. It opens this page there, signed in as you.')}</p>
              <QRCode rows={link.qr} label={t('QR code that opens this page on your phone')} />
              <p style={{ margin: 0 }}>
                {t('Or open this on your phone:')} <code>{link.url.replace(/^https?:\/\//, '')}</code>
              </p>
              <p className="hint" style={{ margin: 0 }}>
                {t('Code {code} · works for {time}', { code: formatTVCode(link.code), time: clockText(left) })}
              </p>
            </>
          )}
          {opened && (
            <p className="auth-done" role="status">
              <Icon name="check" size={14} /> {t('Opened on your phone. Finish there, then press Done.')}
            </p>
          )}
          {ranOut && (
            <>
              <p className="error-text">{t('That link ran out.')}</p>
              <button onClick={() => setRound((n) => n + 1)}>{t('Make a new link')}</button>
            </>
          )}
        </div>
        <div className="modal-foot">
          <button className="primary" onClick={done} autoFocus>
            {t('Done')}
          </button>
        </div>
      </div>
    </div>,
    document.body,
  )
}

/** True on a TV-size screen (a TV's browser, or TV size picked under You). */
export function tvScreen(): boolean {
  return typeof document !== 'undefined' && document.documentElement.dataset.tv === '1'
}

/**
 * Offers Finish on your phone from any page. offer is true on TV-size screens
 * (where typing is hard); open() shows the sheet for path (the current page
 * when left out); put sheet somewhere in the page.
 */
export function usePhoneHandoff(opts: { onDone?: () => void; always?: boolean } = {}): {
  offer: boolean
  open: (path?: string, what?: string) => void
  sheet: ReactNode
} {
  const location = useLocation()
  const [target, setTarget] = useState<{ path: string; what?: string } | null>(null)
  const here = location.pathname + location.search
  const open = useCallback((path?: string, what?: string) => setTarget({ path: path ?? here, what }), [here])
  const sheet = target ? <PhoneHandoff path={target.path} what={target.what} onDone={opts.onDone} onClose={() => setTarget(null)} /> : null
  return { offer: !!opts.always || tvScreen(), open, sheet }
}

/** A ready-made button: shown only on TV-size screens unless always is set. */
export function FinishOnPhoneButton({ path, what, onDone, always, className }: { path?: string; what?: string; onDone?: () => void; always?: boolean; className?: string }) {
  const phone = usePhoneHandoff({ onDone, always })
  if (!phone.offer) return null
  return (
    <>
      <button type="button" className={className} data-walk="phone-finish" onClick={() => phone.open(path, what)}>{/* Usherr Broadcast: data-walk for Show me how */}
        <Icon name="share" size={14} /> {t('Finish on your phone')}
      </button>
      {phone.sheet}
    </>
  )
}
