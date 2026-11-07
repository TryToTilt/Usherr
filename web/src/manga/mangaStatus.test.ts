// Run with: npm test
import assert from 'node:assert/strict'
import { test } from 'node:test'
import { chapterStatus, chapterWhy, looksIn, mangaStatus, mangaWhen, mangaWhy, nyaaLines, sayDay } from './mangaStatus.ts'
import type { MangaChapterSummary, MangaSeries } from './mangaApi.ts'

const dex = (over: Partial<MangaChapterSummary>): MangaChapterSummary => ({
  allowed: true, follow: 'new', link: 'linked', total: 0, have: 0, wanted: 0, stats: { listed: 0, readable: 0, external: 0, unavailable: 0 }, ...over,
})

test('an empty series on MangaDex says why, as Tranga never did', () => {
  const taken = chapterWhy(dex({ empties: 3, stats: { listed: 61, readable: 0, external: 1, unavailable: 60 } }), true).join(' ')
  assert.match(taken, /61 chapters, and none can be downloaded/)
  assert.match(taken, /60 were taken down/)
  assert.match(taken, /1 is only a link/)
  assert.match(taken, /licensed in English/)
  assert.match(chapterWhy(dex({ empties: 1, stats: { listed: 3, readable: 0, external: 0, unavailable: 3, licensedLink: 'https://x' } }), false).join(' '), /licensed in English/)
  assert.match(chapterWhy(dex({ empties: 1, stats: { listed: 3, readable: 0, external: 0, unavailable: 3 } }), false).join(' '), /even if the series looks fine/)
  assert.match(chapterWhy(dex({ empties: 1, stats: { listed: 0, readable: 0, external: 0, unavailable: 0, languages: ['es'] } }), false).join(' '), /It has chapters in: es/)
  assert.match(chapterWhy(dex({ link: 'none' }), false).join(' '), /found no entry/)
  assert.deepEqual(chapterWhy(dex({ have: 2, stats: { listed: 2, readable: 2, external: 0, unavailable: 0 } }), false), [], 'nothing to explain')
  assert.deepEqual(chapterWhy(dex({ allowed: false, link: 'none' }), false), [], 'MangaDex is off: it has no say')
})

test('chapters say what’s happening, and when MangaDex is away', () => {
  assert.match(chapterStatus(dex({ total: 40, have: 12, wanted: 2 })), /12 of 40 chapters here. 2 more are on their way/)
  assert.match(chapterStatus(dex({ total: 300, wanted: 300, tonight: true })), /between 2 and 9/)
  assert.match(chapterStatus(dex({ away: { kind: 'busy', until: '2030-01-01T10:00:00Z' } })), /slow down/)
  assert.match(chapterStatus(dex({ away: { kind: 'down', until: '2030-01-01T10:00:00Z' } })), /isn’t answering/)
  assert.match(chapterStatus(dex({ link: 'unsure' })), /isn’t sure/)
  assert.match(chapterStatus(dex({ allowed: false, have: 3 })), /stay yours/)
  assert.match(chapterStatus(dex({ follow: '' })), /Official volumes stay the default/)
})

test('a series following chapters isn’t told nothing can come', () => {
  const st = mangaStatus({ ...baseSeries, licensedEn: -1, have: 0, wanted: 0, total: 0, following: true, chaptersHave: 3 }, { official: true, fan: false, nyaa: false }, [])
  assert.equal(st.askFan, false)
  assert.match(st.line, /3 here so far/)
})

const baseSeries: MangaSeries = {
  id: 1, title: 'Lanternfish Harbor', format: 'manga', status: 'finished', startYear: 2015, author: '', publisher: 'VIZ', licensedEn: 1, volumeCount: 37,
  monitorNew: true, wantAll: true, sourceMode: '', addedAt: '', total: 37, have: 12, wanted: 25, downloading: 0, waiting: 0,
}

test('Nyaa says what it will do with your VPN', () => {
  const base = { client: 'builtin', vpnRequired: false, vpnConnected: false, indexersThroughVpn: false }
  assert.match(nyaaLines(base).join(' '), /can see this server/)
  assert.match(nyaaLines({ ...base, vpnRequired: true }).join(' '), /wait for it/)
  assert.match(nyaaLines({ ...base, vpnRequired: true, vpnConnected: true }).join(' '), /it’s connected/)
  assert.match(nyaaLines({ ...base, client: 'debrid' }).join(' '), /debrid/)
  assert.match(nyaaLines({ ...base, seedRatio: '0' }).join(' '), /not to seed/)
  assert.match(nyaaLines({ ...base, indexersThroughVpn: true }).join(' '), /Searches go through your VPN/)
})

