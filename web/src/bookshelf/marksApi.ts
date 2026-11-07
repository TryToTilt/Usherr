import { withBase } from '../base'
import { t } from '../i18n'

// Bookmarks and highlights (reading_marks.go). item is "book:<id>" for an ebook
// of the Books library, "audio:<id>" for the same book's audiobook, or
// "comic:<issue id>" for the Comics library.
export type MarkKind = 'bookmark' | 'highlight'
export type MarkColor = '' | 'yellow' | 'green' | 'blue' | 'pink'

export interface ReadingMark {
  id: number
  item: string
  kind: MarkKind
  position: string // an EPUB CFI (a range for a highlight), "page:<n>" or an audiobook's "track:seconds"
  label: string
  excerpt: string
  note: string
  color: MarkColor
  percent: number
  createdAt: string
  updatedAt: string
}

export interface NewMark {
  kind: MarkKind
  position: string
  label?: string
  excerpt?: string
  note?: string
  color?: MarkColor
  percent?: number
}

export const MARK_COLORS: MarkColor[] = ['yellow', 'green', 'blue', 'pink']

async function call<T>(method: string, path: string, body?: unknown): Promise<T> {
  let res: Response
  try {
    res = await fetch(withBase('/api' + path), {
      method,
      credentials: 'include',
      cache: 'no-store',
      headers: body !== undefined ? { 'Content-Type': 'application/json' } : undefined,
      body: body !== undefined ? JSON.stringify(body) : undefined,
    })
  } catch {
    throw new Error(t("Usherr can't be reached right now. Try again when you're back online."))
  }
  if (res.status === 204) return undefined as T
  const data = (await res.json().catch(() => null)) as (T & { error?: string }) | null
  if (!res.ok) throw new Error(data?.error || t('Something went wrong on the server (error {status}).', { status: String(res.status) }))
  return data as T
}

// The address of an item's marks: a book, or a comic issue.
function itemPath(item: string): string {
  const [kind, id] = item.split(':')
  if (kind === 'audio') return `/books/${id}/marks?format=audiobook`
  if (kind === 'manga') return `/manga/volumes/${item.split(':')[2]}/marks`
  return kind === 'comic' ? `/comics/issues/${id}/marks` : `/books/${id}/marks`
}

export const marksApi = {
  list: (item: string) => call<ReadingMark[]>('GET', itemPath(item)),
  add: (item: string, m: NewMark) => call<ReadingMark>('POST', itemPath(item), m),
  update: (id: number, p: { note?: string; color?: MarkColor; label?: string }) => call<ReadingMark>('PUT', `/reading-marks/${id}`, p),
  remove: (id: number) => call<void>('DELETE', `/reading-marks/${id}`),
  series: (seriesId: number) => call<ReadingMark[]>('GET', `/comics/series/${seriesId}/marks`),
}

// pageOfMark is the page a "page:<n>" mark is on (1 when it has none).
export function pageOfMark(position: string): number {
  const m = /^page:(\d+)$/.exec(position)
  return m ? Math.max(1, Number(m[1])) : 1
}
