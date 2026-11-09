// Usherr Broadcast: the remote's Options key on any poster that opens a film
// or a show: that title's More sheet, wherever the poster is (Home, the
// Library, Find, More like this). The page's own sheet stays for the page's
// own title (useTitleMore). AppShell draws this in Broadcast only.
import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { api } from '../api'
import { useToast } from '../components/Toast'
import { openShowtimeBooking } from '../home/ShowtimePill'
import { addToStack, removeFromStack, useStack } from '../home/stack'
import { t } from '../i18n'
import { useRemote } from '../remote'
import { openSmhMenu } from '../smh/engine'
import type { PosterTitle } from './logic'
import { CARD, cardTitle, markWatched, MoreSheet, type MoreRow } from './MoreSheet'

type Shown = { what: PosterTitle; title: string; poster?: string; tmdbId?: number }

function nameOf(el: Element | null): { title: string; poster?: string } {
  const card = el?.closest(CARD)
  const title = card?.querySelector('.pcard-title, .sc-title, .cx-title')?.textContent?.trim() || card?.querySelector('a[aria-label]')?.getAttribute('aria-label') || card?.querySelector('img[alt]')?.getAttribute('alt') || ''
  const poster = card?.querySelector('img')?.getAttribute('src') ?? undefined
  return { title, poster }
}

export default function PosterOptions() {
  const [shown, setShown] = useState<Shown | null>(null)
  useRemote((action, target) => {
    if (action !== 'options') return
    if (shown) {
      // Options again closes it
      setShown(null)
      return true
    }
    const what = cardTitle(target)
    if (!what) return
    setShown({ what, ...nameOf(target), tmdbId: what.kind === 'series' ? undefined : what.tmdbId })
    return true
  })
  // a show in the library is linked by its own number: its TMDB one is asked for
  const seriesId = shown?.what.kind === 'series' ? shown.what.id : 0
  useEffect(() => {
    if (!seriesId) return
    let gone = false
    api
      .getSeries(seriesId)
      .then((s) => !gone && setShown((cur) => (cur && cur.what.kind === 'series' && cur.what.id === seriesId ? { ...cur, tmdbId: s.tmdbId, title: cur.title || s.title } : cur)))
      .catch(() => undefined)
    return () => {
      gone = true
    }
  }, [seriesId])
  const stack = useStack()
  const navigate = useNavigate()
  const toast = useToast()
  if (!shown) return null
  const { what, title, poster, tmdbId } = shown
  const kind = what.kind === 'movie' ? 'movie' : 'tv'
  const to = what.kind === 'movie' ? `/title/${what.tmdbId}` : what.kind === 'show' ? `/show/${what.tmdbId}` : `/series/${what.id}`
  const stacked = !!tmdbId && !!stack?.some((i) => i.kind === kind && i.tmdbId === tmdbId)
  const rows: MoreRow[] = [
    { key: 'open', icon: 'open', label: t('Open'), hint: what.kind === 'movie' ? t('The film’s page') : t('The show’s page'), run: () => navigate(to) },
    ...(what.kind === 'movie' ? [{ key: 'showtime', icon: 'clock' as const, label: t('Book a showtime'), run: () => openShowtimeBooking({ tmdbId: what.tmdbId, title, posterUrl: poster }) }] : []),
    ...(tmdbId
      ? [
          {
            key: 'stack',
            icon: 'stack' as const,
            label: stacked ? t('Take it off your stack') : t('Add to stack'),
            run: () => void (stacked ? removeFromStack(kind, tmdbId) : addToStack({ kind, tmdbId, title, posterUrl: poster })).catch((e: unknown) => toast.error(e instanceof Error ? e.message : String(e))),
          },
          { key: 'watched', icon: 'check' as const, label: t('Mark watched'), hint: t('In your diary'), run: () => markWatched(kind, tmdbId).then(() => toast.success(t('Marked watched.')), (e: Error) => toast.error(e.message)) },
        ]
      : []),
    { key: 'smh', icon: 'sparkles', label: t('Show me how'), hint: t('From here'), run: openSmhMenu },
  ]
  return <MoreSheet eyebrow={title || undefined} rows={rows} onClose={() => setShown(null)} />
}
