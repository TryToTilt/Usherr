// Run with: npm test (uses node's built-in test runner, no extra packages).
import assert from 'node:assert/strict'
import { test } from 'node:test'
import { formatBytes } from './format.ts'
import { setCatalogue, st } from './i18n.ts'
import { areaName } from './problemsView.ts'

test('file sizes use the decimal mark of the language in use', () => {
  setCatalogue('de', {})
  assert.equal(formatBytes(1.5 * 1024 ** 3), '1,5 GB')
  setCatalogue('en', {})
})

test('log areas are named in the language in use, unknown ones as the server says', () => {
  setCatalogue('de', { 'Disk and folders': 'Datenträger und Ordner', Database: 'Datenbank' })
  assert.equal(areaName('disk', 'Disk and folders'), 'Datenträger und Ordner')
  assert.equal(areaName('database', 'Database'), 'Datenbank')
  assert.equal(areaName('somethingnew', 'Something new'), 'Something new')
  setCatalogue('en', {})
})

test('the import notes and the Change folder message are translated as whole sentences', () => {
  setCatalogue('de', {
    "Left out {0} episodes that the movie database doesn't list for this show.": '{0} Folgen ausgelassen, die die Filmdatenbank für diese Serie nicht kennt.',
    'Found {0} of {1} episodes in {2}.': '{0} von {1} Folgen in {2} gefunden.',
  })
  assert.equal(st("Left out 26 episodes that the movie database doesn't list for this show."), '26 Folgen ausgelassen, die die Filmdatenbank für diese Serie nicht kennt.')
  // A folder name with a dash in it is still one piece.
  assert.equal(st('Found 18 of 18 episodes in /tv/Detectorists - complete.'), '18 von 18 Folgen in /tv/Detectorists - complete gefunden.')
  setCatalogue('en', {})
})
