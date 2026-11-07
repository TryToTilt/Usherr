import assert from 'node:assert/strict'
import { test } from 'node:test'
import { panelsOrHalves, placeFitted, spreadsOf, type PageSize } from './mangaReading.ts'
import { trimBox } from './pageTrim.ts'
import { lookFilter, parseLook } from './readerLook.ts'

const tall: PageSize = [800, 1200]
const wide: PageSize = [1600, 1200]

test('shifting the pairs by one: the second page is alone too', () => {
  assert.deepEqual(spreadsOf(6, undefined, true, true), [[1], [2], [3, 4], [5, 6]])
  assert.deepEqual(spreadsOf(2, undefined, true, true), [[1], [2]])
  assert.deepEqual(spreadsOf(1, undefined, true, true), [[1]])
  // Only two pages at a time has pairs.
  assert.deepEqual(spreadsOf(3, undefined, false, true), [[1], [2], [3]])
  // A wide page still stands alone, and pairing starts again after it.
  assert.deepEqual(spreadsOf(6, [tall, tall, tall, wide, tall, tall], true, true), [[1], [2], [3], [4], [5, 6]])
})

test('fit to width fills the stage and scrolls down', () => {
  const { box, items } = placeFitted([2], 400, 600, [tall, tall], 1.5, false, 'width')
  assert.deepEqual(box, { x0: 0, y0: 0, x1: 400, y1: 600 })
  assert.equal(items[0].w, 400)
  assert.equal(items[0].h, 600)
  const big = placeFitted([2], 800, 600, [tall, tall], 1.5, false, 'width')
  assert.equal(big.box.y1 - big.box.y0, 1200)
  assert.equal(big.box.y0, 0)
})

test('fit to height fills the height and may run wider than the stage', () => {
  const { box, items } = placeFitted([1], 400, 900, [wide], 1.5, false, 'height')
  assert.equal(items[0].h, 900)
  assert.equal(items[0].w, 1200)
  assert.equal(box.x0, 0)
  assert.equal(box.x1, 1200)
})

test('actual size is the picture, in the middle when smaller than the stage', () => {
  const { box, items } = placeFitted([1], 1000, 1400, [tall], 1.5, false, 'original')
  assert.equal(items[0].w, 800)
  assert.equal(box.x0, 100)
  assert.equal(box.y0, 100)
})

test('a fitted pair sits side by side, right to left when it reads that way', () => {
  const ltr = placeFitted([2, 3], 1600, 1200, [tall, tall, tall], 1.5, false, 'width').items
  assert.deepEqual(ltr.map((p) => [p.n, p.x]), [[2, 0], [3, 800]])
  const rtl = placeFitted([2, 3], 1600, 1200, [tall, tall, tall], 1.5, true, 'width').items
  assert.deepEqual(rtl.map((p) => [p.n, p.x]), [[3, 0], [2, 800]])
})

test('an unknown page size falls back on the volume’s shape', () => {
  const { items } = placeFitted([1], 500, 900, [[0, 0]], 1.4, false, 'width')
  assert.equal(items[0].w, 500)
  assert.equal(Math.round(items[0].h), 700)
})

test('a spread with no panels is read a page at a time on an upright screen', () => {
  const found = [{ x: 0, y: 0, w: 1, h: 0.5 }]
  assert.deepEqual(panelsOrHalves(found, wide, true, true), found) // panels were found: those
  assert.deepEqual(panelsOrHalves([], tall, true, true), []) // a normal page is shown whole
  assert.deepEqual(panelsOrHalves([], wide, true, false), []) // on a wide screen too
  assert.equal(panelsOrHalves([], wide, true, true)[0].x, 0.5) // right to left, the right page first
  assert.equal(panelsOrHalves([], wide, false, true)[0].x, 0)
})

// A grey picture: fill(x, y) says the shade.
function picture(w: number, h: number, fill: (x: number, y: number) => number): Uint8ClampedArray {
  const data = new Uint8ClampedArray(w * h * 4)
  for (let y = 0; y < h; y++) {
    for (let x = 0; x < w; x++) {
      const v = fill(x, y)
      const i = (y * w + x) * 4
      data[i] = data[i + 1] = data[i + 2] = v
      data[i + 3] = 255
    }
  }
  return data
}

const inked = (x0: number, y0: number, x1: number, y1: number, border: number) => (x: number, y: number) => (x >= x0 && x < x1 && y >= y0 && y < y1 ? ((x + y) % 2 ? 60 : 120) : border)

test('white borders are trimmed to the content', () => {
  assert.deepEqual(trimBox(picture(100, 150, inked(10, 15, 90, 135, 255)), 100, 150), { x: 10, y: 15, w: 80, h: 120 })
})

test('black borders too', () => {
  assert.deepEqual(trimBox(picture(100, 150, inked(5, 10, 95, 140, 0)), 100, 150), { x: 5, y: 10, w: 90, h: 130 })
})

test('a page that fills itself has nothing to trim', () => {
  assert.equal(trimBox(picture(100, 150, inked(0, 0, 100, 150, 255)), 100, 150), null)
})

test('a mostly blank page stays whole', () => {
  assert.equal(trimBox(picture(100, 150, inked(45, 70, 55, 80, 255)), 100, 150), null)
})

test('a speck of dust in the border doesn’t stop the trim', () => {
  const fill = inked(10, 15, 90, 135, 255)
  const box = trimBox(picture(100, 150, (x, y) => (x === 50 && y === 3 ? 0 : fill(x, y))), 100, 150)
  assert.equal(box?.y, 15)
})

test('the look of the pages', () => {
  assert.equal(lookFilter({ brightness: 100, dark: false }), undefined)
  assert.equal(lookFilter({ brightness: 60, dark: false }), 'brightness(0.6)')
  assert.equal(lookFilter({ brightness: 100, dark: true }), 'invert(1) hue-rotate(180deg)')
  assert.equal(parseLook(null).fit, 'page')
  assert.equal(parseLook('not json').brightness, 100)
  assert.deepEqual(parseLook('{"fit":"width","trim":true,"brightness":5,"dark":true}'), { fit: 'width', trim: true, brightness: 30, dark: true })
  assert.equal(parseLook('{"fit":"sideways"}').fit, 'page')
})
