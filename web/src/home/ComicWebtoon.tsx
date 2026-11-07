// Usherr: the comic reader's webtoon view: one long vertical scroll with no
// gaps. Pages load as they come near (the browser's own lazy loading). A page
// not loaded yet holds a screen's height, so far-off pages stay unloaded and a
// jump lands where it should; the view stays pinned to the page jumped to while
// the pages around it load, until you scroll yourself.
import { forwardRef, useCallback, useEffect, useImperativeHandle, useLayoutEffect, useRef } from 'react'
import { t } from '../i18n'

export interface ComicWebtoonHandle {
  scroll: (screens: number) => void // down (or up, negative) by that share of the screen
  toPage: (page: number) => void
}

interface Props {
  pageUrl: (n: number) => string
  total: number
  start: number
  onPage: (page: number) => void
}

const ComicWebtoon = forwardRef<ComicWebtoonHandle, Props>(function ComicWebtoon({ pageUrl, total, start, onPage }, ref) {
  const box = useRef<HTMLDivElement>(null)
  const pin = useRef(start)
  const onPageRef = useRef(onPage)
  onPageRef.current = onPage

  const showPinned = useCallback(() => {
    const el = box.current?.querySelector<HTMLElement>(`[data-page="${pin.current}"]`)
    if (el && box.current) box.current.scrollTop = el.offsetTop
  }, [])
  // The last page may be too short ever to reach the middle of the screen.
  const checkEnd = useCallback(() => {
    const el = box.current
    if (el && pin.current === 0 && el.scrollTop + el.clientHeight >= el.scrollHeight - 4) onPageRef.current(total)
  }, [total])

  useLayoutEffect(showPinned, [showPinned])

  // The page you're on is the one across the middle of the screen.
  useEffect(() => {
    const el = box.current
    if (!el) return
    const seen = new IntersectionObserver(
      (entries) => {
        if (pin.current !== 0) return
        for (const e of entries) if (e.isIntersecting) onPageRef.current(Number((e.target as HTMLElement).dataset.page))
      },
      { root: el, rootMargin: '-50% 0px -50% 0px' },
    )
    el.querySelectorAll('[data-page]').forEach((im) => seen.observe(im))
    return () => seen.disconnect()
  }, [total])

  useImperativeHandle(
    ref,
    () => ({
      scroll: (screens: number) => {
        pin.current = 0
        const el = box.current
        if (el) el.scrollTop += el.clientHeight * screens
      },
      toPage: (page: number) => {
        pin.current = page
        showPinned()
      },
    }),
    [showPinned],
  )

  const letGo = () => {
    pin.current = 0
  }
  return (
    <div ref={box} className="cx-webtoon" onScroll={checkEnd} onWheel={letGo} onTouchStart={letGo} onPointerDown={letGo}>
      {Array.from({ length: total }, (_, i) => i + 1).map((n) => (
        <img
          key={n}
          loading={Math.abs(n - start) <= 1 ? 'eager' : 'lazy'}
          data-page={n}
          className="cx-webtoon-page"
          src={pageUrl(n)}
          alt={t('Page {n}', { n })}
          draggable={false}
          onLoad={(e) => {
            e.currentTarget.style.minHeight = '0'
            if (pin.current !== 0) showPinned()
            checkEnd()
          }}
        />
      ))}
    </div>
  )
})

export default ComicWebtoon
