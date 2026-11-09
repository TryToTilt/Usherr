// Run with: npm test
import assert from 'node:assert/strict'
import { test } from 'node:test'
import { readerKey, remoteTurn } from './remoteReading.ts'

const LTR = { rtl: false, vertical: false }
const RTL = { rtl: true, vertical: false }
const DOWN = { rtl: true, vertical: true }

test('left to right: Right goes on, Left goes back', () => {
  assert.deepEqual(readerKey('ArrowRight', LTR), { kind: 'turn', d: 1 })
  assert.deepEqual(readerKey('ArrowLeft', LTR), { kind: 'turn', d: -1 })
})

test('right to left (manga): Left goes on, Right goes back', () => {
  assert.deepEqual(readerKey('ArrowLeft', RTL), { kind: 'turn', d: 1 })
  assert.deepEqual(readerKey('ArrowRight', RTL), { kind: 'turn', d: -1 })
})

test('a long scroll has no sides: Right and Down go on, Up goes back', () => {
  assert.deepEqual(readerKey('ArrowRight', DOWN), { kind: 'turn', d: 1 })
  assert.deepEqual(readerKey('ArrowDown', DOWN), { kind: 'turn', d: 1 })
  assert.deepEqual(readerKey('ArrowUp', DOWN), { kind: 'turn', d: -1 })
})

test('on pages, Up shows the bar, Down goes on, OK shows or hides the bar', () => {
  assert.deepEqual(readerKey('ArrowUp', RTL), { kind: 'bar' })
  assert.deepEqual(readerKey('ArrowDown', RTL), { kind: 'turn', d: 1 })
  assert.deepEqual(readerKey('Enter', LTR), { kind: 'toggle' })
})

test('Space and Page Down always go on, whichever way the book reads', () => {
  assert.deepEqual(readerKey(' ', RTL), { kind: 'turn', d: 1 })
  assert.deepEqual(readerKey('PageDown', RTL), { kind: 'turn', d: 1 })
  assert.deepEqual(readerKey('PageUp', LTR), { kind: 'turn', d: -1 })
})

test('Escape closes; other keys are not the reader’s', () => {
  assert.deepEqual(readerKey('Escape', LTR), { kind: 'close' })
  assert.equal(readerKey('a', LTR), null)
  assert.equal(readerKey('m', LTR), null) // the Options key: the remote's
})

test('fast forward and rewind turn pages in reading order', () => {
  assert.equal(remoteTurn('fastforward'), 1)
  assert.equal(remoteTurn('next'), 1)
  assert.equal(remoteTurn('rewind'), -1)
  assert.equal(remoteTurn('previous'), -1)
  assert.equal(remoteTurn('playpause'), 0)
})
