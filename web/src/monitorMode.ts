import type { MonitorChange, MonitorMode } from './api'
import { plural, t } from './i18n.ts'

// What the confirmation says before a show's monitoring changes: how many
// episodes stop being monitored (and, for "only what hasn't aired yet", how
// many start again). Nothing here deletes a file.
export function monitorModeSentence(mode: MonitorMode, change: MonitorChange): string {
  const parts: string[] = []
  if (change.stopped > 0) {
    parts.push(
      mode === 'future'
        ? plural(change.stopped, 'This stops monitoring {n} episode that has already aired.', 'This stops monitoring {n} episodes that have already aired.')
        : plural(change.stopped, 'This stops monitoring {n} episode.', 'This stops monitoring {n} episodes.'),
    )
  }
  if (change.started > 0) {
    parts.push(
      mode === 'future'
        ? plural(change.started, 'It also starts monitoring {n} episode that has not aired yet.', 'It also starts monitoring {n} episodes that have not aired yet.')
        : plural(change.started, 'This starts monitoring {n} episode.', 'This starts monitoring {n} episodes.'),
    )
  }
  parts.push(t('Nothing is deleted.'))
  return parts.join(' ')
}

// The message after the change went through.
export function monitorModeDone(mode: MonitorMode): string {
  if (mode === 'all') return t('All episodes are monitored now.')
  if (mode === 'future') return t("Only episodes that haven't aired yet are monitored now.")
  return t('No episodes are monitored now.')
}
