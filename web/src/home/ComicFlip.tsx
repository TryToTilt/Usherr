// Home fork: the comic reader's page-turn view. Pages curl like paper
// (StPageFlip, MIT): drag a corner, click or swipe. On a wide screen it is an
// open comic, two pages side by side with the cover on its own; held upright
// it is one page at a time. Loaded only when a comic is opened.
import { forwardRef, useEffect, useImperativeHandle, useMemo, useRef, useState } from 'react'
import { PageFlip } from 'page-flip'
import { bookLeaves, bookPad, bookToPage, pageToBook } from './comicReading'

export interface ComicFlipHandle {
  next: () => void
  prev: () => void
  turnTo: (page: number) => void // 1-based
}

interface Props {
  pages: string[] // page image URLs, in order
  start: number // 1-based page to open at
  onPage: (page: number) => void // 1-based, as the reader turns
  flipMs?: number // how long one page turn takes
  rtl?: boolean // right to left: the book is built backwards (comicReading.ts)
}


const ComicFlip = forwardRef<ComicFlipHandle, Props>(function ComicFlip({ pages, start, onPage, flipMs = 650, rtl = false }, ref) {
  const total = pages.length
  // Worked out once: the reader builds a new book when the direction changes.
  const [pad] = useState(() => bookPad(total, rtl, window.innerHeight <= window.innerWidth))
  const leaves = useMemo(() => bookLeaves(total, rtl, pad), [total, rtl, pad])
  const host = useRef<HTMLDivElement>(null)
  const book = useRef<PageFlip | null>(null)
  const onPageRef = useRef(onPage)
  onPageRef.current = onPage
  const current = useRef(start) // the page shown, so a rebuilt book opens there
  const [ratio, setRatio] = useState<number | null>(null) // height / width of a page
  // Held upright (taller than wide) the book is one page at a time; turned
  // sideways, an open comic. The library decides by its minimum page width,
  // so that follows the screen, and the book is rebuilt when it turns.
  const [upright, setUpright] = useState(() => window.innerHeight > window.innerWidth)
  useEffect(() => {
    const onResize = () => setUpright(window.innerHeight > window.innerWidth)
    window.addEventListener('resize', onResize)
    return () => window.removeEventListener('resize', onResize)
  }, [])

  // The page shape comes from the first page, so spreads aren't squashed.
  useEffect(() => {
    let alive = true
    const img = new Image()
    img.onload = () => alive && setRatio(img.naturalWidth > 0 ? img.naturalHeight / img.naturalWidth : 1.5)
    img.onerror = () => alive && setRatio(1.5)
    img.src = pages[0]
    return () => {
      alive = false
    }
  }, [pages])

  useEffect(() => {
    const el = host.current
    if (!el || ratio === null) return
    const width = 800
    const height = Math.round(width * ratio)
    // One page per screen when upright: a page may then be no narrower than
    // half the stage, which is what makes the library show a single page.
    const minWidth = upright ? Math.ceil(el.clientWidth / 2) + 1 : 150
    const pf = new PageFlip(el, {
      width,
      height,
      size: 'stretch',
      minWidth,
      maxWidth: 2000,
      minHeight: 300,
      maxHeight: 3000,
      showCover: true,
      usePortrait: true,
      autoSize: true,
      maxShadowOpacity: 0.45,
      mobileScrollSupport: false,
      flippingTime: flipMs,
      startPage: pageToBook(current.current, total, rtl, pad),
    })
    pf.loadFromHTML(el.querySelectorAll<HTMLElement>('.cxf-page'))
    pf.on('flip', (e) => {
      current.current = bookToPage(Number(e.data), total, rtl, pad, pf.getOrientation() === 'landscape')
      onPageRef.current(current.current)
    })
    book.current = pf
    return () => {
      book.current = null
      try {
        pf.destroy()
      } catch {
        // already gone
      }
    }
    // Rebuild only when the comic itself changes, or the screen turns.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [ratio, pages, upright])

  useImperativeHandle(
    ref,
    () => ({
      // Forward in the comic is backward in a book built backwards.
      next: () => (rtl ? book.current?.flipPrev() : book.current?.flipNext()),
      prev: () => (rtl ? book.current?.flipNext() : book.current?.flipPrev()),
      turnTo: (page: number) => book.current?.turnToPage(pageToBook(page, total, rtl, pad)),
    }),
    [total, rtl, pad],
  )

  return (
    <div className="cxf-stage">
      <div ref={host} className="cxf-book">
        {leaves.map((page, i) => (
          <div key={page} className="cxf-page" data-density={i === 0 || i === leaves.length - 1 ? 'hard' : 'soft'}>
            {page > 0 && <img src={pages[page - 1]} alt={`Page ${page}`} loading={Math.abs(page - start) <= 3 ? 'eager' : 'lazy'} draggable={false} />}
          </div>
        ))}
      </div>
    </div>
  )
})

export default ComicFlip
