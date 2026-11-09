import assert from 'node:assert/strict'
import { test } from 'node:test'
import { KEY_ROWS, addRecent, clockText, formatTVCode, gridMove, isAppPath, normalizeTVCode, secondsLeft } from './tvSignIn.ts'

test('a code is shown in two groups and read back however it was typed', () => {
  assert.equal(formatTVCode('ABCDEFGH'), 'ABCD-EFGH')
  assert.equal(normalizeTVCode('abcd-efgh'), 'ABCDEFGH')
  assert.equal(normalizeTVCode(' abcd efgh '), 'ABCDEFGH')
  assert.equal(normalizeTVCode('ABCD-EFG'), '')
  assert.equal(normalizeTVCode('ABCD0FGH'), '') // 0 is never in a code
})

test('the countdown', () => {
  assert.equal(secondsLeft('2026-11-08T10:05:00Z', Date.parse('2026-11-08T10:00:00.500Z')), 300)
  assert.equal(secondsLeft('2026-11-08T10:05:00Z', Date.parse('2026-11-08T10:06:00Z')), 0)
  assert.equal(secondsLeft('nonsense', 0), 0)
  assert.equal(clockText(299), '4:59')
  assert.equal(clockText(5), '0:05')
})

test('only paths inside the app go to the phone', () => {
  for (const p of ['/', '/title/603', '/you/diary?new=1', '/crews/4#chat']) assert.equal(isAppPath(p), true, p)
  for (const p of ['', 'title/1', '//evil.example', 'https://evil.example', '/\\evil', '/../x', '/a b', '/títle', '/x/..?y']) assert.equal(isAppPath(p), false, p)
})

test('arrow keys move around the keyboard and stop at its edges', () => {
  const rows = [7, 7, 3]
  assert.deepEqual(gridMove(rows, 0, 0, 'ArrowRight'), [0, 1])
  assert.equal(gridMove(rows, 0, 0, 'ArrowLeft'), null)
  assert.equal(gridMove(rows, 0, 3, 'ArrowUp'), null)
  assert.deepEqual(gridMove(rows, 1, 6, 'ArrowDown'), [2, 2]) // a shorter row: the nearest key
  assert.equal(gridMove(rows, 2, 1, 'ArrowDown'), null)
  assert.equal(gridMove(rows, 0, 0, 'Enter'), null)
  assert.ok(KEY_ROWS.every((r) => r.length === KEY_ROWS[0].length))
})

test('recent searches: newest first, once each, at most eight', () => {
  let list: string[] = []
  for (const q of ['dune', 'alien', 'Dune', '  ', 'a', 'b', 'c', 'd', 'e', 'f']) list = addRecent(list, q)
  assert.deepEqual(list, ['f', 'e', 'd', 'c', 'b', 'a', 'Dune', 'alien'])
})
