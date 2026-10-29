// Usherr: Read-to-Me's copied voices, on the Read-to-Me tab. A short recording
// of one person speaking becomes a Chatterbox voice: hear it, hear the
// recording it was copied from, delete it. Usherr keeps the recording and
// gives it to Chatterbox (standard or Turbo) the first time it reads with it.
import { useEffect, useRef, useState } from 'react'
import { useConfirm } from '../components/ConfirmProvider'
import Icon from '../components/Icon'
import { t } from '../i18n'
import { readToMeApi, type RtvCopiedVoice } from './readToMeApi'

export default function RtmVoiceLib({ chatterboxOn }: { chatterboxOn: boolean }) {
  const confirm = useConfirm()
  const audio = useRef<HTMLAudioElement>(null)
  const fileInput = useRef<HTMLInputElement>(null)
  const [voices, setVoices] = useState<RtvCopiedVoice[] | null>(null)
  const [error, setError] = useState('')
  const [name, setName] = useState('')
  const [file, setFile] = useState<File | null>(null)
  const [rights, setRights] = useState(false)
  const [copying, setCopying] = useState(false)
  const [hearing, setHearing] = useState('')
  const load = () =>
    readToMeApi
      .copiedVoices()
      .then((d) => setVoices(d.voices ?? []))
      .catch((e: Error) => setError(e.message))
  useEffect(() => {
    void load()
  }, [])
  const play = (key: string, src: string) => {
    const a = audio.current
    if (!a) return
    setError('')
    setHearing(key)
    const done = () => {
      setHearing('')
      a.removeEventListener('playing', done)
      a.removeEventListener('error', failed)
    }
    const failed = () => {
      done()
      setError(t("That couldn't be played. Chatterbox may be busy or stopped."))
    }
    a.addEventListener('playing', done)
    a.addEventListener('error', failed)
    a.src = src
    void a.play().catch(() => undefined)
  }
  const copy = () => {
    if (!file || !name.trim() || !rights) return
    setError('')
    setCopying(true)
    readToMeApi
      .copyVoice(name.trim(), file, rights)
      .then(() => {
        setName('')
        setFile(null)
        setRights(false)
        if (fileInput.current) fileInput.current.value = ''
        return load()
      })
      .catch((e: Error) => setError(e.message))
      .finally(() => setCopying(false))
  }
  const remove = async (v: RtvCopiedVoice) => {
    if (!(await confirm({ title: t('Delete the voice {name}?', { name: v.name }), body: <p>{t('Presets and casts that use it fall back to their other voices. Audiobooks already made keep it.')}</p>, confirmLabel: t('Delete'), danger: true }))) return
    readToMeApi
      .deleteCopiedVoice(v.name)
      .then(() => load())
      .catch((e: Error) => setError(e.message))
  }
  if (!chatterboxOn) return <p className="hint">{t('Copying a voice needs Chatterbox (Settings > Extras > Read-to-Me).')}</p>
  return (
    <>
      {error && <p className="error-text">{error}</p>}
      {voices && voices.length > 0 && (
        <ul className="rtm-list rtm-presets">
          {voices.map((v) => (
            <li key={v.name} className="rtm-job">
              <span className="rtm-row">
                <strong>{v.name}</strong>
                <small className="hint">{t('copied from {n} s of recording · {date}', { n: Math.round(v.seconds), date: new Date(v.createdMs).toLocaleDateString() })}</small>
                <span className="spacer" />
                <span className="rtm-made-actions">
                  <button className="btn-sm btn-with-icon" disabled={hearing === 'v:' + v.name} onClick={() => play('v:' + v.name, readToMeApi.sampleUrl({ engine: 'chatterbox', id: v.name }))}>
                    <Icon name={hearing === 'v:' + v.name ? 'refresh' : 'play'} size={14} /> {t('Hear it')}
                  </button>
                  <button className="btn-sm btn-with-icon" onClick={() => play('r:' + v.name, readToMeApi.recordingUrl(v.name))}>
                    <Icon name="play" size={14} /> {t('The recording')}
                  </button>
                  {v.canChange && (
                    <button className="btn-sm btn-with-icon danger" onClick={() => void remove(v)}>
                      <Icon name="trash" size={14} /> {t('Delete')}
                    </button>
                  )}
                </span>
              </span>
            </li>
          ))}
        </ul>
      )}
      {hearing.startsWith('v:') && <small className="hint">{t('Chatterbox reads it (if it has to be started first, about a minute).')}</small>}
      <audio ref={audio} className="rtm-audio" controls hidden={!voices?.length} />
      <details className="rtm-more" open={voices?.length === 0 || undefined}>
        <summary>{t('Copy a voice')}</summary>
        <p className="hint">{t('A recording of one person speaking clearly, 10 to 20 seconds, with no music or other voices: a WAV, MP3, M4A or FLAC. Usherr keeps the first 20 seconds, as mono, at even loudness.')}</p>
        <div className="rtm-say-foot">
          <input value={name} onChange={(e) => setName(e.target.value)} placeholder={t('Name, like Grandpa')} aria-label={t('Voice name')} maxLength={40} />
          <input ref={fileInput} type="file" accept="audio/*,.wav,.mp3,.m4a,.flac,.ogg" onChange={(e) => setFile(e.target.files?.[0] ?? null)} aria-label={t('The recording')} />
        </div>
        <label className="rtm-check">
          <input type="checkbox" checked={rights} onChange={(e) => setRights(e.target.checked)} /> {t('I have the right to use this voice (my own, or with the speaker’s permission).')}
        </label>
        <button type="button" className="btn-sm primary" onClick={copy} disabled={!file || !name.trim() || !rights || copying}>
          {copying ? t('Copying…') : t('Copy the voice')}
        </button>
      </details>
    </>
  )
}
