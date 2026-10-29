// Usherr: A/B in the Read to me dialog. The same short passage of the book read
// two ways: these settings (A) against a preset, or against another take of
// these settings (Chatterbox reads a line a little differently each time; a
// take that sounds right can be kept).
import { useEffect, useRef, useState } from 'react'
import Icon from '../components/Icon'
import { t } from '../i18n'
import { newSeed, readToMeApi, type RtvOptions, type RtvPreset } from './readToMeApi'

type Req = { bookId: number; engine: string; voice: string; speed: number } & RtvOptions
const WORDS = 60

export default function RtmCompare({ a, presets, takes, startNote, onSpoke, onUseSeed, onUsePreset }: {
  a: Req
  presets: RtvPreset[]
  takes: boolean // Chatterbox reads it: takes differ
  startNote: string // said while Chatterbox is being started for it ('' when it runs)
  onSpoke: () => void
  onUseSeed: (seed: number) => void
  onUsePreset: (id: number) => void
}) {
  const audio = useRef<HTMLAudioElement>(null)
  const [chosen, setB] = useState('take')
  const [seedA] = useState(newSeed) // A's take, when these settings keep none
  const [seedB, setSeedB] = useState(newSeed)
  const [busy, setBusy] = useState('')
  const [error, setError] = useState('')
  const made = useRef<Record<string, string>>({}) // request -> its audio
  useEffect(() => {
    const urls = made.current
    return () => Object.values(urls).forEach((u) => URL.revokeObjectURL(u))
  }, [])
  // B: what was chosen, or the first preset when takes don't apply (Kokoro reads a line the same way every time).
  const b = !takes && chosen === 'take' ? (presets[0] ? String(presets[0].id) : '') : chosen

  const keptA = a.tune?.seed
  const reqA: Req = takes ? { ...a, tune: { ...a.tune, seed: keptA ?? seedA } } : a
  const preset = presets.find((p) => String(p.id) === b)
  const reqB: Req | null =
    b === 'take'
      ? { ...a, tune: { ...a.tune, seed: seedB } }
      : preset
        ? { ...a, engine: preset.engine, voice: preset.voice, speed: preset.speed, ...preset.options, chapters: a.chapters, cast: a.cast, cbModel: preset.options.cbModel ?? a.cbModel }
        : null

  const hear = (which: 'A' | 'B', req: Req | null) => {
    if (!req) return
    const key = JSON.stringify(req)
    const play = (url: string) => {
      const el = audio.current
      if (!el) return
      el.src = url
      void el.play().catch(() => undefined)
    }
    if (made.current[key]) return play(made.current[key])
    setError('')
    setBusy(which)
    readToMeApi
      .preview({ ...req, words: WORDS })
      .then((url) => {
        made.current[key] = url
        onSpoke()
        play(url)
      })
      .catch((e: Error) => setError(e.message))
      .finally(() => setBusy(''))
  }

  return (
    <details className="rtm-more">
      <summary>{t('Compare two ways of reading')}</summary>
      <p className="hint">{t('The same {n} words of the book, read two ways. A is the settings above.', { n: WORDS })}</p>
      <label className="rtm-cast-row">
        <span>{t('B is')}</span>
        <select value={b} onChange={(e) => setB(e.target.value)} aria-label={t('What B is')}>
          {takes && <option value="take">{t('Another take of these settings')}</option>}
          {presets.map((p) => (
            <option key={p.id} value={String(p.id)}>
              {t('The preset {name}', { name: p.name })}
            </option>
          ))}
          {!takes && presets.length === 0 && <option value="">{t('No presets yet')}</option>}
        </select>
      </label>
      {error && <p className="error-text">{error}</p>}
      <div className="rtm-say-foot">
        <button type="button" className="btn-sm btn-with-icon" disabled={!!busy} onClick={() => hear('A', reqA)}>
          <Icon name={busy === 'A' ? 'refresh' : 'play'} size={14} /> {t('Hear A')}
        </button>
        <button type="button" className="btn-sm btn-with-icon" disabled={!!busy || !reqB} onClick={() => hear('B', reqB)}>
          <Icon name={busy === 'B' ? 'refresh' : 'play'} size={14} /> {t('Hear B')}
        </button>
        {b === 'take' && (
          <button type="button" className="btn-sm" disabled={!!busy} onClick={() => setSeedB(newSeed())}>
            {t('Another B')}
          </button>
        )}
        <span className="spacer" />
        {takes && !keptA && (
          <button type="button" className="btn-sm" onClick={() => onUseSeed(seedA)}>
            {t('Keep take A')}
          </button>
        )}
        {reqB && (
          <button type="button" className="btn-sm primary" onClick={() => (b === 'take' ? onUseSeed(seedB) : preset && onUsePreset(preset.id))}>
            {b === 'take' ? t('Keep take B') : t('Use B')}
          </button>
        )}
      </div>
      {busy && <small className="hint">{startNote || t('Reading it…')}</small>}
      <audio ref={audio} controls className="rtm-audio" />
    </details>
  )
}
