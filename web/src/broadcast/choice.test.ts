// Run with: npm test
import assert from 'node:assert/strict'
import { test } from 'node:test'
import { arrowDir, cycleChoice, switchFlips } from './choice.ts'

const SIZES = [
  { id: 'regular', label: 'Regular' },
  { id: 'large', label: 'Large', disabled: true },
  { id: 'tv', label: 'TV' },
]

test('Left and Right step through the choices and go round', () => {
  assert.equal(cycleChoice(SIZES, 'regular', 1), 'tv') // Large is skipped
  assert.equal(cycleChoice(SIZES, 'tv', 1), 'regular')
  assert.equal(cycleChoice(SIZES, 'regular', -1), 'tv')
  assert.equal(cycleChoice(SIZES, 'tv', -1), 'regular')
})

test('an unknown value starts from the first choice, or the last going back', () => {
  assert.equal(cycleChoice(SIZES, 'nope', 1), 'regular')
  assert.equal(cycleChoice(SIZES, 'nope', -1), 'tv')
})

test('nothing to pick keeps the value', () => {
  assert.equal(cycleChoice([], 'x', 1), 'x')
  assert.equal(cycleChoice([{ id: 'a', label: 'A', disabled: true }], 'a', 1), 'a')
})

test('only the arrow keys change a row', () => {
  assert.equal(arrowDir('ArrowRight'), 1)
  assert.equal(arrowDir('ArrowLeft'), -1)
  assert.equal(arrowDir('Right'), 1)
  assert.equal(arrowDir('ArrowUp'), 0)
  assert.equal(arrowDir('Enter'), 0)
})

test('a switch turns on with Right and off with Left, and otherwise lets the remote move on', () => {
  assert.equal(switchFlips(false, 1), true)
  assert.equal(switchFlips(true, 1), false)
  assert.equal(switchFlips(true, -1), true)
  assert.equal(switchFlips(false, -1), false)
})
