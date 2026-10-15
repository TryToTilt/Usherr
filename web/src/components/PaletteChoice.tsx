import { useState } from 'react'
import { t } from '../i18n'
import { applyPalette } from '../palette'

// Usherr: the colours, wine (the brand's own) or one of the others in
// usherr-palettes.css (tools/usherr/palettes.py), kept per browser like light
// or dark. main.tsx applies the saved choice before the first paint.
const PALETTES = [
  ['wine', 'Wine'],
  ['cherry', 'Cherry'],
  ['amber', 'Amber'],
  ['emerald', 'Emerald'],
  ['teal', 'Teal'],
  ['blue', 'Blue'],
  ['violet', 'Violet'],
  ['rose', 'Rose'],
  ['graphite', 'Graphite'],
] as const
type Palette = (typeof PALETTES)[number][0]

function isPalette(v: string | null): v is Palette {
  return PALETTES.some(([id]) => id === v)
}

const STORAGE_KEY = 'usherr-palette'

function savedPalette(): Palette {
  try {
    const v = localStorage.getItem(STORAGE_KEY)
    return isPalette(v) ? v : 'wine'
  } catch {
    return 'wine'
  }
}

export default function PaletteChoice() {
  const [palette, setPalette] = useState<Palette>(savedPalette)

  const pick = (p: Palette) => {
    setPalette(p)
    applyPalette(p)
    try {
      localStorage.setItem(STORAGE_KEY, p)
    } catch {
      // a blocked localStorage just means the choice will not survive a reload
    }
  }

  return (
    <div style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 14, marginTop: 12 }}>
      <div role="radiogroup" aria-label={t('Colours')} style={{ display: 'flex', flexWrap: 'wrap', gap: 6 }}>
        {PALETTES.map(([p, name]) => (
          <button key={p} type="button" role="radio" aria-checked={palette === p} className={palette === p ? 'primary' : ''} onClick={() => pick(p)}>
            <span className={`palette-swatch palette-swatch-${p}`} aria-hidden="true" /> {t(name)}
          </button>
        ))}
      </div>
      <span style={{ color: 'var(--text-dim)' }}>{t('The colours of the app. Also kept in this browser, so everyone can pick their own.')}</span>
    </div>
  )
}
