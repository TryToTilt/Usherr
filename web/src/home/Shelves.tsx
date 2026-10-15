// Home fork: the Library as shelves: rows of posters that scroll sideways,
// grouped the way you choose (genre, decade, A–Z, mood…). Each row draws its
// first posters only once it's near the screen, adds more as you swipe to its
// end, and remembers how far you swiped.
import { useEffect, useRef, useState, type ReactNode } from 'react'
import { t } from '../i18n'
import { useNearViewport } from '../useNearViewport'
import Icon from '../components/Icon'
import { useRowMemory } from './rowMemory'
import './news.css'

export interface Shelf<T> {
  key: string
  title: string
  hint?: string
  items: T[]
  // Opens everything in this row as the usual grid (a filter), when there's one.
  seeAll?: () => void
  // Something more in the heading (a button).
  extra?: ReactNode
}

const FIRST = 24
const MORE = 24

export default function Shelves<T>({ shelves, render, itemKey, empty, rowClass = '', bar, memory = '' }: { shelves: Shelf<T>[]; render: (item: T) => ReactNode; itemKey: (item: T) => string; empty?: ReactNode; rowClass?: string; bar?: ReactNode; memory?: string }) {
  if (shelves.length === 0)
    return (
      <>
        {bar}
        {empty ?? null}
      </>
    )
  return (
    <div className="shelves">
      {bar}
      {shelves.map((s) => (
        <ShelfRow key={s.key} shelf={s} render={render} itemKey={itemKey} rowClass={rowClass} memory={memory} />
      ))}
    </div>
  )
}

function ShelfRow<T>({ shelf, render, itemKey, rowClass, memory }: { shelf: Shelf<T>; render: (item: T) => ReactNode; itemKey: (item: T) => string; rowClass: string; memory: string }) {
  const box = useRef<HTMLElement>(null)
  const row = useRef<HTMLDivElement>(null)
  const near = useNearViewport(box, 900)
  const [count, setCount] = useState(FIRST)
  const shown = shelf.items.slice(0, count)
  const more = count < shelf.items.length
  useRowMemory(row, `shelf:${memory}:${shelf.key}`, near)

  // More posters as the row is swiped near its end.
  useEffect(() => {
    const node = row.current
    if (!node || !near || !more) return
    const check = () => {
      if (node.scrollLeft + node.clientWidth >= node.scrollWidth - 400) setCount((c) => Math.min(shelf.items.length, c + MORE))
    }
    node.addEventListener('scroll', check, { passive: true })
    check()
    return () => node.removeEventListener('scroll', check)
  }, [near, more, shelf.items.length])

  return (
    <section ref={box} className="shelf" aria-label={shelf.title}>
      <div className="shelf-head">
        <h2>{shelf.title}</h2>
        <small className="hint">{shelf.items.length}</small>
        {shelf.hint && <span className="hint shelf-hint">{shelf.hint}</span>}
        {shelf.extra}
        {shelf.seeAll && (
          <button className="rail-more btn-link" onClick={shelf.seeAll}>
            {t('See all')} <Icon name="chevron-right" size={14} />
          </button>
        )}
      </div>
      <div ref={row} className={`poster-scroll shelf-row ${rowClass}`}>
        {near ? shown.map((item) => <div key={itemKey(item)}>{render(item)}</div>) : Array.from({ length: Math.min(4, shelf.items.length) }, (_, i) => <div key={i} className="skeleton" style={{ aspectRatio: '2 / 3.5' }} />)}
        {near && more && <div className="skeleton shelf-more" style={{ aspectRatio: '2 / 3.5' }} aria-hidden="true" />}
      </div>
    </section>
  )
}
