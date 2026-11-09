// Run with: npm test
import assert from 'node:assert/strict'
import { test } from 'node:test'
import { bc, bcTo, fixPath, forLook, lookOf, remoteHint, walkInLook } from './look.ts'
import type { SmhCtx, SmhStep } from './types.ts'

const ctx = (over: Partial<SmhCtx> = {}): SmhCtx => ({ answers: {}, mode: 'simple', watch: false, admin: true, ...over })

const steps: SmhStep[] = [
  { kind: 'say', title: 'everyone' },
  { kind: 'point', target: 'title-more', look: 'broadcast', title: 'More' },
  { kind: 'point', target: 'title-manage', title: 'Manage' },
  { kind: 'say', look: 'other', title: 'classic only' },
]

test('Classic and Neu keep every step they had, and only those', () => {
  assert.deepEqual(
    forLook(steps, false).map((s) => s.title),
    ['everyone', 'Manage', 'classic only'],
  )
})

test('Broadcast gets its own steps and drops the other looks’', () => {
  assert.deepEqual(
    forLook(steps, true).map((s) => s.title),
    ['everyone', 'More', 'Manage'],
  )
})

test('a walkthrough is listed in its look only', () => {
  assert.equal(walkInLook({}, true), true)
  assert.equal(walkInLook({}, false), true)
  assert.equal(walkInLook({ look: 'broadcast' }, false), false)
  assert.equal(walkInLook({ look: 'other' }, true), false)
  assert.equal(lookOf(undefined), 'other')
})

test('words and pages by look', () => {
  const words = bc('Server, Watching', 'Settings, Extras')
  assert.equal(typeof words === 'function' && words(ctx({ broadcast: true })), 'Server, Watching')
  assert.equal(typeof words === 'function' && words(ctx()), 'Settings, Extras')
  const to = bcTo('/settings/play-on-tv', '/settings/extras#play-on-tv')
  assert.equal(to(ctx({ broadcast: true })), '/settings/play-on-tv')
  assert.equal(to(ctx({ broadcast: false })), '/settings/extras#play-on-tv')
})

test('remote hints only with the remote in hand, and only where a press is wanted', () => {
  const click: SmhStep = { kind: 'point', target: 'x', title: '' }
  const next: SmhStep = { kind: 'point', target: 'x', until: 'next', title: '' }
  const acted: SmhStep = { kind: 'point', target: 'x', act: 'click', title: '' }
  const own: SmhStep = { kind: 'point', target: 'title-more', until: 'next', remote: 'Press Options', title: '' }
  const none: SmhStep = { kind: 'point', target: 'x', remote: '', title: '' }
  const say: SmhStep = { kind: 'say', title: '' }
  assert.equal(remoteHint(click, ctx()), '')
  assert.equal(remoteHint(click, ctx({ remote: true })), 'Press OK on it')
  assert.equal(remoteHint(next, ctx({ remote: true })), '')
  assert.equal(remoteHint(acted, ctx({ remote: true, watch: true })), '')
  assert.equal(remoteHint(acted, ctx({ remote: true })), 'Press OK on it')
  assert.equal(remoteHint(own, ctx({ remote: true })), 'Press Options')
  assert.equal(remoteHint(none, ctx({ remote: true })), '')
  assert.equal(remoteHint(say, ctx({ remote: true })), '')
})

test('Doctor fixes on Extras go to the card’s page in Broadcast only', () => {
  assert.equal(fixPath('/settings/extras#address', true), '/settings/addresses#address')
  assert.equal(fixPath('/settings/extras#virtual-drive', true), '/settings/streaming#virtual-drive')
  assert.equal(fixPath('/settings/extras', true), '/server')
  assert.equal(fixPath('/settings/extras#virtual-drive', false), '/settings/extras#virtual-drive')
  assert.equal(fixPath('/settings/usenet', true), '/settings/usenet')
})
