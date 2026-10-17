import assert from 'node:assert/strict'
import { test } from 'node:test'
import { forgetRestored, markRestored, RESTORE_NOTICE_MS, takeRestored } from './restoreNotice.ts'

function memory() {
  const m = new Map<string, string>()
  return {
    getItem: (k: string) => m.get(k) ?? null,
    setItem: (k: string, v: string) => void m.set(k, v),
    removeItem: (k: string) => void m.delete(k),
  }
}

test('a restore just started is reported once', () => {
  const s = memory()
  markRestored(1000, s)
  assert.equal(takeRestored(5000, s), true)
  assert.equal(takeRestored(6000, s), false)
})

test('nothing is reported without a restore', () => {
  assert.equal(takeRestored(1000, memory()), false)
})

test('an old note is not reported', () => {
  const s = memory()
  markRestored(1000, s)
  assert.equal(takeRestored(1000 + RESTORE_NOTICE_MS, s), false)
})

test('a restore that did not come back is forgotten', () => {
  const s = memory()
  markRestored(1000, s)
  forgetRestored(s)
  assert.equal(takeRestored(2000, s), false)
})

test('no storage at all is fine', () => {
  markRestored(1000, null)
  assert.equal(takeRestored(2000, null), false)
})
