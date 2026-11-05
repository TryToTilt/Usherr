// Usherr: the profile editor's Simple mode. A few choices most people need:
// the quality, the largest file, better versions and bad releases. Whatever
// the Advanced mode set stays as it is; the note says so.
import type { ReactNode } from 'react'
import type { QualityProfileInput } from '../api'
import { SIMPLE_QUALITIES, SLIDER_MAX, sliderGB, sliderValue, type ExtrasDraft, type SimpleQuality } from '../profileExtras'
import { t } from '../i18n'
import { SamplePicks } from './ProfileTester'

const CHOICES: { key: SimpleQuality; label: () => string; hint: () => string }[] = [
  { key: 'save', label: () => t('Save space'), hint: () => t('720p, about 2–4 GB a film') },
  { key: 'hd', label: () => t('HD'), hint: () => t('1080p web or Blu-ray, 5–15 GB') },
  { key: 'besthd', label: () => t('Best HD'), hint: () => t('1080p Blu-ray or Remux, 10–45 GB') },
  { key: '4k', label: () => t('4K'), hint: () => t('2160p only, 15–100+ GB (Remux)') },
  { key: '4kfb', label: () => t('4K, else HD'), hint: () => t('HD now, 4K when one turns up') },
]

const SIZE_CHIPS = [8, 20, 60, 100, 0]

function sizeHint(gb: number): string {
  if (gb <= 0) return t('Any size is taken. Fakes that are far too small are still skipped.')
  if (gb >= 60) return t('Room for 4K and Remux releases, some of which pass 100 GB.')
  if (gb >= 20) return t('Fits most 1080p Blu-ray and web releases. Remuxes are usually bigger.')
  return t('Small files only. Most 4K and Remux releases will be skipped.')
}

export interface SimpleProps {
  nameField: ReactNode
  quality: SimpleQuality | null
  onQuality: (q: SimpleQuality) => void
  maxSize: string
  onMaxSize: (gb: string) => void
  upgradeAllowed: boolean
  onUpgrade: (on: boolean) => void
  skipBad: boolean
  onSkipBad: (on: boolean) => void
  x: ExtrasDraft
  onX: (x: ExtrasDraft) => void
  // What the Advanced mode set that isn't shown here ([] when nothing).
  kept: string[]
  onShowAdvanced: () => void
  body: QualityProfileInput
}

export default function ProfileSimple(p: SimpleProps) {
  const gb = Number(p.maxSize) > 0 ? Number(p.maxSize) : 0
  return (
    <div className="ps-simple">
      {p.nameField}

      <fieldset className="profile-fieldset">
        <legend>{t('What quality do you want?')}</legend>
        <div className="ps-choices" role="radiogroup" aria-label={t('Quality')}>
          {CHOICES.map((c) => (
            <button key={c.key} type="button" role="radio" aria-checked={p.quality === c.key} className={`ps-choice${p.quality === c.key ? ' active' : ''}`} onClick={() => p.onQuality(c.key)}>
              <strong>{c.label()}</strong>
              <small>{c.hint()}</small>
            </button>
          ))}
        </div>
        {p.quality === null && <p className="ps-note">{t('This profile’s qualities were set in Advanced. Pick one above to replace them, or switch to Advanced to change them there.')}</p>}
      </fieldset>

      <fieldset className="profile-fieldset">
        <legend>{t('Largest file')}</legend>
        <div className="ps-sizer">
          <input
            type="range"
            min={1}
            max={SLIDER_MAX + 1}
            step={1}
            value={sliderValue(gb)}
            aria-label={t('Largest file in GB')}
            aria-valuetext={gb > 0 ? `${gb} GB` : t('No limit')}
            onChange={(e) => p.onMaxSize(String(sliderGB(Number(e.target.value)) || ''))}
          />
          <label className="ps-sizebox">
            <input inputMode="decimal" aria-label={t('Largest file in GB, typed')} value={p.maxSize} placeholder={t('no limit')} onChange={(e) => p.onMaxSize(e.target.value)} />
            <span>GB</span>
          </label>
        </div>
        <div className="ps-chips" role="group" aria-label={t('Size presets')}>
          {SIZE_CHIPS.map((c) => (
            <button key={c} type="button" className={`chip${gb === c ? ' active' : ''}`} aria-pressed={gb === c} onClick={() => p.onMaxSize(c ? String(c) : '')}>
              {c ? `${c} GB` : t('No limit')}
            </button>
          ))}
        </div>
        <p className="ps-note">{sizeHint(gb)}</p>
        <label className="check-hint">
          <input type="checkbox" checked={p.x.preferSmaller} onChange={(e) => p.onX({ ...p.x, preferSmaller: e.target.checked })} />
          <span>
            {t('Pick the smaller file when two are equally good')}
            <small>{t('Off: the bigger one wins, since it usually looks better.')}</small>
          </span>
        </label>
      </fieldset>

      <label className="check-hint">
        <input type="checkbox" checked={p.upgradeAllowed} onChange={(e) => p.onUpgrade(e.target.checked)} />
        <span>
          {t('Keep looking for a better version')}
          <small>{t('Swaps the file for a better one until it reaches the best quality above.')}</small>
        </span>
      </label>
      <label className="check-hint">
        <input type="checkbox" checked={p.skipBad} onChange={(e) => p.onSkipBad(e.target.checked)} />
        <span>
          {t('Skip bad releases')}
          <small>{t('Leaves out fakes, 3D, upscales, burned-in subtitles and poor re-encodes.')}</small>
        </span>
      </label>
      <label className="check-hint">
        <input type="checkbox" checked={p.x.preferHevc} onChange={(e) => p.onX({ ...p.x, preferHevc: e.target.checked })} />
        <span>
          {t('Prefer smaller files that look the same')}
          <small>{t('Picks x265/HEVC over x264 when the quality is the same.')}</small>
        </span>
      </label>

      {p.kept.length > 0 && (
        <div className="ps-kept">
          {t('This profile also has advanced settings: {list}. Simple mode keeps them as they are.', { list: p.kept.join(', ') })}{' '}
          <button type="button" className="link-btn" onClick={p.onShowAdvanced}>
            {t('Show them in Advanced')}
          </button>
        </div>
      )}

      <fieldset className="profile-fieldset">
        <legend>{t('What this profile would pick')}</legend>
        <SamplePicks body={p.body} />
      </fieldset>
    </div>
  )
}

export { SIMPLE_QUALITIES }
