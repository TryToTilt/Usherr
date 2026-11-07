// Usherr: the sentences on a manga series: what's happening, when Usherr looks
// next, and why nothing has come yet. An empty list without a reason is what made
// other apps confusing, so every state here says one plainly.
//
// No React in this file, so it can be tested on its own.

import { plural, t } from '../i18n.ts'
import type { MangaChapterSummary, MangaFormat, MangaSearchState, MangaSeries, MangaSources, MangaSummary } from './mangaApi.ts'

export const FORMAT_LABEL: Record<MangaFormat, () => string> = {
  manga: () => t('Manga'),
  manhwa: () => t('Manhwa'),
  manhua: () => t('Manhua'),
  oneshot: () => t('One-shot'),
}

export function stateLabel(status: string): string {
  switch (status) {
    case 'finished':
      return t('Finished')
    case 'ongoing':
      return t('Ongoing')
    case 'hiatus':
      return t('On hiatus')
    case 'cancelled':
      return t('Cancelled')
    case 'upcoming':
      return t('Not out yet')
  }
  return ''
}

export interface MangaStatus {
  line: string
  askFan: boolean // offer "Allow fan scans for this series"
  label: string // the card's short word
  tone: '' | 'good' | 'warn' | 'manga'
}

// What's being looked for, for the end of a sentence.
function lookingIn(src: MangaSources): string {
  if (src.official && src.fan) return t('official English editions, then fan scans')
  if (src.fan) return t('fan scans')
  return t('official English editions')
}

export function mangaStatus(s: MangaSeries, src: MangaSources, problems: readonly string[]): MangaStatus {
  const haveAll = s.total > 0 && s.have >= s.total
  const having = s.total > 0 ? t('You have {have} of {total} volumes.', { have: s.have, total: s.total }) : ''
  if (problems.includes('no-folder') && !haveAll) {
    return { line: t('Nothing can be filed yet: Usherr has no manga folder. Choose one in Settings > Library > Folders.'), askFan: false, label: t('Needs a folder'), tone: 'warn' }
  }
  // Chapters come from MangaDex, whatever the volumes are doing.
  if (s.following && !haveAll && (s.have === 0 || s.wanted === 0)) {
    const n = s.chaptersHave ?? 0
    const volumes = s.total > 0 && s.have > 0 ? `${having} ` : ''
    return { line: volumes + plural(n, 'Following chapters from MangaDex: {n} here so far.', 'Following chapters from MangaDex: {n} here so far.'), askFan: false, label: n > 0 ? plural(n, '{n} chapter', '{n} chapters') : t('Following'), tone: 'manga' }
  }
  if (problems.includes('no-indexers') && !haveAll) {
    return { line: [having, t('You have no indexers set up, so there is nowhere to look. Add one in Settings > Indexers & Search.')].filter(Boolean).join(' '), askFan: false, label: t('Needs an indexer'), tone: 'warn' }
  }
  if (!src.official && !src.fan && !haveAll) {
    return { line: [having, t('Every source is switched off, so only your own files count here.')].filter(Boolean).join(' '), askFan: false, label: s.total ? `${s.have} / ${s.total}` : t('Sources off'), tone: 'warn' }
  }
  if (s.waiting > 0) {
    return { line: [having, plural(s.waiting, '{n} possible match is waiting for you.', '{n} possible matches are waiting for you.')].filter(Boolean).join(' '), askFan: false, label: t('Waiting for you'), tone: 'warn' }
  }
  if (s.downloading > 0) {
    return { line: [having, plural(s.downloading, '{n} download on its way.', '{n} downloads on their way.')].filter(Boolean).join(' '), askFan: false, label: t('Downloading'), tone: 'manga' }
  }
  if (haveAll) {
    const line = s.status === 'finished' ? t('You have all {total} volumes. Nothing more to get.', { total: s.total }) : s.monitorNew ? t('You have every volume known. Usherr will get the next one when it turns up.') : t('You have every volume known.')
    return { line, askFan: false, label: t('All here'), tone: 'good' }
  }
  // A series no official edition is known for, with fan scans off: nothing is
  // likely to come, and the person should hear it now rather than wait.
  if (s.licensedEn !== 1 && !src.fan && s.have === 0 && (s.wanted > 0 || s.monitorNew)) {
    return { line: t('Nothing yet. No official English edition is known, and fan scans are off, so nothing is likely to come with your current sources.'), askFan: true, label: t('Needs a choice'), tone: 'warn' }
  }
  const count = s.total ? `${s.have} / ${s.total}` : t('Nothing yet')
  if (s.total === 0) {
    return { line: t('AniList doesn’t say how many volumes this series has yet. Usherr adds them as it finds them, looking in {where}.', { where: lookingIn(src) }), askFan: false, label: count, tone: 'manga' }
  }
  if (s.wanted > 0) {
    return { line: [having, s.have === 0 ? t('Usherr is looking for them in {where}.', { where: lookingIn(src) }) : t('Usherr is looking for the rest in {where}.', { where: lookingIn(src) })].join(' '), askFan: false, label: count, tone: 'manga' }
  }
  if (s.monitorNew) {
    return { line: [having, t('Usherr only looks for new volumes. Mark any others you want below.')].join(' '), askFan: false, label: count, tone: 'manga' }
  }
  return { line: [having, t('Usherr isn’t looking for any. Mark the ones you want below.')].join(' '), askFan: false, label: count, tone: '' }
}

