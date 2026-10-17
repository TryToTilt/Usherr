// Which Library tabs there are and which one is open. The Library page shows
// them as tabs and the left menu lists them under "Library", so both ask here.
//
// No React in this file, so it can be tested on its own.

export type LibraryTab = 'movie' | 'tv' | 'music' | 'ebook' | 'audiobook' | 'anime' | 'comics' | 'nsfw' // Usherr: anime, comics, NSFW

// Usherr: the kinds this fork adds. Anime sits next to TV; Comics and NSFW 18+
// come last and have pages of their own.
export interface HomeTabs {
  comics: boolean
  nsfw: boolean
}

// The switches in Settings, Media types, that decide which tabs exist.
export type LibraryModule = 'movies' | 'tv' | 'music' | 'ebooks' | 'audiobooks'

const ALL_TABS: readonly LibraryTab[] = ['movie', 'tv', 'music', 'ebook', 'audiobook', 'anime', 'comics', 'nsfw']

export function isLibraryTab(v: string | null | undefined): v is LibraryTab {
  return !!v && (ALL_TABS as readonly string[]).includes(v)
}

// The tabs that are switched on: movies, TV and music (whichever are on), then
// ebooks and audiobooks. Never empty.
export function libraryTabs(on: (key: LibraryModule) => boolean, home?: HomeTabs): LibraryTab[] {
  const video: LibraryTab[] = []
  if (on('movies')) video.push('movie')
  if (on('tv')) video.push(...(home ? (['tv', 'anime'] as const) : (['tv'] as const)))
  if (on('music')) video.push('music')
  // With nothing on at all, the Library still opens on Movies.
  const videoKinds: LibraryTab[] = video.length > 0 ? video : ['movie']
  const booksOn = on('ebooks') || on('audiobooks')
  return [
    ...(on('movies') || on('tv') || on('music') || !booksOn ? videoKinds : []),
    ...(on('ebooks') ? (['ebook'] as const) : []),
    ...(on('audiobooks') ? (['audiobook'] as const) : []),
    ...(home?.comics ? (['comics'] as const) : []),
    ...(home?.nsfw ? (['nsfw'] as const) : []),
  ]
}

// The tab that is open: the one the address asks for (`?kind=tv`), Movies when
// it asks for none, and the first tab that is on when the one asked for is off.
export function activeLibraryTab(tabs: readonly LibraryTab[], kindParam: string | null | undefined): LibraryTab {
  const asked: LibraryTab = isLibraryTab(kindParam) ? kindParam : 'movie'
  return tabs.includes(asked) ? asked : tabs[0]
}

// The address that opens the Library on a tab.
export function libraryTabPath(tab: LibraryTab): string {
  if (tab === 'comics') return '/comics' // Usherr
  if (tab === 'nsfw') return '/nsfw' // Usherr
  return `/library?kind=${tab}`
}

export interface LibraryEntry {
  tab: LibraryTab
  to: string
  active: boolean
}

// What the left menu lists under "Library": a line for each tab that is on, and
// the one that is open (only while the Library page itself is showing).
export function libraryEntries(on: (key: LibraryModule) => boolean, pathname: string, search: string, home?: HomeTabs): LibraryEntry[] {
  const tabs = libraryTabs(on, home)
  const inLibrary = pathname === '/library' || pathname === '/library/'
  const open = inLibrary ? activeLibraryTab(tabs, new URLSearchParams(search).get('kind')) : null
  // Usherr: Comics and NSFW 18+ are open on their own pages.
  const own = (tab: LibraryTab) => (tab === 'comics' || tab === 'nsfw') && (pathname === libraryTabPath(tab) || pathname.startsWith(libraryTabPath(tab) + '/'))
  return tabs.map((tab) => ({ tab, to: libraryTabPath(tab), active: tab === open || own(tab) }))
}
