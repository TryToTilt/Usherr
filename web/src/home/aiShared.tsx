// Home fork: what the AI features share: whether AI is set up (asked once a
// page load), a POST helper, and the dialog the answers open in.
import { useEffect, useRef, useState, type ReactNode } from 'react'
import { createPortal } from 'react-dom'
import Icon from '../components/Icon'
import { currentLanguage, t } from '../i18n'
import { useFocusTrap } from '../useFocusTrap'
import './news.css'

let aiOn: Promise<boolean> | null = null

// useAIOn is true once an administrator has set up AI.
export function useAIOn(): boolean {
  const [on, setOn] = useState(false)
  useEffect(() => {
    aiOn ??= fetch('/api/home/ai/on', { credentials: 'include' })
      .then((r) => (r.ok ? r.json() : { on: false }))
      .then((d: { on: boolean }) => d.on)
      .catch(() => false)
    let live = true
    void aiOn.then((v) => live && setOn(v))
    return () => {
      live = false
    }
  }, [])
  return on
}

// askAI posts to an AI endpoint with the page's language, and throws the
// server's message when it fails.
export async function askAI<T>(path: string, body: Record<string, unknown>): Promise<T> {
  const r = await fetch(path, {
    method: 'POST',
    credentials: 'include',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ ...body, lang: currentLanguage() }),
  })
  const d = await r.json().catch(() => null)
  if (!r.ok) throw new Error(d?.error ?? t('Something went wrong (error {status}).', { status: r.status }))
  return d as T
}

// useAsk runs an AI request when asked, with its busy and error state.
export function useAsk<T>() {
  const [data, setData] = useState<T | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const run = async (p: Promise<T>) => {
    setBusy(true)
    setError('')
    try {
      setData(await p)
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }
  return { data, busy, error, run, setData }
}

// AIDialog is the window an AI answer opens in.
export function AIDialog({ title, subtitle, onClose, children, wide }: { title: string; subtitle?: string; onClose: () => void; children: ReactNode; wide?: boolean }) {
  const box = useRef<HTMLDivElement>(null)
  useFocusTrap(box)
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && onClose()
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose])
  return createPortal(
    <div className="modal-backdrop" onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      <div ref={box} tabIndex={-1} className={`modal recap-dialog${wide ? ' ai-wide' : ''}`} role="dialog" aria-modal="true" aria-label={title}>
        <div className="recap-head">
          <Icon name="sparkles" size={20} />
          <div>
            <h2>{title}</h2>
            {subtitle && <p>{subtitle}</p>}
          </div>
          <button className="icon-btn" onClick={onClose} aria-label={t('Close')}>
            <Icon name="x" size={18} />
          </button>
        </div>
        <div className="ai-dialog-body">{children}</div>
      </div>
    </div>,
    document.body,
  )
}

// AIButton is the small sparkle button that opens an AI feature.
export function AIButton({ label, title, onClick, small }: { label: string; title?: string; onClick: () => void; small?: boolean }) {
  return (
    <button className={`${small ? 'btn-sm ' : ''}btn-with-icon`} onClick={onClick} title={title}>
      <Icon name="sparkles" size={small ? 14 : 16} /> {label}
    </button>
  )
}

// ScrollText keeps a long AI answer to a few lines that scroll (no bar),
// with a fade at the bottom while there's more to read.
export function ScrollText({ children, className = '' }: { children: ReactNode; className?: string }) {
  const el = useRef<HTMLDivElement>(null)
  const [more, setMore] = useState(false)
  useEffect(() => {
    const node = el.current
    if (!node) return
    const check = () => setMore(node.scrollHeight - node.scrollTop - node.clientHeight > 4)
    check()
    node.addEventListener('scroll', check, { passive: true })
    const ro = new ResizeObserver(check)
    ro.observe(node)
    return () => {
      node.removeEventListener('scroll', check)
      ro.disconnect()
    }
  }, [children])
  return (
    <div ref={el} className={`ai-scroll${more ? ' more' : ''} ${className}`}>
      {children}
    </div>
  )
}
