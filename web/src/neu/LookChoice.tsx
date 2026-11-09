import { useState } from 'react'
import { t } from '../i18n'
import { chooseLook, chooseTvSize, savedLook, tvSizeOn, type Look } from './look'

// Usherr Neu or Usherr Classic, kept in this browser. Picking one reloads the
// page so every icon and picture follows.
// Usherr: Broadcast first, the default for a browser that never picked.
export default function LookChoice() {
  const [look] = useState<Look>(savedLook)
  const [tv, setTv] = useState(tvSizeOn)
  const looks: [Look, string][] = [
    ['broadcast', t('Broadcast')],
    ['neu', t('Usherr Neu')],
    ['lobby', t('Neu, new layout')],
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
      {(look === 'lobby' || look === 'broadcast') && (
        <label style={{ display: 'inline-flex', alignItems: 'center', gap: 8 }}>
          <input
            type="checkbox"
            checked={tv}
            onChange={(e) => {
              chooseTvSize(e.target.checked)
              setTv(e.target.checked)
            }}
          />
          {t('TV size: everything bigger, for the sofa')}
        </label>
      )}
      <span style={{ color: 'var(--text-dim)' }}>{t('Broadcast is dark and clear, made to be used from the sofa with a TV remote as well as a mouse.')}</span>
      <span style={{ color: 'var(--text-dim)' }}>{t('Neu is Usherr’s own soft look; Classic is the look it has always had. Neu, new layout is Neu with your libraries down the side, tabs along the bottom of a phone and a Server area for admins. Also kept in this browser.')}</span>
    </div>
  )
}
