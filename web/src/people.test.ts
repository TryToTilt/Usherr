import assert from 'node:assert/strict'
import { test } from 'node:test'
import { ageOf, arrangeCredits, bioIsLong, creditGenres, creditHaveFrom, creditSortFrom, crewWith, filterByRole, firstOf, longDate, personPath, roleFilterFrom, showRoleFilter } from './people.ts'

test('a person link needs a real id', () => {
  const cases: [number | undefined, string | undefined][] = [
    [6384, '/person/6384'],
    [0, undefined],
    [-3, undefined],
    [undefined, undefined],
  ]
  for (const [id, want] of cases) assert.equal(personPath(id), want, String(id))
})

test('the role filter keeps the titles with that kind of work', () => {
  const items = [
    { title: 'A', roles: ['acting'] },
    { title: 'B', roles: ['acting', 'directing'] },
    { title: 'C', roles: ['other'] },
    { title: 'D', roles: ['writing'] },
  ]
  const names = (role: Parameters<typeof filterByRole>[1]) => filterByRole(items, role).map((i) => i.title).join('')
  assert.equal(names('all'), 'ABCD')
  assert.equal(names('acting'), 'AB')
  assert.equal(names('directing'), 'B')
  assert.equal(names('writing'), 'D')
})

test('the filter only shows when there is a choice, and ignores kinds the person has no work in', () => {
  assert.equal(showRoleFilter([]), false)
  assert.equal(showRoleFilter(['acting']), false)
  assert.equal(showRoleFilter(['acting', 'writing']), true)
  const cases: [string | null, string[], string][] = [
    ['directing', ['acting', 'directing'], 'directing'],
    ['directing', ['acting'], 'all'],
    ['nonsense', ['acting', 'directing'], 'all'],
    [null, ['acting', 'directing'], 'all'],
    ['all', ['acting', 'directing'], 'all'],
  ]
  for (const [value, kinds, want] of cases) assert.equal(roleFilterFrom(value, kinds), want, `${value} / ${kinds}`)
})

test('a long filmography shows its first part and counts what is left', () => {
  const items = Array.from({ length: 130 }, (_, i) => i)
  assert.deepEqual([firstOf(items, 60).visible.length, firstOf(items, 60).hidden], [60, 70])
  assert.deepEqual([firstOf(items, 120).visible.length, firstOf(items, 120).hidden], [120, 10])
  assert.deepEqual([firstOf(items, 500).visible.length, firstOf(items, 500).hidden], [130, 0])
  assert.deepEqual([firstOf([], 60).visible.length, firstOf([], 60).hidden], [0, 0])
})

test('directors, writers and creators come out of the crew list once each', () => {
  const crew = [
    { name: 'Lana', job: 'Director', id: 1 },
    { name: 'Lilly', job: 'Director', id: 2 },
    { name: 'Lana', job: 'Writer', id: 1 },
    { name: 'Lana', job: 'Director', id: 1 },
  ]
  assert.deepEqual(crewWith(crew, 'Director').map((c) => c.name), ['Lana', 'Lilly'])
  assert.deepEqual(crewWith(crew, 'Writer').map((c) => c.name), ['Lana'])
  assert.deepEqual(crewWith(crew, 'Creator'), [])
  assert.deepEqual(crewWith(undefined, 'Director'), [])
})

test('only a long biography is folded', () => {
  assert.equal(bioIsLong(undefined), false)
  assert.equal(bioIsLong('Short.'), false)
  assert.equal(bioIsLong('x'.repeat(420)), false)
  assert.equal(bioIsLong('x'.repeat(421)), true)
})

test('dates read out in full, and odd ones pass through', () => {
  assert.equal(longDate('1964-09-02', 'en-GB'), '2 September 1964')
  assert.equal(longDate('1964-09-02', 'en-US'), 'September 2, 1964')
  assert.equal(longDate('1964-09-02', 'de'), '2. September 1964')
  assert.equal(longDate('1964', 'en-GB'), '1964')
  assert.equal(longDate('', 'en-GB'), '')
  assert.equal(longDate(undefined, 'en-GB'), '')
})

