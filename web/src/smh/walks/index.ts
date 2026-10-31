// Usherr SMH: every walkthrough, and the menu's groups.
import { t } from '../../i18n'
import type { SmhGroup, Walkthrough } from '../types'
import { completeCollection } from './collection'
import { comicArcs, comicReview, comicWeek, readComic, setupComics } from './comics'
import { setUpPreshow } from './preshow'
import { ebookToAudiobook } from './readtome'
import { bookShowtime } from './showtime'

export const WALKS: Walkthrough[] = [bookShowtime, setUpPreshow, readComic, comicArcs, comicWeek, comicReview, setupComics, ebookToAudiobook, completeCollection]

export function walkById(id: string): Walkthrough | undefined {
  return WALKS.find((w) => w.id === id)
}

export const GROUPS: { id: SmhGroup; label: () => string }[] = [
  { id: 'watch', label: () => t('Watch') },
  { id: 'night', label: () => t('Movie night') },
  { id: 'listen', label: () => t('Listen') },
  { id: 'read', label: () => t('Read') },
  { id: 'people', label: () => t('People and devices') },
  { id: 'library', label: () => t('My library') },
  { id: 'setup', label: () => t('Setup') },
]
