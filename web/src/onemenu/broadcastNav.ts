// Usherr Broadcast: the settings as Broadcast's Server and You show them.
// The same pages as settingsNav.ts (they stay where they are in the code), a
// few new pages that gather cards which used to sit on Settings > Extras, and
// seven groups for administrators plus "Your settings" for everyone.
// Classic and Neu keep settingsNav.ts's own groups exactly as they were;
// settingsNav.ts asks here only while the look is Broadcast.
//
// No React in this file, so it can be tested on its own.

import type { IconName } from '../components/Icon'
import { t } from '../i18n.ts'

export interface BroadcastPage {
  to: string
  label: string
  icon: IconName
  color: string
  blurb: string
  member?: boolean
  notViewer?: boolean
  help?: string
}

const C = {
  setup: '#b4bfcc',
  libraries: '#7cc4f8',
  getting: '#86dca4',
  watching: '#f2c14e',
  kinds: '#8fa8ff',
  people: '#8fb8f5',
  smarts: '#c1acf7',
  you: '#e7a6f2',
}

// The pages only Broadcast lists. Each gathers the cards of one feature
// (onemenu/BroadcastSettings.tsx); the cards themselves are Settings >
// Extras' cards (home/ExtrasSettings.tsx), reused as they are.
export const BROADCAST_NEW_PAGES: BroadcastPage[] = [
  { to: '/settings/streaming', label: t('Streaming'), icon: 'play', color: C.getting, blurb: t('Stream a film or an episode right away, without downloading it first: the engine, StreamNZB, the streamed library and the virtual drive for Plex.'), help: 'home-setup#streaming-streamnzb' },
  { to: '/settings/live-tv', label: t('Live TV'), icon: 'tv', color: C.watching, blurb: t('Live TV channels in Jellyfin, Emby and Plex, and the picture they send.'), help: 'home-setup#live-tv' },
  { to: '/settings/play-on-tv', label: t('Play on TV'), icon: 'monitor', color: C.watching, blurb: t('Send a stream to an Android TV box on your network.'), help: 'home-setup#play-on-tv' },
  { to: '/settings/apps', label: t('Apps on phones and TVs'), icon: 'grid', color: C.watching, blurb: t('Stremio and Nuvio on phones and TVs, and music apps that speak Subsonic.') },
  { to: '/settings/comics', label: t('Comics'), icon: 'book', color: C.kinds, blurb: t('Comic series, new issues and a reader, and direct downloads from GetComics.'), help: 'home-setup#comics' },
  { to: '/settings/music', label: t('Music'), icon: 'music', color: C.kinds, blurb: t('Tidy up your music files and bring music in from a folder.') },
  { to: '/settings/books', label: t('Books and Read-to-Me'), icon: 'headphones', color: C.kinds, blurb: t('Audiobooks made from your ebooks with a computer voice.'), help: 'home-setup#read-to-me' },
  { to: '/settings/sports', label: t('Sports'), icon: 'trophy', color: C.kinds, blurb: t('Leagues, teams, games to stream and live scores.'), help: 'home-setup#sports' },
  { to: '/settings/nsfw', label: t('NSFW 18+'), icon: 'shield', color: C.kinds, blurb: t('Adult videos for the accounts allowed them, kept apart from everything else.') },
  { to: '/settings/ai', label: t('AI'), icon: 'sparkles', color: C.smarts, blurb: t('The Ask bar, Tonight, Catch me up, the family filter, news summaries and AI-built channels.'), help: 'home-setup#ai' },
  { to: '/settings/news', label: t('News and in theaters'), icon: 'globe', color: C.smarts, blurb: t('Entertainment news on Home, and the films in cinemas near you.'), help: 'home-setup#entertainment-news' },
  { to: '/settings/addresses', label: t('Addresses'), icon: 'globe', color: C.smarts, blurb: t('The address Usherr is reached at from outside, and its address on your own network for your TV and media server.') },
]

// Pages that are named a little differently in Broadcast.
export const BROADCAST_LABELS: Record<string, string> = {
  '/settings/general': t('Look and language'),
  '/settings/system': t('Server and updates'),
  '/settings/schedules': t('Schedules and quiet times'),
  '/settings/accounts': t('Accounts'),
  '/settings/re-posterr': t('Artwork (Re-Posterr)'),
}

// Pages Broadcast doesn't list: their cards now live on their features' pages.
export const BROADCAST_DROPPED = ['/settings/extras']

export interface BroadcastGroup {
  key: string
  label: string
  icon: IconName
  color: string
  pages: string[]
}

