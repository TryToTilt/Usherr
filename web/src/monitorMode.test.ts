// Run with: npm test (uses node's built-in test runner, no extra packages).
import assert from 'node:assert/strict'
import { test } from 'node:test'
import { monitorModeSentence } from './monitorMode.ts'

test('stopping the episodes that have aired says how many, and that nothing is deleted', () => {
  assert.equal(monitorModeSentence('future', { stopped: 214, started: 0 }), 'This stops monitoring 214 episodes that have already aired. Nothing is deleted.')
  assert.equal(monitorModeSentence('future', { stopped: 1, started: 0 }), 'This stops monitoring 1 episode that has already aired. Nothing is deleted.')
})

test('episodes that start again are mentioned', () => {
  assert.equal(
    monitorModeSentence('future', { stopped: 2, started: 3 }),
    'This stops monitoring 2 episodes that have already aired. It also starts monitoring 3 episodes that have not aired yet. Nothing is deleted.',
  )
})

test('none does not talk about aired episodes', () => {
  assert.equal(monitorModeSentence('none', { stopped: 40, started: 0 }), 'This stops monitoring 40 episodes. Nothing is deleted.')
})
