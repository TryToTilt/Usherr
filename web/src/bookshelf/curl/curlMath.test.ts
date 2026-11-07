import assert from 'node:assert/strict'
import { test } from 'node:test'
import { arcFor, finishes, foldAt, shapeFor } from './curlMath.ts'

const page = { x0: 0, y0: 0, x1: 400, y1: 800 }

test('the held point of the sheet lands where the finger put it', () => {
  for (const moved of [0.5, 10, 60, 200, 700]) {
    const r = 40
    const s = arcFor(moved, r)
    const landed = s <= Math.PI * r ? s - r * Math.sin(s / r) : 2 * s - Math.PI * r
    assert.ok(Math.abs(landed - moved) < 0.01, `${moved} -> ${landed}`)
  }
  assert.equal(arcFor(0, 40), 0)
})

test('a page at rest has no fold on it', () => {
  const shape = shapeFor(page, false, 400)
  const f = foldAt(shape, 0, 0)
  assert.equal(f.ax, shape.cx)
})

test('a single page leaves altogether by the end of the turn', () => {
  const shape = shapeFor(page, false, 720)
  const f = foldAt(shape, shape.dMax, 0)
  // Past the far edge, by more than the roll.
  assert.ok(f.ax + f.r < page.x0, `axis ${f.ax}, radius ${f.r}`)
})

test('in an open book the leaf lands flat on the left page', () => {
  const wide = { x0: 0, y0: 0, x1: 1000, y1: 700 }
  const shape = shapeFor(wide, true, 650)
  assert.equal(shape.leaf.x0, 500)
  const f = foldAt(shape, shape.dMax, 0)
  assert.ok(Math.abs(f.ax - 500) < 1, `axis ${f.ax}`)
  assert.ok(f.r < 1)
  // ... and the roll never crosses the spine on the way.
  for (let d = 0; d <= shape.dMax; d += 25) {
    const g = foldAt(shape, d, 0)
    assert.ok(g.ax >= 499, `at ${d} the axis is at ${g.ax}`)
  }
})

test('where the page is held changes the bend', () => {
  const lean = (gy: number) => foldAt(shapeFor(page, false, gy), 200, 0).ny
  assert.ok(lean(60) < -0.1, 'top: the top corner comes up first')
  assert.ok(lean(740) > 0.1, 'bottom: the bottom corner')
  assert.ok(Math.abs(lean(400)) < 0.05, 'middle: upright')
})

test('letting go: past halfway or a flick finishes, otherwise it falls back', () => {
  assert.equal(finishes(0.6, 0), true)
  assert.equal(finishes(0.4, 0), false)
  assert.equal(finishes(0.2, 0.8), true)
  assert.equal(finishes(0.9, -0.8), false)
})
