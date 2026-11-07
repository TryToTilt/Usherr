import assert from 'node:assert/strict'
import { test } from 'node:test'
import { gapFor, layoutOf, pageCss, readPrefs } from './readerPrefs.ts'

test('a saved Fade becomes Slide, and the comic reader keeps its choice', () => {
  assert.equal(readPrefs({ effect: 'fade' }).effect, 'slide')
  const p = readPrefs({ turn: 'fade' })
  assert.equal(p.effect, 'slide')
  assert.equal(p.turn, 'fade')
})

test('a page turn chosen since Fade stays', () => {
  assert.equal(readPrefs({ turn: 'fade', effect: 'curl' }).effect, 'curl')
  assert.equal(readPrefs({ turn: 'fade', effect: 'none' }).effect, 'none')
  assert.equal(readPrefs({ turn: 'slide' }).effect, undefined)
})

test('anything else saved gives the settings of before', () => {
  for (const saved of [null, 'x', { size: 9000, theme: 'neon', font: 'comic', line: 3, margin: {}, justify: 'x' }]) {
    const p = readPrefs(saved)
    assert.deepEqual([p.theme, p.font, p.line, p.margin, p.justify], ['light', 'serif', 'book', 'normal', 'book'])
  }
  assert.equal(readPrefs({ size: 9000 }).size, 200)
  assert.equal(readPrefs(null).size, 105)
})

test('what is left as published adds no rule', () => {
  assert.equal(pageCss(readPrefs({ font: 'original' })), '')
})

test('reading is in pages unless Scroll was chosen', () => {
  assert.equal(readPrefs({}).reading, 'pages')
  assert.equal(readPrefs({ reading: 'x' }).reading, 'pages')
  assert.equal(readPrefs({ reading: 'scroll' }).reading, 'scroll')
})

test('Scroll has margins of its own, and pages get no such rule', () => {
  assert.match(pageCss(readPrefs({ reading: 'scroll', margin: 'wide', font: 'original' })), /padding: 20px 16%/)
  assert.doesNotMatch(pageCss(readPrefs({ margin: 'wide', font: 'original' })), /padding/)
})

test('the rules are ones a book cannot beat', () => {
  const css = pageCss(readPrefs({ font: 'mono', line: 'wide', justify: 'off' }))
  assert.match(css, /monospace !important/)
  assert.match(css, /line-height: 1\.85 !important/)
  assert.match(css, /text-align: start !important/)
})

test('whatever moves words counts as layout, and colour does not', () => {
  const base = layoutOf(readPrefs({}))
  for (const change of [{ size: 120 }, { font: 'sans' }, { line: 'wide' }, { margin: 'wide' }, { justify: 'on' }]) assert.notEqual(layoutOf(readPrefs(change)), base)
  assert.equal(layoutOf(readPrefs({ theme: 'black', speed: 'slow' })), base)
})

test('gaps are even, and the normal margin is left to epub.js', () => {
  assert.equal(gapFor(1000, 'normal'), undefined)
  assert.equal(gapFor(1000, 'narrow'), 40)
  assert.equal(gapFor(1000, 'wide'), 166)
})
