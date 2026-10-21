import { plural, t } from './i18n.ts'

// What a rescan of a title's files found, in a sentence. A title that isn't
// monitored isn't looked for, so a file that is gone doesn't make it wanted.
export type RescanResult = { found: number; renamed: number; missing: number; skipped: number; added?: string; looked?: string; busy?: boolean; addedEpisodes?: number; notMatched?: string[]; notMatchedTotal?: number }

export function rescanMessage(r: RescanResult, monitored = true): string {
  // A show: episodes put in its folder by hand are told first, then the check
  // of the files Mediarium already knew, and the files that matched nothing.
  if (r.addedEpisodes || r.notMatched?.length) {
    const parts: string[] = []
    if (r.addedEpisodes) parts.push(plural(r.addedEpisodes, 'Found {n} episode you added yourself.', 'Found {n} episodes you added yourself.'))
    const known = knownFilesMessage(r, monitored)
    if (known && (r.found + r.renamed + r.missing + r.skipped > 0 || !r.addedEpisodes)) parts.push(known)
    if (r.notMatched?.length) {
      const total = r.notMatchedTotal || r.notMatched.length
      let names = r.notMatched.join(', ')
      if (total > r.notMatched.length) names += ' ' + t('and {n} more', { n: total - r.notMatched.length })
      parts.push(plural(total, "Couldn't match {n} video file in the show's folder to an episode: {names}.", "Couldn't match {n} video files in the show's folder to an episode: {names}.", { names }))
    }
    return parts.join(' ')
  }
  return knownFilesMessage(r, monitored)
}

function knownFilesMessage(r: RescanResult, monitored: boolean): string {
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
