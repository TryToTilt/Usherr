import { lazy, Suspense, useEffect } from 'react'
import { Route, Routes } from 'react-router-dom'
import Shelf from './Shelf'
import { t } from '../i18n'
import './bookshelf.css'
import { withBase } from '../base'
import { refreshAppFiles, registerBooksWorker } from './offline'
import { warmFor } from './Keep'
import { watchConnection } from './progress'

// The reader brings in the EPUB library, so it loads only when a book is opened.
const Reader = lazy(() => import('./Reader'))
const Player = lazy(() => import('./Player'))
const ComicIssue = lazy(() => import('./ComicIssue'))
const MangaVolume = lazy(() => import('./MangaVolume'))

// Mediarium Books: reading and listening, as an app of its own. It has no
// Mediarium sidebar, its own name and icon when installed (its own web app
// manifest, scoped to /bookshelf/), and opens in a window of its own.
export default function BookshelfApp() {
  useEffect(() => {
    const link = document.querySelector<HTMLLinkElement>('link[rel="manifest"]')
    const before = link?.getAttribute('href') ?? null
    link?.setAttribute('href', withBase('/bookshelf.webmanifest'))
    document.documentElement.classList.add('bookshelf-mode')
    return () => {
      if (link && before) link.setAttribute('href', before)
      document.documentElement.classList.remove('bookshelf-mode')
    }
  }, [])

  // The Books worker keeps books on this device for reading offline, and
  // places read offline go to the server once the connection is back.
  useEffect(() => {
    // After an update, the app's files are stored again for the kept books.
    void registerBooksWorker().then((reg) => window.setTimeout(() => void refreshAppFiles(reg, warmFor), 3000))
    return watchConnection()
  }, [])

  return (
    <div className="bks">
      <Suspense fallback={<div className="bks-loading">{t('Opening…')}</div>}>
        <Routes>
          <Route index element={<Shelf />} />
          <Route path="read/:id" element={<Reader />} />
          <Route path="listen/:id" element={<Player />} />
          <Route path="comic/:id" element={<ComicIssue />} />
          <Route path="manga/:id" element={<MangaVolume />} />
          <Route path="*" element={<Shelf />} />
        </Routes>
      </Suspense>
    </div>
  )
}
