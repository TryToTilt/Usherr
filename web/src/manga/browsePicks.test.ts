import assert from 'node:assert/strict'
import { test } from 'node:test'
import type { DexTitle } from './browseApi.ts'
import { firstReadable, fromDex, fromFound, mergePicks, nextTagState } from './browsePicks.ts'
import type { MangaFound } from './mangaApi.ts'

const found = (id: number, title: string, mangadexId?: string): MangaFound => ({ anilistId: id, title, aliases: null, format: 'manga', status: 'releasing', startYear: 2020, author: '', publisher: '', licensedEn: -1, volumes: 0, mangadexId })
const dex = (id: string, title: string, anilistId?: number, canRead = true): DexTitle => ({ id, title, anilistId, canRead, coverUrl: `/c/${id}` })

test('a title on both AniList and MangaDex shows once, with what each knows', () => {
  const a = [fromFound(found(1, 'Tower'), true), fromFound(found(2, 'River'), true)]
  const d = [fromDex(dex('d-tower', 'Tower (MD)', 1)), fromDex(dex('d-solo', 'Solo'))]
  const all = mergePicks(a, d)
  assert.deepEqual(
    all.map((p) => p.title),
    ['Tower', 'River', 'Solo'],
  )
  assert.equal(all[0].dexId, 'd-tower')
  assert.equal(all[0].canRead, true)
  assert.equal(all[0].coverUrl, '/c/d-tower') // AniList had none here
  assert.equal(all[1].dexId, undefined)
  assert.equal(all[1].canRead, false)
})

test('AniList’s link to MangaDex counts only when MangaDex is on', () => {
  assert.equal(fromFound(found(3, 'Link', 'abc'), true).dexId, 'abc')
  assert.equal(fromFound(found(3, 'Link', 'abc'), false).dexId, undefined)
  assert.equal(fromFound(found(3, 'Link', 'abc'), false).canRead, false)
})

test('MangaDex’s score is out of 10, the tile’s out of 100', () => {
  assert.equal(fromDex({ ...dex('x', 'X'), score: 8.37 }).score, 84)
})

test('a tag goes round: wanted, left out, neither', () => {
  assert.equal(nextTagState(undefined), 'in')
  assert.equal(nextTagState('in'), 'out')
  assert.equal(nextTagState('out'), undefined)
})

test('Read now starts where you stopped, else at the first chapter to read here', () => {
  const list = [{ id: 'a', externalUrl: 'https://x' }, { id: 'b' }, { id: 'c' }]
  assert.equal(firstReadable(list), 'b')
  assert.equal(firstReadable(list, 'c'), 'c')
  assert.equal(firstReadable(list, 'gone'), 'b')
  assert.equal(firstReadable([{ id: 'a', externalUrl: 'https://x' }]), undefined)
})
