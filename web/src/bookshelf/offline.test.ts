import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { test } from 'node:test'
import vm from 'node:vm'
import { addPending, bookPaths, keptId, newerOnServer, sizeText, toRemove, type Pending } from './offlineMath.ts'

// The Books worker's rules (public/bookshelf-sw-rules.js), loaded the way the
// worker loads them: a plain script that sets self.BookshelfRules.
interface Rules {
  kind: (method: string, url: URL, mode: string, base: string) => string
  key: (url: URL, base: string) => string
  range: (header: string | null, size: number) => [number, number] | null
  shellOf: (html: string) => string
}
function loadRules(): Rules {
  const code = readFileSync(new URL('../../public/bookshelf-sw-rules.js', import.meta.url), 'utf8')
  const sandbox: { self: { BookshelfRules?: Rules } } = { self: {} }
  vm.runInNewContext(code, sandbox)
  return sandbox.self.BookshelfRules!
}
const rules = loadRules()
const u = (p: string) => new URL(p, 'https://books.example')

test('the worker answers only Books pages, built files and kept-book requests', () => {
  const k = (p: string, method = 'GET', mode = 'cors', base = '') => rules.kind(method, u(p), mode, base)
  assert.equal(k('/bookshelf/read/5', 'GET', 'navigate'), 'page')
  assert.equal(k('/bookshelf', 'GET', 'navigate'), 'page')
  assert.equal(k('/library', 'GET', 'navigate'), '', 'the rest of Usherr is not the worker’s')
  assert.equal(k('/assets/index-abc.js'), 'asset')
  assert.equal(k('/api/books/5/read'), 'file')
  assert.equal(k('/api/books/5/read?as=epub'), 'file')
  assert.equal(k('/api/books/5/comic/12'), 'file')
  assert.equal(k('/api/books/5/listen/2'), 'file')
  assert.equal(k('/api/comics/issues/9/pages/3?w=1200'), 'file')
  assert.equal(k('/api/books/5'), 'data')
  assert.equal(k('/api/books/5/progress?format=ebook'), 'data')
  assert.equal(k('/api/auth/me'), 'data')
  assert.equal(k('/api/home/position?item=comic:9'), 'data')
  // Never: changes, settings, other answers.
  assert.equal(k('/api/books/5/progress', 'PUT'), '')
  assert.equal(k('/api/books/5/marks', 'POST'), '')
  assert.equal(k('/api/settings'), '')
  assert.equal(k('/api/queue'), '')
  assert.equal(k('/api/books/5/releases'), '')
  assert.equal(k('/api/comics/issues/9/file'), '')
  assert.equal(k('/push-sw.js'), '')
  // Under a base address only the app's own addresses count.
  assert.equal(k('/m/api/books/5/read', 'GET', 'cors', '/m'), 'file')
  assert.equal(k('/m/bookshelf/', 'GET', 'navigate', '/m'), 'page')
  assert.equal(k('/other/api/books/5/read', 'GET', 'cors', '/m'), '')
})

test('a comic page is stored once whatever width is asked; other keys keep their query', () => {
  assert.equal(rules.key(u('/api/comics/issues/9/pages/3?w=1200'), ''), 'https://books.example/api/comics/issues/9/pages/3')
  assert.equal(rules.key(u('/api/comics/issues/9/pages/3'), ''), 'https://books.example/api/comics/issues/9/pages/3')
  assert.equal(rules.key(u('/api/books/5/read?as=epub'), ''), 'https://books.example/api/books/5/read?as=epub')
  assert.equal(rules.key(u('/api/home/position?item=comic:9'), ''), 'https://books.example/api/home/position?item=comic:9')
})

test('audio ranges are read the way players ask for them', () => {
  assert.deepEqual(Array.from(rules.range('bytes=0-', 1000) ?? []), [0, 999])
  assert.deepEqual(Array.from(rules.range('bytes=100-199', 1000) ?? []), [100, 199])
  assert.deepEqual(Array.from(rules.range('bytes=900-5000', 1000) ?? []), [900, 999])
  assert.deepEqual(Array.from(rules.range('bytes=-100', 1000) ?? []), [900, 999])
  assert.equal(rules.range('bytes=1000-', 1000), null)
  assert.equal(rules.range('bytes=5-2', 1000), null)
  assert.equal(rules.range('items=0-1', 1000), null)
  assert.equal(rules.range(null, 1000), null)
})

