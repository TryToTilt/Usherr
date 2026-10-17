// Run with: npm test (uses node's built-in test runner, no extra packages).
import assert from 'node:assert/strict'
import { test } from 'node:test'
import { nameParts, qualityText } from './format.ts'

test('a release name is cut after its separators and joins back unchanged', () => {
  for (const name of ['Coco.2017.1080p.BluRay.x264-Geek', 'Coco.(2017).VFF.2160p', 'plain', '', 'a_b-c.d[e]']) {
    assert.equal(nameParts(name).join(''), name)
  }
  assert.deepEqual(nameParts('Coco.2017.x264-Geek'), ['Coco.', '2017.', 'x264-', 'Geek'])
})

test('a quality is shown as it is stored', () => {
  assert.equal(qualityText('WEBDL-1080p'), 'WEBDL-1080p')
  assert.equal(qualityText('Bluray-2160p'), 'Bluray-2160p')
})

test('a file that does not say its quality reads Not stated', () => {
  assert.equal(qualityText('Unknown'), 'Not stated')
})

test('a missing quality is a dash', () => {
  assert.equal(qualityText(''), '—')
  assert.equal(qualityText(undefined), '—')
  assert.equal(qualityText(null), '—')
})
