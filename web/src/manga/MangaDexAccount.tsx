// Usherr: Settings > Manga > MangaDex account. Each person connects their own,
// and only they see it. The sign-in is saved encrypted and never shown again.
import { useCallback, useEffect, useState, type FormEvent } from 'react'
import { Link } from 'react-router-dom'
import { useConfirm } from '../components/ConfirmProvider'
import { useToast } from '../components/Toast'
import { plural, t } from '../i18n'
import { useModules } from '../ModulesContext'
import { mangaDexAccountApi, type MangaDexAccount as Account } from './mangaDexAccountApi'
import './mangaDex.css'
import './manga.css'

function when(at?: string): string {
  if (!at) return ''
  const d = new Date(at)
  return Number.isNaN(d.getTime()) ? '' : d.toLocaleString(undefined, { dateStyle: 'medium', timeStyle: 'short' })
}

const EMPTY = { clientId: '', clientSecret: '', username: '', password: '' }

export function MangaDexAccountCard() {
  const toast = useToast()
  const confirm = useConfirm()
  const [acc, setAcc] = useState<Account | null>(null)
  const [form, setForm] = useState(EMPTY)
  const [editing, setEditing] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  const load = useCallback(() => {
    mangaDexAccountApi
      .account()
      .then((a) => {
        setAcc(a)
        setError('')
      })
      .catch((e: Error) => setError(e.message))
  }, [])
  useEffect(load, [load])

  const save = async (e: FormEvent) => {
    e.preventDefault()
    setBusy(true)
    try {
      const a = await mangaDexAccountApi.connect(form)
      setAcc(a)
      setForm(EMPTY)
      setEditing(false)
      toast.success(t('MangaDex is connected. The first sync has started.'))
    } catch (err) {
      setForm({ ...form, password: '' })
      toast.error(err instanceof Error ? err.message : String(err))
      load()
    } finally {
      setBusy(false)
    }
  }

  const disconnect = async () => {
    const ok = await confirm({
      title: t('Disconnect MangaDex?'),
      body: t('Usherr deletes your saved sign-in and everything it kept from MangaDex. Nothing changes on MangaDex, and your follows and reading status in Usherr stay.'),
      confirmLabel: t('Disconnect'),
      danger: true,
    })
    if (!ok) return
    setBusy(true)
    try {
      await mangaDexAccountApi.disconnect()
      toast.success(t('MangaDex is disconnected.'))
      load()
    } catch (err) {
      toast.error(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  const syncNow = async () => {
    setBusy(true)
    try {
      const r = await mangaDexAccountApi.syncNow()
      toast.success(r.message)
      window.setTimeout(load, 8000)
    } catch (err) {
      toast.error(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  const field = (key: keyof typeof EMPTY, label: string, type = 'text', auto = 'off') => (
    <label className="mg-mdx-field">
      <span>{label}</span>
      <input type={type} value={form[key]} autoComplete={auto} spellCheck={false} required onChange={(e) => setForm({ ...form, [key]: e.target.value })} />
    </label>
  )

  const refused = acc?.state === 'refused'
  const showForm = !!acc && (!acc.connected || editing || refused)
  const s = acc?.stats

  return (
    <section className="mg-card mg-stack" data-walk="manga-mangadex-account">
      <div>
        <h2>{t('MangaDex account')}</h2>
        <p className="mg-hint">{t('Connect your own MangaDex account, and your follows, read chapters, reading status and lists stay the same here and on MangaDex. It’s just for you: everyone in the house connects their own.')}</p>
      </div>
      {error && <p className="error-text">{error}</p>}
      {!acc && !error && <div className="skeleton" style={{ height: 120 }} />}
      {acc?.connected && (
        <div className="mg-well mg-stack" style={{ gap: 6 }}>
          {refused ? <p className="mg-mdx-bad">{acc.problem || t('MangaDex didn’t accept the sign-in.')}</p> : <p>{t('Connected. Your sign-in is saved.')}</p>}
          {!refused && acc.problem && <p className="mg-hint">{acc.problem}</p>}
          {acc.away && <p className="mg-hint">{t('MangaDex asked Usherr to wait, so syncing waits too.')}</p>}
          {acc.lightAt && <p className="mg-hint">{t('Last synced {when}.', { when: when(acc.lightAt) })}</p>}
          {s && !refused && (
            <p className="mg-hint">
              {[plural(s.follows, '{n} series followed on MangaDex', '{n} series followed on MangaDex'), s.outside > 0 ? plural(s.outside, '{n} of them isn’t in Manga', '{n} of them aren’t in Manga') : '', s.waiting ? t('{n} more come in tonight', { n: s.waiting }) : ''].filter(Boolean).join(' · ')}
            </p>
          )}
          {s?.unfinished && <p className="mg-hint">{t('You follow a lot of series, so the rest come in tonight, between 2 and 9.')}</p>}
          <p className="mg-hint">{acc.chaptersOn ? t('Series you follow on MangaDex come into Manga, and follow their new chapters from MangaDex.') : t('Series you follow on MangaDex come into Manga, but nothing downloads for them: MangaDex chapters are switched off. An administrator can switch them on here in Settings > Manga.')}</p>
          <div className="mg-tools">
            {!refused && (
              <button className="btn-sm" disabled={busy} onClick={() => void syncNow()}>
                {t('Sync now')}
              </button>
            )}
            {!editing && (
              <button className="btn-sm" disabled={busy} onClick={() => setEditing(true)}>
                {t('Change the sign-in')}
              </button>
            )}
            <button className="btn-sm" disabled={busy} onClick={() => void disconnect()}>
              {t('Disconnect')}
            </button>
          </div>
        </div>
      )}
      {showForm && (
        <form className="mg-stack" onSubmit={(e) => void save(e)}>
          <details className="mg-well">
            <summary>{t('How to get a client id and secret')}</summary>
            <ol className="mg-mdx-steps">
              <li>{t('Sign in on mangadex.org.')}</li>
              <li>{t('Open Settings, then API Clients, and make a new personal client. Any name will do.')}</li>
              <li>{t('Wait until MangaDex approves it. Then copy its client id, and get its secret, into the boxes below.')}</li>
            </ol>
          </details>
          <div className="mg-mdx-grid">
            {field('clientId', t('Client id'))}
            {field('clientSecret', t('Client secret'), 'password', 'new-password')}
            {field('username', t('MangaDex username'), 'text', 'username')}
            {field('password', t('MangaDex password'), 'password', 'current-password')}
          </div>
          <p className="mg-hint">{t('A personal client signs in without two-factor login, even when your account uses it. Keep the secret as safe as your password.')}</p>
          <p className="mg-hint">{t('Usherr saves these encrypted, sends them only to MangaDex, and never shows them again.')}</p>
          <div className="mg-tools">
            <button className="btn-sm primary" type="submit" disabled={busy}>
              {busy ? t('Checking…') : t('Connect')}
            </button>
            {editing && (
              <button className="btn-sm" type="button" disabled={busy} onClick={() => setEditing(false)}>
                {t('Cancel')}
              </button>
            )}
          </div>
        </form>
      )}
    </section>
  )
}

// The page for every account, at Settings > MangaDex account.
export default function MangaDexAccountPage() {
  const { on } = useModules()
  if (!on('manga')) {
    return (
      <div className="mg-page">
        <p className="mg-hint">{t('Manga is switched off.')}</p>
      </div>
    )
  }
  return (
    <div className="mg-page mg-stack">
      <MangaDexAccountCard />
      <p className="mg-hint">
        <Link to="/manga">{t('Back to Manga')}</Link>
      </p>
    </div>
  )
}
