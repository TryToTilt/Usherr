import assert from 'node:assert/strict'
import { test } from 'node:test'
import { absoluteTime, bookPercent, buildChapters, chapterAt, formatAudioPosition, formatClock, locateTime, nextSpeed, openingPage, overallPercent, parseAudioPosition, resumePoint, smartRewind, timelineOf, trackLabel } from './shelfMath.ts'

test('one timeline across the files', () => {
  const tl = [100, 50.5, 200]
  for (const [track, seconds, at] of [[0, 0, 0], [0, 99.5, 99.5], [1, 0, 100], [1, 50, 150], [2, 10, 160.5], [2, 200, 350.5]]) {
    assert.equal(absoluteTime(tl, track, seconds), at)
    assert.deepEqual(locateTime(tl, at), { track, seconds })
  }
  assert.equal(absoluteTime(tl, 0, 100.4), 100, 'a moment past the end stays in its file')
  assert.deepEqual(locateTime(tl, 1000), { track: 2, seconds: 200 })
  assert.deepEqual(locateTime(tl, -5), { track: 0, seconds: 0 })
})

test('the timeline is known when every file is', () => {
  assert.deepEqual(timelineOf([{ duration: 10 }, { duration: 20 }]), [10, 20])
  assert.equal(timelineOf([{ duration: 10 }, {}]), null)
  assert.equal(timelineOf([]), null)
  assert.deepEqual(timelineOf([{ duration: 10 }, {}], 1, 30), [10, 30], 'the file playing fills in its own length')
  assert.equal(timelineOf([{ duration: 10 }, {}], 1, Number.NaN), null)
  assert.equal(timelineOf([{ duration: 10 }, {}, {}], 1, 30), null)
})

test('a place saved before the lengths were known carries over', () => {
  // The same "track:seconds" means the same spot; only the percent becomes true.
  const saved = parseAudioPosition('1:500')
  const tracks = [{ size: 100 }, { size: 900 }]
  assert.equal(bookPercent(tracks, null, saved.track, saved.seconds, 1000), 55, 'from sizes')
  const tl = [3000, 1000]
  assert.equal(bookPercent(tracks, tl, saved.track, saved.seconds, 1000), 87.5, 'true')
  assert.equal(formatAudioPosition(locateTime(tl, absoluteTime(tl, saved.track, saved.seconds))), '1:500')
  assert.equal(bookPercent(tracks, tl, 0, 3000.4, 3000), 75, 'rounding past a file does not move on')
  assert.equal(resumePoint(93.5, 1000), 93.5)
  assert.equal(resumePoint(1000.4, 1000), 999, 'just before the end, not the start')
  assert.equal(resumePoint(50, Number.POSITIVE_INFINITY), 50)
  assert.equal(resumePoint(50, Number.NaN), 50)
})

test('smart rewind goes back more after a longer pause', () => {
  const sec = 1000
  const min = 60 * sec
  assert.equal(smartRewind(2 * sec, 'more'), 0, 'not after a moment')
  assert.equal(smartRewind(30 * sec, 'more'), 2)
  assert.equal(smartRewind(5 * min, 'more'), 5)
  assert.equal(smartRewind(30 * min, 'more'), 10)
  assert.equal(smartRewind(5 * 60 * min, 'more'), 20)
  assert.equal(smartRewind(48 * 60 * min, 'more'), 30)
  assert.equal(smartRewind(30 * sec, 'little'), 1)
  assert.equal(smartRewind(48 * 60 * min, 'little'), 15)
  assert.equal(smartRewind(48 * 60 * min, 'off'), 0)
  assert.equal(smartRewind(Number.NaN, 'more'), 0)
})

test('audiobook positions read and write', () => {
  assert.deepEqual(parseAudioPosition('2:93.5'), { track: 2, seconds: 93.5 })
  assert.deepEqual(parseAudioPosition(''), { track: 0, seconds: 0 })
  assert.deepEqual(parseAudioPosition('epubcfi(/6/2)'), { track: 0, seconds: 0 })
  assert.equal(formatAudioPosition({ track: 1, seconds: 12.345 }), '1:12.3')
})

test('how far through the book', () => {
  assert.equal(overallPercent([100, 100], 0, 0.5), 25)
  assert.equal(overallPercent([100, 300], 1, 0), 25)
  assert.equal(overallPercent([100, 300], 1, 1), 100)
  assert.equal(overallPercent([], 0, 0.5), 0)
  assert.equal(overallPercent([100], 3, 0.5), 0)
  assert.equal(overallPercent([100], 0, Number.NaN), 0)
})

test('clock times', () => {
  assert.equal(formatClock(75), '1:15')
  assert.equal(formatClock(3725), '1:02:05')
  assert.equal(formatClock(Number.NaN), '0:00')
})

test('speeds go round', () => {
  assert.equal(nextSpeed(1), 1.2)
  assert.equal(nextSpeed(2), 0.8)
  assert.equal(nextSpeed(1.1), 1.2)
})

test('track names', () => {
  assert.equal(trackLabel('01 - Chapter One', 0), 'Chapter One')
  assert.equal(trackLabel('07', 6), 'Part 7')
  assert.equal(trackLabel('The Hobbit', 0), 'The Hobbit')
})

test('chapters from files and from marks inside a file', () => {
  const files = buildChapters([{ name: '01 - Opening' }, { name: '02 - The End' }])
  assert.deepEqual(
    files.map((c) => [c.track, c.start, c.end, c.title]),
    [
      [0, 0, undefined, 'Opening'],
      [1, 0, undefined, 'The End'],
    ],
  )
  const one = buildChapters([
    {
      name: 'Book',
      chapters: [
        { title: 'Two', start: 600 },
        { title: 'Credits', start: 0.4 },
        { title: '', start: 1800 },
      ],
    },
  ])
  assert.deepEqual(
    one.map((c) => [c.track, c.start, c.end, c.title]),
    [
      [0, 0, 600, 'Credits'],
      [0, 600, 1800, 'Two'],
      [0, 1800, undefined, 'Chapter 3'],
    ],
  )
  // A file with a single mark counts as one chapter.
  assert.equal(buildChapters([{ name: '01 - Solo', chapters: [{ title: 'Only', start: 0 }] }])[0].title, 'Solo')
})

test('which chapter is playing', () => {
  const ch = buildChapters([{ name: 'a' }, { name: 'b', chapters: [{ title: 'x', start: 0 }, { title: 'y', start: 100 }] }, { name: 'c' }])
  assert.equal(chapterAt(ch, 0, 50), 0)
  assert.equal(chapterAt(ch, 1, 0), 1)
  assert.equal(chapterAt(ch, 1, 99.9), 2)
  assert.equal(chapterAt(ch, 1, 150), 2)
  assert.equal(chapterAt(ch, 2, 10), 3)
  assert.equal(chapterAt(ch, 9, 10), 0)
})

test('a comic opens on the saved page when it is one of its pages', () => {
  assert.equal(openingPage('page:37', 40), 37)
  assert.equal(openingPage('page:40', 40), 40)
  assert.equal(openingPage('page:41', 40), 1)
  assert.equal(openingPage('page:0', 40), 1)
  assert.equal(openingPage('page:x', 40), 1)
  assert.equal(openingPage('epubcfi(/6/2)', 40), 1)
  assert.equal(openingPage(undefined, 40), 1)
})
