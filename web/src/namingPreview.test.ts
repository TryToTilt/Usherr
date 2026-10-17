import assert from 'node:assert/strict'
import { test } from 'node:test'
import { namingPreviewPath } from './namingPreview.ts'

test('a preset preview asks for that preset and kind only', () => {
  assert.equal(namingPreviewPath('plex'), '/settings/naming-preview?preset=plex&kind=movie')
  assert.equal(namingPreviewPath('kodi', undefined, 'tv'), '/settings/naming-preview?preset=kodi&kind=tv')
})

test('a custom format is sent encoded', () => {
  const path = namingPreviewPath('custom', '{Movie Title} & {Original Filename}')
  assert.equal(new URLSearchParams(path.split('?')[1]).get('format'), '{Movie Title} & {Original Filename}')
})

test('keeping the original names asks for the kept name', () => {
  assert.equal(new URLSearchParams(namingPreviewPath('plex', undefined, 'movie', true).split('?')[1]).get('keep'), '1')
  assert.equal(new URLSearchParams(namingPreviewPath('plex').split('?')[1]).has('keep'), false)
})