// A date as people say it: today, tomorrow, a weekday this week, else the date.
export function sayDay(iso: string, now: Date = new Date()): string {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return ''
  const days = Math.round((new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime() - new Date(now.getFullYear(), now.getMonth(), now.getDate()).getTime()) / 86400000)
  if (days === 0) return t('today')
  if (days === -1) return t('yesterday')
  if (days === 1) return t('tomorrow')
  if (days > 1 && days < 7) return d.toLocaleDateString(undefined, { weekday: 'long' })
  return d.toLocaleDateString(undefined, { day: 'numeric', month: 'short' })
}

// "Last search today. Next look Friday."
export function mangaWhen(search: MangaSearchState, now: Date = new Date()): string {
  const parts: string[] = []
  if (search.last) parts.push(t('Last search {when}.', { when: sayDay(search.last.searchedAt, now) }))
  if (search.nextLook) parts.push(new Date(search.nextLook).getTime() <= now.getTime() ? t('Next look soon.') : t('Next look {when}.', { when: sayDay(search.nextLook, now) }))
  return parts.join(' ')
}

// What was turned down, as a phrase with its count: "2 in French".
function rejectPhrase(key: string, n: number): string {
  if (key.startsWith('language:')) return t('{n} in {language}', { n, language: key.slice('language:'.length) })
  switch (key) {
    case 'volume':
      return plural(n, '{n} other volume', '{n} other volumes')
    case 'chapter':
      return plural(n, '{n} chapter', '{n} chapters')
    case 'series':
      return plural(n, '{n} other series', '{n} other series')
    case 'fan':
      return plural(n, '{n} fan scan (those are off)', '{n} fan scans (those are off)')
    case 'official':
      return plural(n, '{n} official edition (those are off)', '{n} official editions (those are off)')
    case 'nyaa':
      return plural(n, '{n} from Nyaa (that’s off)', '{n} from Nyaa (that’s off)')
    case 'torrent':
      return plural(n, '{n} torrent (those are off)', '{n} torrents (those are off)')
    case 'dead':
      return plural(n, '{n} torrent nobody is sharing', '{n} torrents nobody is sharing')
    case 'blocked':
      return plural(n, '{n} on your blocklist', '{n} on your blocklist')
    case 'format':
      return plural(n, '{n} not a volume of pictures', '{n} not volumes of pictures')
    case 'category':
      return plural(n, '{n} outside the book categories', '{n} outside the book categories')
  }
  return `${n} ${key}`
}

