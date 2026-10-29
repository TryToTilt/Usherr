// Usherr: the voice engines on the Read-to-Me tab: which run, which model
// Chatterbox answers with, and (with a runner) Start and Stop. Usherr starts
// Chatterbox by itself for a job and stops it when the queue is empty.
import { useEffect, useState } from 'react'
import { t } from '../i18n'
import { readToMeApi, type RtvEngines } from './readToMeApi'

const ENGINES: { name: string; label: string; note: string }[] = [
  { name: 'kokoro', label: 'Kokoro', note: t('Everyday voices, fast, about 1 GB of graphics memory while working') },
  { name: 'chatterbox', label: t('Chatterbox (standard)'), note: t('Expressive, the best sound, about 3.5 GB') },
  { name: 'chatterbox-turbo', label: t('Chatterbox Turbo'), note: t('About twice as fast as standard, a little flatter') },
]

const STATE_WORD: Record<string, string> = { running: t('Running'), stopped: t('Stopped'), missing: t('Not set up'), unknown: t('Unknown') }

export default function RtmEngines() {
  const [v, setV] = useState<RtvEngines | null>(null)
  const [busy, setBusy] = useState('')
  const [error, setError] = useState('')
  const load = () =>
    readToMeApi
      .engines()
      .then(setV)
      .catch((e: Error) => setError(e.message))
  useEffect(() => {
    void load()
  }, [])
  const act = (name: string, action: 'start' | 'stop') => {
    setBusy(name)
    setError('')
    readToMeApi
      .engineAction(name, action)
      .then(setV)
      .catch((e: Error) => setError(e.message))
      .finally(() => setBusy(''))
  }
  if (!v) return error ? <p className="error-text">{error}</p> : null
  if (!v.runner)
    return (
      <p className="hint">
        {v.model ? t('Chatterbox is running ({model}).', { model: v.model }) : t('Chatterbox is stopped.')}{' '}
        {t('With the runner set up (Settings > Extras > Read-to-Me), Usherr starts and stops Chatterbox by itself.')}
      </p>
    )
  return (
    <>
      {error && <p className="error-text">{error}</p>}
      {v.problem && <p className="error-text">{v.problem}</p>}
      <ul className="rtm-list">
        {ENGINES.map((e) => {
          const st = v.states?.[e.name] ?? 'unknown'
          const mine = v.started?.includes(e.name)
          return (
            <li key={e.name} className="rtm-job">
              <span className="rtm-row">
                <strong>{e.label}</strong>
                <small className={st === 'running' ? 'badge' : 'hint'}>{STATE_WORD[st] ?? st}</small>
                {mine && <small className="hint">{t('started by Usherr, stopped again when idle')}</small>}
                <span className="spacer" />
                {st === 'stopped' && (
                  <button className="btn-sm" disabled={!!busy} onClick={() => act(e.name, 'start')}>
                    {busy === e.name ? t('Starting…') : t('Start')}
                  </button>
                )}
                {st === 'running' && (
                  <button className="btn-sm" disabled={!!busy} onClick={() => act(e.name, 'stop')}>
                    {busy === e.name ? t('Stopping…') : t('Stop')}
                  </button>
                )}
              </span>
              <small className="hint">{e.note}</small>
            </li>
          )
        })}
      </ul>
      <p className="hint">
        {t('Only one Chatterbox runs at a time: starting one stops the other. Jobs use the {model} model unless they ask for the other one.', { model: v.default === 'turbo' ? 'Turbo' : t('standard') })}
      </p>
      <button className="btn-sm" onClick={() => void load()}>
        {t('Refresh')}
      </button>
    </>
  )
}
