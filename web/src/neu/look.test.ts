import { test } from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { lookAttrs, lookFrom } from './look.ts'

test('a browser that never picked gets Broadcast; a saved choice is kept as it is', () => {
  assert.equal(lookFrom(null), 'broadcast')
  assert.equal(lookFrom('something old'), 'broadcast')
  assert.equal(lookFrom('classic'), 'classic')
  assert.equal(lookFrom('neu'), 'neu')
  assert.equal(lookFrom('lobby'), 'lobby')
  assert.equal(lookFrom('broadcast'), 'broadcast')
})

test('each look puts the same marks on the page as before; Broadcast has its own layout', () => {
  assert.deepEqual(lookAttrs('classic'), {})
  assert.deepEqual(lookAttrs('neu'), { look: 'neu' })
  assert.deepEqual(lookAttrs('lobby'), { look: 'neu', layout: 'lobby' })
  assert.deepEqual(lookAttrs('broadcast'), { look: 'broadcast', layout: 'broadcast' })
})

test('main.tsx sets Broadcast before the first paint when nothing (or nothing known) is saved', () => {
  const main = readFileSync(new URL('../main.tsx', import.meta.url), 'utf8')
  assert.match(main, /look !== 'neu' && look !== 'lobby' && look !== 'classic'/)
  assert.match(main, /dataset\.look = 'broadcast'/)
  assert.match(main, /dataset\.layout = 'broadcast'/)
})

test('every Broadcast rule is behind data-look or data-layout="broadcast"', () => {
  const css = readFileSync(new URL('broadcast.css', import.meta.url), 'utf8').replace(/\/\*[\s\S]*?\*\//g, '')
  const selectors = [...css.matchAll(/(^|[{}])\s*([^{}@]+)\{/g)].map((m) => m[2].trim()).filter((s) => s && !s.startsWith('from') && !/^\d/.test(s))
  assert.ok(selectors.length > 100, 'the rules were not read')
  for (const list of selectors) {
    for (const sel of list.split(/,(?![^(]*\))/)) {
      const s = sel.trim()
      if (s === '' || s === 'font-family') continue
      assert.match(s, /data-(look|layout)='broadcast'/, `"${s}" would change Neu or Classic`)
    }
  }
})
