// What the Books worker (bookshelf-sw.js) does with each request. Kept apart,
// with no browser-only calls, so the tests can load it too
// (src/bookshelf/offline.test.ts).
//
// Only these requests are ever answered from the device:
//   "asset"  the app's own built files (/assets/..., named after their
//            contents, so a stored copy is never out of date);
//   "file"   a kept book's file, pages and tracks: stored when the person
//            chose "Keep on this device", answered from the device first;
//   "data"   the few answers a kept book needs to open (the account, the book,
//            its place and bookmarks): always asked of the server first, and
//            the stored copy used only when the server can't be reached.
// Everything else, and every request that isn't a GET, goes to the network
// as if there were no worker.
;(function (root) {
  var FILE = [
    /^\/api\/books\/\d+\/read$/, // an ebook, or a Kindle book's EPUB copy (?as=epub)
    /^\/api\/books\/\d+\/comic\/\d+$/, // a page of a comic in the Books library
    /^\/api\/books\/\d+\/listen\/\d+$/, // an audiobook track
    /^\/api\/comics\/issues\/\d+\/pages\/\d+$/, // a page of a Comics library issue
    /^\/api\/manga\/volumes\/\d+\/pages\/\d+$/, // a page of a manga volume
  ]
  var DATA = [
    /^\/api\/onboarding\/status$/,
    /^\/api\/auth\/me$/,
    /^\/api\/modules$/,
    /^\/api\/books$/,
    /^\/api\/books\/progress$/,
    /^\/api\/books\/\d+$/,
    /^\/api\/books\/\d+\/(progress|marks|tracks|comic|file-cover|own-cover)$/,
    /^\/api\/comics\/issues\/\d+\/(pages|marks)$/,
    /^\/api\/manga\/volumes\/\d+\/(pages|marks)$/,
    /^\/api\/home\/position$/,
  ]

  // A page of a Comics library issue or a manga volume is the same page at any width (?w=).
  var ANY_WIDTH = /^\/api\/(comics\/issues|manga\/volumes)\/\d+\/pages\/\d+$/

  // rel is a path without the app's base ("/mediarium"), or null when the
  // address isn't the app's.
  function rel(pathname, base) {
    if (!base) return pathname
    if (pathname === base || pathname.indexOf(base + '/') === 0) return pathname.slice(base.length) || '/'
    return null
  }

  function matches(list, path) {
    for (var i = 0; i < list.length; i++) if (list[i].test(path)) return true
    return false
  }

  // kind says what a request is: "page" (a Books page being opened), "asset",
  // "file", "data", or "" for one the worker leaves alone.
  function kind(method, url, mode, base) {
    if (method !== 'GET') return ''
    var path = rel(url.pathname, base)
    if (path === null) return ''
    if (mode === 'navigate') return path === '/bookshelf' || path.indexOf('/bookshelf/') === 0 ? 'page' : ''
    if (path.indexOf('/assets/') === 0) return 'asset'
    if (matches(FILE, path)) return 'file'
    if (matches(DATA, path)) return 'data'
    return ''
  }

  // key is the address a request is stored under: the same as asked for,
  // except a comic page, which is stored once whatever width is asked.
  function key(url, base) {
    var path = rel(url.pathname, base)
    if (path !== null && ANY_WIDTH.test(path)) return url.origin + url.pathname
    return url.origin + url.pathname + url.search
  }

  // range reads a "bytes=" Range header against a size: [start, end] (both
  // included), or null when it can't be served (the whole file is sent).
  function range(header, size) {
    var m = /^bytes=(\d*)-(\d*)$/.exec((header || '').trim())
    if (!m || size <= 0 || (m[1] === '' && m[2] === '')) return null
    var start, end
    if (m[1] === '') {
      // the last n bytes
      var n = Number(m[2])
      if (n <= 0) return null
      start = Math.max(0, size - n)
      end = size - 1
    } else {
      start = Number(m[1])
      end = m[2] === '' ? size - 1 : Math.min(Number(m[2]), size - 1)
    }
    if (start > end || start >= size) return null
    return [start, end]
  }

  // shellOf names the build a Books page belongs to: a short hash of the
  // page, which lists the build's scripts by their content-named files (the
  // main script alone can stay the same when other parts change). "" when it
  // isn't a page of the app.
  function shellOf(html) {
    if (!/assets\/index-[A-Za-z0-9_-]+\.js/.test(html || '')) return ''
    var h = 5381
    for (var i = 0; i < html.length; i++) h = ((h * 33) ^ html.charCodeAt(i)) >>> 0
    return h.toString(36)
  }

  root.BookshelfRules = { kind: kind, key: key, range: range, shellOf: shellOf, rel: rel }
})(typeof self !== 'undefined' ? self : globalThis)
