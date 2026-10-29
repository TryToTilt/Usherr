import { useState } from 'react'
import { t } from '../i18n'
import { applyBase } from '../palette'

// Usherr: the main colour, the tint of the whole app under the highlight
// colour (PaletteChoice.tsx). Made by tools/usherr/bases.py into
// usherr-bases.css and kept per browser like the rest; main.tsx applies the
// saved choice before the first paint. 'palette' keeps the highlight's own tint.
const BASES = [
  ['palette', 'Matches the colours'],
  ['navy', 'Midnight navy'],
  ['espresso', 'Espresso'],
  ['forest', 'Forest'],
  ['plum', 'Plum'],
  ['oxblood', 'Oxblood'],
  ['ocean', 'Ocean'],
  ['stone', 'Stone'],
  ['oled', 'OLED black'],
] as const
type Base = (typeof BASES)[number][0]

function isBase(v: string | null): v is Base {
  return BASES.some(([id]) => id === v)
}

const STORAGE_KEY = 'usherr-base'

function savedBase(): Base {
  try {
    const v = localStorage.getItem(STORAGE_KEY)
    return isBase(v) ? v : 'palette'
  } catch {
    return 'palette'
  }
}

export default function BaseChoice() {
  const [base, setBase] = useState<Base>(savedBase)

  const pick = (b: Base) => {
    setBase(b)
    applyBase(b)
    try {
      localStorage.setItem(STORAGE_KEY, b)
    } catch {
      // a blocked localStorage just means the choice will not survive a reload
    }
  }

  return (
    <div style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 14, marginTop: 12 }}>
      <div role="radiogroup" aria-label={t('Main colour')} style={{ display: 'flex', flexWrap: 'wrap', gap: 6 }}>
        {BASES.map(([b, name]) => (
          <button key={b} type="button" role="radio" aria-checked={base === b} className={base === b ? 'primary' : ''} onClick={() => pick(b)}>
            <span className={`palette-swatch base-swatch-${b}`} aria-hidden="true" /> {t(name)}
          </button>
        ))}
      </div>
      <span style={{ color: 'var(--text-dim)' }}>{t('The main colour of the app, under the colours above. Kept in this browser too.')}</span>
    </div>
  )
}
