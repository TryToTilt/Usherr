// Usherr: let friends sign in with the Jellyfin or Plex account they already
// have (internal/api/media_signin.go). Off until switched on here.
import { useEffect, useState } from 'react'
import { api } from '../api'
import { t } from '../i18n'
import Switch from './Switch'
import { useToast } from './Toast'

type Cfg = { jellyfin: boolean; plex: boolean; role: 'member' | 'viewer' }

export default function MediaSignInCard() {
  const toast = useToast()
  const [cfg, setCfg] = useState<Cfg | null>(null)
  const [busy, setBusy] = useState(false)
  useEffect(() => {
    api.getMediaSignIn().then(setCfg).catch((e) => toast.error(e instanceof Error ? e.message : String(e)))
  }, [toast])
  if (!cfg) return null

  async function save(next: Cfg) {
    setBusy(true)
    try {
      setCfg(await api.putMediaSignIn(next))
      toast.success(t('Saved.'))
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  return (
    <section className="card">
      <h2>{t('Sign in with Jellyfin or Plex')}</h2>
      <p style={{ color: 'var(--text-dim)' }}>
        {t('Friends sign in with the account they already use for watching. Their first sign-in makes an account here. Names that have a password here keep signing in here.')}
      </p>
      <Switch
        checked={cfg.jellyfin}
        disabled={busy}
        onChange={(jellyfin) => void save({ ...cfg, jellyfin })}
        label={t('Sign in with Jellyfin')}
        description={t('The sign-in form checks the name and password with your Jellyfin server.')}
        showState
      />
      <Switch
        checked={cfg.plex}
        disabled={busy}
        onChange={(plex) => void save({ ...cfg, plex })}
        label={t('Sign in with Plex')}
        description={t('Adds a "Sign in with Plex" button. Only Plex accounts your Plex server is shared with get in.')}
        showState
      />
      <label style={{ maxWidth: 320 }}>
        {t('New accounts are')}
        <select value={cfg.role} disabled={busy} onChange={(e) => void save({ ...cfg, role: e.target.value as Cfg['role'] })}>
          <option value="member">{t('Members (can ask for titles)')}</option>
          <option value="viewer">{t('Viewers (can only watch)')}</option>
        </select>
      </label>
    </section>
  )
}
