import assert from 'node:assert/strict'
import { test } from 'node:test'
import { fixedInNewerVersion } from './knownFixes.ts'

const ADDRESS = "the site definition wants to connect to apibay.org, which is not one of the site's own addresses"

test('known errors on an older version point at the fixing version', () => {
  assert.equal(fixedInNewerVersion(ADDRESS, '3.1.3'), '3.2.1')
  assert.equal(fixedInNewerVersion('Not supported yet: field title: more than 32 filters', '3.2.0'), '3.2.1')
  assert.equal(fixedInNewerVersion('XML syntax error on line 12: unexpected end element </link>', '3.0.5'), '3.2.1')
  assert.equal(fixedInNewerVersion(`Last test failed: ${ADDRESS}`, 'v3.1.3-beta.2'), '3.2.1')
})

test('nothing is added when the fix is installed or the text is unknown', () => {
  assert.equal(fixedInNewerVersion(ADDRESS, '3.2.1'), '')
  assert.equal(fixedInNewerVersion(ADDRESS, '3.3.0'), '')
  assert.equal(fixedInNewerVersion('connection refused', '3.1.3'), '')
  assert.equal(fixedInNewerVersion('', '3.1.3'), '')
  assert.equal(fixedInNewerVersion(ADDRESS, ''), '')
})
