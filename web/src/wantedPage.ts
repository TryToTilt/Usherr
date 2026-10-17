// The Wanted list as the server pages it (GET /api/wanted?group=1): one row
// per movie and one per show, a page at a time, with totals for the counters.
// pageOfWanted does the same from a full list, for the demo and the tests, so
// both sides follow one set of rules.
import type { WantedItem, WantedPage, WantedPageQuery, WantedRow } from './api'

// The rows of a full list: movies as they come, then one row per show in the
// order the shows first appear. A show with a single episode is that episode.
export function wantedRows(items: readonly WantedItem[]): WantedRow[] {
  const movies: WantedRow[] = []
  const shows: WantedRow[] = []
  const first = new Map<number, { row: WantedRow; only: WantedItem }>()
  for (const i of items) {
    if (i.kind !== 'episode' || !i.seriesId) {
      movies.push(i)
      continue
    }
    const seen = first.get(i.seriesId)
    if (seen) {
      seen.row.episodes = (seen.row.episodes ?? 1) + 1
      continue
    }
    const row: WantedRow = {
      kind: 'show',
      id: i.seriesId,
      seriesId: i.seriesId,
      title: i.title,
      year: i.year,
      posterUrl: i.posterUrl,
      profileName: i.profileName,
      copy: i.copy,
      subtitle: i.copy ? 'Second copy' : undefined,
      lastSearch: i.lastSearch,
      lastSearchAt: i.lastSearchAt,
      episodes: 1,
    }
    first.set(i.seriesId, { row, only: i })
    shows.push(row)
  }
  return [...movies, ...shows.map((r) => (r.episodes === 1 ? first.get(r.seriesId!)!.only : r))]
}

// One page of a full list, as the server would answer it.
export function pageOfWanted(items: readonly WantedItem[], query: WantedPageQuery = {}): WantedPage {
  const needle = (query.q ?? '').trim().toLowerCase()
  const rows = wantedRows(items).filter(
    (r) => (query.media !== 'movie' || r.kind === 'movie') && (query.media !== 'tv' || r.kind !== 'movie') && (needle === '' || r.title.toLowerCase().includes(needle)),
  )
  const limit = query.limit ?? 50
  const offset = query.offset ?? 0
  return {
    rows: rows.slice(offset, offset + limit),
    total: rows.length,
    movies: rows.filter((r) => r.kind === 'movie').length,
    episodes: rows.reduce((n, r) => n + (r.kind === 'movie' ? 0 : (r.episodes ?? 1)), 0),
    offset,
    limit,
  }
}

// The address for one page.
export function wantedPagePath(kind: 'missing' | 'cutoff', query: WantedPageQuery = {}): string {
  const p = new URLSearchParams({ kind, group: '1' })
  if (query.limit !== undefined) p.set('limit', String(query.limit))
  if (query.offset) p.set('offset', String(query.offset))
  if (query.media) p.set('media', query.media)
  if (query.q?.trim()) p.set('q', query.q.trim())
  return `/wanted?${p.toString()}`
}

// After a quiet refresh of the first rows, the rows further down that were
// already loaded stay on screen, so the list does not shrink under the reader.
export function mergeRefreshed(loaded: readonly WantedRow[], fresh: WantedPage): WantedRow[] {
  if (loaded.length <= fresh.rows.length) return fresh.rows
  return [...fresh.rows, ...loaded.slice(fresh.rows.length, fresh.total)]
}
