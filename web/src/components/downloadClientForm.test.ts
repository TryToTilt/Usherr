// Run with: npm test
import assert from 'node:assert/strict'
import { test } from 'node:test'
import { reloadForm } from './downloadClientForm.ts'

const base = { kind: 'qbittorrent', url: 'http://a:8080', username: 'u', password: '', category: 'mediarium', removeAfterImport: false }

test('a form nobody touched takes everything the server now has', () => {
  const server = { ...base, url: 'http://b:9091', kind: 'transmission' }
  assert.deepEqual(reloadForm({ ...base }, base, server), server)
})

test('a field the person changed and did not save is kept', () => {
  const server = { ...base, kind: '', url: '' }
  const form = { ...base, username: 'me', password: 'secret' }
  assert.deepEqual(reloadForm(form, base, server), { ...server, username: 'me', password: 'secret' })
})

test('untouched fields still follow the server while an edited one stays', () => {
  const server = { ...base, category: 'tv' }
  const form = { ...base, url: 'http://typed:1' }
  assert.deepEqual(reloadForm(form, base, server), { ...server, url: 'http://typed:1' })
})

test('the first load has no saved copy: the blank form is the base', () => {
  const blank = { ...base, kind: '', url: '', username: '' }
  const server = { ...base }
  assert.deepEqual(reloadForm(blank, blank, server), server)
  assert.deepEqual(reloadForm({ ...blank, url: 'http://typed:1' }, blank, server), { ...server, url: 'http://typed:1' })
})
