// Usherr: the slim toolbar at the top of Discover and the Library. One row:
// the kind as a drop-down, a Filters button (with how many are on) that opens
// the rest, and the sort. Filters that are on show as small chips under it,
// each with an x. The row stays pinned under the top bar while you scroll.
import type { ReactNode } from 'react'
import Icon from './Icon'
import KindGlyph, { type GlyphKind } from './KindGlyph'
import { t } from '../i18n'
import './compactBar.css'

export interface KindOption {
  key: string
  label: string
  glyph: GlyphKind
  color: string
  count?: number
}

// KindPicker: the kind that is showing, and a native list of the others.
export function KindPicker({ options, value, onChange, label }: { options: KindOption[]; value: string; onChange: (key: string) => void; label?: string }) {
  const cur = options.find((o) => o.key === value) ?? options[0]
  if (!cur) return null
  return (
    <label className="kind-pick" style={{ ['--kc' as string]: cur.color }} title={label ?? t('What to show')}>
      <KindGlyph kind={cur.glyph} size={18} />
      <span className="kind-pick-name">{cur.label}</span>
      {cur.count !== undefined && <small className="mono kind-pick-count">{cur.count}</small>}
      <Icon name="chevron-down" size={14} />
      <select value={cur.key} onChange={(e) => onChange(e.target.value)} aria-label={label ?? t('What to show')}>
        {options.map((o) => (
          <option key={o.key} value={o.key}>
            {o.count !== undefined ? `${o.label} (${o.count})` : o.label}
          </option>
        ))}
      </select>
    </label>
  )
}

// FiltersButton: opens the panel; says how many filters are on.
export function FiltersButton({ open, onToggle, count }: { open: boolean; onToggle: () => void; count: number }) {
  return (
    <button type="button" className={`cbar-filters${open ? ' open' : ''}${count > 0 ? ' on' : ''}`} aria-expanded={open} onClick={onToggle} aria-label={t('Filters')}>
      <Icon name="sliders" size={16} /> <span className="cbar-filters-label">{t('Filters')}</span>
      {count > 0 && <span className="cbar-count">{count}</span>}
      <Icon name="chevron-down" size={14} />
    </button>
  )
}

export interface ActiveFilter {
  key: string
  label: string
  clear: () => void
}

// ActiveChips: the filters that are on, each with an x; nothing when none are.
export function ActiveChips({ filters, onClearAll }: { filters: ActiveFilter[]; onClearAll?: () => void }) {
  if (filters.length === 0) return null
  return (
    <div className="cbar-chips">
      {filters.map((f) => (
        <button key={f.key} type="button" className="cbar-chip" onClick={f.clear} aria-label={t('Remove the filter {name}', { name: f.label })}>
          {f.label} <Icon name="x" size={12} />
        </button>
      ))}
      {filters.length > 1 && onClearAll && (
        <button type="button" className="cbar-chip-clear" onClick={onClearAll}>
          {t('Clear all')}
        </button>
      )}
    </div>
  )
}

// CompactBar: the pinned row, the chips under it and the panel that opens.
export function CompactBar({ row, chips, panel, open }: { row: ReactNode; chips?: ReactNode; panel?: ReactNode; open: boolean }) {
  return (
    <div className="cbar">
      <div className="cbar-row">{row}</div>
      {open && panel && <div className="cbar-panel">{panel}</div>}
      {chips}
    </div>
  )
}
