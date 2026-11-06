// The parts of keeping books offline that need no browser (offline.ts,
// progress.ts), so the tests can run them (offline.test.ts).

// What can be kept: a book's ebook (an EPUB, a Kindle book read as EPUB, a
// PDF, a comic of pictures), its audiobook, or an issue of the Comics library.
export type KeptKind = 'epub' | 'kindle' | 'pdf' | 'comic' | 'audiobook' | 'issue'

export function keptId(kind: KeptKind, id: number): string {
  return kind === 'issue' ? `comic:${id}` : kind === 'audiobook' ? `book:${id}:audiobook` : `book:${id}:ebook`
}

// sharedPaths are the answers every kept book needs to open offline: the
// server's state, the account, and the shelf.
export const sharedPaths = ['/api/onboarding/status', '/api/auth/me', '/api/modules', '/api/books?brief=1', '/api/books/progress']

// bookPaths are the addresses to store for one book or issue. n is a comic's
// page count or an audiobook's number of tracks. base puts the app's base
// address in front.
export function bookPaths(kind: KeptKind, id: number, n: number, base: (p: string) => string): string[] {
  const b = (p: string) => base(`/api/books/${id}${p}`)
  const list = (count: number) => Array.from({ length: Math.max(0, count) }, (_, i) => i + 1)
  switch (kind) {
    case 'epub':
    case 'pdf':
      return [b(''), b('/progress?format=ebook'), b('/marks'), b('/read')]
    case 'kindle':
      return [b(''), b('/progress?format=ebook'), b('/marks'), b('/read?as=epub')]
    case 'comic':
      return [b(''), b('/progress?format=ebook'), b('/marks'), b('/comic'), ...list(n).map((p) => b(`/comic/${p}`))]
    case 'audiobook':
      return [b(''), b('/progress?format=audiobook'), b('/tracks'), ...list(n).map((p) => b(`/listen/${p}`))]
    case 'issue': {
      // The comic reader asks for these without the base (comicsApi.ts).
      const c = (p: string) => `/api/comics/issues/${id}${p}`
      return [c('/pages'), c('/marks'), `/api/home/position?item=comic:${id}`, ...list(n).map((p) => c(`/pages/${p}`))]
    }
  }
}

// stillUsed are the addresses of the books that stay kept, so removing one
// book never takes a part another one needs.
export function toRemove(gone: string[], staying: string[][]): string[] {
  const used = new Set(staying.flat())
  return gone.filter((u) => !used.has(u))
}

export function sizeText(bytes: number): string {
  if (bytes >= 1 << 30) return `${(bytes / (1 << 30)).toFixed(1)} GB`
  if (bytes >= 1 << 20) return `${Math.round(bytes / (1 << 20))} MB`
  return `${Math.max(1, Math.round(bytes / 1024))} KB`
}

// A place that couldn't reach the server (read offline), kept in this browser
// until it can. at is when it was read there (ms), so a place saved later on
// another device is never replaced by an older one from here.
export interface Pending {
  kind: 'book' | 'comic'
  id: number
  format?: 'ebook' | 'audiobook'
  body: { format: 'ebook' | 'audiobook'; position: string; percent: number; finished?: boolean } | { item: string; position: number; duration: number }
  at: number
}

export function pendingKey(p: Pick<Pending, 'kind' | 'id' | 'format'>): string {
  return p.kind === 'comic' ? `comic:${p.id}` : `book:${p.id}:${p.format}`
}

// addPending keeps a place to send later; a newer one for the same book replaces it.
export function addPending(all: Record<string, Pending>, p: Pending): Record<string, Pending> {
  const k = pendingKey(p)
  const old = all[k]
  if (old && old.at > p.at) return all
  return { ...all, [k]: p }
}

// newerOnServer says whether the server's place was saved after this one was
// read here, so this one must not replace it.
export function newerOnServer(serverUpdatedAt: string | undefined, at: number): boolean {
  if (!serverUpdatedAt) return false
  const t = Date.parse(serverUpdatedAt)
  return Number.isFinite(t) && t > at
}
