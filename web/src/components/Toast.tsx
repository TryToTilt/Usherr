import { createContext, useCallback, useContext, useMemo, useRef, useState, type ReactNode } from 'react'
import BusyNotice from './BusyNotice'

interface ToastItem {
  id: number
  kind: 'success' | 'error' | 'info'
  message: string
}

interface ToastApi {
  success: (message: string) => void
  error: (message: string) => void
  info: (message: string) => void
}

const ToastContext = createContext<ToastApi | null>(null)

// A small, unmissable confirmation for controls that save the moment you
// change them (toggles, switches): "Saved" slides in, then goes away.
export function ToastProvider({ children }: { children: ReactNode }) {
  const [items, setItems] = useState<ToastItem[]>([])
  const nextId = useRef(1)
  const hold = useRef(new Map<number, { kind: ToastItem['kind']; timer: ReturnType<typeof setTimeout> }>())
  const drop = useCallback((id: number) => {
    hold.current.delete(id)
    setItems((cur) => cur.filter((t) => t.id !== id))
  }, [])

  const push = useCallback((kind: ToastItem['kind'], message: string) => {
    const id = nextId.current++
    setItems((cur) => [...cur.slice(-3), { id, kind, message }])
    hold.current.set(id, { kind, timer: setTimeout(() => drop(id), kind === 'error' ? 6000 : 2600) })
  }, [drop])

  // Pointing at a toast keeps it on screen; it goes shortly after the
  // pointer leaves.
  const pause = useCallback((id: number) => {
    const h = hold.current.get(id)
    if (h) clearTimeout(h.timer)
  }, [])
  const resume = useCallback(
    (id: number) => {
      const h = hold.current.get(id)
      if (h) {
        clearTimeout(h.timer)
        h.timer = setTimeout(() => drop(id), 2000)
      }
    },
    [drop],
  )

  const api = useMemo<ToastApi>(
    () => ({
      success: (m) => push('success', m),
      error: (m) => push('error', m),
      info: (m) => push('info', m),
    }),
    [push],
  )

  return (
    <ToastContext.Provider value={api}>
      {children}
      <div className="toast-stack" role="status" aria-live="polite">
        {items.map((t) => (
          <div key={t.id} className={`toast toast-${t.kind}`} role={t.kind === 'error' ? 'alert' : undefined} onMouseEnter={() => pause(t.id)} onMouseLeave={() => resume(t.id)}>
            <span className="toast-icon" aria-hidden="true">
              {t.kind === 'success' ? '✓' : t.kind === 'error' ? '!' : 'i'}
            </span>
            {t.message}
          </div>
        ))}
        <BusyNotice />
      </div>
    </ToastContext.Provider>
  )
}

export function useToast(): ToastApi {
  const ctx = useContext(ToastContext)
  if (!ctx) throw new Error('useToast must be used inside <ToastProvider>')
  return ctx
}
