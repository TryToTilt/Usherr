import { useEffect, useState } from 'react'
import { api, type WatchedTitle, type WatchTitleSummary } from '../api'
import { locale, plural, t } from '../i18n'
import Icon from './Icon'

// "Watched 2 times · last on 3 Mar 2026" on a movie's or show's page, when
// reading what's been watched is on (Settings > Media servers). Usherr: with
// who watched it, from the watch history (Settings > System > Watching).

let cache: { at: number; data: Promise<Awaited<ReturnType<typeof api.watched>>> } | null = null

function watchedData() {
  if (!cache || Date.now() - cache.at > 60_000) cache = { at: Date.now(), data: api.watched() }
  return cache.data
}

export function watchedText(kind: 'movie' | 'tv', w: WatchedTitle): string {
  const last = w.lastPlayed ? new Date(w.lastPlayed) : null
  const on = last && !Number.isNaN(last.getTime()) ? ` · ${t('last on {date}', { date: last.toLocaleDateString(locale(), { day: 'numeric', month: 'short', year: 'numeric' }) })}` : ''
  if (kind === 'tv') return `${plural(w.episodes ?? 0, '{n} episode watched', '{n} episodes watched')}${on}`
  return `${w.plays > 1 ? plural(w.plays, 'Watched {n} time', 'Watched {n} times') : t('Watched')}${on}`
}

// Usherr: the same from the watch history, when the play history above has
// nothing, and who watched it.
function historyText(kind: 'movie' | 'tv', h: WatchTitleSummary, withCount: boolean): string {
  const parts: string[] = []
  if (withCount) {
    parts.push(kind === 'tv' ? plural(h.plays, '{n} episode played', '{n} episodes played') : plural(h.plays, 'Watched {n} time', 'Watched {n} times'))
    const last = h.last ? new Date(h.last) : null
    if (last && !Number.isNaN(last.getTime())) parts.push(t('last on {date}', { date: last.toLocaleDateString(locale(), { day: 'numeric', month: 'short', year: 'numeric' }) }))
  }
  if (h.people.length > 0) parts.push(t('by {names}', { names: h.people.join(', ') }))
  return parts.join(' · ')
}

export default function WatchedLine({ kind, id }: { kind: 'movie' | 'tv'; id: number }) {
  const [w, setW] = useState<WatchedTitle | null>(null)
  const [h, setH] = useState<WatchTitleSummary | null>(null)
  useEffect(() => {
    let live = true
    watchedData()
      .then((d) => live && setW((kind === 'movie' ? d.movies : d.series)[String(id)] ?? null))
      .catch(() => undefined)
    api
      .watchTitle(kind, id)
      .then((d) => live && setH(d.plays > 0 ? d : null))
      .catch(() => undefined)
    return () => {
      live = false
    }
  }, [kind, id])
  if (!w && !h) return null
  const text = [w ? watchedText(kind, w) : '', h ? historyText(kind, h, !w) : ''].filter(Boolean).join(' · ')
  if (!text) return null
  return (
    <p className="watched-line">
      <Icon name="eye" size={13} /> {text}
    </p>
  )
}
