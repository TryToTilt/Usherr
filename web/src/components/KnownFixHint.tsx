import { useEffect, useState } from 'react'
import { api } from '../api'
import { t } from '../i18n'
import { fixedInNewerVersion } from '../knownFixes'

let running: Promise<string> | null = null
function runningVersion(): Promise<string> {
  running ??= api
    .version()
    .then((v) => v.version)
    .catch(() => {
      running = null
      return ''
    })
  return running
}

// A line under an error that a newer version already fixed. Shows nothing for
// other errors, or once the running version has the fix.
export default function KnownFixHint({ error }: { error?: string | null }) {
  const [version, setVersion] = useState('')
  const wanted = !!error && fixedInNewerVersion(error, '0.0.0') !== ''
  useEffect(() => {
    if (wanted) void runningVersion().then(setVersion)
  }, [wanted])
  const fixedIn = fixedInNewerVersion(error, version)
  if (!fixedIn) return null
  return <small className="indexer-health known-fix">{t('This is fixed in {fixedIn}. Update Mediarium and press Try again.', { fixedIn })}</small>
}
