import { useEffect, useRef, useState } from 'react'
import { api, type SubsonicStatus } from '../api'
import { withBase } from '../base'
import { useConfirm } from './ConfirmProvider'
import { useModules } from '../ModulesContext'
import { t } from '../i18n'
import Icon from './Icon'
import Switch from './Switch'
import { useToast } from './Toast'
import '../musicpanel.css'

// Usherr: your profile: play Usherr's music in a phone app that speaks
// Subsonic (Symfonium, DSub, Substreamer, play:Sub, Feishin…). The app signs
// in with your app password for reading and music apps (the same one e-reader
// apps use), never your own password.
export default function SubsonicCard() {
  const { on } = useModules()
  const toast = useToast()
  const confirm = useConfirm()
  const [st, setSt] = useState<SubsonicStatus | null>(null)
  const [made, setMade] = useState('')
  const [busy, setBusy] = useState(false)
  const box = useRef<HTMLFieldSetElement>(null)
  const musicOn = on('music')
  const address = window.location.origin + withBase('/')

  useEffect(() => {
    if (!musicOn) return
    let live = true
    api
      .subsonic()
      .then((s) => live && setSt(s))
      .catch(() => undefined)
    return () => {
      live = false
    }
  }, [musicOn])

  useEffect(() => {
    if (st && window.location.hash === '#phone-apps') box.current?.scrollIntoView({ block: 'start' })
  }, [st])

  async function run(work: () => Promise<void>) {
    setBusy(true)
    try {
      await work()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  function copy(text: string) {
    void navigator.clipboard?.writeText(text).then(() => toast.success(t('Copied.')))
  }

  async function makePassword() {
    if (st?.hasPassword) {
      const ok = await confirm({
        title: t('Make a new app password?'),
        body: t('The old one stops working in every reading and music app that uses it. Those apps need the new one.'),
        confirmLabel: t('Make a new one'),
      })
      if (!ok) return
    }
    await run(async () => {
      const r = await api.newAppPassword()
      setMade(r.password)
      setSt(r.status)
    })
  }

  if (!musicOn || !st) return null
  return (
    <fieldset className="group" id="phone-apps" data-walk="phone-apps" ref={box}>
      <legend>
        <Icon name="headphones" size={16} /> {t('Phone apps')}
      </legend>
      {!st.serverOn ? (
        <p style={{ marginTop: 0 }}>{t('An administrator has switched phone apps off on this server.')}</p>
      ) : (
        <>
          <p style={{ marginTop: 0 }}>{t('Play your music in a Subsonic app such as Symfonium, DSub, Substreamer, play:Sub or Feishin. The app uses your app password, never your own password.')}</p>
          <Switch
            checked={st.enabled}
            disabled={busy}
            onChange={(v) => void run(async () => setSt(await api.saveSubsonic(v)))}
            label={t('Let phone apps sign in')}
            description={t('Off, music apps can’t sign in, whatever password they have.')}
          />
          {st.enabled && (
            <>
              <dl className="ss-details">
                <dt>{t('Server')}</dt>
                <dd>
                  <code>{address}</code>{' '}
                  <button type="button" className="btn-sm" onClick={() => copy(address)}>
                    {t('Copy')}
                  </button>
                </dd>
                <dt>{t('User name')}</dt>
                <dd>
                  <code>{st.username}</code>
                </dd>
                <dt>{t('Password')}</dt>
                <dd>{st.hasPassword ? t('Your app password for reading and music apps') : t('Make an app password below')}</dd>
              </dl>
              {made && (
                <div className="ss-made" role="status">
                  <p>
                    <strong>{t('Your new app password')}</strong> — {t('copy it into the app now. It isn’t shown again.')}
                  </p>
                  <code className="ss-secret">{made}</code>{' '}
                  <button type="button" className="btn-sm" onClick={() => copy(made)}>
                    {t('Copy')}
                  </button>
                </div>
              )}
              {st.hasPassword && !st.tokenReady && !made && (
                <p className="hint">{t('Your app password is older than phone-app support. Some apps can’t sign in with it: make a new one.')}</p>
              )}
              <div className="form-actions" style={{ justifyContent: 'flex-start' }}>
                <button type="button" className="primary btn-with-icon" disabled={busy} onClick={() => void makePassword()}>
                  <Icon name="key" size={15} /> {st.hasPassword ? t('Make a new app password') : t('Make an app password')}
                </button>
                {st.hasPassword && (
                  <button
                    type="button"
                    className="btn-with-icon btn-danger"
                    disabled={busy}
                    onClick={() =>
                      void run(async () => {
                        await api.removeAppPassword()
                        setMade('')
                        setSt(await api.subsonic())
                      })
                    }
                  >
                    <Icon name="x" size={15} /> {t('Remove')}
                  </button>
                )}
              </div>
              <details>
                <summary>{t('Advanced')}</summary>
                <p className="hint">
                  {t('It is the same app password that e-readers use: making a new one or removing it changes both. It also works as an OpenSubsonic API key. Apps on mobile data can ask for smaller files.')}
                </p>
              </details>
            </>
          )}
        </>
      )}
    </fieldset>
  )
}
