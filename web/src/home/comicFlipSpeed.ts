// Home fork: how fast comic pages turn (shared by the reader and ComicFlip,
// kept apart so the page-flip library still loads only with a comic).
import { t } from '../i18n'

export type FlipSpeed = 'slow' | 'normal' | 'fast'
export const flipSpeeds: { id: FlipSpeed; label: string; ms: number }[] = [
  { id: 'slow', label: t('Slow'), ms: 1100 },
  { id: 'normal', label: t('Normal'), ms: 650 },
  { id: 'fast', label: t('Fast'), ms: 350 },
]
