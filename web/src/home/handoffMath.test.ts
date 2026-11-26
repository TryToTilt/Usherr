import { test } from 'node:test'
import assert from 'node:assert/strict'
import { handoffFor } from './handoffMath.ts'
import type { Showtime } from './showtimes.ts'

const at = 1_000_000_000
const show = (o: Partial<Showtime> = {}): Showtime => ({ id: 1, tmdbId: 18, title: 'The Fifth Element', atMs: at, preshowMin: 15, playOn: 'here', state: 'booked', mine: true, ...o })

test('the move happens when the pre-show starts, with a heads-up before', () => {
  const pre = at - 15 * 60_000
  assert.equal(handoffFor([show()], pre - 11 * 60_000, new Set()), null)
  assert.equal(handoffFor([show()], pre - 5 * 60_000, new Set())?.go, false)
  assert.equal(handoffFor([show()], pre, new Set())?.go, true)
  assert.equal(handoffFor([show({ preshowMin: 0 })], at - 60_000, new Set())?.go, false)
  assert.equal(handoffFor([show({ preshowMin: 0 })], at, new Set())?.go, true)
})

test('not long after it started, not twice, not when it is watched elsewhere or declined', () => {
  assert.equal(handoffFor([show()], at + 6 * 60_000, new Set()), null)
  assert.equal(handoffFor([show()], at, new Set([1])), null)
  assert.equal(handoffFor([show({ playOn: 'tv' })], at, new Set()), null)
  assert.equal(handoffFor([show({ state: 'done' })], at, new Set()), null)
  assert.equal(handoffFor([show({ playOn: 'none', linked: { invited: 2, going: 1, people: 2, mine: 'not' } })], at, new Set()), null)
  assert.equal(handoffFor([show({ playOn: 'none', hosted: { host: 'h', screening: 3 } })], at, new Set())?.go, true)
})

test('the earliest one wins', () => {
  const later = show({ id: 2, atMs: at + 20 * 60_000 })
  assert.equal(handoffFor([later, show()], at - 15 * 60_000, new Set())?.s.id, 1)
})
