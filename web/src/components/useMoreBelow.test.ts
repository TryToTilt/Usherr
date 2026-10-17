// Run with: npm test (uses node's built-in test runner, no extra packages).
import assert from 'node:assert/strict'
import { test } from 'node:test'
import { moreBelow } from './useMoreBelow.ts'

test('moreBelow is true only while the list can still scroll down', () => {
  const cases: [string, number, number, number, boolean][] = [
    ['short list, no scrolling', 300, 400, 0, false],
    ['top of a long list', 900, 400, 0, true],
    ['middle of a long list', 900, 400, 250, true],
    ['at the bottom', 900, 400, 500, false],
    ['a few pixels from the bottom', 900, 400, 497, false],
    ['rounding at the bottom', 900, 400, 499.6, false],
  ]
  for (const [name, scrollHeight, clientHeight, scrollTop, want] of cases) {
    assert.equal(moreBelow(scrollHeight, clientHeight, scrollTop), want, name)
  }
})
