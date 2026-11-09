// Run with: npm test
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { test } from 'node:test'
import { BROADCAST_GROUPS, BROADCAST_NEW_PAGES, broadcastPages, extrasMovedTo, inYourSettings, YOUR_SETTINGS } from './broadcastNav.ts'
import { broadcastKinds, KIND_ORDER } from './kinds.ts'
import { asMenuMode } from './menuMode.ts'

// settingsNav.ts's pages, read from its text (it imports modules node can't
// load on its own): each line of SETTINGS_NAV.
const SETTINGS_NAV = (() => {
  const src = readFileSync(new URL('../settingsNav.ts', import.meta.url), 'utf8')
  const block = src.slice(src.indexOf('export const SETTINGS_NAV'), src.indexOf('export const SETTINGS_REDIRECTS'))
  return [...block.matchAll(/\{ to: '([^']+)', label: [^\n]*/g)].map((m) => ({ to: m[1], label: '', icon: 'grid' as const, color: '', blurb: '', member: m[0].includes('member: true') }))
})()

test('every settings page is in exactly one Broadcast group, and Extras in none', () => {
  const pages = broadcastPages(SETTINGS_NAV).map((p) => p.to)
  const grouped = BROADCAST_GROUPS.flatMap((g) => g.pages)
  assert.equal(new Set(grouped).size, grouped.length, 'a page is in two groups')
  for (const to of pages) assert.ok(grouped.includes(to), `${to} is in no group`)
  for (const to of grouped) assert.ok(pages.includes(to), `${to} is grouped but not a page`)
  assert.ok(!pages.includes('/settings/extras'))
  assert.equal(BROADCAST_GROUPS.filter((g) => g.key !== YOUR_SETTINGS).length, 7)
})

test('the pages only Broadcast has are routes', () => {
  const app = readFileSync(new URL('../App.tsx', import.meta.url), 'utf8')
  for (const p of BROADCAST_NEW_PAGES) assert.ok(app.includes(`<Route path="${p.to.replace('/settings/', '')}"`), `${p.to} is not a route`)
})

test('your own settings are under You, everything else under Server', () => {
  assert.ok(inYourSettings('/settings/profile'))
  assert.ok(inYourSettings('/settings/general'))
  assert.ok(!inYourSettings('/settings/media'))
  assert.ok(SETTINGS_NAV.filter((p) => p.member).every((p) => inYourSettings(p.to)), 'a member page is under Server')
})

test('an old Settings > Extras link goes to its card’s new page', () => {
  assert.equal(extrasMovedTo('#sports'), '/settings/sports#sports')
  assert.equal(extrasMovedTo('#virtual-drive'), '/settings/streaming#virtual-drive')
  assert.equal(extrasMovedTo('#address'), '/settings/addresses#address')
  assert.equal(extrasMovedTo(''), '/server')
  assert.equal(extrasMovedTo('#nothing-like-it'), '/server')
})

test('the kinds: one order and one set of names in Broadcast, unchanged elsewhere', () => {
  const opts = [
    { key: 'comics', label: 'Comics', glyph: 'comic' as const, color: 'x' },
    { key: 'tv', label: 'TV', glyph: 'tv' as const, color: 'x', count: 3 },
    { key: 'manga', label: 'Manga', glyph: 'manga' as const, color: 'x' },
    { key: 'music', label: 'Music', glyph: 'music' as const, color: 'x' },
    { key: 'ebook', label: 'Ebooks', glyph: 'book' as const, color: 'x' },
    { key: 'movie', label: 'Movies', glyph: 'movie' as const, color: 'x' },
  ]
  assert.deepEqual(broadcastKinds(opts, false), opts)
  const b = broadcastKinds(opts, true)
  assert.deepEqual(b.map((o) => o.key), ['movie', 'tv', 'music', 'ebook', 'manga', 'comics'])
  assert.equal(b[1].label, 'TV shows')
  assert.equal((b[1] as { count?: number }).count, 3)
  assert.deepEqual([...KIND_ORDER], ['movie', 'tv', 'anime', 'music', 'ebook', 'audiobook', 'manga', 'comics', 'nsfw'])
})

test('the menu mode falls back to icons', () => {
  assert.equal(asMenuMode('open'), 'open')
  assert.equal(asMenuMode('hidden'), 'hidden')
  assert.equal(asMenuMode(null), 'icons')
  assert.equal(asMenuMode('lobby'), 'icons')
})
