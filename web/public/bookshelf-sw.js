// Usherr Books' service worker: books kept on this device open with no
// connection. It is registered for the Books pages only (scope /bookshelf),
// so the rest of Usherr never goes through it, and push notifications stay
// with push-sw.js.
//
// What it answers, and from where, is in bookshelf-sw-rules.js. In short:
// a Books page is asked of the server first (so an update shows at once) and
// the stored copy is used offline; the app's built files are stored as the
// Books pages load them (their names change with every version, so a copy is
// never stale); a kept book's file and pages come from the device; the few
// answers a kept book needs come from the server, and from the device only
// when the server can't be reached. Nothing is stored unless the person kept
// a book, except the Books page and its built files.
//
// Updating: this file is fetched again on every visit (no-cache); a new one
// takes over at once (skipWaiting + clients.claim). A change to what is
// stored gets a new VERSION, and the old stores are removed when it starts.
importScripts('bookshelf-sw-rules.js')

var VERSION = 1
var BOOKS = 'usherr-books-files-v' + VERSION // kept books (offline.ts writes here too)
var SHELL = 'usherr-books-shell-v' + VERSION + '-' // + the build: the page and its scripts
var R = self.BookshelfRules
var base = new URL(self.registration.scope).pathname.replace(/\/bookshelf\/?$/, '')
var shellName = '' // the store of the build last seen

self.addEventListener('install', function () {
  self.skipWaiting()
})

self.addEventListener('activate', function (event) {
  event.waitUntil(
    caches
      .keys()
      .then(function (names) {
        return Promise.all(
          names
            .filter(function (n) {
              return n.indexOf('usherr-books-') === 0 && n !== BOOKS && n.indexOf(SHELL) !== 0
            })
            .map(function (n) {
              return caches.delete(n)
            }),
        )
      })
      .catch(function () {})
      .then(function () {
        return self.clients.claim()
      }),
  )
})

// currentShell is the store for the build in use: the one last seen, else
// the newest there is.
function currentShell() {
  if (shellName) return Promise.resolve(shellName)
  return caches.keys().then(function (names) {
    var shells = names.filter(function (n) {
      return n.indexOf(SHELL) === 0
    })
    shellName = shells.length ? shells[shells.length - 1] : SHELL + 'none'
    return shellName
  })
}

// keepPage stores a Books page as the one to show offline, in the store of
// its build. Stores of older builds are removed: a new version was loaded.
function keepPage(res) {
  return res.text().then(function (html) {
    var build = R.shellOf(html)
    if (!build) return
    var name = SHELL + build
    var headers = new Headers({ 'Content-Type': 'text/html; charset=utf-8' })
    return caches
      .open(name)
      .then(function (c) {
        return c.put(base + '/bookshelf/', new Response(html, { headers: headers }))
      })
      .then(function () {
        if (shellName === name) return
        shellName = name
        return caches.keys().then(function (names) {
          return Promise.all(
            names
              .filter(function (n) {
                return n.indexOf(SHELL) === 0 && n !== name
              })
              .map(function (n) {
                return caches.delete(n)
              }),
          )
        })
      })
  })
}

// shellPage is the stored Books page of the build in use (an older build's
// page would ask for scripts no longer stored).
function shellPage() {
  return currentShell().then(function (name) {
    return caches.open(name).then(function (c) {
      return c.match(base + '/bookshelf/')
    })
  })
}

function offlinePage() {
  return shellPage().then(function (hit) {
    if (hit) return hit
    return caches.match(base + '/offline.html').then(function (page) {
      return page || Response.error()
    })
  })
}

// down is an answer from a proxy in front of a server that isn't running.
function down(res) {
  return res.status === 502 || res.status === 503 || res.status === 504
}

// fromStore answers a kept file, with the part asked for when the request
// has a Range (audio players ask for one).
function fromStore(req, hit) {
  var asked = req.headers.get('Range')
  if (!asked) return hit
  return hit.blob().then(function (blob) {
    var r = R.range(asked, blob.size)
    if (!r) return hit
    var headers = new Headers(hit.headers)
    headers.set('Content-Range', 'bytes ' + r[0] + '-' + r[1] + '/' + blob.size)
    headers.set('Content-Length', String(r[1] - r[0] + 1))
    headers.set('Accept-Ranges', 'bytes')
    return new Response(blob.slice(r[0], r[1] + 1), { status: 206, statusText: 'Partial Content', headers: headers })
  })
}

self.addEventListener('fetch', function (event) {
  var req = event.request
  var url = new URL(req.url)
  if (url.origin !== self.location.origin) return
  var what = R.kind(req.method, url, req.mode, base)
  if (!what) return
  var key = R.key(url, base)

  if (what === 'page') {
    event.respondWith(
      fetch(req).then(function (res) {
        if (res.ok && res.type === 'basic') event.waitUntil(keepPage(res.clone()).catch(function () {}))
        if (down(res)) {
          return shellPage().then(function (hit) {
            return hit || res
          })
        }
        return res
      }, offlinePage),
    )
    return
  }

  if (what === 'asset') {
    event.respondWith(
      currentShell().then(function (name) {
        return caches.open(name).then(function (c) {
          return c.match(req).then(function (hit) {
            if (hit) return hit
            return fetch(req).then(function (res) {
              if (res.ok && res.type === 'basic') event.waitUntil(c.put(req, res.clone()).catch(function () {}))
              return res
            })
          })
        })
      }),
    )
    return
  }

  if (what === 'file') {
    event.respondWith(
      caches.open(BOOKS).then(function (c) {
        return c.match(key).then(function (hit) {
          return hit ? fromStore(req, hit) : fetch(req)
        })
      }),
    )
    return
  }

  // "data": the server first. A fresh answer replaces a stored one (only
  // answers already stored, for a kept book, are ever written).
  event.respondWith(
    caches.open(BOOKS).then(function (c) {
      return fetch(req).then(
        function (res) {
          if (res.ok) {
            var copy = res.clone()
            event.waitUntil(
              c
                .match(key)
                .then(function (had) {
                  if (had) return c.put(key, copy)
                })
                .catch(function () {}),
            )
          }
          if (down(res)) {
            return c.match(key).then(function (hit) {
              return hit || res
            })
          }
          return res
        },
        function (err) {
          return c.match(key).then(function (hit) {
            if (hit) return hit
            throw err
          })
        },
      )
    }),
  )
})

// The page asks for the app's files to be stored when a book is kept, so the
// Books pages open offline even if they loaded before this worker was there.
self.addEventListener('message', function (event) {
  var msg = event.data || {}
  if (msg.type !== 'keep-shell' || !Array.isArray(msg.urls)) return
  var urls = msg.urls.filter(function (u) {
    try {
      var p = new URL(u, self.location.origin)
      return p.origin === self.location.origin && R.kind('GET', p, 'no-cors', base) === 'asset'
    } catch (e) {
      return false
    }
  })
  event.waitUntil(
    fetch(base + '/bookshelf/', { credentials: 'same-origin', cache: 'no-store' })
      .then(function (res) {
        return res.ok ? keepPage(res) : undefined
      })
      .then(currentShell)
      .then(function (name) {
        return caches.open(name).then(function (c) {
          return Promise.all(
            urls.map(function (u) {
              return c.match(u).then(function (hit) {
                return hit ? undefined : c.add(u)
              })
            }),
          )
        })
      })
      .then(
        function () {
          if (event.source) event.source.postMessage({ type: 'shell-kept', ok: true })
        },
        function () {
          if (event.source) event.source.postMessage({ type: 'shell-kept', ok: false })
        },
      ),
  )
})
