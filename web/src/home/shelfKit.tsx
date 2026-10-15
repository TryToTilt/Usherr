// Home fork: the shelves' shared parts for the libraries that aren't films or
// shows (books, music, comics, scenes): a remembered Shelves/Grid choice with
// its "Rows by" pick, the toolbar control for both, and grouping helpers.
import { useState } from 'react'
import Dropdown from '../components/Dropdown'
import Icon from '../components/Icon'
import { locale, t } from '../i18n'
import type { Shelf } from './Shelves'
import { compareTitles, sortTitle } from './sortTitle'

function recall(key: string): string | null {
  try {
    return localStorage.getItem(key)
  } catch {
    return null
  }
}

function remember(key: string, v: string) {
  try {
    localStorage.setItem(key, v)
  } catch {
    // remembering is optional
  }
}

// useShelfView: shelves on (the default) or the old grid, and how the rows
// are cut, both remembered on this device under id.
export function useShelfView<R extends string>(id: string, choices: readonly R[], first: R) {
  const [on, setOn] = useState(() => recall(`mediarium-shelves-${id}`) !== 'grid')
  const [by, setBy] = useState<R>(() => {
    const v = recall(`mediarium-rows-${id}`) as R | null
    return v && choices.includes(v) ? v : first
  })
  return {
    on,
    by,
    setOn: (v: boolean) => {
      setOn(v)
      remember(`mediarium-shelves-${id}`, v ? 'shelves' : 'grid')
    },
    setBy: (v: R) => {
      setBy(v)
      remember(`mediarium-rows-${id}`, v)
    },
  }
}

// RowsByPick: the "Rows by" pick, shown above the shelves (never folded away on a phone).
export function RowsByPick<R extends string>({ value, onChange, options }: { value: R; onChange: (v: R) => void; options: { value: R; label: string }[] }) {
  return (
    <div className="shelves-bar">
      <Dropdown icon="list" label={t('Rows by')} value={value} onChange={(v) => onChange(v as R)} options={options} />
    </div>
  )
}

// ShelfControls: the Shelves/Grid buttons.
export function ShelfControls<R extends string>({ view }: { view: ReturnType<typeof useShelfView<R>> }) {
  return (
    <>
      <div className="seg">
        <button className={view.on ? 'active' : ''} onClick={() => view.setOn(true)} aria-label={t('Shelves')} title={t('Shelves: rows that scroll sideways')}>
          <Icon name="menu" size={16} />
        </button>
        <button className={view.on ? '' : 'active'} onClick={() => view.setOn(false)} aria-label={t('Grid view')}>
          <Icon name="grid" size={16} />
        </button>
      </div>
    </>
  )
}

// groupShelves puts each item in a row for every key it has; the biggest rows
// come first. Keys with fewer than min items go to one "rest" row at the end.
export function groupShelves<T>(
  items: T[],
  keysOf: (i: T) => string[],
  opts: { prefix: string; min?: number; rest?: string; order?: (a: T, b: T) => number; byName?: boolean; seeAll?: (key: string) => void } = { prefix: 'g' },
): Shelf<T>[] {
  const by = new Map<string, T[]>()
  const rest: T[] = []
  for (const i of items) {
    const keys = keysOf(i).filter(Boolean)
    if (keys.length === 0) rest.push(i)
    for (const k of keys) by.set(k, [...(by.get(k) ?? []), i])
  }
  const min = opts.min ?? 1
  const rows: Shelf<T>[] = []
  const entries = [...by.entries()].sort((a, b) => (opts.byName ? compareTitles(a[0], b[0]) : b[1].length - a[1].length || compareTitles(a[0], b[0])))
  for (const [k, list] of entries) {
    if (list.length < min) {
      for (const i of list) if (!rest.includes(i)) rest.push(i)
      continue
    }
    rows.push({ key: `${opts.prefix}:${k}`, title: k, items: opts.order ? [...list].sort(opts.order) : list, seeAll: opts.seeAll ? () => opts.seeAll!(k) : undefined })
  }
  if (rest.length > 0 && opts.rest) rows.push({ key: `${opts.prefix}:~rest`, title: opts.rest, items: opts.order ? [...rest].sort(opts.order) : rest })
  return rows
}

// letterShelves: A–Z, in a handful of letter groups.
const LETTERS: [string, RegExp][] = [
  ['A–B', /^[ab]/i],
  ['C–D', /^[cd]/i],
  ['E–G', /^[e-g]/i],
  ['H–K', /^[h-k]/i],
  ['L–M', /^[lm]/i],
  ['N–P', /^[n-p]/i],
  ['Q–S', /^[q-s]/i],
  ['T', /^t/i],
  ['U–Z', /^[u-z]/i],
]

export function letterShelves<T>(items: T[], nameOf: (i: T) => string): Shelf<T>[] {
  const rows: Shelf<T>[] = [{ key: 'az:#', title: '#', items: [] }, ...LETTERS.map(([l]) => ({ key: `az:${l}`, title: l, items: [] as T[] }))]
  for (const i of [...items].sort((a, b) => compareTitles(nameOf(a), nameOf(b)))) {
    const n = LETTERS.findIndex(([, re]) => re.test(sortTitle(nameOf(i))))
    rows[n + 1].items.push(i)
  }
  return rows.filter((r) => r.items.length > 0)
}

// monthShelves: one row per month things were added, newest first.
export function monthShelves<T>(items: T[], addedOf: (i: T) => string): Shelf<T>[] {
  const by = new Map<string, T[]>()
  for (const i of [...items].sort((a, b) => addedOf(b).localeCompare(addedOf(a)))) {
    const m = addedOf(i).slice(0, 7)
    by.set(m, [...(by.get(m) ?? []), i])
  }
  return [...by.entries()].map(([m, list]) => {
    const d = new Date(`${m}-01T12:00:00`)
    const title = Number.isNaN(d.getTime()) ? t('Recently added') : d.toLocaleDateString(locale(), { month: 'long', year: 'numeric' })
    return { key: `added:${m}`, title, items: list }
  })
}
