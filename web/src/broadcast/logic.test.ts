// Run with: npm test
import assert from 'node:assert/strict'
import { test } from 'node:test'
import { firstPlayable, gameToday, minutesLeft, newVolume, onNow, openVote, percentOf, pickHero, playClock, playerBack, posterTitle, resumeTarget, seekTarget, shortCode } from './logic.ts'

test('a seek stays between the start and the end', () => {
  assert.equal(seekTarget(5, -10, 100), 0)
  assert.equal(seekTarget(95, 30, 100), 100)
  assert.equal(seekTarget(40, 10, 100), 50)
  // a live stream or a copy still being made: no known end
  assert.equal(seekTarget(40, 30, Infinity), 70)
  assert.equal(seekTarget(40, 30, NaN), 70)
})

test('the clock and how far through', () => {
  assert.equal(playClock(0), '0:00')
  assert.equal(playClock(65), '1:05')
  assert.equal(playClock(3725), '1:02:05')
  assert.equal(playClock(NaN), '0:00')
  assert.equal(percentOf(50, 200), 25)
  assert.equal(percentOf(10, 0), 0)
  assert.equal(percentOf(300, 200), 100)
})

test('Back in the player: the list, then the controls, then out', () => {
  assert.equal(playerBack({ listOpen: true, controls: true }), 'close-list')
  assert.equal(playerBack({ listOpen: false, controls: true }), 'hide')
  assert.equal(playerBack({ listOpen: false, controls: false }), 'leave')
})

test('Resume plays a library film or episode; a streamed title opens its page', () => {
  assert.deepEqual(resumeTarget({ item: 'movie:12' }), { kind: 'movie', id: 12 })
  assert.deepEqual(resumeTarget({ item: 'episode:7' }), { kind: 'episode', id: 7 })
  assert.equal(resumeTarget({ item: 'title:movie:603' }), null)
  assert.equal(minutesLeft({ position: 3000, duration: 3010 }), 1)
  assert.equal(minutesLeft({ position: 0, duration: 1800 }), 30)
})

test('the hero is the latest thing part way through', () => {
  const c = (item: string, position: number, duration: number) => ({ item, kind: 'movie' as const, title: item, link: '/', position, duration })
  assert.equal(pickHero([c('a', 10, 0), c('b', 100, 1000), c('c', 5, 50)])?.item, 'b')
  assert.equal(pickHero([]), null)
})

test('On now: only what is airing, with how far through', () => {
  const now = Date.parse('2026-11-08T20:30:00Z')
  const ch = (id: number, start?: string, end?: string) => ({ id, number: id, name: `C${id}`, now: start && end ? { title: 'x', start, end } : undefined })
  const list = onNow([ch(1, '2026-11-08T20:00:00Z', '2026-11-08T21:00:00Z'), ch(2), ch(3, '2026-11-08T21:00:00Z', '2026-11-08T22:00:00Z'), ch(4, '2026-11-08T19:00:00Z', '2026-11-08T20:30:00Z')], now)
  assert.deepEqual(
    list.map((c) => [c.id, c.pct]),
    [[1, 50]],
  )
})

test('Tonight: an open vote, a game today, a new volume', () => {
  const now = Date.parse('2026-11-08T18:00:00')
  assert.equal(openVote([{ state: 'closed', closes: now + 1 }, { state: 'open', closes: now - 1 }], now), null)
  assert.equal(openVote([{ state: 'open', closes: now + 50 }, { state: 'open', closes: now + 10 }], now)?.closes, now + 10)
  assert.equal(openVote(undefined, now), null)
  const g = (when: string) => ({ when })
  assert.equal(gameToday([g('2026-11-09T19:00:00'), g('2026-11-08T20:00:00'), g('2026-11-08T19:00:00')], now)?.when, '2026-11-08T19:00:00')
  assert.equal(gameToday([g('2026-11-08T10:00:00')], now), null) // long over
  assert.equal(newVolume([{ volumeId: 1, page: 4 }, { chapterId: 2, page: 0 }, { volumeId: 3, page: 0 }])?.volumeId, 3)
})

test("a poster's link says which title it is", () => {
  assert.deepEqual(posterTitle('/title/603'), { kind: 'movie', tmdbId: 603 })
  assert.deepEqual(posterTitle('http://h:1/usherr/show/1399'), { kind: 'show', tmdbId: 1399 })
  assert.deepEqual(posterTitle('/series/12'), { kind: 'series', id: 12 })
  assert.equal(posterTitle('/library?kind=movie'), null)
  assert.equal(posterTitle('/title/0'), null)
  assert.equal(posterTitle(null), null)
})

test("a show's Play starts at the first downloaded episode, specials last", () => {
  const e = (id: number, season: number, episode: number, status = 'downloaded') => ({ id, season, episode, status })
  assert.equal(firstPlayable([e(1, 0, 1), e(2, 2, 1), e(3, 1, 2), e(4, 1, 1, 'missing')])?.id, 3)
  assert.equal(firstPlayable([e(1, 1, 1, 'missing')]), null)
  assert.equal(shortCode(2, 4), 'S2E4')
})