test('age counts whole years, to the death when there was one', () => {
  const today = new Date(2026, 9, 17) // 17 October 2026
  const cases: [string | undefined, string | undefined, number | undefined][] = [
    ['1964-09-02', undefined, 62],
    ['1964-10-17', undefined, 62], // birthday today
    ['1964-10-18', undefined, 61], // tomorrow
    ['1940-12-08', '1980-12-08', 40],
    ['1940-12-09', '1980-12-08', 39],
    [undefined, undefined, undefined],
    ['1964', undefined, undefined],
    ['2030-01-01', undefined, undefined], // born in the future
    ['1800-01-01', undefined, undefined], // nobody is 226
  ]
  for (const [b, d, want] of cases) assert.equal(ageOf(b, d, today), want, `${b} ${d}`)
})

// Usherr: sorting and narrowing a filmography.
const films = [
  { tmdbId: 1, title: 'Big', year: 1988, releaseDate: '1988-06-03', genres: ['Comedy', 'Fantasy'], rating: 6.8, voteCount: 4000 },
  { tmdbId: 2, title: 'Cast Away', year: 2000, releaseDate: '2000-12-22', genres: ['Drama'], rating: 7.7, voteCount: 11000 },
  { tmdbId: 3, title: 'apollo 13', year: 1995, genres: ['Drama', 'History'], rating: 7.4, voteCount: 6000 },
  { tmdbId: 4, title: 'Untitled Project', genres: ['Drama'], rating: 9.5, voteCount: 3 },
  { tmdbId: 5, title: 'Pokémon Thing', year: 2010, genres: [], rating: 5, voteCount: 100 },
]
const none = () => false
const view = (v: Partial<import('./people.ts').CreditView>) => ({ sort: 'popular' as const, genre: '', query: '', have: 'all' as const, ...v })

test('popular keeps the order it came in', () => {
  assert.deepEqual(arrangeCredits([...films], view({}), none).map((f) => f.tmdbId), [1, 2, 3, 4, 5])
})

test('newest and oldest go by date, undated titles last', () => {
  assert.deepEqual(arrangeCredits([...films], view({ sort: 'newest' }), none).map((f) => f.tmdbId), [5, 2, 3, 1, 4])
  assert.deepEqual(arrangeCredits([...films], view({ sort: 'oldest' }), none).map((f) => f.tmdbId), [1, 3, 2, 5, 4])
})

test('highest rated puts a rating from a few votes last', () => {
  assert.deepEqual(arrangeCredits([...films], view({ sort: 'rated' }), none).map((f) => f.tmdbId), [2, 3, 1, 5, 4])
})

test('A to Z ignores case', () => {
  assert.deepEqual(arrangeCredits([...films], view({ sort: 'az' }), none).map((f) => f.title), ['apollo 13', 'Big', 'Cast Away', 'Pokémon Thing', 'Untitled Project'])
})

test('genre, a part of the title and what you have narrow the list', () => {
  assert.deepEqual(arrangeCredits([...films], view({ genre: 'Drama' }), none).map((f) => f.tmdbId), [2, 3, 4])
  assert.deepEqual(arrangeCredits([...films], view({ query: 'pokemon' }), none).map((f) => f.tmdbId), [5])
  const have = (id: number) => id === 2
  assert.deepEqual(arrangeCredits([...films], view({ have: 'have' }), have).map((f) => f.tmdbId), [2])
  assert.equal(arrangeCredits([...films], view({ have: 'missing' }), have).length, 4)
})

test('genres are listed most common first', () => {
  assert.deepEqual(creditGenres(films), ['Drama', 'Comedy', 'Fantasy', 'History'])
  assert.equal(creditSortFrom('bogus'), 'popular')
  assert.equal(creditHaveFrom('have'), 'have')
})
