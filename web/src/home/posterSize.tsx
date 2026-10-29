// Usherr: the poster size slider of every library (Music, Ebooks, Audiobooks,
// Comics, NSFW), like the one Movies and TV shows have. Each kind keeps its
// own size in this browser. The size goes on the page as --poster-min (the
// grids) and --shelf-w (the rows that scroll sideways).
import { useState, type CSSProperties } from 'react'
import Icon from '../components/Icon'
import { t } from '../i18n'

export interface PosterSize {
  size: number
  set: (n: number) => void
  min: number
  max: number
  style: CSSProperties
}

export function usePosterSize(kind: string, fallback = 168, min = 110, max = 280): PosterSize {
  const key = `usherr-poster-size-${kind}`
  const [size, setSize] = useState(() => {
    try {
      const n = Number(localStorage.getItem(key))
      return n >= min && n <= max ? n : fallback
    } catch {
      return fallback
    }
  })
  const set = (n: number) => {
    setSize(n)
    try {
      localStorage.setItem(key, String(n))
    } catch {
      // not kept
    }
  }
  return { size, set, min, max, style: { ['--poster-min' as string]: `${size}px`, ['--shelf-w' as string]: `${size}px` } }
}

export function PosterSizeSlider({ value }: { value: PosterSize }) {
  return (
    <label className="poster-size" title={t('Poster size')}>
      <Icon name="image" size={14} />
      <input type="range" min={value.min} max={value.max} step={2} value={value.size} onChange={(e) => value.set(Number(e.target.value))} aria-label={t('Poster size')} />
    </label>
  )
}
