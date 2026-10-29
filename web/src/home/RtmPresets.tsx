// Usherr: the household's Read-to-Me presets, on the Read-to-Me tab: hear
// each one's voice, choose the default, delete one. Presets are made in the
// Read to me dialog on an ebook's page.
import { useEffect, useRef, useState } from 'react'
import { useConfirm } from '../components/ConfirmProvider'
import Icon from '../components/Icon'
import { t } from '../i18n'
import { parseMixId, readToMeApi, type RtvPreset } from './readToMeApi'

export default function RtmPresets({ voiceNames }: { voiceNames: Record<string, string> }) {
  const confirm = useConfirm()
  const audio = useRef<HTMLAudioElement>(null)
  const [presets, setPresets] = useState<RtvPreset[] | null>(null)
  const [error, setError] = useState('')
  useEffect(() => {
    readToMeApi
      .presets()
      .then((d) => setPresets(d.presets ?? []))
      .catch((e: Error) => setError(e.message))
  }, [])
  const run = (p: Promise<{ presets: RtvPreset[] }>) =>
    p.then((d) => setPresets(d.presets)).catch((e: Error) => setError(e.message))
  const voiceOf = (p: RtvPreset) => {
    const name = (id: string) => voiceNames[`${p.engine}:${id}`] ?? id
    const m = p.engine === 'kokoro' ? parseMixId(p.voice) : null
    return m ? t('{a} {x}% + {b} {y}%', { a: name(m.main), x: m.weight * 10, b: name(m.second), y: 100 - m.weight * 10 }) : name(p.voice)
  }
  const hear = (p: RtvPreset) => {
    const a = audio.current
    if (!a) return
    a.src = readToMeApi.sampleUrl({ engine: p.engine, id: p.voice })
    void a.play().catch(() => setError(t("That sample couldn't be made. The voice engine may be busy or stopped.")))
  }
  const remove = async (p: RtvPreset) => {
    if (!(await confirm({ title: t('Delete the preset {name}?', { name: p.name }), confirmLabel: t('Delete'), danger: true }))) return
    void run(readToMeApi.deletePreset(p.id))
  }
  if (!presets) return error ? <p className="error-text">{error}</p> : null
  return (
    <>
      {error && <p className="error-text">{error}</p>}
      {presets.length === 0 ? (
        <p className="hint">{t('No presets yet. In the Read to me dialog on an ebook, pick a voice and how it reads, then Save these settings as a preset.')}</p>
      ) : (
        <ul className="rtm-list rtm-presets">
          {presets.map((p) => (
            <li key={p.id} className="rtm-job">
              <span className="rtm-row">
                <strong>{p.name}</strong>
                {p.default && <small className="badge">{t('Default')}</small>}
                <small className="hint">
                  {voiceOf(p)} · {p.engine === 'chatterbox' ? 'Chatterbox' : 'Kokoro'} · {t('speed {n}×', { n: p.speed })}
                </small>
                <span className="spacer" />
                <span className="rtm-made-actions">
                  <button className="btn-sm btn-with-icon" onClick={() => hear(p)}>
                    <Icon name="play" size={14} /> {t('Hear')}
                  </button>
                  <button className="btn-sm" onClick={() => void run(readToMeApi.setDefaultPreset(p.id, !p.default))}>
                    {p.default ? t('Not the default') : t('Make it the default')}
                  </button>
                  <button className="btn-sm btn-with-icon danger" onClick={() => void remove(p)}>
                    <Icon name="trash" size={14} /> {t('Delete')}
                  </button>
                </span>
              </span>
            </li>
          ))}
        </ul>
      )}
      <audio ref={audio} className="rtm-audio" controls hidden={presets.length === 0} />
    </>
  )
}
