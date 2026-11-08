// Usherr: one tile for every title in Discover, from AniList or MangaDex. A title
// both know (MangaDex links its AniList entry) shows once, with what each knows.
// Nothing here touches the page, so it can be tested on its own.
import type { DexTitle } from './browseApi'
import type { MangaFound } from './mangaApi'

export interface Pick {
  key: string
  title: string
  coverUrl?: string
  status?: string
  year?: number
  anilistId?: number
  dexId?: string
  inLibrary?: number
  canRead: boolean // MangaDex is on and has chapters to read in your language (or may, from AniList's link)
  score?: number // 0 to 100
  tags?: string[]
  description?: string
  found?: MangaFound
  dex?: DexTitle
}

export function fromFound(f: MangaFound, dexOn: boolean): Pick {
  const dexId = dexOn ? f.mangadexId : undefined
  return { key: `a${f.anilistId}`, title: f.title, coverUrl: f.coverUrl, status: f.status, year: f.startYear || undefined, anilistId: f.anilistId, dexId, inLibrary: f.inLibrary, canRead: !!dexId, score: f.score, tags: f.genres, description: f.description, found: f }
}

export function fromDex(d: DexTitle): Pick {
  return {
    key: d.anilistId ? `a${d.anilistId}` : `d${d.id}`,
    title: d.title,
    coverUrl: d.coverUrl,
    status: d.status,
    year: d.year,
    anilistId: d.anilistId,
    dexId: d.id,
    inLibrary: d.inLibrary,
    canRead: d.canRead,
    score: d.score ? Math.round(d.score * 10) : undefined,
    tags: d.tags,
    description: d.description,
    dex: d,
  }
}

// mergePicks puts AniList's and MangaDex's answers together: AniList's first, each
// filled in with MangaDex's where both have the title, then MangaDex's others.
export function mergePicks(anilist: Pick[], dex: Pick[]): Pick[] {
  const out: Pick[] = []
  const at = new Map<string, number>()
  const add = (p: Pick) => {
    const i = at.get(p.key)
    if (i === undefined) {
      at.set(p.key, out.length)
      out.push(p)
      return
    }
    const a = out[i]
    out[i] = {
      ...a,
      dexId: a.dexId ?? p.dexId,
      dex: a.dex ?? p.dex,
      canRead: a.canRead || p.canRead,
      inLibrary: a.inLibrary ?? p.inLibrary,
      coverUrl: a.coverUrl ?? p.coverUrl,
      description: a.description || p.description,
      tags: a.tags?.length ? a.tags : p.tags,
    }
  }
  anilist.forEach(add)
  dex.forEach(add)
  return out
}

// A tag in the search: wanted, not wanted, or neither. A press goes round the three.
export type TagState = 'in' | 'out' | undefined

export function nextTagState(s: TagState): TagState {
  return s === undefined ? 'in' : s === 'in' ? 'out' : undefined
}

// firstReadable is where Read now starts: where you stopped, else the first chapter
// that can be read here.
export function firstReadable(chapters: { id: string; externalUrl?: string }[], stopped?: string): string | undefined {
  if (stopped && chapters.some((c) => c.id === stopped)) return stopped
  return chapters.find((c) => !c.externalUrl)?.id
}
