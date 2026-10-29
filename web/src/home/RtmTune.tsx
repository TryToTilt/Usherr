// Usherr: how Chatterbox reads, in the Read to me dialog. Emotion and pacing
// (the standard model only: Turbo has no such dials), variety, and a seed that
// keeps one take, so a remake or the whole book reads the way the preview did.
import { t } from '../i18n'
import { newSeed, TUNE_USUAL, type CbModel, type RtvTune } from './readToMeApi'

export default function RtmTune({ tune, onChange, model }: { tune: RtvTune | null; onChange: (t: RtvTune | null) => void; model?: CbModel }) {
  const turbo = model === 'turbo'
  const dials = [
    ['exaggeration', t('Emotion'), 0.25, 2, t('Flat at the low end, dramatic at the high end. Above about 1 it can overact.')],
    ['cfg', t('Pacing'), 0, 1, t('Lower is slower and more deliberate; with more emotion, lower pacing keeps it steady.')],
    ['temperature', t('Variety'), 0.05, 2, t('How much each line differs from the last. Too high and it can mumble.')],
  ] as const
  const set = (patch: RtvTune) => {
    const next: RtvTune = { ...tune, ...patch }
    for (const k of Object.keys(next) as (keyof RtvTune)[]) if (next[k] === undefined) delete next[k]
    onChange(Object.keys(next).length ? next : null)
  }
  const changed = !!tune && (tune.exaggeration !== undefined || tune.cfg !== undefined || tune.temperature !== undefined)
  return (
    <details className="rtm-more">
      <summary>
        {t('How Chatterbox reads')} <small className="hint">{changed ? t('your own way') : t('its usual way')}{tune?.seed ? ' · ' + t('take {n}', { n: tune.seed }) : ''}</small>
      </summary>
      <div className="rtm-pauses">
        {dials.map(([key, label, lo, hi, hint]) => {
          const off = turbo && key !== 'temperature'
          const value = tune?.[key] ?? TUNE_USUAL[key]
          return (
            <label key={key} className={off ? 'rtm-speed rtm-off' : 'rtm-speed'}>
              <span>
                {label}: <strong>{value.toFixed(2)}</strong> <small className="hint">{off ? t('Turbo has no such dial') : hint}</small>
              </span>
              <input type="range" min={lo} max={hi} step={0.05} value={value} disabled={off} onChange={(e) => set({ [key]: Number(e.target.value) })} />
            </label>
          )
        })}
        <label className="rtm-check">
          <input type="checkbox" checked={!!tune?.seed} onChange={(e) => set({ seed: e.target.checked ? newSeed() : undefined })} /> {t('Keep this take: the same text is read the same way every time')}
        </label>
        {!!tune?.seed && (
          <div className="rtm-say-foot">
            <label className="rtm-check">
              {t('Take')} <input type="number" min={1} max={2147483647} value={tune.seed} onChange={(e) => set({ seed: Math.max(1, Math.min(2147483647, Math.round(Number(e.target.value)) || 1)) })} aria-label={t('Take number')} />
            </label>
            <button type="button" className="btn-sm" onClick={() => set({ seed: newSeed() })}>
              {t('Another take')}
            </button>
          </div>
        )}
        <small className="hint">{t('Hear the first words of the book (or Compare) to try a setting. A kept take is used for the whole book and when it is remade.')}</small>
        <button type="button" className="btn-sm" onClick={() => onChange(null)} disabled={!tune}>
          {t('The usual way')}
        </button>
      </div>
    </details>
  )
}
