// Home fork: a show's status in words ("Ended", "Continuing"), from the
// library's showStatus or TMDB's own status text.
import { t } from '../i18n'

export type ShowStatus = 'continuing' | 'ended' | 'upcoming'

// statusOf turns TMDB's status ("Returning Series", "Canceled"…) into ours.
export function statusOf(tmdb?: string): ShowStatus | undefined {
  const s = (tmdb ?? '').toLowerCase()
  if (!s) return undefined
  if (s === 'ended' || s === 'canceled' || s === 'cancelled') return 'ended'
  if (s === 'in production' || s === 'planned' || s === 'pilot') return 'upcoming'
  return 'continuing'
}

export function showStatusLabel(status?: ShowStatus, tmdb?: string): string | undefined {
  const s = status ?? statusOf(tmdb)
  if (!s) return undefined
  if (s === 'ended') return (tmdb ?? '').toLowerCase().startsWith('cancel') ? t('Canceled') : t('Ended')
  if (s === 'upcoming') return t('Upcoming')
  return t('Continuing')
}
