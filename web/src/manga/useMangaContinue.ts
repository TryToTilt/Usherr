// Usherr: where you got to in your manga, for Home. With Manga off, or nothing being
// read, there's nothing to show.
import { useEffect, useState } from 'react'
import { t } from '../i18n'
import type { MangaContinue } from './mangaApi'

export function useMangaContinue(): MangaContinue[] {
  const [items, setItems] = useState<MangaContinue[]>([])
  useEffect(() => {
    fetch('/api/manga/continue', { credentials: 'include', cache: 'no-store' })
      .then((r) => (r.ok ? r.json() : []))
      .then((x) => setItems(Array.isArray(x) ? (x as MangaContinue[]) : []))
      .catch(() => undefined)
  }, [])
  return items
}

export const mangaContinueKey = (c: MangaContinue) => (c.chapterId ? `c${c.chapterId}` : `v${c.volumeId}`)

export const mangaContinueLink = (c: MangaContinue) => (c.chapterId ? `/manga/${c.seriesId}?read=${c.chapterId}&chapter=1` : `/manga/${c.seriesId}?read=${c.volumeId}`)

export function mangaContinueMeta(c: MangaContinue): string {
  const started = c.page > 0 && c.pages > 0
  if (c.chapterId) return started ? t('Chapter {n}, page {page} of {pages}', { n: c.number, page: c.page, pages: c.pages }) : t('Next: Chapter {n}', { n: c.number })
  return started ? t('Volume {n}, page {page} of {pages}', { n: c.number, page: c.page, pages: c.pages }) : t('Next: Volume {n}', { n: c.number })
}
