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

test('episodes the person put in a show folder are told first', () => {
  assert.equal(rescanMessage({ ...none, found: 4, addedEpisodes: 3 }), 'Found 3 episodes you added yourself. Checked the files: 4 fine.')
  assert.equal(rescanMessage({ ...none, addedEpisodes: 1 }), 'Found 1 episode you added yourself.')
})

test('files that matched no episode are listed by name', () => {
  assert.equal(
    rescanMessage({ ...none, found: 2, notMatched: ['Clip.mkv', 'Bonus.mkv'], notMatchedTotal: 2 }),
    "Checked the files: 2 fine. Couldn't match 2 video files in the show's folder to an episode: Clip.mkv, Bonus.mkv.",
  )
  assert.equal(
    rescanMessage({ ...none, addedEpisodes: 1, notMatched: ['a.mkv'], notMatchedTotal: 13 }),
    "Found 1 episode you added yourself. Couldn't match 13 video files in the show's folder to an episode: a.mkv and 12 more.",
  )
})

