import { t } from './i18n.ts'

// The sentence behind the "Folder for its files" tip on a movie's or a show's
// page: the path is the one Mediarium sees, which in Docker is not the one on
// the host, and what Rescan files does with files put there by hand.
export function folderHint(kind: 'movie' | 'series'): string {
  const where = t('This is the path as Mediarium sees it inside its container. With Docker it can differ from the path on your computer or NAS: look at the volume mapping to find the matching folder.')
  const rescan =
    kind === 'movie'
      ? t('Put a video file in this folder, then press Rescan files and it is added to the movie.')
      : t('Rescan files checks the files Mediarium already knows about. To add episodes you pasted in by hand, use Import existing on the Library page.')
  return `${where}\n${rescan}`
}
