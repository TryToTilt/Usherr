// Home fork: rows that scroll sideways remember how far you swiped, for the
// session (and through a reload), keyed by the page and the row (the row's id
// says which tab it's on).
import { useEffect, type RefObject } from 'react'
import { useLocation } from 'react-router-dom'

function read(key: string): number {
  try {
    return Number(sessionStorage.getItem(key)) || 0
  } catch {
    return 0
  }
}

function write(key: string, v: number) {
  try {
    if (v > 0) sessionStorage.setItem(key, String(Math.round(v)))
    else sessionStorage.removeItem(key)
  } catch {
    // remembering is optional
  }
}

// useRowMemory keeps el's sideways position under id. ready says the row's
// items are there, so the old position can be reached.
export function useRowMemory(el: RefObject<HTMLElement | null>, id: string, ready = true) {
  const { pathname } = useLocation()
  const key = `row:${pathname}:${id}`
  useEffect(() => {
    const node = el.current
    if (!node || !ready) return
    const want = read(key)
    let tries = 0
    let timer = 0
    const put = () => {
      if (want > 0) node.scrollLeft = want
      if (want > 0 && Math.abs(node.scrollLeft - want) > 4 && tries++ < 20) timer = window.setTimeout(put, 150)
    }
    put()
    let pending = 0
    const save = () => {
      window.clearTimeout(pending)
      pending = window.setTimeout(() => write(key, node.scrollLeft), 120)
    }
    node.addEventListener('scroll', save, { passive: true })
    return () => {
      window.clearTimeout(timer)
      window.clearTimeout(pending)
      node.removeEventListener('scroll', save)
    }
  }, [el, key, ready])
}
