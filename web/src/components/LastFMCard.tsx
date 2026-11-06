import { useEffect, useRef, useState } from 'react'
import { api, isAdmin, type LastFMStatus } from '../api'
import { useAuth } from '../AuthContext'
import { useModules } from '../ModulesContext'
import { plural, t, tx } from '../i18n'
import Icon from './Icon'
import { useMusicPlayer } from './musicPlayerContext'
import Switch from './Switch'
import { useToast } from './Toast'

// Usherr: your profile: send what you play to your own Last.fm account. An
// administrator adds the server's Last.fm API key once; each person then
// connects on last.fm.
export default function LastFMCard() {
  const { on } = useModules()
  const admin = isAdmin(useAuth().user)
  const player = useMusicPlayer()
  const toast = useToast()
  const [st, setSt] = useState<LastFMStatus | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [key, setKey] = useState('')
  const [secret, setSecret] = useState('')
  const box = useRef<HTMLFieldSetElement>(null)
  const musicOn = on('music')

  useEffect(() => {
    if (!musicOn) return
    let live = true
    api
      .lastFM()
      .then((s) => live && setSt(s))
      .catch(() => undefined)
    if (admin) {
      api
        .lastFMApp()
        .then((a) => live && setKey(a.apiKey))
        .catch(() => undefined)
    }
    return () => {
      live = false
    }
  }, [musicOn, admin])

  // Coming back from last.fm.
  useEffect(() => {
    if (!st) return
    const result = new URLSearchParams(window.location.search).get('lastfm')
    if (window.location.hash === '#lastfm' || result) box.current?.scrollIntoView({ block: 'start' })
    if (!result) return
    if (result === 'ok') toast.success(t('Last.fm connected.'))
    else if (result === 'expired') toast.error(t('That took too long. Try connecting again.'))
    else toast.error(t('Last.fm didn’t connect. Try again.'))
    const url = new URL(window.location.href)
    url.searchParams.delete('lastfm')
    window.history.replaceState(null, '', url.toString())
  }, [st, toast])

  async function run(work: () => Promise<void>) {
    setBusy(true)
    setError('')
    try {
      await work()
      player?.refreshScrobbling()
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  if (!musicOn || !st) return null
  if (!st.configured && !admin) return null
  return (
    <fieldset className="group" id="lastfm" ref={box}>
      <legend>
        <Icon name="headphones" size={16} /> {t('Last.fm')}
      </legend>
      {admin && (
        <details open={!st.configured} style={{ marginBottom: 12 }}>
          <summary>{st.configured ? t('Server key (set)') : t('Set up Last.fm for this server')}</summary>
          <p>
            {tx('Make an API account at {link} (any name works), then paste its API key and shared secret. Everyone on this server uses it to connect.', {
              link: (
                <a href="https://www.last.fm/api/account/create" target="_blank" rel="noreferrer">
                  last.fm/api/account/create
                </a>
              ),
            })}
          </p>
          <form
            className="lb-row"
            onSubmit={(e) => {
              e.preventDefault()
              void run(async () => {
                await api.saveLastFMApp({ apiKey: key, ...(secret ? { secret } : {}) })
                setSecret('')
                setSt(await api.lastFM())
                toast.success(t('Saved.'))
              })
            }}
          >
            <input type="text" value={key} onChange={(e) => setKey(e.target.value)} placeholder={t('API key')} aria-label={t('API key')} autoComplete="off" spellCheck={false} disabled={busy} />
            <input type="password" value={secret} onChange={(e) => setSecret(e.target.value)} placeholder={st.configured ? t('Shared secret (kept)') : t('Shared secret')} aria-label={t('Shared secret')} autoComplete="off" spellCheck={false} disabled={busy} />
            <button type="submit" className="btn-with-icon" disabled={busy || key.trim() === '' || (!st.configured && secret.trim() === '')}>
              <Icon name="check" size={15} /> {t('Save')}
            </button>
          </form>
        </details>
      )}
      {!st.configured ? null : !st.connected ? (
        <>
          <p style={{ marginTop: 0 }}>{t('Send what you play to your Last.fm account. You sign in on last.fm and come back here.')}</p>
          <button
            type="button"
            className="primary btn-with-icon"
            disabled={busy}
            onClick={() =>
              void run(async () => {
                const r = await api.connectLastFM()
                window.location.assign(r.url)
              })
            }
          >
            <Icon name="external" size={15} /> {t('Connect Last.fm')}
          </button>
        </>
      ) : (
        <>
          <p style={{ marginTop: 0 }}>{tx('Connected as {name}.', { name: <strong>{st.user || 'Last.fm'}</strong> })}</p>
          <Switch
            checked={st.enabled}
            disabled={busy}
            onChange={(v) => void run(async () => setSt(await api.saveLastFM({ enabled: v })))}
            label={t('Send my plays to Last.fm')}
            description={t('A track longer than 30 seconds counts after half of it, or four minutes. Plays made offline are sent later.')}
          />
          <Switch
            checked={st.loveSync}
            disabled={busy}
            onChange={(v) => void run(async () => setSt(await api.saveLastFM({ loveSync: v })))}
            label={t('Love what I like')}
            description={t('A heart on a track in Usherr loves it on Last.fm too.')}
          />
          <p style={{ color: 'var(--text-dim)', fontSize: '0.85rem', margin: '10px 0' }}>
            {plural(st.submitted, '{n} play sent so far.', '{n} plays sent so far.')} {st.pending > 0 && plural(st.pending, '{n} is waiting to be sent.', '{n} are waiting to be sent.')}
          </p>
          {st.lastError && <p className="error-text">{st.lastError}</p>}
          <div className="form-actions" style={{ justifyContent: 'flex-start' }}>
            <button
              className="btn-with-icon btn-danger"
              disabled={busy}
              onClick={() =>
                void run(async () => {
                  await api.forgetLastFM()
                  setSt(await api.lastFM())
                  toast.success(t('Last.fm disconnected.'))
                })
              }
            >
              <Icon name="x" size={15} /> {t('Disconnect')}
            </button>
          </div>
        </>
      )}
      {error && <p className="error-text">{error}</p>}
    </fieldset>
  )
}
