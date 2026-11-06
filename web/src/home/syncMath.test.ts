import { test } from 'node:test'
import assert from 'node:assert/strict'
import { clockOffset, correct, driftWords, project, roomAt } from './syncMath.ts'

test('a playing film moves on with the clock; a paused one stays', () => {
  assert.deepEqual(project('film', 100, 1000, 3500), { state: 'film', position: 102.5 })
  assert.deepEqual(project('paused', 100, 1000, 61000), { state: 'paused', position: 100 })
  assert.deepEqual(project('preshow', -30, 0, 10000), { state: 'preshow', position: -20 })
  assert.deepEqual(project('preshow', -5, 0, 7000), { state: 'film', position: 2 })
  assert.deepEqual(project('ended', 7000, 0, 1000), { state: 'ended', position: 7000 })
})

test('drift: in step, nudged, or seek', () => {
  assert.deepEqual(correct(0), { rate: 1, seek: false })
  assert.deepEqual(correct(0.05), { rate: 1, seek: false })
  assert.deepEqual(correct(-0.09), { rate: 1, seek: false })
  assert.deepEqual(correct(0.25), { rate: 1.02, seek: false })
  assert.deepEqual(correct(-0.5), { rate: 0.96, seek: false })
  assert.deepEqual(correct(0.9), { rate: 1.06, seek: false })
  assert.deepEqual(correct(-0.95), { rate: 0.94, seek: false })
  assert.deepEqual(correct(1.5), { rate: 1, seek: true })
  assert.deepEqual(correct(-12), { rate: 1, seek: true })
  assert.deepEqual(correct(Number.NaN), { rate: 1, seek: false })
})

test('the clock offset comes from the shortest round trip', () => {
  assert.equal(clockOffset([]), 0)
  const off = clockOffset([
    { sent: 0, got: 400, server: 5300 }, // slow: 5300 - 200
    { sent: 1000, got: 1040, server: 6020 }, // quick: 6020 - 1020
  ])
  assert.equal(off, 5000)
})

test('a room: by the schedule until the host says otherwise', () => {
  const r = { atMs: 100_000, preshowSeconds: 60, runtime: 2, now: { state: 'waiting' as const, position: 0, atMs: 0, seq: 0 } }
  assert.deepEqual(roomAt(r, 0), { state: 'waiting', position: -100 })
  assert.deepEqual(roomAt(r, 70_000), { state: 'preshow', position: -30 })
  assert.deepEqual(roomAt(r, 110_000), { state: 'film', position: 10 })
  assert.deepEqual(roomAt(r, 300_000), { state: 'ended', position: 150 })
  const paused = { ...r, now: { state: 'paused' as const, position: 42, atMs: 0, seq: 9 } }
  assert.deepEqual(roomAt(paused, 999_000), { state: 'paused', position: 42 })
})

test('drift said gently', () => {
  assert.deepEqual(driftWords(0.1), { key: 'in', secs: '0' })
  assert.deepEqual(driftWords(0.42), { key: 'behind', secs: '0.4' })
  assert.deepEqual(driftWords(-1.25), { key: 'ahead', secs: '1.3' })
})
