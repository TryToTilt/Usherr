import { test } from 'node:test'
import assert from 'node:assert/strict'
import { compareTitles, sortTitle } from './sortTitle.ts'
import { sortReleases } from '../releaseSort.ts'

test('leading English articles are ignored', () => {
  assert.equal(sortTitle('The Matrix'), 'Matrix')
  assert.equal(sortTitle('A Knight of the Seven Kingdoms'), 'Knight of the Seven Kingdoms')
  assert.equal(sortTitle('Die Hard'), 'Die Hard')
  assert.equal(sortTitle('The'), 'The')
  const list = ['The Matrix', 'Memento', 'Alien', 'The Abyss'].sort(compareTitles)
  assert.deepEqual(list, ['The Abyss', 'Alien', 'The Matrix', 'Memento'])
})

test('age sorts newest first, undated last', () => {
  const r = (title: string, publishDate?: string) => ({ title, indexerName: '', protocol: 'usenet', sizeBytes: 0, publishDate })
  const got = sortReleases([r('old', '2020-01-01T00:00:00Z'), r('none'), r('new', '2026-10-01T00:00:00Z')], 'age', 1).map((x) => x.title)
  assert.deepEqual(got, ['new', 'old', 'none'])
})
