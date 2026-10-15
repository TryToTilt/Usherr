// Home fork: whether to show the first-visit tour, and starting it again from
// the user guide. Small on purpose: the tour itself loads only when shown.
import { lazy, Suspense, useEffect, useState } from 'react'
import { useAuth } from '../AuthContext'

const Tour = lazy(() => import('./Tour'))
const KEY = (id: number | string) => `mediarium.tour.done.${id}`
const START = 'mediarium-tour-start'

export function tourDone(id: number | string): void {
  try {
    localStorage.setItem(KEY(id), '1')
  } catch {
    // Private windows can refuse storage: the tour may then show again next time.
  }
}

function seen(id: number | string): boolean {
  try {
    return localStorage.getItem(KEY(id)) === '1'
  } catch {
    return true // can't remember it, so don't keep showing it
  }
}

// Plays the tour again (Help → Take the tour).
export function startTour(): void {
  window.dispatchEvent(new Event(START))
}

export default function TourGate() {
  const { user } = useAuth()
  const [open, setOpen] = useState(false)
  useEffect(() => {
    // A new account sees it once, after a moment for the page to settle.
    if (!user || seen(user.id)) return
    const id = window.setTimeout(() => setOpen(true), 1200)
    return () => window.clearTimeout(id)
  }, [user])
  useEffect(() => {
    const on = () => setOpen(true)
    window.addEventListener(START, on)
    return () => window.removeEventListener(START, on)
  }, [])
  if (!open) return null
  return (
    <Suspense fallback={null}>
      <Tour onClose={() => setOpen(false)} />
    </Suspense>
  )
}
