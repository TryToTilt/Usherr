import { BASE_PATH, withBase } from '../base'
import { demoEnabled } from '../demo'
import { t } from '../i18n'
import { bookPaths, keptId, sharedPaths, toRemove, type KeptKind } from './offlineMath'

export { keptId, sizeText, type KeptKind } from './offlineMath'

// Keeping books on this device (Usherr Books, bookshelf-sw.js): the book's
// file (or pages, or tracks) and the few answers it needs to open are stored
// in the browser's Cache Storage, and the Books worker answers from there
// when there's no connection. Nothing is stored until a person picks "Keep on
// this device", and "Remove from this device" takes it all away again.

export const BOOKS_CACHE = 'usherr-books-files-v1' // the worker's BOOKS store
const KEPT_KEY = '/__usherr-books/kept.json' // the list of kept books, in the store itself
export const BOOKS_WORKER = withBase('/bookshelf-sw.js')
// No slash at the end: Books opens at /bookshelf as well as /bookshelf/...
const SCOPE = withBase('/bookshelf')

export interface Kept {
  id: string // "book:12:ebook", "book:12:audiobook" or "comic:40"
  kind: KeptKind
  title: string
  subtitle?: string
  cover?: string
  open: string // where it is read, under /bookshelf
  urls: string[] // what was stored for it alone
  bytes: number
  savedAt: string
}

// offlineSupport says whether books can be kept here, and if not, why.
export function offlineSupport(): { ok: boolean; why?: string } {
  if (typeof window === 'undefined' || demoEnabled()) return { ok: false }
  if (!('serviceWorker' in navigator) || !('caches' in window)) return { ok: false, why: t("This browser can't keep books for reading offline.") }
  if (!window.isSecureContext) return { ok: false, why: t('Keeping books on this device needs Usherr opened over https (or at localhost).') }
  return { ok: true }
}

// registerBooksWorker starts the Books worker (Books pages only).
export async function registerBooksWorker(): Promise<ServiceWorkerRegistration | null> {
  if (!offlineSupport().ok) return null
  try {
    return await navigator.serviceWorker.register(BOOKS_WORKER, { scope: SCOPE })
  } catch {
    return null
  }
}

async function readKept(cache: Cache): Promise<Kept[]> {
  const hit = await cache.match(KEPT_KEY)
  if (!hit) return []
  try {
    const list = (await hit.json()) as Kept[]
    return Array.isArray(list) ? list : []
  } catch {
    return []
  }
}

async function writeKept(cache: Cache, list: Kept[]) {
  await cache.put(KEPT_KEY, new Response(JSON.stringify(list), { headers: { 'Content-Type': 'application/json' } }))
}

// keptBooks lists what is kept on this device.
export async function keptBooks(): Promise<Kept[]> {
  if (!('caches' in window)) return []
  try {
    return await readKept(await caches.open(BOOKS_CACHE))
  } catch {
    return []
  }
}

// storeUrl fetches an address and stores it; it returns the size.
async function storeUrl(cache: Cache, url: string): Promise<number> {
  const res = await fetch(url, { credentials: 'include', cache: 'no-store' })
  if (!res.ok) throw new Error(res.status === 404 ? t("A part of this book isn't on the server any more.") : t('The server said no (error {status}).', { status: String(res.status) }))
  const blob = await res.blob()
  const headers = new Headers()
  for (const h of ['Content-Type', 'Content-Disposition', 'Last-Modified', 'ETag']) {
    const v = res.headers.get(h)
    if (v) headers.set(h, v)
  }
  headers.set('Content-Length', String(blob.size))
  await cache.put(url, new Response(blob, { status: 200, headers }))
  return blob.size
}

