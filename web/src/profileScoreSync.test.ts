// Run with: npm test (uses node's built-in test runner, no extra packages).
import assert from 'node:assert/strict'
import { test } from 'node:test'
import { syncFormatScores } from './profileScoreSync.ts'

type S = Record<number, string>

const cases: { name: string; base: S; draft: S; server: S; want: S }[] = [
  { name: 'nothing changed anywhere', base: { 1: '100' }, draft: { 1: '100' }, server: { 1: '100' }, want: { 1: '100' } },
  { name: 'scores added on the server show up in the open form', base: { 1: '100' }, draft: { 1: '100' }, server: { 1: '100', 2: '500', 3: '-10000' }, want: { 1: '100', 2: '500', 3: '-10000' } },
  { name: 'a score the person typed is kept over the server', base: { 1: '100' }, draft: { 1: '250' }, server: { 1: '300' }, want: { 1: '250' } },
  { name: 'a score the person has not touched follows the server', base: { 1: '100' }, draft: { 1: '100' }, server: { 1: '300' }, want: { 1: '300' } },
  { name: 'a score removed on the server leaves the form when untouched', base: { 1: '100', 2: '5' }, draft: { 1: '100', 2: '5' }, server: { 1: '100' }, want: { 1: '100' } },
  { name: 'a new score the person typed stays next to server additions', base: {}, draft: { 9: '40' }, server: { 2: '500' }, want: { 9: '40', 2: '500' } },
  { name: 'a score the person cleared stays cleared', base: { 1: '100' }, draft: { 1: '' }, server: { 1: '100', 2: '500' }, want: { 1: '', 2: '500' } },
]
for (const c of cases) {
  test(`syncFormatScores: ${c.name}`, () => {
    assert.deepEqual(syncFormatScores(c.base, c.draft, c.server), c.want)
  })
}
