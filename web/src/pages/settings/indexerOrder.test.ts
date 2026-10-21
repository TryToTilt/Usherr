import assert from 'node:assert/strict'
import { test } from 'node:test'
import { filterIndexers, sortIndexers, visibleIndexers } from './indexerOrder.ts'
import type { IndexerConfig } from '../../api'

const ix = (id: number, name: string, priority?: number, protocol: 'usenet' | 'torrent' = 'usenet'): IndexerConfig =>
  ({ id, name, priority, protocol, definitionId: '', baseUrl: `https://${name.toLowerCase()}.example`, enabled: true }) as IndexerConfig

test('lower priority comes first, ties by name, torrent and usenet mixed', () => {
  const out = sortIndexers([ix(1, 'Zeta', 25), ix(2, 'alpha', 25, 'torrent'), ix(3, 'Mid', 10), ix(4, 'Top', 1), ix(5, 'Bare')])
  assert.deepEqual(out.map((i) => i.name), ['Top', 'Mid', 'alpha', 'Bare', 'Zeta'])
})

test('sorting does not change the list it was given', () => {
  const list = [ix(1, 'B', 5), ix(2, 'A', 1)]
  sortIndexers(list)
  assert.equal(list[0].name, 'B')
})

test('the filter matches part of a name, any case', () => {
  const list = [ix(1, 'NZBgeek'), ix(2, 'DrunkenSlug'), ix(3, 'nzb.su')]
  assert.deepEqual(filterIndexers(list, ' NZB ').map((i) => i.id), [1, 3])
  assert.equal(filterIndexers(list, '').length, 3)
})

test('a word typed earlier stops filtering once the filter box is gone', () => {
  const few = [ix(1, 'NZBgeek'), ix(2, 'DrunkenSlug')]
  assert.equal(visibleIndexers(few, 'zzz').length, 2)
  const many = Array.from({ length: 9 }, (_, i) => ix(i + 1, i === 0 ? 'NZBgeek' : `Site${i}`))
  assert.deepEqual(visibleIndexers(many, 'geek').map((i) => i.id), [1])
})