test('the build is named after the whole page', () => {
  const page = (preload: string) => `<script type="module" crossorigin src="/m/assets/index-D-oFxjvl.js"></script><link rel="modulepreload" href="/m/assets/${preload}.js">`
  const a = rules.shellOf(page('start-aaa'))
  assert.match(a, /^[0-9a-z]+$/)
  assert.equal(rules.shellOf(page('start-aaa')), a, 'the same page, the same build')
  assert.notEqual(rules.shellOf(page('start-bbb')), a, 'another part changed: a new build, though the main script is the same')
  assert.equal(rules.shellOf('<html></html>'), '')
})

test('what is stored for each kind of book', () => {
  const base = (p: string) => '/m' + p
  assert.deepEqual(bookPaths('epub', 5, 0, base), ['/m/api/books/5', '/m/api/books/5/progress?format=ebook', '/m/api/books/5/marks', '/m/api/books/5/read'])
  assert.ok(bookPaths('kindle', 5, 0, base).includes('/m/api/books/5/read?as=epub'))
  const comic = bookPaths('comic', 5, 3, base)
  assert.deepEqual(comic.slice(-3), ['/m/api/books/5/comic/1', '/m/api/books/5/comic/2', '/m/api/books/5/comic/3'])
  const audio = bookPaths('audiobook', 5, 2, base)
  assert.ok(audio.includes('/m/api/books/5/progress?format=audiobook') && audio.includes('/m/api/books/5/listen/2') && !audio.includes('/m/api/books/5/listen/3'))
  const issue = bookPaths('issue', 9, 2, base)
  assert.deepEqual(issue, ['/api/comics/issues/9/pages', '/api/comics/issues/9/marks', '/api/home/position?item=comic:9', '/api/comics/issues/9/pages/1', '/api/comics/issues/9/pages/2'])
  const volume = bookPaths('volume', 7, 2, base)
  assert.deepEqual(volume, ['/api/manga/volumes/7/pages', '/api/manga/volumes/7/marks', '/api/home/position?item=manga:v:7', '/api/manga/volumes/7/pages/1', '/api/manga/volumes/7/pages/2'])
  // Every address is one the worker answers.
  for (const p of [...comic, ...audio, ...issue, ...volume]) assert.notEqual(rules.kind('GET', u(p), 'cors', p.startsWith('/m/') ? '/m' : ''), '', p)
  // A page is the same at any width: the reader asks for a copy sized to the screen.
  assert.equal(rules.key(u('/api/manga/volumes/7/pages/1?w=800'), ''), rules.key(u('/api/manga/volumes/7/pages/1'), ''))
  assert.equal(keptId('volume', 7), 'manga:7')
  assert.equal(keptId('issue', 9), 'comic:9')
  assert.equal(keptId('pdf', 5), 'book:5:ebook')
  assert.equal(keptId('audiobook', 5), 'book:5:audiobook')
})

test('removing a book keeps the parts another kept book needs', () => {
  assert.deepEqual(toRemove(['/a', '/b', '/c'], [['/b'], ['/x']]), ['/a', '/c'])
  assert.deepEqual(toRemove(['/a'], []), ['/a'])
})

test('places read offline: the newest wins, and a newer one on the server is kept', () => {
  const p = (at: number, position: string): Pending => ({ kind: 'book', id: 5, format: 'ebook', body: { format: 'ebook', position, percent: 10 }, at })
  let all = addPending({}, p(100, 'a'))
  all = addPending(all, p(300, 'c'))
  all = addPending(all, p(200, 'b')) // older than the one waiting
  assert.equal(Object.keys(all).length, 1)
  assert.equal((all['book:5:ebook'].body as { position: string }).position, 'c')
  all = addPending(all, { kind: 'comic', id: 9, body: { item: 'comic:9', position: 4, duration: 20 }, at: 50 })
  assert.equal(Object.keys(all).length, 2)

  const read = Date.parse('2026-11-06T10:00:00Z')
  assert.equal(newerOnServer('2026-11-06T11:00:00Z', read), true)
  assert.equal(newerOnServer('2026-11-06T09:00:00Z', read), false)
  assert.equal(newerOnServer(undefined, read), false)
  assert.equal(newerOnServer('not a date', read), false)
})

test('sizes read plainly', () => {
  assert.equal(sizeText(500), '1 KB')
  assert.equal(sizeText(5 * 1024 * 1024), '5 MB')
  assert.equal(sizeText(3 * 1024 * 1024 * 1024), '3.0 GB')
})
