// Usherr: the pre-show in the Jellyfin app, on the Pre-show page. The NeXroll
// Intros plugin, pointed at Usherr, plays the same pre-show before a film in
// every Jellyfin app. Usherr sets it up step by step: the plugin's
// repository and the plugin, a restart of Jellyfin (only when nobody is
// watching), then the plugin's settings.
import { useCallback, useEffect, useState } from 'react'
import Icon from '../components/Icon'
import Switch from '../components/Switch'
import { useConfirm } from '../components/ConfirmProvider'
import { useToast } from '../components/Toast'
import { locale, t } from '../i18n'
import { preshowApi, type PreshowJF } from './preshowApi'

const pluginOn = (j: PreshowJF) => j.plugin === 'active'

// The next thing to do, and the button for it.
function nextStep(j: PreshowJF): { text: string; button?: 'setup' | 'restart' } {
  if (!j.connected) return { text: j.problem ?? t('Jellyfin isn’t reachable.') }
  if (!j.repo || j.plugin === '') return { text: t('Usherr adds the plugin’s repository to Jellyfin and installs NeXroll Intros.'), button: 'setup' }
  if (j.plugin === 'installing') return { text: t('Jellyfin is downloading the plugin…') }
  if (!pluginOn(j)) return { text: t('Jellyfin loads the plugin when it restarts.'), button: 'restart' }
  if (!j.configured) return { text: t('Usherr points the plugin at itself, with its key.'), button: 'setup' }
  return { text: t('All set. The pre-show plays before films in the Jellyfin app.') }
}