const series = (over: Partial<MangaSeries>): MangaSeries => ({ ...baseSeries, ...over })
const official = { official: true, fan: false, nyaa: false }

test('every state of a series says what is happening', () => {
  assert.match(mangaStatus(series({}), official, []).line, /12 of 37 volumes.*looking for the rest in official English editions/)
  assert.match(mangaStatus(series({ have: 37, wanted: 0 }), official, []).line, /all 37 volumes/)
  assert.match(mangaStatus(series({ status: 'ongoing', have: 37, wanted: 0 }), official, []).line, /next one when it turns up/)
  assert.match(mangaStatus(series({ waiting: 2 }), official, []).line, /2 possible matches/)
  assert.match(mangaStatus(series({ downloading: 1 }), official, []).line, /1 download on its way/)
  assert.match(mangaStatus(series({ wanted: 0, wantAll: false }), official, []).line, /only looks for new volumes/)
  assert.match(mangaStatus(series({ wanted: 0, monitorNew: false }), official, []).line, /isn.t looking for any/)
  assert.match(mangaStatus(series({ total: 0, have: 0, wanted: 0 }), official, []).line, /doesn.t say how many/)
})

test('what stops anything coming is said before anything else', () => {
  assert.match(mangaStatus(series({}), official, ['no-folder']).line, /no manga folder/)
  assert.match(mangaStatus(series({}), official, ['no-indexers']).line, /no indexers/)
  assert.match(mangaStatus(series({}), { official: false, fan: false, nyaa: false }, []).line, /Every source is switched off/)
})

test('no official edition known and fan scans off: says so and offers the choice', () => {
  const s = series({ licensedEn: -1, publisher: '', have: 0 })
  const st = mangaStatus(s, official, [])
  assert.equal(st.askFan, true)
  assert.match(st.line, /No official English edition is known/)
  assert.equal(mangaStatus(s, { official: true, fan: true, nyaa: false }, []).askFan, false)
})

test('why nothing came, in plain words', () => {
  assert.deepEqual(mangaWhy({ asked: 3, found: 4, rejected: { 'language:French': 2, volume: 2 } }, '14'), [
    'Searched 3 indexers for Volume 14.',
    'Found 4 results: 2 in French, 2 other volumes.',
    'Nothing was taken, so nothing was wasted.',
  ])
  assert.deepEqual(mangaWhy({ asked: 3, found: 0 }, '6'), ['Searched 3 indexers for Volume 6.', 'No results at all.'])
  assert.deepEqual(mangaWhy({ asked: 0, found: 0, skipped: 2 }), ['2 indexers weren’t asked: they’re paused, or have used their searches for today.'])
  assert.deepEqual(mangaWhy({ asked: 0, found: 0, noIndex: true }), ['You have no indexers to search. Add one in Settings > Indexers & Search.'])
  assert.deepEqual(mangaWhy({ asked: 2, failed: 1, found: 1, doubtful: 1 }), ['Searched 1 indexer by the series’ name.', '1 indexer didn’t answer.', 'Found 1 result.', '1 might be it, and is waiting for you.'])
})

test('days as people say them', () => {
  const now = new Date(2026, 10, 6, 12, 0)
  assert.equal(sayDay(new Date(2026, 10, 6, 8, 0).toISOString(), now), 'today')
  assert.equal(sayDay(new Date(2026, 10, 7, 8, 0).toISOString(), now), 'tomorrow')
  assert.equal(sayDay(new Date(2026, 10, 5, 8, 0).toISOString(), now), 'yesterday')
  assert.equal(mangaWhen({ last: { searchedAt: new Date(2026, 10, 6, 8, 0).toISOString(), outcome: 'nothing', summary: { asked: 1, found: 0 } }, nextLook: new Date(2026, 10, 7, 8, 0).toISOString() }, now), 'Last search today. Next look tomorrow.')
})

test('the source sentence follows the switches', () => {
  assert.equal(looksIn({ official: true, fan: false, mangadex: false, nyaa: false }), 'Usherr looks at your own files, then official English editions on your indexers.')
  assert.equal(looksIn({ official: false, fan: false, mangadex: false, nyaa: false }), 'Usherr only looks at your own files. Nothing will download.')
})
