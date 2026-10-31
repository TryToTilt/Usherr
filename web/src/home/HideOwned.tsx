// Home fork: Discover's "Hide what I have" switch, with a menu to choose what
// counts: on disk, streamed, wanted. Styled as v3's dropdowns.
import { useEffect, useRef, useState } from 'react'
import Icon from '../components/Icon'
import Switch from '../components/Switch'
import { useHideOwned } from '../useHideOwned'
import { t } from '../i18n'
import { useHideWhat, type HideWhat } from './useHave'

export default function HideOwned({ label }: { label: string }) {
  const [on, setOn] = useHideOwned()
  const [what, setWhat] = useHideWhat()
  const [open, setOpen] = useState(false)
  const root = useRef<HTMLDivElement>(null)
  useEffect(() => {
    if (!open) return
    const onDoc = (e: MouseEvent) => {
      if (!root.current?.contains(e.target as Node)) setOpen(false)
    }
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && setOpen(false)
    document.addEventListener('mousedown', onDoc)
    document.addEventListener('keydown', onKey)
    return () => {
      document.removeEventListener('mousedown', onDoc)
      document.removeEventListener('keydown', onKey)
    }
  }, [open])
  const opts: { key: keyof HideWhat; label: string; hint: string }[] = [
    { key: 'disk', label: t('On disk'), hint: t('downloaded files') },
    { key: 'streamed', label: t('Streamed'), hint: t('StreamNZB and the Stream libraries') },
    { key: 'wanted', label: t('Wanted'), hint: t('added, not downloaded yet') },
  ]
  const chosen = opts.filter((o) => what[o.key])
  const summary = chosen.length === opts.length ? t('All') : chosen.length === 0 ? t('Nothing') : chosen.map((o) => o.label).join(', ')
  return (
    <span className="hide-owned" data-walk="hide-owned">
      <Switch checked={on} onChange={setOn} label={label} />
      <div className={`dropdown hide-owned-pick${open ? ' open' : ''}`} ref={root}>
        <button type="button" className="dropdown-btn" onClick={() => setOpen((o) => !o)} aria-haspopup="true" aria-expanded={open} aria-label={t('What to hide')} title={t('What to hide')} disabled={!on}>
          <span>{summary}</span>
          <Icon name="chevron-down" size={15} className="dd-chev" />
        </button>
        {open && (
          <ul className="dropdown-list" aria-label={t('What to hide')}>
            {opts.map((o) => (
              <li key={o.key} className={what[o.key] ? 'selected' : ''}>
                <label className="hide-owned-opt">
                  <input type="checkbox" checked={what[o.key]} onChange={(e) => setWhat({ ...what, [o.key]: e.target.checked })} />
                  <span>
                    {o.label}
                    <small>{o.hint}</small>
                  </span>
                </label>
              </li>
            ))}
          </ul>
        )}
      </div>
    </span>
  )
}
