// Home fork: a short first-visit tour for a new account. Seven cards, each
// pointing at one place (the AI bar, Discover, the Library, Upcoming, your
// stack, the guide); where that place isn't on screen (a phone's folded
// menu) the card sits in the middle instead. Done or skipped once, it doesn't
// come back; Help → "Take the tour" plays it again.
import { useEffect, useLayoutEffect, useMemo, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { useAuth } from '../AuthContext'
import Icon from '../components/Icon'
import { t } from '../i18n'
import { tourDone } from './tourGate'
import './news.css'

interface Step {
  target?: string // CSS selector of what to point at
  title: string
  text: string
  path?: string // the page to be on for this step
}

function steps(): Step[] {
  return [
    { title: t('Welcome to Mediarium'), text: t('A quick look around, seven steps. You can skip it, and see it again any time from the user guide.') },
    { path: '/', target: '.omni-bar.ai, .omni-bar', title: t('Ask for anything'), text: t('Type what you are in the mood for, in your own words, and Mediarium suggests titles. "What should I watch tonight?" picks from what is already here.') },
    { target: 'nav a[href="/discover"]', title: t('Discover'), text: t('Rows of films and shows to browse. Add what you like; the family filter and "Hide what I have" are at the top.') },
    { target: 'nav a[href="/library"]', title: t('Library'), text: t('Everything the house has. Play what is downloaded; Stream plays the rest straight away.') },
    { target: 'nav a[href="/calendar"]', title: t('Upcoming'), text: t('New episodes and films on the way, and the actors and directors you follow.') },
    { target: '.side-stack', title: t('Your stack'), text: t('What you want to watch next, as a stack of tickets. Put a title on it with the stack button on any poster, swipe through it, and tap a poster to play it.') },
    { target: '.help-tool', title: t('Help is always here'), text: t('The ? opens the user guide. Small ? marks on pages open the part about that page.') },
  ]
}

const visible = (el: Element | null): el is HTMLElement => {
  if (!el) return false
  const r = el.getBoundingClientRect()
  // On screen, and not a folded-away menu sitting off its edge
  return r.width > 0 && r.height > 0 && r.bottom > 0 && r.top < window.innerHeight && r.left >= 0 && r.right <= window.innerWidth
}

export default function Tour({ onClose }: { onClose: () => void }) {
  const { user } = useAuth()
  const navigate = useNavigate()
  const list = useMemo(steps, []) // made once: the effects below follow the step
  const [i, setI] = useState(0)
  const [box, setBox] = useState<DOMRect | null>(null)
  const step = list[i]
  const finish = () => {
    if (user) tourDone(user.id)
    onClose()
  }

  useEffect(() => {
    if (step.path && window.location.pathname !== step.path) navigate(step.path)
  }, [step, navigate])
  // Find what this step points at, again whenever the page moves.
  useLayoutEffect(() => {
    const find = () => {
      const el = step.target ? [...document.querySelectorAll(step.target)].find(visible) ?? null : null
      const r = el ? el.getBoundingClientRect() : null
      // Only when it moved, so the page isn't drawn again for nothing.
      setBox((b) => (b && r && b.left === r.left && b.top === r.top && b.width === r.width && b.height === r.height) || (!b && !r) ? b : r)
    }
    find()
    const id = window.setInterval(find, 400) // the page may still be loading
    window.addEventListener('resize', find)
    return () => {
      window.clearInterval(id)
      window.removeEventListener('resize', find)
    }
  }, [step])
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') finish()
      if (e.key === 'ArrowRight') setI((n) => Math.min(n + 1, list.length - 1))
      if (e.key === 'ArrowLeft') setI((n) => Math.max(n - 1, 0))
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  })

  // The card goes under what it points at, or above it near the bottom of the
  // screen, or beside the menu; in the middle when there's nothing to point at.
  const W = Math.min(340, window.innerWidth - 32)
  let style: React.CSSProperties = { left: '50%', top: '50%', transform: 'translate(-50%, -50%)', width: W }
  if (box) {
    const beside = box.left < 300 && box.width < 300 // a menu item
    const left = beside ? box.right + 16 : Math.min(Math.max(16, box.left), window.innerWidth - W - 16)
    const top = beside ? Math.max(16, Math.min(box.top - 8, window.innerHeight - 260)) : box.bottom + 16 + 200 < window.innerHeight ? box.bottom + 16 : Math.max(16, box.top - 216)
    style = { left, top, width: W }
  }
  const last = i === list.length - 1
  return (
    <div className="tour" role="dialog" aria-modal="true" aria-label={t('Tour')}>
      {box ? (
        <div className="tour-spot" style={{ left: box.left - 8, top: box.top - 8, width: box.width + 16, height: box.height + 16 }} />
      ) : (
        <div className="tour-shade" />
      )}
      <div className="tour-card card" style={style}>
        <span className="tour-count">{t('{n} of {total}', { n: i + 1, total: list.length })}</span>
        <h2>{step.title}</h2>
        <p>{step.text}</p>
        <div className="tour-nav">
          <button className="btn-link link-btn" onClick={finish}>
            {t('Skip the tour')}
          </button>
          <span className="spacer" />
          {i > 0 && (
            <button className="btn-sm" onClick={() => setI(i - 1)}>
              <Icon name="chevron-left" size={14} /> {t('Back')}
            </button>
          )}
          <button className="btn-sm primary" onClick={() => (last ? finish() : setI(i + 1))} autoFocus>
            {last ? t('Done') : t('Next')} {!last && <Icon name="chevron-right" size={14} />}
          </button>
        </div>
      </div>
    </div>
  )
}
