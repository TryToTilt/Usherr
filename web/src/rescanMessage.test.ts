// Run with: npm test (uses node's built-in test runner, no extra packages).
import assert from 'node:assert/strict'
import { test } from 'node:test'
import { rescanMessage } from './rescanMessage.ts'

const none = { found: 0, renamed: 0, missing: 0, skipped: 0 }

test('a movie that was given a file says which one', () => {
  assert.equal(rescanMessage({ ...none, added: 'movie.mkv' }), 'Found movie.mkv and added it.')
})

test('a movie with nothing in its folder says where it looked', () => {
  assert.equal(rescanMessage({ ...none, looked: '/data/movies/The Matrix (1999)' }), 'No video file found in /data/movies/The Matrix (1999).')
})

test('a movie that is downloading is left alone', () => {
  assert.equal(rescanMessage({ ...none, busy: true }), 'This movie is downloading right now, so nothing was changed.')
})

test('a gone file is wanted again only when the title is monitored', () => {
  assert.equal(rescanMessage({ ...none, missing: 1 }), 'Checked the files: 0 fine, 1 gone, wanted again.')
  assert.equal(rescanMessage({ ...none, missing: 1 }, false), 'Checked the files: 0 fine, 1 gone.')
})

test('a normal rescan still counts its files', () => {
  assert.equal(rescanMessage({ ...none, found: 2, renamed: 1 }), 'Checked the files: 2 fine, 1 found under a new name.')
})