// The seven groups under Server, in order, then Your settings (under You).
export const BROADCAST_GROUPS: BroadcastGroup[] = [
  { key: 'setup', label: t('Setup and health'), icon: 'check', color: C.setup, pages: ['/settings/setup', '/settings/service-checks', '/settings/logs', '/settings/system', '/settings/backup', '/settings/migrate'] },
  { key: 'libraries', label: t('Libraries'), icon: 'folder', color: C.libraries, pages: ['/settings/modules', '/settings/media', '/settings/naming', '/settings/quality', '/settings/automation', '/settings/metadata', '/settings/subtitles', '/settings/lists', '/settings/re-posterr', '/settings/collections', '/settings/poster-badges'] },
  { key: 'getting', label: t('Getting media'), icon: 'download', color: C.getting, pages: ['/settings/indexers', '/settings/usenet', '/settings/torrents', '/settings/vpn', '/settings/download-rules', '/settings/schedules', '/settings/streaming'] },
  { key: 'watching', label: t('Watching'), icon: 'monitor', color: C.watching, pages: ['/settings/media-servers', '/settings/live-tv', '/preshow/manage', '/marquee/manage', '/settings/play-on-tv', '/settings/apps'] },
  { key: 'kinds', label: t('Kinds of media'), icon: 'grid', color: C.kinds, pages: ['/settings/manga', '/settings/comics', '/settings/music', '/settings/books', '/settings/sports', '/settings/nsfw'] },
  { key: 'people', label: t('People'), icon: 'user', color: C.people, pages: ['/settings/accounts', '/settings/sign-in', '/settings/requests', '/settings/linked', '/settings/community', '/settings/notifications'] },
  { key: 'smarts', label: t('Smarts and connections'), icon: 'sparkles', color: C.smarts, pages: ['/settings/ai', '/settings/news', '/settings/other-apps', '/settings/addons', '/settings/addresses'] },
  { key: 'you', label: t('Your settings'), icon: 'user', color: C.you, pages: ['/settings/general', '/settings/profile', '/settings/security', '/settings/services', '/settings/manga-account', '/settings/statistics', '/settings/watching', '/settings/about'] },
]

// The key of the group under You (every other group is under Server).
export const YOUR_SETTINGS = 'you'

// The pages Broadcast lists, from the pages every look has: renamed where
// Broadcast names them differently, without Extras, plus Broadcast's own.
export function broadcastPages<P extends BroadcastPage>(base: readonly P[]): BroadcastPage[] {
  return [...base.filter((p) => !BROADCAST_DROPPED.includes(p.to)).map((p) => (BROADCAST_LABELS[p.to] ? { ...p, label: BROADCAST_LABELS[p.to] } : p)), ...BROADCAST_NEW_PAGES]
}

// Your own settings pages (under You), so You stays lit on them and Server doesn't.
export function inYourSettings(path: string): boolean {
  const you = BROADCAST_GROUPS.find((g) => g.key === YOUR_SETTINGS)?.pages ?? []
  return you.some((to) => path === to || path.startsWith(to + '/'))
}

// Where Settings > Extras' cards went (the old page's #anchors and the cards'
// names), so an old link lands next to its card.
export const EXTRAS_MOVED: Record<string, string> = {
  server: '/settings/media-servers',
  'jellyfin-or-emby': '/settings/media-servers',
  convert: '/settings/media-servers',
  address: '/settings/addresses',
  'usherrs-address-on-your-network': '/settings/addresses',
  streaming: '/settings/streaming',
  'streaming-streamnzb': '/settings/streaming',
  streamed: '/settings/streaming',
  'streamed-library': '/settings/streaming',
  drive: '/settings/streaming',
  'virtual-drive': '/settings/streaming',
  livetv: '/settings/live-tv',
  'live-tv': '/settings/live-tv',
  tvpicture: '/settings/live-tv',
  addon: '/settings/apps',
  playontv: '/settings/play-on-tv',
  'play-on-tv': '/settings/play-on-tv',
  ai: '/settings/ai',
  scores: '/settings/metadata',
  'scores-mdblist': '/settings/metadata',
  news: '/settings/news',
  'entertainment-news': '/settings/news',
  cinema: '/settings/news',
  'in-theaters': '/settings/news',
  marquee: '/marquee/manage',
  preshow: '/preshow/manage',
  reposterr: '/settings/re-posterr',
  comics: '/settings/comics',
  direct: '/settings/comics',
  sports: '/settings/sports',
  readtome: '/settings/books',
  'read-to-me': '/settings/books',
  nsfw: '/settings/nsfw',
}

// Where an old Settings > Extras address goes in Broadcast: its card's new
// page when the address names one (#sports), otherwise the Server area.
export function extrasMovedTo(hash: string): string {
  const key = hash.replace(/^#/, '')
  const to = EXTRAS_MOVED[key]
  return to ? `${to}${key ? `#${key}` : ''}` : '/server'
}

// Cards that sit on another page in Broadcast (settings search follows them):
// card name -> the page it's on now.
export function broadcastCardMoves(): Record<string, string> {
  return {
    [t('Speed schedule')]: 'schedules',
    [t('Quiet hours')]: 'schedules',
    [t('Address of Mediarium')]: 'addresses',
    [t('Updates')]: 'system',
    [t('Updates pushed through the API')]: 'system',
    [t('Anonymous usage numbers')]: 'system',
    [t('Legal notice')]: 'system',
  }
}