// appFiles are the built scripts and styles this page has loaded.
function appFiles(): string[] {
  const out = new Set<string>()
  for (const e of performance.getEntriesByType('resource')) {
    const u = new URL(e.name, location.href)
    if (u.origin === location.origin && u.pathname.startsWith(BASE_PATH + '/assets/')) out.add(u.pathname)
  }
  for (const el of document.querySelectorAll<HTMLScriptElement | HTMLLinkElement>('script[src], link[rel="stylesheet"], link[rel="modulepreload"]')) {
    const raw = el instanceof HTMLScriptElement ? el.src : el.href
    const u = new URL(raw, location.href)
    if (u.origin === location.origin && u.pathname.startsWith(BASE_PATH + '/assets/')) out.add(u.pathname)
  }
  return [...out]
}

// activeWorker waits for a registration's worker to be running (a new one
// installs first). A page outside /bookshelf/ (a comic series) isn't served by
// it, so navigator.serviceWorker.ready can't be used there.
function activeWorker(reg: ServiceWorkerRegistration): Promise<ServiceWorker | null> {
  if (reg.active) return Promise.resolve(reg.active)
  const w = reg.installing ?? reg.waiting
  if (!w) return Promise.resolve(null)
  return new Promise((resolve) => {
    const timer = window.setTimeout(() => resolve(reg.active), 15000)
    w.addEventListener('statechange', () => {
      if (w.state === 'activated' || w.state === 'redundant') {
        window.clearTimeout(timer)
        resolve(reg.active)
      }
    })
  })
}

// keepAppFiles asks the Books worker to store the app's files. warm loads
// the parts a kind of book needs first, so they are among them.
async function keepAppFiles(reg: ServiceWorkerRegistration, warm: () => Promise<unknown>) {
  await warm().catch(() => undefined)
  const worker = await activeWorker(reg)
  if (!worker) throw new Error(t("The part of Usherr that keeps books offline didn't start. Reload the page and try again."))
  await new Promise<void>((resolve) => {
    const done = (e: MessageEvent) => {
      if ((e.data as { type?: string } | null)?.type === 'shell-kept') {
        navigator.serviceWorker.removeEventListener('message', done)
        resolve()
      }
    }
    navigator.serviceWorker.addEventListener('message', done)
    worker.postMessage({ type: 'keep-shell', urls: appFiles() })
    window.setTimeout(() => {
      navigator.serviceWorker.removeEventListener('message', done)
      resolve()
    }, 20000)
  })
}

// thisBuild names this version of Usherr: the page's own scripts and styles,
// whose names change with their contents.
function thisBuild(): string {
  const parts = [...document.querySelectorAll<HTMLScriptElement | HTMLLinkElement>('script[src*="/assets/"], link[href*="/assets/"]')].map((el) =>
    el instanceof HTMLScriptElement ? new URL(el.src).pathname : new URL(el.href).pathname,
  )
  return parts.sort().join(' ')
}

// refreshAppFiles stores the app's files again after Usherr was updated, so
// the books already kept still open offline: the worker drops the old
// version's files when a new one loads. warmFor loads what each kind needs.
export async function refreshAppFiles(reg: ServiceWorkerRegistration | null, warmFor: (k: KeptKind) => () => Promise<unknown>) {
  if (!reg || !navigator.onLine) return
  const build = thisBuild()
  let seen = ''
  try {
    seen = localStorage.getItem('usherr.books.build') ?? ''
  } catch {
    seen = ''
  }
  if (!build || build === seen) return
  const kept = await keptBooks()
  if (kept.length === 0) return
  const kinds = [...new Set(kept.map((k) => k.kind))]
  try {
    await keepAppFiles(reg, () => Promise.all(kinds.map((k) => warmFor(k)())))
    localStorage.setItem('usherr.books.build', build)
  } catch {
    // tried again next time
  }
}

