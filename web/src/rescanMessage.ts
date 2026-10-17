import { t } from './i18n.ts'

// What a rescan of a title's files found, in a sentence. A title that isn't
// monitored isn't looked for, so a file that is gone doesn't make it wanted.
export function rescanMessage(r: { found: number; renamed: number; missing: number; skipped: number; added?: string; looked?: string; busy?: boolean }, monitored = true): string {
  if (r.added) return t('Found {file} and added it.', { file: r.added })
  if (r.busy) return t('This movie is downloading right now, so nothing was changed.')
  if (r.looked) return t('No video file found in {folder}.', { folder: r.looked })
  if (r.skipped > 0 && r.found + r.renamed + r.missing === 0) {
    return t('The library folder looks empty or not available, so nothing was changed. Is the drive plugged in?')
  }
  const parts = [t('{n} fine', { n: r.found })]
  if (r.renamed > 0) parts.push(t('{n} found under a new name', { n: r.renamed }))
  if (r.missing > 0) parts.push(monitored ? t('{n} gone, wanted again', { n: r.missing }) : t('{n} gone', { n: r.missing }))
  if (r.skipped > 0) parts.push(t('{n} left alone because the library folder looks unavailable', { n: r.skipped }))
  return t('Checked the files: {what}.', { what: parts.join(', ') })
}
