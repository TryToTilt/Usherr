import { test } from 'node:test'
import assert from 'node:assert/strict'
import { pickedSuggestion } from './tagPick.ts'

test('typing letters that spell an existing tag is not a pick', () => {
  assert.equal(pickedSuggestion('insertText', 'Kids', ['Kids', '4K']), false)
  assert.equal(pickedSuggestion(undefined, 'Kids', ['Kids']), false)
  assert.equal(pickedSuggestion('insertFromPaste', 'Kids', ['Kids']), false)
})

test('choosing from the list is a pick', () => {
  assert.equal(pickedSuggestion('insertReplacementText', 'Kids', ['Kids', '4K']), true)
})

test('a replacement that is not a known tag is not a pick', () => {
  assert.equal(pickedSuggestion('insertReplacementText', 'Other', ['Kids']), false)
})
