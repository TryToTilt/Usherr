import { useEffect, useState } from 'react'

// True while a scrolling list has more below the part you can see, so the
// sidebar can fade its bottom edge and show that the menu goes on.
export function moreBelow(scrollHeight: number, clientHeight: number, scrollTop: number): boolean {
  return scrollHeight - clientHeight - scrollTop > 4
}

export function useMoreBelow(target: React.RefObject<HTMLElement | null>): boolean {
  const [more, setMore] = useState(false)
  useEffect(() => {
    const el = target.current
    if (!el) return
    const measure = () => setMore(moreBelow(el.scrollHeight, el.clientHeight, el.scrollTop))
    measure()
    el.addEventListener('scroll', measure, { passive: true })
    window.addEventListener('resize', measure)
    const size = new ResizeObserver(measure)
    size.observe(el)
    const kids = new MutationObserver(measure)
    kids.observe(el, { childList: true, subtree: true })
    return () => {
      el.removeEventListener('scroll', measure)
      window.removeEventListener('resize', measure)
      size.disconnect()
      kids.disconnect()
    }
  }, [target])
  return more
}
