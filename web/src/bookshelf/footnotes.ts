// Footnotes: which of a book's links are the small marks that lead to a note, and
// the note's words, to read in a card over the page. As plain text: a book's markup
// has no business in the app's own page.
const EPUB_NS = 'http://www.idpf.org/2007/ops'

const typeOf = (el: Element) => `${el.getAttributeNS(EPUB_NS, 'type') ?? ''} ${el.getAttribute('epub:type') ?? ''} ${el.getAttribute('role') ?? ''}`.toLowerCase()

// 1, [1], (a), iv, *, †. A bare number could as well be a page in a list of contents,
// so that needs something more to go on.
const MARK = /^[[(]?(\d{1,4}|[a-z]|[ivxlc]{1,6}|[*†‡§¶]+)[\])]?[.:]?$/i
const SURE_MARK = /^(\[.+\]|\(.+\)|[*†‡§¶]+)$/

export const hrefOf = (a: Element) => a.getAttribute('data-rdr-href') ?? a.getAttribute('href') ?? ''

export function isNoteLink(a: Element): boolean {
  const href = hrefOf(a)
  if (!href.includes('#') || href.endsWith('#')) return false
  if (/\b(noteref|doc-noteref)\b/.test(typeOf(a))) return true
  // A note's own way back is not a note.
  if (/\b(backlink|doc-backlink)\b/.test(typeOf(a))) return false
  const target = href.startsWith('#') ? a.ownerDocument.getElementById(decodeURIComponent(href.slice(1))) : null
  if (target?.closest('aside, section, div, li, p')?.matches('[*|type~="footnote"], [*|type~="endnote"], [*|type~="rearnote"], [*|type~="note"], [role="doc-footnote"], [role="doc-endnote"]')) return true
  const text = (a.textContent ?? '').trim()
  if (!MARK.test(text)) return false
  if (SURE_MARK.test(text) || a.closest('sup') || a.querySelector('sup')) return true
  return /note|foot|\bfn/i.test(`${a.getAttribute('class') ?? ''} ${a.getAttribute('id') ?? ''}`)
}

const INLINE = new Set(['a', 'span', 'sup', 'sub', 'em', 'i', 'b', 'strong', 'small', 'cite', 'u'])
const BLOCK = 'p, li, dd, div, blockquote, h1, h2, h3, h4, h5, h6, tr'
const MAX_NOTE = 6000

// noteText reads the note at an element: the block around it if it is only a mark in a
// line. Empty: nothing readable there, follow the link the ordinary way.
export function noteText(target: Element): string {
  let el: Element | null = target
  while (el && INLINE.has(el.localName)) el = el.parentElement
  if (!el || el.localName === 'body' || el.localName === 'html') return ''
  const copy = el.cloneNode(true) as Element
  // The way back, and the note's own number in front of it, are for the page only.
  for (const a of Array.from(copy.querySelectorAll('a'))) {
    const text = (a.textContent ?? '').trim()
    if (/\b(backlink|doc-backlink)\b/.test(typeOf(a)) || /^[↩↑⏎]/.test(text) || (a.getAttribute('href') && MARK.test(text))) a.remove()
  }
  const blocks = Array.from(copy.querySelectorAll(BLOCK)).filter((b) => !b.querySelector(BLOCK))
  const parts = blocks.length > 0 ? blocks.map((b) => b.textContent ?? '') : [copy.textContent ?? '']
  const text = parts
    .map((s) => s.replace(/\s+/g, ' ').trim())
    .filter(Boolean)
    .join('\n\n')
  return text.length > MAX_NOTE ? `${text.slice(0, MAX_NOTE)}…` : text
}

// Where a link points, from the chapter it is in: the chapter's address in the book, and the place in it.
export function resolveHref(href: string, from: string): { path: string; id: string } {
  const u = new URL(href, `http://book/${from.split('/').map(encodeURIComponent).join('/')}`)
  return { path: decodeURIComponent(u.pathname.slice(1)), id: decodeURIComponent(u.hash.slice(1)) }
}
