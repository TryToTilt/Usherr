import { useState } from 'react'
import { t } from '../i18n'
import { MENU_HIDE_EVENT, menuHideSeconds, STORAGE_KEY } from './menuHide'

const say = (s: number) => (s === 0 ? t('Right away') : s === 1 ? t('After 1 second') : t('After {n} seconds', { n: s }))

export default function MenuHideChoice() {
  const [s, setS] = useState(menuHideSeconds)
  const pick = (v: number) => {
    setS(v)
    try {
      localStorage.setItem(STORAGE_KEY, String(v))
    } catch {
      // a blocked localStorage just means the choice will not survive a reload
    }
    window.dispatchEvent(new Event(MENU_HIDE_EVENT))
  }
  return (
    <div style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 14, marginTop: 12 }}>
      <label style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
        <span>{t('The menu slides away')}</span>
        <input type="range" min={0} max={10} step={1} value={s} onChange={(e) => pick(Number(e.target.value))} aria-valuetext={say(s)} style={{ width: 180 }} />
        <strong style={{ minWidth: 120 }}>{say(s)}</strong>
      </label>
      <span style={{ color: 'var(--text-dim)' }}>{t('On a wider screen, the open menu stays while your mouse is over it, and slides away this long after the mouse leaves. Kept in this browser.')}</span>
    </div>
  )
}
