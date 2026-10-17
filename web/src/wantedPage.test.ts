// Run with: npm test
import assert from 'node:assert/strict'
import { test } from 'node:test'
import { mergeRefreshed, pageOfWanted, wantedPagePath, wantedRows } from './wantedPage.ts'
import type { WantedItem } from './api.ts'

const movie = (id: number, title: string): WantedItem => ({ kind: 'movie', id, movieId: id, tmdbId: id, title })
const episode = (id: number, seriesId: number, title: string): WantedItem => ({ kind: 'episode', id, seriesId, title, subtitle: `S01E${String(id).padStart(2, '0')}` })

const list: WantedItem[] = [
  movie(1, 'Arrival'),
  movie(2, 'Brazil'),
  episode(11, 100, 'Severance'),
  episode(12, 100, 'Severance'),
  episode(13, 100, 'Severance'),
  episode(21, 200, 'Detectorists'),
  episode(31, 300, 'Slow Horses'),
  episode(32, 300, 'Slow Horses'),
]

test('a show is one row with its count, and a show with one episode is that episode', () => {
  const rows = wantedRows(list)
  assert.deepEqual(
    rows.map((r) => [r.kind, r.title, r.episodes ?? null]),
    [
      ['movie', 'Arrival', null],
      ['movie', 'Brazil', null],
      ['show', 'Severance', 3],
      ['episode', 'Detectorists', null],
      ['show', 'Slow Horses', 2],
    ],
  )
})

test('a page has its rows and the totals of the whole list', () => {
  const rows: [string, Parameters<typeof pageOfWanted>[1], string[], number, number, number][] = [
    ['first page', { limit: 2 }, ['Arrival', 'Brazil'], 5, 2, 6],
    ['next page', { limit: 2, offset: 2 }, ['Severance', 'Detectorists'], 5, 2, 6],
    ['past the end', { offset: 9 }, [], 5, 2, 6],
    ['totals only', { limit: 0 }, [], 5, 2, 6],
    ['shows only', { media: 'tv' }, ['Severance', 'Detectorists', 'Slow Horses'], 3, 0, 6],
    ['movies only', { media: 'movie' }, ['Arrival', 'Brazil'], 2, 2, 0],
    ['a search', { q: ' slow ' }, ['Slow Horses'], 1, 0, 2],
  ]
  for (const [name, query, titles, total, movies, episodes] of rows) {
    const page = pageOfWanted(list, query)
    assert.deepEqual(page.rows.map((r) => r.title), titles, name)
    assert.equal(page.total, total, `${name}: total`)
    assert.equal(page.movies, movies, `${name}: movies`)
    assert.equal(page.episodes, episodes, `${name}: episodes`)
  }
})

test('the address carries only what was asked for', () => {
  assert.equal(wantedPagePath('missing'), '/wanted?kind=missing&group=1')
  assert.equal(wantedPagePath('cutoff', { limit: 0 }), '/wanted?kind=cutoff&group=1&limit=0')
  assert.equal(wantedPagePath('missing', { limit: 50, offset: 100, media: 'tv', q: ' the office ' }), '/wanted?kind=missing&group=1&limit=50&offset=100&media=tv&q=the+office')
})

test('a quiet refresh keeps the rows loaded further down', () => {
  const all = wantedRows(list)
  const fresh = pageOfWanted(list, { limit: 2 })
  assert.deepEqual(mergeRefreshed(all, fresh).map((r) => r.title), all.map((r) => r.title))
  // The list got shorter: rows past the new end go.
  const shorter = pageOfWanted(list.slice(0, 5), { limit: 2 })
  assert.deepEqual(mergeRefreshed(all, shorter).map((r) => r.title), ['Arrival', 'Brazil', 'Severance'])
  // Fewer rows loaded than the refresh brought: the refresh wins.
  assert.equal(mergeRefreshed(all.slice(0, 1), fresh).length, 2)
})
