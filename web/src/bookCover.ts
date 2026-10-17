import { t } from './i18n.ts'

// Changing a book's cover: what the page checks before it sends anything. The
// server checks again and has the last word (it opens the picture to be sure
// it is one), so this only saves a long upload and gives a clear message early.

// The same limit the server applies.
export const MAX_COVER_BYTES = 8 * 1024 * 1024
const MAX_LINK_LENGTH = 2048

export type CoverKind = 'jpeg' | 'png' | 'webp' | ''

// What kind of picture a chosen file claims to be, by its type and else by the
// end of its name. Only JPEG, PNG and WebP are offered.
export function coverKind(f: { type: string; name: string }): CoverKind {
  const type = f.type.toLowerCase()
  if (type === 'image/jpeg' || type === 'image/jpg') return 'jpeg'
  if (type === 'image/png') return 'png'
  if (type === 'image/webp') return 'webp'
  if (type) return ''
  const m = /\.([a-z0-9]+)$/i.exec(f.name)
  const ext = m ? m[1].toLowerCase() : ''
  if (ext === 'jpg' || ext === 'jpeg') return 'jpeg'
  if (ext === 'png') return 'png'
  if (ext === 'webp') return 'webp'
  return ''
}

// "" when the file can be sent, else the sentence to show.
export function coverFileProblem(f: { size: number; type: string; name: string } | null | undefined): string {
  if (!f) return t('Choose a picture.')
  if (f.size === 0) return t('That file is empty.')
  if (coverKind(f) === '') return t('Choose a JPEG, PNG or WebP picture.')
  if (f.size > MAX_COVER_BYTES) return t('That picture is bigger than 8 MB. Use a smaller one.')
  return ''
}

// The server reads JPEG and PNG, so a WebP picture is turned into a JPEG in
// the browser before it is sent.
export function needsConverting(f: { type: string; name: string }): boolean {
  return coverKind(f) === 'webp'
}

// "" when the link can be sent, else the sentence to show.
export function coverLinkProblem(link: string): string {
  const l = link.trim()
  if (!l) return t('Paste the link of a picture.')
  if (l.length > MAX_LINK_LENGTH) return t('That link is too long.')
  if (!/^https?:\/\/[^\s/]+\S*$/i.test(l)) return t('Paste a link that starts with http:// or https://.')
  return ''
}