// keepBook stores one book or issue. progress is told (done, total) as the
// parts come in. The parts already stored for something else are shared.
export async function keepBook(
  item: { kind: KeptKind; id: number; title: string; subtitle?: string; cover?: string; open: string; count?: number },
  warm: () => Promise<unknown>,
  progress?: (done: number, total: number) => void,
): Promise<Kept> {
  const ok = offlineSupport()
  if (!ok.ok) throw new Error(ok.why ?? t("This browser can't keep books for reading offline."))
  const reg = await registerBooksWorker()
  if (!reg) throw new Error(t("This browser won't let Usherr keep books offline here (a private window or a blocked setting)."))
  // Ask the browser not to clear the books when space runs low.
  await navigator.storage?.persist?.().catch(() => false)
  await keepAppFiles(reg, warm)
  try {
    localStorage.setItem('usherr.books.build', thisBuild())
  } catch {
    // only means the files are stored again next time
  }
  const cache = await caches.open(BOOKS_CACHE)
  const shared = sharedPaths.map(withBase)
  const own = bookPaths(item.kind, item.id, item.count ?? 0, withBase)
  const cover = item.cover && item.cover.startsWith(withBase('/api/')) ? [item.cover] : []
  const all = [...shared, ...own, ...cover]
  let done = 0
  let bytes = 0
  const stored: string[] = []
  progress?.(0, all.length)
  try {
    // A few at a time: a comic has dozens of pages.
    const queue = [...all]
    const work = async () => {
      for (let u = queue.shift(); u; u = queue.shift()) {
        bytes += await storeUrl(cache, u)
        stored.push(u)
        done++
        progress?.(done, all.length)
      }
    }
    await Promise.all([work(), work(), work(), work()])
  } catch (e) {
    // Half a book is no use: what this one alone added goes again.
    const list = await readKept(cache)
    await Promise.all(toRemove(stored.filter((u) => !shared.includes(u)), list.map((k) => k.urls)).map((u) => cache.delete(u)))
    throw e
  }
  const kept: Kept = {
    id: keptId(item.kind, item.id),
    kind: item.kind,
    title: item.title,
    subtitle: item.subtitle,
    cover: cover[0] ?? item.cover,
    open: item.open,
    urls: [...own, ...cover],
    bytes,
    savedAt: new Date().toISOString(),
  }
  const list = (await readKept(cache)).filter((k) => k.id !== kept.id)
  await writeKept(cache, [...list, kept])
  return kept
}

// unkeep removes a kept book from this device. Parts another kept book
// still uses stay; with nothing left kept, the shared answers go too.
export async function unkeep(id: string): Promise<void> {
  const cache = await caches.open(BOOKS_CACHE)
  const list = await readKept(cache)
  const gone = list.find((k) => k.id === id)
  const rest = list.filter((k) => k.id !== id)
  if (gone) await Promise.all(toRemove(gone.urls, rest.map((k) => k.urls)).map((u) => cache.delete(u)))
  if (rest.length === 0) {
    await caches.delete(BOOKS_CACHE)
    return
  }
  await writeKept(cache, rest)
}

// forgetAllBooks removes every kept book and the Books worker's stores: on
// signing out, so the next person on this device finds nothing.
export async function forgetAllBooks(): Promise<void> {
  if (!('caches' in window)) return
  try {
    const names = await caches.keys()
    await Promise.all(names.filter((n) => n.startsWith('usherr-books-')).map((n) => caches.delete(n)))
    // Places read offline and not sent yet belong to this account too.
    localStorage.removeItem('usherr.books.pending')
  } catch {
    // nothing kept, or storage blocked
  }
}

// storageUsed is what this site stores in all, and how much it may.
export async function storageUsed(): Promise<{ usage: number; quota: number } | null> {
  try {
    const e = await navigator.storage?.estimate?.()
    return e ? { usage: e.usage ?? 0, quota: e.quota ?? 0 } : null
  } catch {
    return null
  }
}

// updateStored changes a stored answer in place (a book's place after reading
// offline), when one is stored.
export async function updateStored(url: string, body: unknown): Promise<void> {
  if (!('caches' in window)) return
  try {
    const cache = await caches.open(BOOKS_CACHE)
    if (!(await cache.match(url))) return
    await cache.put(url, new Response(JSON.stringify(body), { headers: { 'Content-Type': 'application/json' } }))
  } catch {
    // only the stored copy is behind; the server has the place once online
  }
}
