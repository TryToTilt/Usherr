// Usherr: your own reading in the Comics tab: a Continue row above the shelves,
// and a Reading tab with Continue, Want to Read and Read. Kept per person on the
// server (home_comic_reading.go). After Pullbox's reading workspace.
import { useCallback, useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import Icon from '../components/Icon'
import { t } from '../i18n'
import { ComicReader } from './Comics'
import { comicsApi, type ComicIssue, type ComicReadingItem, type ComicReadingViews } from './comicsApi'
import Shelves from './Shelves'

// Issues read before read state was kept on the server are only noted in this
// browser. They're sent once.
const SENT = 'comic-done-sent'
async function sendOldReads() {
  let ids: number[] = []
  try {
    if (localStorage.getItem(SENT)) return
    ids = Object.keys(localStorage)
      .filter((k) => k.startsWith('comic-done-') && localStorage.getItem(k) === '1')
      .map((k) => Number(k.slice('comic-done-'.length)))
      .filter((n) => n > 0)
      .slice(0, 5000)
  } catch {
    return
  }
  if (ids.length > 0) await comicsApi.markIdsRead(ids)
  try {
    localStorage.setItem(SENT, '1')
  } catch {
    /* private mode */
  }
}

function useReading() {
  const [views, setViews] = useState<ComicReadingViews | null>(null)
  const [error, setError] = useState('')
  const load = useCallback(() => {
    comicsApi
      .reading()
      .then(setViews)
      .catch((e: Error) => setError(e.message))
  }, [])
  useEffect(() => {
    void sendOldReads()
      .catch(() => undefined)
      .then(load)
  }, [load])
  return { views, error, load }
}

function itemName(i: ComicReadingItem) {
  return i.issueId ? `#${i.number}${i.title ? ` · ${i.title}` : ''}` : t('The whole series')
}

function progress(i: ComicReadingItem) {
  if (i.next) return t('Next up')
  if (i.page && i.pages) return t('Page {n} of {total}', { n: i.page, total: i.pages })
  return ''
}

function ReadingCard({ item, onRead }: { item: ComicReadingItem; onRead: (issueId: number) => void }) {
  const cover = item.imageUrl ? <img src={item.imageUrl} alt="" loading="lazy" /> : <div className="cx-noimg"><Icon name="book" size={28} /></div>
  const note = progress(item)
  const text = (
    <>
      {cover}
      <strong>{item.series}</strong>
      <span className="hint">{itemName(item)}</span>
      {note && <span className="cx-count">{note}</span>}
    </>
  )
  // An issue with a file opens to read; anything else opens its series.
  if (item.issueId && item.status === 'downloaded') {
    const id = item.issueId
    return (
      <button className="cx-card cx-arc-pick" onClick={() => onRead(id)}>
        {text}
      </button>
    )
  }
  return (
    <Link to={`/comics/${item.seriesId}`} className="cx-card">
      {text}
    </Link>
  )
}

const readerIssue = (id: number) => ({ id }) as ComicIssue

// The Continue row, above the shelves. Nothing to continue: no row at all.
export function ComicContinueRow() {
  const { views, load } = useReading()
  const [reading, setReading] = useState(0)
  const list = views?.continue ?? []
  return (
    <>
      {list.length > 0 && (
        <div className="shelf-featured">
          <Shelves
            shelves={[{ key: 'continue', title: t('Continue'), hint: t('where you left off, and what comes next'), items: list }]}
            render={(item) => <ReadingCard item={item} onRead={setReading} />}
            itemKey={(item) => String(item.issueId)}
            rowClass="comic-shelf"
            memory="comics"
          />
        </div>
      )}
      {reading > 0 && (
        <ComicReader
          issue={readerIssue(reading)}
          onClose={() => {
            setReading(0)
            load()
          }}
        />
      )}
    </>
  )
}

type View = 'continue' | 'want' | 'read'

// The Reading tab.
export default function ComicReadingTab() {
  const { views, error, load } = useReading()
  const [view, setView] = useState<View>('continue')
  const [reading, setReading] = useState(0)
  const [problem, setProblem] = useState('')
  if (error) return <p className="error-text">{error}</p>
  if (!views) return <div className="skeleton" style={{ height: 200 }} />
  const tabs: { id: View; label: string; empty: string }[] = [
    { id: 'continue', label: t('Continue'), empty: t('Nothing to continue. Open an issue and it shows up here.') },
    { id: 'want', label: t('Want to Read'), empty: t('Nothing here yet. Open a series and press Want to read.') },
    { id: 'read', label: t('Read'), empty: t('Nothing read yet. Issues you read to the end, or mark read, show up here.') },
  ]
  const list = views[view]
  return (
    <div>
      <div className="seg cx-reading-tabs" role="tablist" aria-label={t('Your reading')}>
        {tabs.map((x) => (
          <button key={x.id} role="tab" data-walk={`cx-reading-${x.id}`} aria-selected={view === x.id} className={view === x.id ? 'active' : ''} onClick={() => setView(x.id)}>
            {x.label} ({views[x.id].length})
          </button>
        ))}
      </div>
      {problem && <p className="error-text">{problem}</p>}
      {list.length === 0 && <p className="hint">{tabs.find((x) => x.id === view)?.empty}</p>}
      <ul className="cx-grid">
        {list.map((item) => (
          <li key={`${item.seriesId}-${item.issueId ?? 0}`} className="cx-reading-item">
            <ReadingCard item={item} onRead={setReading} />
            {view === 'want' && (
              <button
                className="btn-sm cx-reading-remove"
                aria-label={t('Take {name} off Want to Read', { name: item.series })}
                title={t('Take it off Want to Read')}
                onClick={() =>
                  comicsApi
                    .wantRead(item.seriesId, item.issueId ?? 0, false)
                    .then(load)
                    .catch((e: Error) => setProblem(e.message))
                }
              >
                <Icon name="x" size={14} />
              </button>
            )}
          </li>
        ))}
      </ul>
      {reading > 0 && (
        <ComicReader
          issue={readerIssue(reading)}
          onClose={() => {
            setReading(0)
            load()
          }}
        />
      )}
    </div>
  )
}
