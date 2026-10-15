// Home fork: on a phone every Discover row is one row you swipe, which loads
// more as you near its end (no "Load more" button). Shared by the movie, TV,
// anime, book and music rows.
import { useEffect, useRef, useState } from 'react'

const QUERY = '(max-width: 700px)'

// useNarrow is true on a phone-width screen.
export function useNarrow(): boolean {
  const [narrow, setNarrow] = useState(() => typeof window !== 'undefined' && window.matchMedia(QUERY).matches)
  useEffect(() => {
    const m = window.matchMedia(QUERY)
    const on = () => setNarrow(m.matches)
    m.addEventListener('change', on)
    return () => m.removeEventListener('change', on)
  }, [])
  return narrow
}

// MoreSentinel sits at the end of a swipe row and asks for more when the row
// is scrolled near its end.
export function MoreSentinel({ onSeen, busy, ratio = '2 / 3.5' }: { onSeen: () => void; busy: boolean; ratio?: string }) {
  const el = useRef<HTMLDivElement>(null)
  useEffect(() => {
    const node = el.current
    if (!node || busy) return
    const row = node.parentElement
    if (!row) return
    const check = () => {
      if (row.scrollLeft + row.clientWidth >= row.scrollWidth - 320) onSeen()
    }
    row.addEventListener('scroll', check, { passive: true })
    const io = new IntersectionObserver((es) => es.some((e) => e.isIntersecting) && onSeen(), { root: row, rootMargin: '0px 300px 0px 0px' })
    io.observe(node)
    return () => {
      row.removeEventListener('scroll', check)
      io.disconnect()
    }
  }, [onSeen, busy])
  return <div ref={el} className="skeleton poster-scroll-more" style={{ aspectRatio: ratio }} aria-hidden="true" />
}