// The lines under "Why nothing yet?".
export function mangaWhy(sum: MangaSummary, number?: string): string[] {
  const lines: string[] = []
  if (sum.noIndex) return [t('You have no indexers to search. Add one in Settings > Indexers & Search.')]
  const asked = sum.asked - (sum.failed ?? 0)
  if (asked > 0) {
    lines.push(number ? plural(asked, 'Searched {n} indexer for Volume {number}.', 'Searched {n} indexers for Volume {number}.', { number }) : plural(asked, 'Searched {n} indexer by the series’ name.', 'Searched {n} indexers by the series’ name.'))
  }
  if (sum.failed) lines.push(plural(sum.failed, '{n} indexer didn’t answer.', '{n} indexers didn’t answer.'))
  if (sum.skipped) lines.push(plural(sum.skipped, '{n} indexer wasn’t asked: it’s paused, or has used its searches for today.', '{n} indexers weren’t asked: they’re paused, or have used their searches for today.'))
  if (asked === 0) return lines
  if (sum.found === 0) {
    lines.push(t('No results at all.'))
    return lines
  }
  const reasons = Object.entries(sum.rejected ?? {})
    .sort((a, b) => b[1] - a[1])
    .map(([k, n]) => rejectPhrase(k, n))
  lines.push(reasons.length ? plural(sum.found, 'Found {n} result: {why}.', 'Found {n} results: {why}.', { why: reasons.join(', ') }) : plural(sum.found, 'Found {n} result.', 'Found {n} results.'))
  if (sum.doubtful) lines.push(plural(sum.doubtful, '{n} might be it, and is waiting for you.', '{n} might be it, and are waiting for you.'))
  if (!sum.taken && !sum.doubtful) lines.push(t('Nothing was taken, so nothing was wasted.'))
  return lines
}

// One sentence on a series' chapters, and when Usherr looks next.
export function chapterStatus(c: MangaChapterSummary, now: Date = new Date()): string {
  if (!c.allowed) return c.have > 0 ? t('MangaDex is off, so no new chapters come. The ones here stay yours to read.') : t('Chapters come from MangaDex, which is off for this series. Turn it on in Settings > Manga.')
  if (c.away) {
    const when = sayTime(c.away.until, now)
    return c.away.kind === 'busy' ? t('MangaDex asked Usherr to slow down, so it isn’t being asked until {when}.', { when }) : c.away.kind === 'refused' ? t('MangaDex is refusing requests from this server, so it isn’t being asked until {when}. If this keeps happening, your address may be on a shared or VPN network.', { when }) : t('MangaDex isn’t answering. Usherr tries again at {when}.', { when })
  }
  if (c.link === 'unsure') return t('Usherr isn’t sure which entry on MangaDex is this series. Choose it below.')
  if (c.link === 'none') return t('Nothing yet. MangaDex doesn’t seem to have this series.')
  if (!c.follow) return c.have > 0 ? plural(c.have, '{n} chapter here. Usherr isn’t looking for more.', '{n} chapters here. Usherr isn’t looking for more.') : t('You aren’t following this series’ chapters. Official volumes stay the default.')
  const parts: string[] = []
  if (c.stats.listed > 0 && c.stats.readable === 0) parts.push(t('MangaDex lists this series, but no chapter can be downloaded from there.'))
  else if (c.have > 0 || c.total > 0) parts.push(t('{have} of {total} chapters here.', { have: c.have, total: c.total }))
  else parts.push(t('Nothing yet. Usherr is about to look on MangaDex.'))
  if (c.wanted > 0 && !c.tonight) parts.push(plural(c.wanted, '{n} more is on its way.', '{n} more are on their way.'))
  if (c.tonight) parts.push(plural(c.wanted, '{n} is waiting for the night: big catch-ups run between 2 and 9 in the morning.', '{n} are waiting for the night: big catch-ups run between 2 and 9 in the morning.'))
  if (c.nextAt) parts.push(new Date(c.nextAt).getTime() <= now.getTime() ? t('Next look soon.') : t('Next look {when}.', { when: sayDay(c.nextAt, now) }))
  return parts.join(' ')
}

function sayTime(iso: string, now: Date): string {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return t('later')
  const clock = d.toLocaleTimeString(undefined, { hour: 'numeric', minute: '2-digit' })
  return d.toDateString() === now.toDateString() ? clock : `${sayDay(iso, now)} ${clock}`
}

