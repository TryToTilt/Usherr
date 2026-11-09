// Usherr: the Sign in on a TV card. Everyone gets the way to approve a TV's
// code from here; administrators also get the switch for the whole server
// (on unless switched off). Self-contained, so it can sit on any settings page.
import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { api, isAdmin } from '../api'
import { useAuth } from '../AuthContext'
import { DOCS_URL } from '../docs'
import { t } from '../i18n'
import Icon from './Icon'
import { useToast } from './Toast'

export function TVSignInCard() {
  const { user } = useAuth()
  const admin = isAdmin(user)
  const toast = useToast()
  const [enabled, setEnabled] = useState<boolean | null>(null)
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    // Members read whether it works from the sign-in page's list.
    const read = admin ? api.tvSignInSettings() : api.signInMethods().then((m) => ({ enabled: !!m.tvSignIn }))
    read.then((s) => setEnabled(s.enabled)).catch(() => setEnabled(null))
  }, [admin])

  async function save(on: boolean) {
    setBusy(true)
    try {
      setEnabled((await api.putTVSignInSettings(on)).enabled)
      toast.success(on ? t('Sign in with your phone is on.') : t('Sign in with your phone is off.'))
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  return (
    <fieldset className="group" data-walk="tv-signin-card">{/* Usherr Broadcast: data-walk for Show me how */}
      <legend>
        <Icon name="tv" size={14} /> {t('Sign in on a TV')}
      </legend>
      <p style={{ color: 'var(--text-dim)', marginTop: 0 }}>
        {t('No typing with the remote: on the TV, choose Sign in with your phone. It shows a code and a QR code. Scan it with your phone while signed in here and approve it, and the TV signs in as you. A code works once, for five minutes.')}{' '}
        <a href={`${DOCS_URL}/security.md#sign-in-on-a-tv`} target="_blank" rel="noreferrer">
          {t('How it stays safe')}
        </a>
      </p>
      {enabled === false && <p className="hint">{t('Off. The sign-in page doesn’t offer it.')}</p>}
      <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
        {enabled !== false && (
          <Link className="btn" to="/tv-code">
            {t('Type a TV’s code')}
          </Link>
        )}
        {admin && enabled !== null && (
          <button className={enabled ? undefined : 'primary'} disabled={busy} onClick={() => void save(!enabled)}>
            {enabled ? t('Turn off Sign in with your phone') : t('Turn on Sign in with your phone')}
          </button>
        )}
      </div>
      <p className="hint" style={{ marginBottom: 0 }}>
        {t('A TV signed in this way is listed under Signed-in devices, where you can sign it out.')}
      </p>
    </fieldset>
  )
}

export default TVSignInCard
