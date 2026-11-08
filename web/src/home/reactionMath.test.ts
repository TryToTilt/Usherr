import { test } from 'node:test'
import assert from 'node:assert/strict'
import { dueReplays, filmClock, heatOf, type Reaction } from './reactionMath.ts'

const r = (id: string, word: Reaction['word'], pos: number): Reaction => ({ id, word, pos })

test('heat bars: by stretch of the film, the top word, no pre-show', () => {
  const list = [r('a', 'HA', 12), r('b', 'EEK', 4361), r('c', 'EEK', 4370), r('d', 'NOPE', 4362), r('e', 'WOW', -20), r('f', 'CLAP', 29.9)]
  assert.deepEqual(heatOf(list), [
    { pos: 0, count: 2, top: 'HA' },
    { pos: 4350, count: 3, top: 'EEK' },
  ])
  assert.deepEqual(heatOf([]), [])
})

test('replays float only while the film plays on past them', () => {
  const list = [r('a', 'HA', 10.5), r('b', 'EEK', 11), r('c', 'WOW', 13)]
  assert.deepEqual(
    dueReplays(list, 10, 11.2, new Set()).map((x) => x.id),
    ['a', 'b'],
  )
  assert.deepEqual(
    dueReplays(list, 10, 11.2, new Set(['a'])).map((x) => x.id),
    ['b'],
  )
  assert.deepEqual(dueReplays(list, 0, 30, new Set()), []) // a seek ahead
  assert.deepEqual(dueReplays(list, 14, 10, new Set()), []) // a seek back
})

test('a film position as a clock', () => {
  assert.equal(filmClock(4360), '1:12:40')
  assert.equal(filmClock(65.9), '1:05')
  assert.equal(filmClock(-30), '-0:30')
})
