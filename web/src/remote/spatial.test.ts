import { test } from 'node:test'
import assert from 'node:assert/strict'
import { fieldKind, inDirection, keyIntent, pickFirst, pickNext, remoteTakesArrow, type Rect } from './spatial.ts'

const box = (left: number, top: number, w = 100, h = 150): Rect => ({ left, top, right: left + w, bottom: top + h })

// A poster grid: three rows of four, 100x150 with 20px gaps.
const grid: Rect[] = []
for (let r = 0; r < 3; r++) for (let c = 0; c < 4; c++) grid.push(box(c * 120, r * 170))
const at = (r: number, c: number) => r * 4 + c

test('arrows move one step in a grid, staying in the row or column', () => {
  const from = grid[at(1, 1)]
  const others = grid.map((g, i) => (i === at(1, 1) ? box(-9999, -9999) : g))
  assert.equal(pickNext(from, others, 'right'), at(1, 2))
  assert.equal(pickNext(from, others, 'left'), at(1, 0))
  assert.equal(pickNext(from, others, 'down'), at(2, 1))
  assert.equal(pickNext(from, others, 'up'), at(0, 1))
})

test('nothing that way gives -1 (the edge of the page)', () => {
  const from = grid[at(0, 0)]
  const others = grid.filter((_, i) => i !== at(0, 0))
  assert.equal(pickNext(from, others, 'left'), -1)
  assert.equal(pickNext(from, others, 'up'), -1)
})

test('the same row wins over a nearer item in another row', () => {
  const from = box(0, 0, 100, 40)
  const sameRowFar = box(400, 0, 100, 40)
  const nearButLower = box(120, 60, 100, 40)
  assert.equal(pickNext(from, [nearButLower, sameRowFar], 'right'), 1)
})

test('going down from a wide header lands on the item under its start, not a far one', () => {
  const header = box(0, 0, 600, 40)
  const row = [box(0, 80), box(120, 80), box(240, 80)]
  // all overlap the header; the one whose centre is nearest the header's centre wins the tie
  const i = pickNext(header, row, 'down')
  assert.ok(i >= 0)
  assert.equal(pickNext(box(0, 0, 100, 40), row, 'down'), 0)
})

test('down takes the next row, even off to the side, over something in line but far away', () => {
  const from = box(400, 0, 100, 40)
  const farInLine = box(400, 1200, 100, 40)
  const nextRow = box(0, 80, 300, 40)
  assert.equal(pickNext(from, [farInLine, nextRow], 'down'), 1)
})

test('a row of different heights still moves sideways', () => {
  const from = box(0, 0, 100, 150)
  const smallNext = box(130, 40, 60, 60)
  const below = box(0, 200, 100, 150)
  assert.equal(pickNext(from, [below, smallNext], 'right'), 1)
})

test('overlapping things fall back to their centres', () => {
  const from = box(0, 0, 200, 50)
  const overlapping = box(150, 0, 200, 50) // starts before `from` ends
  assert.equal(inDirection(from, overlapping, 'right'), false)
  assert.equal(inDirection(from, overlapping, 'right', true), true)
  assert.equal(pickNext(from, [overlapping], 'right'), 0)
})

test('the first thing in sight is the top-left one', () => {
  const view = { left: 0, top: 0, right: 1000, bottom: 800 }
  const rects = [box(300, 400), box(50, 900), box(500, 20), box(20, 20)]
  assert.equal(pickFirst(rects, view), 3)
  assert.equal(pickFirst([box(0, 2000)], view), -1)
})

test('keys: arrows, OK, Back, Options and media', () => {
  assert.deepEqual(keyIntent({ key: 'ArrowLeft' }), { kind: 'move', dir: 'left' })
  assert.deepEqual(keyIntent({ key: 'ArrowDown', shiftKey: true }), { kind: 'none' })
  assert.deepEqual(keyIntent({ key: 'ArrowDown', ctrlKey: true }), { kind: 'none' })
  assert.deepEqual(keyIntent({ key: 'Enter' }), { kind: 'ok' })
  assert.deepEqual(keyIntent({ key: 'Escape' }), { kind: 'escape' })
  assert.deepEqual(keyIntent({ key: 'Backspace' }), { kind: 'back', native: false })
  assert.deepEqual(keyIntent({ key: 'GoBack' }), { kind: 'back', native: true })
  assert.deepEqual(keyIntent({ key: 'BrowserBack' }), { kind: 'back', native: true })
  assert.deepEqual(keyIntent({ key: 'Unidentified', keyCode: 461 }), { kind: 'back', native: true })
  assert.deepEqual(keyIntent({ key: 'ContextMenu' }), { kind: 'options' })
  assert.deepEqual(keyIntent({ key: 'm' }), { kind: 'options' })
  assert.deepEqual(keyIntent({ key: 'MediaPlayPause' }), { kind: 'media', action: 'playpause' })
  assert.deepEqual(keyIntent({ key: 'MediaFastForward' }), { kind: 'media', action: 'fastforward' })
  assert.deepEqual(keyIntent({ key: 'MediaRewind' }), { kind: 'media', action: 'rewind' })
  assert.deepEqual(keyIntent({ key: 'a' }), { kind: 'none' })
})

test('fields: what is typing, a slider, a picker', () => {
  assert.equal(fieldKind('input', null, false), 'typing')
  assert.equal(fieldKind('INPUT', 'search', false), 'typing')
  assert.equal(fieldKind('INPUT', 'number', false), 'typing')
  assert.equal(fieldKind('TEXTAREA', null, false), 'typing')
  assert.equal(fieldKind('DIV', null, true), 'typing')
  assert.equal(fieldKind('INPUT', 'range', false), 'slider')
  assert.equal(fieldKind('INPUT', 'checkbox', false), 'other')
  assert.equal(fieldKind('INPUT', 'radio', false), 'radio')
  assert.equal(fieldKind('SELECT', null, false), 'select')
  assert.equal(fieldKind('BUTTON', null, false), 'other')
})

test('the remote never takes arrows from typing, and a slider only lets go when not grabbed', () => {
  const pc = { grabbed: false, tv: false, multiline: false }
  for (const dir of ['up', 'down', 'left', 'right'] as const) assert.equal(remoteTakesArrow('typing', dir, pc), false)
  // on a TV, Up and Down leave a one-line field; Left and Right stay with the caret
  const tv = { grabbed: false, tv: true, multiline: false }
  assert.equal(remoteTakesArrow('typing', 'down', tv), true)
  assert.equal(remoteTakesArrow('typing', 'left', tv), false)
  assert.equal(remoteTakesArrow('typing', 'down', { ...tv, multiline: true }), false)
  assert.equal(remoteTakesArrow('slider', 'left', pc), true)
  assert.equal(remoteTakesArrow('slider', 'left', { ...pc, grabbed: true }), false)
  assert.equal(remoteTakesArrow('select', 'down', pc), true)
  assert.equal(remoteTakesArrow('radio', 'down', pc), false)
  assert.equal(remoteTakesArrow('other', 'up', pc), true)
})

test('Left stays in the row: nothing to the left in it is the edge (Usherr Broadcast)', () => {
  const card = { left: 100, top: 100, right: 500, bottom: 180 }
  const posterBelowLeft = { left: 40, top: 220, right: 160, bottom: 400 }
  assert.equal(pickNext(card, [posterBelowLeft], 'left'), -1)
  // Right still looks past the row when nothing is beside it
  const posterBelowRight = { left: 520, top: 220, right: 640, bottom: 400 }
  assert.equal(pickNext(card, [posterBelowRight], 'right'), 0)
})
