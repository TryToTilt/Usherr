import assert from 'node:assert/strict'
import { test } from 'node:test'
import { isWide, lastOf, pageBox, pageRatio, pageWidthFor, placeSpread, spreadIndexOf, spreadsOf, type PageSize } from './mangaReading.ts'

const tall: PageSize = [800, 1200]
const wide: PageSize = [1600, 1200]

test('one page at a time', () => {
  assert.deepEqual(spreadsOf(3, undefined, false), [[1], [2], [3]])
})

test('two pages: the cover alone, then pairs, with one left over at the end', () => {
  assert.deepEqual(spreadsOf(6, undefined, true), [[1], [2, 3], [4, 5], [6]])
  assert.deepEqual(spreadsOf(1, undefined, true), [[1]])
  assert.deepEqual(spreadsOf(0, undefined, true), [])
})

test('a wide page is alone, and pairs start again after it', () => {
  const sizes = [tall, tall, wide, tall, tall, tall]
  assert.deepEqual(spreadsOf(6, sizes, true), [[1], [2], [3], [4, 5], [6]])
  assert.equal(isWide(wide), true)
  assert.equal(isWide(tall), false)
  assert.equal(isWide([0, 0]), false)
})

test('finding the spread of a page', () => {
  const s = spreadsOf(6, undefined, true)
  assert.equal(spreadIndexOf(s, 1), 0)
  assert.equal(spreadIndexOf(s, 3), 1)
  assert.equal(spreadIndexOf(s, 99), 3)
  assert.equal(spreadIndexOf(s, 0), 0)
  assert.equal(lastOf([4, 5]), 5)
})

test('the page shape is the middle one, without wide pages', () => {
  assert.equal(pageRatio([wide, tall, tall, [800, 1000]]), 1.5)
  assert.equal(pageRatio(undefined), 1.45)
  assert.equal(pageRatio([[0, 0]]), 1.45)
})

test('the box fits the stage and stays in the middle', () => {
  assert.deepEqual(pageBox(400, 900, 1.5, false), { x0: 0, y0: 150, x1: 400, y1: 750 })
  assert.deepEqual(pageBox(1600, 900, 1.5, true), { x0: 200, y0: 0, x1: 1400, y1: 900 })
})

test('a pair lies right to left when the book does', () => {
  const box = { x0: 0, y0: 0, x1: 1200, y1: 900 }
  const rtl = placeSpread([2, 3], 1, box, [tall, tall, tall], 1.5, true, true)
  assert.deepEqual(rtl.map((p) => [p.n, p.x]), [[3, 0], [2, 600]])
  const ltr = placeSpread([2, 3], 1, box, [tall, tall, tall], 1.5, true, false)
  assert.deepEqual(ltr.map((p) => [p.n, p.x]), [[2, 0], [3, 600]])
})

test('the cover sits on the side a cover opens on', () => {
  const box = { x0: 0, y0: 0, x1: 1200, y1: 900 }
  assert.equal(placeSpread([1], 0, box, [tall], 1.5, true, true)[0].x, 0)
  assert.equal(placeSpread([1], 0, box, [tall], 1.5, true, false)[0].x, 600)
  // A page left over at the end is where a first page goes.
  assert.equal(placeSpread([6], 3, box, undefined, 1.5, true, true)[0].x, 600)
})

test('a wide page fills both sides', () => {
  const box = { x0: 0, y0: 0, x1: 1200, y1: 900 }
  const [p] = placeSpread([3], 2, box, [tall, tall, wide], 1.5, true, true)
  assert.equal(p.w, 1200)
})

test('copies of pages are sized for the screen', () => {
  assert.equal(pageWidthFor(780), 800)
  assert.equal(pageWidthFor(300), 600)
  assert.equal(pageWidthFor(2000), 0)
  assert.equal(pageWidthFor(0), 0)
})
