// Run with: npm test
import assert from 'node:assert/strict'
import { test } from 'node:test'
import { HIDE_AFTER_MS, INITIAL_NOTICE, noticeWait, SHOW_AFTER_MS, stepNotice, type NoticeState } from './busyNotice.ts'

// Feed a list of [time, busy] readings through the logic and return what shows after each.
function run(readings: Array<[number, boolean]>): boolean[] {
  let state: NoticeState = INITIAL_NOTICE
  return readings.map(([now, busy]) => {
    state = stepNotice(state, busy, now)
    return state.shown
  })
}

test('nothing shows while the app answers normally', () => {
  assert.deepEqual(run([[0, false], [5000, false]]), [false, false])
})

test('one slow answer that is over within a few seconds never shows it', () => {
  assert.deepEqual(
    run([[0, true], [SHOW_AFTER_MS - 1, true], [SHOW_AFTER_MS, false], [SHOW_AFTER_MS + 10_000, false]]),
    [false, false, false, false],
  )
})

test('it shows once the app has been slow for the whole delay', () => {
  assert.deepEqual(run([[0, true], [SHOW_AFTER_MS - 1, true], [SHOW_AFTER_MS, true]]), [false, false, true])
})

test('a short gap in a long problem does not hide it', () => {
  const t0 = 10_000
  const out = run([
    [t0, true],
    [t0 + SHOW_AFTER_MS, true], // shown
    [t0 + SHOW_AFTER_MS + 100, false], // one read answered
    [t0 + SHOW_AFTER_MS + 600, true], // slow again
    [t0 + SHOW_AFTER_MS + HIDE_AFTER_MS + 700, true],
  ])
  assert.deepEqual(out, [false, true, true, true, true])
})

test('it goes away after the app has answered normally for a moment', () => {
  const out = run([
    [0, true],
    [SHOW_AFTER_MS, true],
    [5000, false],
    [5000 + HIDE_AFTER_MS - 1, false],
    [5000 + HIDE_AFTER_MS, false],
  ])
  assert.deepEqual(out, [false, true, true, true, false])
})

test('it can come back after it went away', () => {
  const out = run([
    [0, true],
    [SHOW_AFTER_MS, true],
    [4000, false],
    [4000 + HIDE_AFTER_MS, false],
    [20_000, true],
    [20_000 + SHOW_AFTER_MS, true],
  ])
  assert.deepEqual(out, [false, true, true, false, false, true])
})

test('the wait left says when to look again, and nothing when all is settled', () => {
  let state = stepNotice(INITIAL_NOTICE, true, 1000)
  assert.equal(noticeWait(state, true, 1000), SHOW_AFTER_MS)
  assert.equal(noticeWait(state, true, 1000 + SHOW_AFTER_MS - 500), 500)
  state = stepNotice(state, true, 1000 + SHOW_AFTER_MS)
  assert.equal(state.shown, true)
  assert.equal(noticeWait(state, true, 1000 + SHOW_AFTER_MS), null)
  assert.equal(noticeWait(INITIAL_NOTICE, false, 0), null)
})

test('asking again with nothing new returns the same state', () => {
  const first = stepNotice(INITIAL_NOTICE, true, 0)
  assert.equal(stepNotice(first, true, 100), first)
  assert.equal(stepNotice(INITIAL_NOTICE, false, 100), INITIAL_NOTICE)
})
