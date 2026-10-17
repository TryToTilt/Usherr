import { test } from 'node:test'
import assert from 'node:assert/strict'
import { libraryFirst } from './searchOrder.ts'

test('library titles come first and each group keeps its order', () => {
  const list = [
    { id: 1, inLibrary: false },
    { id: 2, inLibrary: true },
    { id: 3, inLibrary: false },
    { id: 4, inLibrary: true },
  ]
  assert.deepEqual(libraryFirst(list).map((r) => r.id), [2, 4, 1, 3])
})

test('an empty list and a list of one kind are unchanged', () => {
  assert.deepEqual(libraryFirst([]), [])
  assert.deepEqual(libraryFirst([{ id: 1 }, { id: 2 }]).map((r) => r.id), [1, 2])
})
