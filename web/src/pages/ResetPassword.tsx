// Usherr: the page a reset email's link opens (/reset/<link>): a new
// password, twice, with the same rules as everywhere. The link is checked
// first, so an expired one says so before anything is typed. Setting the
// password signs the account out everywhere; two-step sign-in still asks for
// its code at the next sign-in.
import { BRAND } from '../brand'
import { useEffect, useState, type FormEvent } from 'react'
import { api } from '../api'
import BrandMark from '../components/BrandMark'
import AmbientBackdrop from '../components/AmbientBackdrop'
import Icon from '../components/Icon'
import { titleFor } from '../documentTitle'
import { password as passwordRule, passwordsMatch } from '../validate'
import { t } from '../i18n'
import { withBase } from '../base'

function PasswordBox({ value, onChange, label, autoFocus }: { value: string; onChange: (v: string) => void; label: string; autoFocus?: boolean }) {
  const [shown, setShown] = useState(false)
  return (
    <label>
      {label}
      <span className="pw-field">
        <input type={shown ? 'text' : 'password'} value={value} onChange={(e) => onChange(e.target.value)} autoComplete="new-password" autoFocus={autoFocus} />
        <button type="button" className="icon-btn" onClick={() => setShown((s) => !s)} aria-label={shown ? t('Hide password') : t('Show password')} title={shown ? t('Hide password') : t('Show password')} aria-pressed={shown}>
          <Icon name="eye" size={16} />
        </button>
      </span>
    </label>
  )
}

export default function ResetPassword({ token }: { token: string }) {
  const [who, setWho] = useState<string | null>(null)
  const [bad, setBad] = useState('')
  const [pw, setPw] = useState('')
  const [again, setAgain] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [done, setDone] = useState('')
  useEffect(() => {
    document.title = titleFor(t('Set a new password'))
    api
      .checkResetLink(token)
      .then((r) => setWho(r.username))
      .catch((e: Error) => setBad(e.message))
  }, [token])

  async function submit(e: FormEvent) {
    e.preventDefault()
    const problem = (pw ? passwordRule(pw) : t('Choose a new password.')) || passwordsMatch(pw, again) || (again ? null : t('Type the new password again.'))
    if (problem) {
      setError(problem)
      return
    }
    setError('')
    setBusy(true)
    try {
      const r = await api.resetPassword(token, pw)
      setDone(r.message)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  const toSignIn = () => window.location.replace(withBase('/'))
  return (
    <main className="auth-page">
      <AmbientBackdrop level="full" />
      <form className="auth-card" onSubmit={submit}>
        <h1 className="sr-only">{t('Set a new password')}</h1>
        <div className="auth-logo">
          <BrandMark className="auth-mark" />
          <span>{BRAND}</span>
        </div>
        {done ? (
          <>
            <p className="auth-done" role="status">
              {done}
            </p>
            <button type="button" className="primary" onClick={toSignIn}>
              {t('Go to sign in')}
            </button>
          </>
        ) : bad ? (
          <>
            <p className="error-text">{bad}</p>
            <button type="button" className="primary" onClick={toSignIn}>
              {t('Go to sign in')}
            </button>
          </>
        ) : who === null ? (
          <p className="auth-sub">{t('Checking the link…')}</p>
        ) : (
          <>
            <p className="auth-sub">{t('Set a new password for {name}', { name: who })}</p>
            {error && <p className="error-text">{error}</p>}
            <PasswordBox label={t('New password')} value={pw} onChange={setPw} autoFocus />
            <PasswordBox label={t('New password again')} value={again} onChange={setAgain} />
            <p className="hint">{t('8 to 72 characters. Setting it signs this account out everywhere; two-step sign-in, if it’s on, still asks for its code.')}</p>
            <button className="primary" type="submit" disabled={busy}>
              {busy ? t('Saving…') : t('Set the password')}
            </button>
          </>
        )}
      </form>
    </main>
  )
}
