import { test } from 'node:test'
import assert from 'node:assert/strict'
import { bookLeaves, bookPad, bookToPage, keyTurn, pageToBook, spreadEnd, spreadOf, stepSpread, swipeTurn, tapTurn } from './comicReading.ts'

test('keys follow the direction; Space and the page keys are always the same', () => {
  assert.equal(keyTurn('ArrowRight', false), 1)
  assert.equal(keyTurn('ArrowLeft', false), -1)
  assert.equal(keyTurn('ArrowRight', true), -1)
  assert.equal(keyTurn('ArrowLeft', true), 1)
  for (const rtl of [false, true]) {
    assert.equal(keyTurn(' ', rtl), 1)
    assert.equal(keyTurn('PageDown', rtl), 1)
    assert.equal(keyTurn('PageUp', rtl), -1)
    assert.equal(keyTurn('Escape', rtl), 0)
  }
})

test('tap zones and swipes follow the direction', () => {
  assert.equal(tapTurn(0.1, false), -1)
  assert.equal(tapTurn(0.5, false), 1)
  assert.equal(tapTurn(0.9, false), 1)
  assert.equal(tapTurn(0.1, true), 1)
  assert.equal(tapTurn(0.5, true), 1)
  assert.equal(tapTurn(0.9, true), -1)
  assert.equal(swipeTurn(-80, false), 1)
  assert.equal(swipeTurn(80, false), -1)
  assert.equal(swipeTurn(-80, true), -1)
  assert.equal(swipeTurn(80, true), 1)
})

test('spreads: the cover alone, then pairs', () => {
  assert.deepEqual(spreadOf(1, 6), [1])
  assert.deepEqual(spreadOf(2, 6), [2, 3])
  assert.deepEqual(spreadOf(3, 6), [2, 3])
  assert.deepEqual(spreadOf(6, 6), [6])
  assert.deepEqual(spreadOf(4, 5), [4, 5])
  assert.equal(spreadEnd(4, 5), 5)
})

test('stepping by spreads reaches the last page and stays in range', () => {
  const walk: number[] = []
  let p = 1
  for (let i = 0; i < 5; i++) walk.push((p = stepSpread(p, 1, 5)))
  assert.deepEqual(walk, [3, 5, 5, 5, 5])
  assert.equal(stepSpread(5, -1, 5), 3)
  assert.equal(stepSpread(3, -1, 5), 1)
  assert.equal(stepSpread(1, -1, 5), 1)
  assert.equal(stepSpread(5, 1, 6), 6)
})

test('the left-to-right book reports the furthest page showing', () => {
  // Odd: 1 | 2 3 | 4 5
  assert.equal(bookToPage(0, 5, false, 0, true), 1)
  assert.equal(bookToPage(1, 5, false, 0, true), 3)
  assert.equal(bookToPage(3, 5, false, 0, true), 5)
  // Even: 1 | 2 3 | 4
  assert.equal(bookToPage(3, 4, false, 0, true), 4)
  // One page at a time
  assert.equal(bookToPage(1, 5, false, 0, false), 2)
  assert.equal(pageToBook(1, 5, false, 0), 0)
  assert.equal(pageToBook(5, 5, false, 0), 4)
})

test('the right-to-left book runs backwards, with the cover alone', () => {
  // Even: 4 | 3 2 | 1
  assert.equal(bookPad(4, true, true), 0)
  assert.deepEqual(bookLeaves(4, true, 0), [4, 3, 2, 1])
  assert.equal(pageToBook(1, 4, true, 0), 3)
  assert.equal(bookToPage(3, 4, true, 0, true), 1)
  assert.equal(bookToPage(1, 4, true, 0, true), 3)
  assert.equal(bookToPage(0, 4, true, 0, true), 4)
  // Odd: a blank at the back keeps the cover alone: blank | 5 4 | 3 2 | 1
  assert.equal(bookPad(5, true, true), 1)
  assert.deepEqual(bookLeaves(5, true, 1), [0, 5, 4, 3, 2, 1])
  assert.equal(pageToBook(1, 5, true, 1), 5)
  assert.equal(bookToPage(5, 5, true, 1, true), 1)
  assert.equal(bookToPage(3, 5, true, 1, true), 3)
  assert.equal(bookToPage(1, 5, true, 1, true), 5)
  assert.equal(bookToPage(0, 5, true, 1, true), 5)
  // Held upright there are no pairs, so no blank.
  assert.equal(bookPad(5, true, false), 0)
  assert.deepEqual(bookLeaves(3, false, 0), [1, 2, 3])
})
