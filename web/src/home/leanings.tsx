// Home fork: Discover learns from Not interested. Genres the list keeps
// dismissing (3 times or more, and a quarter of all dismissals) move to the
// end of every Discover row. Nothing is hidden, and each genre can be
// switched back on (remembered in this browser).
import { useEffect, useState, useSyncExternalStore } from 'react'
import Icon from '../components/Icon'
import { t } from '../i18n'

interface Leaning {
  genre: string
  count: number
}

const OFF_KEY = 'discover-leanings-off'
let leanings: Leaning[] = []
let off: Set<string> = readOff()
let version = 0
const listeners = new Set<() => void>()
let loaded: Promise<void> | null = null

function readOff(): Set<string> {
  try {
    return new Set(JSON.parse(localStorage.getItem(OFF_KEY) ?? '[]') as string[])
  } catch {
    return new Set()
  }
}

function changed() {
  version++
  listeners.forEach((l) => l())
}

function load() {
  loaded ??= fetch('/api/home/discover/leanings', { credentials: 'include' })
    .then((r) => (r.ok ? r.json() : { genres: [] }))
    .then((d: { genres: Leaning[] }) => {
      leanings = d.genres ?? []
      changed()
    })
    .catch(() => undefined)
}

// reloadLeanings reads them again (after a title is marked Not interested).
export function reloadLeanings() {
  loaded = null
  load()
}

function subscribe(l: () => void) {
  listeners.add(l)
  return () => listeners.delete(l)
}

// useFewer is the genres to move back, in lower case.
export function useFewer(): Set<string> {
  useEffect(load, [])
  const v = useSyncExternalStore(subscribe, () => version)
  const [set, setSet] = useState<Set<string>>(new Set())
  useEffect(() => {
    setSet(new Set(leanings.filter((l) => !off.has(l.genre)).map((l) => l.genre.toLowerCase())))
  }, [v])
  return set
}

// leanOrder moves titles in the genres to the end, keeping the order otherwise.
export function leanOrder<T extends { genres?: string[] }>(list: T[], fewer: Set<string>): T[] {
  if (fewer.size === 0) return list
  const keep: T[] = []
  const later: T[] = []
  for (const m of list) {
    if ((m.genres ?? []).some((g) => fewer.has(g.toLowerCase()))) later.push(m)
    else keep.push(m)
  }
  return later.length === 0 ? list : [...keep, ...later]
}

// LeaningsBar says what Discover learned, with a way to switch each off.
export function LeaningsBar() {
  useEffect(load, [])
  useSyncExternalStore(subscribe, () => version)
  if (leanings.length === 0) return null
  const toggle = (g: string) => {
    if (off.has(g)) off.delete(g)
    else off.add(g)
    off = new Set(off)
    try {
      localStorage.setItem(OFF_KEY, JSON.stringify([...off]))
    } catch {
      // remembering is optional
    }
    changed()
  }
  return (
    <div className="leanings-bar" title={t('Learned from your Not interested list: these genres go to the end of each row. Nothing is hidden.')}>
      <span className="hint">{t('Showing fewer:')}</span>
      {leanings.map((l) => (
        <button key={l.genre} type="button" className={`room-chip${off.has(l.genre) ? '' : ' on'}`} aria-pressed={!off.has(l.genre)} onClick={() => toggle(l.genre)} title={off.has(l.genre) ? t('Show fewer {genre} again', { genre: l.genre }) : t('Stop showing fewer {genre}', { genre: l.genre })}>
          {l.genre} <small>×{l.count}</small> {!off.has(l.genre) && <Icon name="x" size={11} />}
        </button>
      ))}
    </div>
  )
}
