import assert from 'node:assert/strict'
import { test } from 'node:test'
import { newerVersion } from './updateAdvice.ts'

test('a newer release is reported, an equal or older one is not', () => {
  assert.equal(newerVersion('3.1.3', 'v3.2.1'), '3.2.1')
  assert.equal(newerVersion('3.2.1', '3.2.1'), '')
  assert.equal(newerVersion('3.3.0', '3.2.1'), '')
  assert.equal(newerVersion('3.2.1-beta.2', '3.2.1'), '')
})

test('nothing is reported before the check has run', () => {
  assert.equal(newerVersion('3.1.3', undefined), '')
  assert.equal(newerVersion('3.1.3', ''), '')
  assert.equal(newerVersion('', '3.2.1'), '')
})
