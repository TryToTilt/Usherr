// Usherr: settings pages with several cards show each card as its title row
// (its badges say whether it's on or needs setting up); a tap on the title
// opens it. A card showing an error opens by itself. Works on every settings
// page without changing them: it reads the cards the page draws.
import { useCallback, useEffect, useState, type RefObject } from 'react'

const MIN_CARDS = 3

function cardsIn(root: HTMLElement): HTMLElement[] {
  return [...root.querySelectorAll<HTMLElement>('section.card')].filter((c) => c.firstElementChild?.tagName === 'H2' && !c.parentElement?.closest('section.card'))
}

function setOpen(card: HTMLElement, open: boolean) {
  if (open) card.removeAttribute('data-folded')
  else card.setAttribute('data-folded', '')
  card.firstElementChild?.setAttribute('aria-expanded', String(open))
}

export function useFoldCards(root: RefObject<HTMLElement | null>, key: string) {
  const [count, setCount] = useState(0) // how many cards fold on this page
  useEffect(() => {
    const el = root.current
    if (!el) return
    const seen = new WeakSet<Element>()
    const apply = () => {
      const cards = cardsIn(el)
      if (cards.length < MIN_CARDS) {
        setCount(0)
        return
      }
      setCount(cards.length)
      for (const card of cards) {
        if (seen.has(card)) continue
        seen.add(card)
        const head = card.firstElementChild as HTMLElement
        card.setAttribute('data-foldable', '')
        head.setAttribute('role', 'button')
        head.tabIndex = 0
        setOpen(card, !!card.querySelector('.error-text, .field-error, .notice-error'))
        const toggle = () => setOpen(card, card.hasAttribute('data-folded'))
        head.addEventListener('click', (e) => {
          if ((e.target as Element).closest('a, button, input, select, label, [role="switch"]') && e.target !== head) return
          toggle()
        })
        head.addEventListener('keydown', (e) => {
          if (e.target !== head || (e.key !== 'Enter' && e.key !== ' ')) return
          e.preventDefault()
          toggle()
        })
      }
    }
    apply()
    const mo = new MutationObserver(apply)
    mo.observe(el, { childList: true, subtree: true })
    return () => mo.disconnect()
  }, [root, key])
  const all = useCallback(
    (open: boolean) => {
      if (root.current) cardsIn(root.current).forEach((c) => c.hasAttribute('data-foldable') && setOpen(c, open))
    },
    [root],
  )
  return { count, openAll: () => all(true), foldAll: () => all(false) }
}