// The lines under "Why nothing yet?", for chapters. This is what the old apps never
// said: a series can look fine on the site and have nothing to download.
export function chapterWhy(c: MangaChapterSummary, licensed: boolean): string[] {
  if (!c.allowed) return []
  if (c.link === 'none') return [t('Usherr searched MangaDex by this series’ titles and found no entry for it.'), t('You can paste its MangaDex address on the Chapters tab, if you know it.')]
  if (c.link === 'unsure' || !c.follow || c.have > 0 || c.stats.readable > 0) return []
  const s = c.stats
  const licensedThere = licensed || !!s.licensedLink
  const lines: string[] = []
  if (s.listed === 0) {
    if (c.empties === undefined || c.empties === 0) return []
    const others = (s.languages ?? []).filter((l) => l !== 'en')
    lines.push(t('MangaDex has this series, with no chapters in your language.'))
    if (others.length) lines.push(t('It has chapters in: {languages}.', { languages: others.join(', ') }))
    if (licensedThere) lines.push(t('Publishers usually have fan scans of a licensed series taken down, so that’s the likely reason. Look for official volumes instead.'))
    else if (others.length === 0) lines.push(t('A series with no chapters at all has usually had them taken down, or nobody has uploaded any.'))
  } else {
    lines.push(plural(s.listed, 'MangaDex lists {n} chapter, and none can be downloaded.', 'MangaDex lists {n} chapters, and none can be downloaded.'))
    if (s.unavailable) lines.push(plural(s.unavailable, '{n} was taken down.', '{n} were taken down.'))
    if (s.external) lines.push(plural(s.external, '{n} is only a link to read on another site, which Usherr can’t download.', '{n} are only links to read on another site, which Usherr can’t download.'))
    if (licensedThere) lines.push(t('This series is licensed in English, and publishers usually have fan scans taken down, so that’s the likely reason. Look for official volumes instead.'))
    else lines.push(t('Publishers often have fan scans taken down, so that’s the likely reason, even if the series looks fine on the site.'))
  }
  if (c.nextAt) lines.push(t('Usherr asks less often while nothing’s there, and looks next on {when}.', { when: sayDay(c.nextAt) }))
  return lines
}

// What a series' source list comes to, in one sentence.
export function looksIn(src: { official: boolean; fan: boolean; mangadex: boolean; nyaa: boolean }): string {
  const l: string[] = []
  if (src.official) l.push(t('official English editions on your indexers'))
  if (src.fan) l.push(t('fan-scan volumes on your indexers'))
  if (src.mangadex) l.push(t('fan-scan chapters on MangaDex, for series you follow by chapter'))
  if (src.nyaa) l.push(t('Nyaa'))
  return l.length ? t('Usherr looks at your own files, then {list}.', { list: l.join(', then ') }) : t('Usherr only looks at your own files. Nothing will download.')
}

// The plain warning before Nyaa is turned on.
export function nyaaWarning(): string {
  return t('Nyaa is a torrent index. While a torrent seeds, your connection uploads it to other people, and that is the part rights holders tend to pursue. Check your VPN and your seeding limits in Settings > Downloading > Torrents.')
}

// What Nyaa will do on this server, in plain sentences.
export function nyaaLines(n: { client: string; vpnRequired: boolean; vpnConnected: boolean; indexersThroughVpn: boolean; seedRatio?: string; seedHours?: string }): string[] {
  const lines: string[] = []
  if (n.client === 'debrid') {
    lines.push(t('Torrents go to your debrid service, so it does the sharing and not this server.'))
  } else if (n.client === 'external') {
    lines.push(t('Torrents go to your own download client, and its VPN and seeding settings apply.'))
  } else {
    lines.push(t('Torrents upload to others while they seed, and that is the part rights holders tend to pursue.'))
    if (n.vpnRequired) lines.push(n.vpnConnected ? t('Your VPN rule applies: torrents only run through the VPN, and it’s connected.') : t('Your VPN rule applies: torrents only run through the VPN, and it isn’t connected, so Nyaa’s downloads wait for it.'))
    else if (n.vpnConnected) lines.push(t('Torrents go through your VPN while it’s connected, but nothing makes them wait for it.'))
    else lines.push(t('No VPN is in use, so other people can see this server’s address.'))
    if (n.seedRatio === '0' || n.seedHours === '0') lines.push(t('Your settings say not to seed.'))
  }
  if (n.indexersThroughVpn) lines.push(t('Searches go through your VPN too.'))
  return lines
}

// What a series may take: the settings, and its own choice on top (as the server does).
export function sourcesOf(s: Pick<MangaSeries, 'sourceMode'>, base: MangaSources): MangaSources {
  if (s.sourceMode === 'official') return { ...base, official: true, fan: false }
  if (s.sourceMode === 'fan') return { ...base, fan: true }
  return base
}

// Two colours for a made-up cover, from a series' title.
export function coverTones(title: string): { a: string; b: string } {
  let h = 0
  for (const c of title) h = (h * 31 + (c.codePointAt(0) ?? 0)) % 360
  return { a: `hsl(${h} 50% 34%)`, b: `hsl(${(h + 24) % 360} 58% 16%)` }
}