export default function PreshowJellyfin({ themes }: { themes: { id: number; name: string }[] }) {
  const toast = useToast()
  const confirm = useConfirm()
  const [j, setJ] = useState<PreshowJF | null>(null)
  const [busy, setBusy] = useState(false)
  const [showKey, setShowKey] = useState(false)
  const load = useCallback(() => {
    preshowApi
      .jellyfin()
      .then(setJ)
      .catch((e: Error) => toast.error(e.message))
  }, [toast])
  useEffect(() => load(), [load])
  // While Jellyfin downloads the plugin or restarts, look again every few seconds.
  const waiting = j?.plugin === 'installing' || (j !== null && !j.connected && busy)
  useEffect(() => {
    if (!waiting) return
    const id = window.setInterval(load, 4000)
    return () => window.clearInterval(id)
  }, [waiting, load])
  if (!j) return null

  const run = async (what: () => Promise<PreshowJF | void>, done?: string) => {
    setBusy(true)
    try {
      const next = await what()
      if (next) setJ(next)
      else load()
      if (done) toast.success(done)
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }
  const save = (patch: Partial<Pick<PreshowJF, 'address' | 'movies' | 'episodes' | 'themeId'>>) =>
    void run(() => preshowApi.saveJellyfin({ address: j.address, movies: j.movies, episodes: j.episodes, themeId: j.themeId, ...patch }))
  const restart = async () => {
    if (!(await confirm({ title: t('Restart Jellyfin?'), body: t('Jellyfin is away for a minute while it restarts. Nobody is playing anything now.'), confirmLabel: t('Restart') }))) return
    setBusy(true)
    try {
      await preshowApi.jellyfinRestart()
      toast.success(t('Jellyfin is restarting.'))
      // Look again once it's back.
      for (let i = 0; i < 30; i++) {
        await new Promise((r) => window.setTimeout(r, 4000))
        const now = await preshowApi.jellyfin().catch(() => null)
        if (now?.connected && now.plugin !== 'installed') {
          setJ(now)
          break
        }
      }
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }
  const step = nextStep(j)
  const steps = [
    { done: j.repo, text: t('The plugin’s repository is added') },
    { done: j.plugin !== '' && j.plugin !== 'installing', text: j.pluginVersion ? t('NeXroll Intros {v} is installed', { v: j.pluginVersion }) : t('NeXroll Intros is installed') },
    { done: pluginOn(j), text: t('Jellyfin has loaded it (a restart)') },
    { done: j.configured, text: t('It plays Usherr’s pre-show') },
  ]
  const address = j.address || j.suggested
  const last = j.lastAsk
  return (
    <section className="card psm-jf">
      <h2>{t('In the Jellyfin app')}</h2>
      <p>{t('The same pre-show before films in the Jellyfin app, on every TV and phone: the cards and your clips. Jellyfin plays them through the NeXroll Intros plugin, which asks Usherr what to play. (Trailers from YouTube don’t play there yet.)')}</p>
      {j.server && <p className="hint">{j.server}</p>}
      <ol className="psm-jf-steps">
        {steps.map((s, i) => (
          <li key={i} className={s.done ? 'done' : ''}>
            <Icon name={s.done ? 'check' : 'clock'} size={14} /> {s.text}
          </li>
        ))}
      </ol>
      <div className="psm-actions">
        {step.button === 'setup' && (
          <button type="button" className="primary btn-with-icon" disabled={busy} onClick={() => void run(preshowApi.jellyfinSetup)} title={t('Adds the plugin’s repository, installs NeXroll Intros and gives it Usherr’s address and key')}>
            <Icon name="sliders" size={16} /> {j.plugin === '' ? t('Set it up') : t('Finish setting it up')}
          </button>
        )}
        {step.button === 'restart' && (
          <button type="button" className="primary btn-with-icon" disabled={busy || j.watching > 0} onClick={() => void restart()} title={t('Jellyfin loads the plugin when it restarts; only while nobody is playing')}>
            <Icon name="refresh" size={16} /> {t('Restart Jellyfin')}
          </button>
        )}
        <span className="hint">
          {step.button === 'restart' && j.watching > 0 ? t('{n} playing on Jellyfin now: restart it when nobody is watching.', { n: j.watching }) : step.text}
        </span>
      </div>

      <div className="psm-jf-opts">
        <Switch checked={j.movies} onChange={(on) => save({ movies: on })} label={t('Before films')} />
        <Switch checked={j.episodes} onChange={(on) => save({ episodes: on })} label={t('Before episodes')} description={t('Every episode, so best with a short theme.')} />
        <label className="psm-field">
          <span>{t('Theme')}</span>
          <select value={j.themeId} onChange={(e) => save({ themeId: Number(e.target.value) })} title={t('The theme played in the Jellyfin app')}>
            <option value={0}>{t('As everywhere (schedules, then the default)')}</option>
            {themes.map((th) => (
              <option key={th.id} value={th.id}>
                {th.name}
              </option>
            ))}
          </select>
        </label>
      </div>
      {last && (
        <p className="hint">
          {t('Jellyfin last asked at {time}', { time: new Date(last.atMs).toLocaleTimeString(locale(), { hour: 'numeric', minute: '2-digit' }) })}
          {': '}
          {last.note ?? (last.title ? t('{title}, {n} pieces', { title: last.title, n: last.steps }) : t('{n} pieces', { n: last.steps }))}
        </p>
      )}
      <p className="hint">{t('Each Jellyfin app plays intros when Cinema mode is on (its Settings, Playback). It is on unless someone turned it off.')}</p>

      <details className="psm-jf-more">
        <summary>{t('Address, key, and setting it up by hand')}</summary>
        <label className="psm-field">
          <span>{t('Usherr, as Jellyfin reaches it')}</span>
          <input type="url" defaultValue={j.address} placeholder={j.suggested} title={t('The address Jellyfin uses to ask Usherr for the pre-show')} onBlur={(e) => e.target.value.trim() !== j.address && save({ address: e.target.value.trim() })} />
          <small className="hint">{t('Jellyfin runs on this computer, so the address you use here usually works. Leave it empty for {addr}.', { addr: j.suggested })}</small>
        </label>
        <div className="psm-field">
          <span>{t('Key')}</span>
          <div className="psm-actions">
            <code className="psm-key">{showKey ? j.key : '•'.repeat(16)}</code>
            <button type="button" className="btn-sm" onClick={() => setShowKey((v) => !v)}>
              {showKey ? t('Hide') : t('Show')}
            </button>
            <button type="button" className="btn-sm" title={t('For the plugin’s API key, when setting it up by hand')} onClick={() => void navigator.clipboard?.writeText(j.key).then(() => toast.success(t('Copied.')))}>
              {t('Copy')}
            </button>
            <button
              type="button"
              className="btn-sm"
              disabled={busy}
              title={t('The old key stops working')}
              onClick={async () => {
                if (await confirm({ title: t('Make a new key?'), body: t('The old one stops working. Usherr gives the plugin the new one when it can.'), confirmLabel: t('New key') })) void run(preshowApi.jellyfinKey, t('A new key.'))
              }}
            >
              {t('New key')}
            </button>
          </div>
        </div>
        <p className="hint">{t('By hand: in Jellyfin, Dashboard, Plugins, Repositories, add the NeXroll Intros repository, install NeXroll Intros from the Catalog and restart Jellyfin. In the plugin’s settings, put {addr} as the NeXroll server’s address and the key above as its API key.', { addr: address })}</p>
      </details>
    </section>
  )
}
