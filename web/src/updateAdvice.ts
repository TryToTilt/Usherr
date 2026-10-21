import { compareVersions } from './whatsNew.ts'

// The newest release the update check knows about, when it is newer than the
// running version. Empty when up to date or the check has not run.
export function newerVersion(running: string, latest: string | undefined | null): string {
  if (!running || !latest) return ''
  return compareVersions(latest, running) > 0 ? latest.replace(/^v/, '') : ''
}
