// Home fork: titles sort without a leading "The", "A" or "An" ("The Matrix"
// files under M), as in Sonarr, Radarr and Jellyfin. English articles only:
// "Die Hard" must stay under D.
const ARTICLE = /^(the|a|an)\s+(?=\S)/i

export function sortTitle(title: string): string {
  return title.trim().replace(ARTICLE, '')
}

export function compareTitles(a: string, b: string): number {
  return sortTitle(a).localeCompare(sortTitle(b), undefined, { sensitivity: 'base', numeric: true })
}
