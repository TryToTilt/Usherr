import type { IndexerConfig } from '../../api'

// The list order: lower priority number first, then by name. An indexer with no
// priority yet counts as the default, 25.
export function sortIndexers(list: IndexerConfig[]): IndexerConfig[] {
  return [...list].sort((a, b) => (a.priority ?? 25) - (b.priority ?? 25) || a.name.localeCompare(b.name, undefined, { sensitivity: 'base' }) || a.id - b.id)
}

// Plain-text filter on the name (and the address), ignoring case.
export function filterIndexers(list: IndexerConfig[], query: string): IndexerConfig[] {
  const q = query.trim().toLowerCase()
  if (!q) return list
  return list.filter((i) => i.name.toLowerCase().includes(q) || i.baseUrl.toLowerCase().includes(q))
}

// The filter box only shows above this many indexers.
export const FILTER_FROM = 8

// What the list shows. With few indexers the box is hidden, so a word typed
// earlier (before indexers were removed) must not keep hiding them.
export function visibleIndexers(list: IndexerConfig[], query: string): IndexerConfig[] {
  return list.length > FILTER_FROM ? filterIndexers(list, query) : list
}
