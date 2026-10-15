// Home fork: the server names a download's steps in English ("Unpacking");
// these are the ones it uses, in the reader's language.
import { t } from '../i18n.ts'

export function stageName(stage: string): string {
  const names: Record<string, string> = {
    Starting: t('Starting'),
    Downloading: t('Downloading'),
    Unpacking: t('Unpacking'),
    'Checking files, repairing if needed': t('Checking files, repairing if needed'),
    'Checking the downloaded files': t('Checking the downloaded files'),
    'Moving into the library': t('Moving into the library'),
    'Repairing damaged files': t('Repairing damaged files'),
  }
  return names[stage] ?? stage
}
