// Usherr: on a series page, your follow, reading status and MangaDex lists. With
// your MangaDex account connected they're the same there; opening the page also
// brings in what you read on MangaDex.
import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { useToast } from '../components/Toast'
import { t } from '../i18n'
import { mangaDexAccountApi, type MangaDexReadingStatus, type MangaDexSeries } from './mangaDexAccountApi'
import './mangaDex.css'

const STATUSES: [MangaDexReadingStatus, () => string][] = [
  ['', () => t('No status')],
  ['reading', () => t('Reading')],
  ['on_hold', () => t('On hold')],
  ['plan_to_read', () => t('Plan to read')],
  ['re_reading', () => t('Reading again')],
  ['completed', () => t('Completed')],
  ['dropped', () => t('Dropped')],
]

export default function MangaDexSeriesBar({ seriesId, onReadChanged }: { seriesId: number; onReadChanged?: () => void }) {
  const toast = useToast()
  const [md, setMd] = useState<MangaDexSeries | null>(null)
  const [busy, setBusy] = useState(false)
  const [listsOpen, setListsOpen] = useState(false)

  useEffect(() => {
    let live = true
    mangaDexAccountApi
      .series(seriesId)
      .then((v) => {
        if (!live) return
        setMd(v)
        if (v.changed) onReadChanged?.()
      })
      .catch(() => undefined)
    return () => {
      live = false
    }
  }, [seriesId, onReadChanged])

  if (!md) return null

  const run = async (fn: () => Promise<{ md: MangaDexSeries; note?: string }>) => {
    setBusy(true)
    try {
      const r = await fn()
      setMd(r.md)
      if (r.note) toast.success(r.note)
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  const synced = md.connected && !md.refused && md.linked
  return (
    <div className="mg-mdx-bar" data-walk="manga-mangadex-bar">
      <button className={`btn-sm${md.following ? ' primary' : ''}`} aria-pressed={md.following} disabled={busy} onClick={() => void run(() => mangaDexAccountApi.follow(seriesId, !md.following))}>
        {md.following ? t('Following') : t('Follow')}
      </button>
      <label className="mg-mdx-status">
        <span className="sr-only">{t('Reading status')}</span>
        <select value={md.status} disabled={busy} onChange={(e) => void run(() => mangaDexAccountApi.status(seriesId, e.target.value as MangaDexReadingStatus))}>
          {STATUSES.map(([v, label]) => (
            <option key={v} value={v}>
              {label()}
            </option>
          ))}
        </select>
      </label>
      {synced && md.lists.length > 0 && (
        <div className="mg-mdx-lists">
          <button className="btn-sm" aria-expanded={listsOpen} onClick={() => setListsOpen(!listsOpen)}>
            {t('MangaDex lists')}
          </button>
          {listsOpen && (
            <div className="mg-well mg-mdx-listpick">
              {md.lists.map((l) => (
                <label key={l.id}>
                  <input type="checkbox" checked={l.in} disabled={busy} onChange={() => void run(() => mangaDexAccountApi.inList(seriesId, l.id, !l.in))} /> {l.name || t('A list')}
                </label>
              ))}
            </div>
          )}
        </div>
      )}
      <span className="mg-hint">
        {synced ? t('Kept the same on MangaDex.') : md.connected && md.refused ? <Link to="/settings/manga-account">{t('MangaDex didn’t accept your sign-in.')}</Link> : md.connected ? t('Not matched to MangaDex yet: kept in Usherr.') : <Link to="/settings/manga-account">{t('Connect MangaDex to keep these the same there.')}</Link>}
      </span>
    </div>
  )
}
