import { test } from 'node:test'
import assert from 'node:assert/strict'
import { looseIncludes, looseText } from './looseText.ts'

test('looseText reads & as and and drops accents', () => {
  const cases: [string, string][] = [
    ['Tom & Jerry', 'tom and jerry'],
    ['Tom&Jerry', 'tom and jerry'],
    ['Tom &amp; Jerry', 'tom and jerry'],
    ['Crosby, Stills & Nash', 'crosby, stills and nash'],
    ['Amélie', 'amelie'],
    ['  Law   &   Order ', 'law and order'],
  ]
  for (const [input, want] of cases) assert.equal(looseText(input), want, input)
})

test('looseIncludes finds a name by either spelling of "and"', () => {
  const cases: [string, string, boolean][] = [
    ['Tom & Jerry', 'tom and jerry', true],
    ['Tom and Jerry', 'tom & jerry', true],
    ['Dungeons & Dragons: Honor Among Thieves', 'dungeons &', true],
    ['Dungeons & Dragons: Honor Among Thieves', 'dragons', true],
    ['Law & Order', 'order', true],
    ['Law & Order', 'sam', false],
    ['Anything', '', true],
    ['Anything', '   ', true],
  ]
  for (const [name, needle, want] of cases) assert.equal(looseIncludes(name, needle), want, `${name} / ${needle}`)
})
