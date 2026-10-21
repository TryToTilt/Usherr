import type { ReactNode } from 'react'
import type { MovieReleaseDates } from './api'
import { locale, plural, t } from './i18n'

// "12 Mar 2025", in the person's language.
export function dayLabel(date: string): string {
  const d = new Date(`${date}T00:00:00`)
  if (Number.isNaN(d.getTime())) return date
  return d.toLocaleDateString(locale(), { day: 'numeric', month: 'short', year: 'numeric' })
}

// "in 3 weeks" for a day still ahead; nothing for today's or an earlier date.
// Today and tomorrow are said in words.
export function untilLabel(date: string, now: Date = new Date()): string {
  const d = new Date(`${date}T00:00:00`)
  if (Number.isNaN(d.getTime())) return ''
  const today = new Date(now.getFullYear(), now.getMonth(), now.getDate())
  const days = Math.round((d.getTime() - today.getTime()) / 86400000)
  if (days < 0) return ''
  if (days === 0) return t('today')
  if (days === 1) return t('tomorrow')
  if (days < 14) return plural(days, 'in {n} day', 'in {n} days')
  if (days < 60) return plural(Math.round(days / 7), 'in {n} week', 'in {n} weeks')
  return plural(Math.round(days / 30), 'in {n} month', 'in {n} months')
}

// One date of the film: the day, when it is ahead how long until then, and the
// country it comes from when that is not the person's own.
function DateValue({ date, from }: { date?: string; from?: string }): ReactNode {
  if (!date) return <span className="release-none">{t('Not announced')}</span>
  const until = untilLabel(date)
  return (
    <span className="release-date">
      {dayLabel(date)}
      {from ? ` (${from})` : ''}
      {until && <span className="release-until"> · {until}</span>}
    </span>
  )
}

// The facts "In cinemas" and "Digital" for the title block of a movie page.
// A film with no cinema date from TMDB's list falls back to its main release
// date; an old film without a digital date leaves that fact out rather than
// saying it is not announced.
export function releaseFacts(dates: MovieReleaseDates | undefined, releaseDate?: string): { label: string; value: ReactNode }[] {
  const cinema = dates?.cinema || releaseDate || ''
  const out: { label: string; value: ReactNode }[] = []
  if (cinema || dates) out.push({ label: t('In cinemas'), value: <DateValue date={cinema} from={dates?.cinema ? dates.cinemaRegion : undefined} /> })
  const old = cinema && new Date(`${cinema}T00:00:00`).getTime() < Date.now() - 2 * 365 * 86400000
  if (dates && (dates.digital || !old)) out.push({ label: t('Digital'), value: <DateValue date={dates.digital} from={dates.digitalRegion} /> })
  return out
}
