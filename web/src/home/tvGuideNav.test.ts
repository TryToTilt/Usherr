import { test } from 'node:test'
import assert from 'node:assert/strict'
import { floorTo, focusTime, MIN, nextRow, pct, sameTimeIndex, SLOT, windowFor, type Span } from './tvGuideNav.ts'

const T0 = Date.UTC(2026, 10, 8, 20, 0) // 8:00 PM
const at = (min: number) => T0 + min * MIN
const p = (from: number, to: number): Span => ({ start: at(from), end: at(to) })

// Two channels: half-hour sitcoms, and a film then a news hour.
const sitcoms = [p(0, 30), p(30, 60), p(60, 90), p(90, 120)]
const movies = [p(-40, 75), p(75, 135)]

test('Up and Down land on what is on at the same time', () => {
  // from the film (on since 7:20, the window starts at 8:00): the 8:00 sitcom
  assert.equal(sameTimeIndex(sitcoms, focusTime(movies[0], T0)), 0)
  // from the 8:30 sitcom: still the film
  assert.equal(sameTimeIndex(movies, focusTime(sitcoms[1], T0)), 0)
  // from the 9:30 sitcom: the 9:15 news
  assert.equal(sameTimeIndex(movies, focusTime(sitcoms[3], T0)), 1)
  // from the 9:15 news: the 9:00 sitcom (on at 9:15)
  assert.equal(sameTimeIndex(sitcoms, focusTime(movies[1], T0)), 2)
})

test('a gap in a row goes to the next programme, past the end to the last', () => {
  const gappy = [p(0, 30), p(60, 90)]
  assert.equal(sameTimeIndex(gappy, at(40)), 1)
  assert.equal(sameTimeIndex(gappy, at(200)), 1)
  assert.equal(sameTimeIndex([], at(0)), -1)
})

test('the focus time never sits before the window', () => {
  assert.equal(focusTime(movies[0], T0), T0)
  assert.equal(focusTime(sitcoms[1], T0), at(30))
})

test('Up and Down skip empty rows and stop at the ends', () => {
  const rows = [sitcoms, [], movies]
  assert.equal(nextRow(rows, 0, 1), 2)
  assert.equal(nextRow(rows, 2, -1), 0)
  assert.equal(nextRow(rows, 2, 1), -1)
  assert.equal(nextRow(rows, 0, -1), -1)
})

test('the window follows the focus at its edges', () => {
  const len = 2 * 60 * MIN
  const min = at(-180)
  const max = at(24 * 60)
  // on screen (even partly): the window stays
  assert.equal(windowFor(T0, len, sitcoms[3], min, max), T0)
  assert.equal(windowFor(T0, len, movies[0], min, max), T0)
  // past the right edge: its start comes into the window's third column
  assert.equal(windowFor(T0, len, p(120, 150), min, max), at(60))
  assert.equal(windowFor(T0, len, p(135, 165), min, max), at(60))
  // off the left edge: the window starts on its half hour
  assert.equal(windowFor(T0, len, p(-50, -20), min, max), at(-60))
  // never before the guide's start, nor past its end
  assert.equal(windowFor(T0, len, p(-400, -380), min, max), min)
  assert.equal(windowFor(T0, len, p(2000, 2030), min, max), max)
})

test('times round down to the half hour, and sit across the window', () => {
  assert.equal(floorTo(at(44)), at(30))
  assert.equal(floorTo(at(30)), at(30))
  assert.equal(SLOT, 30 * MIN)
  assert.equal(pct(at(30), T0, 120 * MIN), 25)
  assert.equal(pct(at(-10), T0, 120 * MIN), 0)
  assert.equal(pct(at(500), T0, 120 * MIN), 100)
})
