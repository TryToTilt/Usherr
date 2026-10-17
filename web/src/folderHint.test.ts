// Run with: npm test
import assert from 'node:assert/strict'
import { test } from 'node:test'
import { folderHint } from './folderHint.ts'

test('both kinds explain that the path is the one inside the container', () => {
  for (const kind of ['movie', 'series'] as const) {
    const text = folderHint(kind)
    assert.match(text, /inside its container/)
    assert.match(text, /volume mapping/)
  }
})

test('a movie says Rescan files adds a video put in the folder', () => {
  assert.match(folderHint('movie'), /press Rescan files and it is added to the movie/)
})

test('a show points to Import existing for episodes pasted in', () => {
  const text = folderHint('series')
  assert.match(text, /Import existing/)
  assert.doesNotMatch(text, /added to the movie/)
})
