import assert from 'node:assert/strict'
import { test } from 'node:test'
import { backgroundColor, COMICS_OPTIONS, DEFAULT_OPTIONS, isHeaderKey, joinFilters, optionsFilter, parseOptions, progressParts, ticksFor } from './readerOptions.ts'

test('out of the box the readers look as they always did', () => {
  assert.deepEqual(parseOptions(null), DEFAULT_OPTIONS)
  assert.equal(DEFAULT_OPTIONS.header, true)
  assert.equal(DEFAULT_OPTIONS.progress, 'normal')
  assert.equal(DEFAULT_OPTIONS.side, 'bottom')
  assert.equal(DEFAULT_OPTIONS.background, 'reader')
  assert.equal(optionsFilter(DEFAULT_OPTIONS), '')
  assert.equal(backgroundColor(DEFAULT_OPTIONS.background), undefined)
  // The Comics reader never showed tap hints, and still doesn't.
  assert.equal(COMICS_OPTIONS.hints, 'none')
  assert.equal(parseOptions(null, COMICS_OPTIONS).hints, 'none')
})

test('a stored note keeps only what makes sense', () => {
  const o = parseOptions(JSON.stringify({ header: false, progress: 'thin', side: 'left', size: 9, hints: 'cursor', greyscale: true, dim: 'yes', background: 'purple', pageNumber: false }))
  assert.equal(o.header, false)
  assert.equal(o.progress, 'thin')
  assert.equal(o.side, 'left')
  assert.equal(o.size, 4) // at most 4
  assert.equal(o.hints, 'cursor')
  assert.equal(o.greyscale, true)
  assert.equal(o.dim, false) // not a yes or no
  assert.equal(o.background, 'reader') // not a background
  assert.equal(o.pageNumber, false)
  assert.deepEqual(parseOptions('not json'), DEFAULT_OPTIONS)
  assert.deepEqual(parseOptions('[1,2]'), DEFAULT_OPTIONS)
})

test('greyscale and dim pages add to the reader’s own filter', () => {
  assert.equal(optionsFilter({ greyscale: true, dim: false }), 'grayscale(1)')
  assert.equal(optionsFilter({ greyscale: true, dim: true }), 'grayscale(1) brightness(0.7)')
  assert.equal(joinFilters('invert(1)', optionsFilter({ greyscale: true, dim: false })), 'invert(1) grayscale(1)')
  assert.equal(joinFilters(undefined, ''), undefined)
})

test('the progress bar: a bar you drag, a line, or a page number', () => {
  assert.deepEqual(progressParts({ progress: 'normal', pageNumber: true }), { bar: true, slider: true, number: false })
  assert.deepEqual(progressParts({ progress: 'thin', pageNumber: true }), { bar: true, slider: false, number: false })
  assert.deepEqual(progressParts({ progress: 'hidden', pageNumber: true }), { bar: false, slider: false, number: true })
  assert.deepEqual(progressParts({ progress: 'hidden', pageNumber: false }), { bar: false, slider: false, number: false })
})

test('a tick a page, unless there are too many to see', () => {
  assert.deepEqual(ticksFor(3), [0, 0.5, 1])
  assert.deepEqual(ticksFor(1), [])
  assert.deepEqual(ticksFor(500), [])
})

test('backgrounds and the header key', () => {
  assert.equal(backgroundColor('white'), '#ffffff')
  assert.equal(backgroundColor('black'), '#000000')
  assert.match(backgroundColor('theme') ?? '', /var\(--bg/)
  assert.ok(isHeaderKey('m') && isHeaderKey('M') && !isHeaderKey('n'))
})
