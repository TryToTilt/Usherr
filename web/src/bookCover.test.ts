import assert from 'node:assert/strict'
import { test } from 'node:test'
import { coverFileProblem, coverKind, coverLinkProblem, MAX_COVER_BYTES, needsConverting } from './bookCover.ts'

test('a picture is known by its type, and by its name when the type is empty', () => {
  const cases: [{ type: string; name: string }, string][] = [
    [{ type: 'image/jpeg', name: 'a.bin' }, 'jpeg'],
    [{ type: 'image/png', name: 'a' }, 'png'],
    [{ type: 'image/webp', name: 'a.png' }, 'webp'], // the type wins over the name
    [{ type: '', name: 'Cover.JPG' }, 'jpeg'],
    [{ type: '', name: 'cover.jpeg' }, 'jpeg'],
    [{ type: '', name: 'cover.webp' }, 'webp'],
    [{ type: 'image/gif', name: 'cover.gif' }, ''],
    [{ type: 'application/pdf', name: 'cover.png' }, ''],
    [{ type: '', name: 'cover' }, ''],
    [{ type: '', name: 'cover.tiff' }, ''],
  ]
  for (const [file, want] of cases) assert.equal(coverKind(file), want, `${file.type} ${file.name}`)
})

test('a file is refused before it is sent when it cannot work', () => {
  const ok = { size: 1000, type: 'image/png', name: 'a.png' }
  assert.equal(coverFileProblem(ok), '')
  assert.equal(coverFileProblem({ ...ok, size: MAX_COVER_BYTES }), '')
  assert.match(coverFileProblem({ ...ok, size: MAX_COVER_BYTES + 1 }), /bigger than 8 MB/)
  assert.match(coverFileProblem({ ...ok, size: 0 }), /empty/)
  assert.match(coverFileProblem({ ...ok, type: 'image/gif', name: 'a.gif' }), /JPEG, PNG or WebP/)
  assert.match(coverFileProblem(null), /Choose a picture/)
  assert.match(coverFileProblem(undefined), /Choose a picture/)
})

test('only a WebP picture is converted in the browser', () => {
  assert.equal(needsConverting({ type: 'image/webp', name: 'a.webp' }), true)
  assert.equal(needsConverting({ type: '', name: 'a.webp' }), true)
  assert.equal(needsConverting({ type: 'image/png', name: 'a.png' }), false)
  assert.equal(needsConverting({ type: 'image/jpeg', name: 'a.jpg' }), false)
})

test('a link has to be a web address', () => {
  const cases: [string, boolean][] = [
    ['https://covers.example.com/hobbit.jpg', true],
    ['  http://example.com/a.png  ', true],
    ['HTTPS://EXAMPLE.COM/A.PNG', true],
    ['https://example.com', true],
    ['', false],
    ['   ', false],
    ['the hobbit cover', false],
    ['ftp://example.com/a.png', false],
    ['file:///etc/passwd', false],
    ['https://', false],
    ['https:// example.com/a.png', false],
    ['javascript:alert(1)', false],
    ['https://example.com/' + 'a'.repeat(2100), false],
  ]
  for (const [link, ok] of cases) assert.equal(coverLinkProblem(link) === '', ok, link.slice(0, 40))
})
