import { useState } from 'react'
import { t } from '../i18n'
import { chooseLook, savedLook, type Look } from './look'

// Usherr Neu or Usherr Classic, kept in this browser. Picking one reloads the
// page so every icon and picture follows.
export default function LookChoice() {
  const [look] = useState<Look>(savedLook)
  const looks: [Look, string][] = [
    ['neu', t('Usherr Neu')],
    ['classic', t('Usherr Classic')],
  ]
  return (
    <div style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 14, marginTop: 12 }}>
      <div className="seg" role="radiogroup" aria-label={t('Look')}>
        {looks.map(([l, name]) => (
          <button key={l} type="button" role="radio" aria-checked={look === l} className={look === l ? 'active' : ''} onClick={() => look !== l && chooseLook(l)}>
            {name}
          </button>
        ))}
      </div>
      <span style={{ color: 'var(--text-dim)' }}>{t('Neu is Usherr’s own soft look; Classic is the look it has always had. Also kept in this browser.')}</span>
    </div>
  )
}
