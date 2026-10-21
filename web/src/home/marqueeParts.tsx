// Usherr: parts the Marquee's looks share: a sign line that fits, a scene
// that reads itself out when tall, the stage that crosses one slide into the
// next, posters and their frames.
import { useEffect, useLayoutEffect, useRef, useState, type CSSProperties, type ReactNode } from 'react'
import type { MarqueeTransition } from './marqueeData'

// FitLine keeps a line of the sign on one line, shrinking its type until it
// fits the sign's width.
export function FitLine({ text }: { text: string }) {
  const ref = useRef<HTMLSpanElement>(null)
  useLayoutEffect(() => {
    const el = ref.current
    const box = el?.parentElement
    if (!el || !box) return
    const fit = () => {
      el.style.fontSize = ''
      const room = box.clientWidth - 2
      let k = 1
      for (let i = 0; i < 40 && el.scrollWidth > room && k > 0.2; i++) {
        k *= Math.max(0.6, Math.min(0.97, room / el.scrollWidth))
        el.style.fontSize = `calc(var(--mq-sign) * ${k.toFixed(3)})`
      }
    }
    fit()
    const ro = new ResizeObserver(fit)
    ro.observe(box)
    void document.fonts?.ready.then(fit)
    return () => ro.disconnect()
  }, [text])
  return (
    <span ref={ref} className="mq-line">
      {text}
    </span>
  )
}


// SceneFrame shows a scene for its time. When the scene is taller than the
// screen it reads it out slowly: a pause at the top, a slow glide to the
// bottom, a pause, back up, and only then the next scene.
const HOLD_TOP_MS = 4000
const HOLD_BOTTOM_MS = 3000
export function SceneFrame({ n, minMs, still, speed, onDone, children }: { n: number; minMs: number; still?: boolean; speed: number; onDone: (n: number) => void; children: ReactNode }) {
  const box = useRef<HTMLDivElement>(null)
  useEffect(() => {
    const el = box.current
    let raf = 0
    const timers: number[] = []
    const later = (ms: number, f: () => void) => timers.push(window.setTimeout(f, ms))
    const glide = (from: number, to: number, ms: number, then: () => void) => {
      const start = performance.now()
      const step = (now: number) => {
        const k = Math.min(1, (now - start) / ms)
        const ease = k < 0.5 ? 2 * k * k : 1 - Math.pow(-2 * k + 2, 2) / 2
        if (el) el.scrollTop = from + (to - from) * ease
        if (k < 1) raf = requestAnimationFrame(step)
        else then()
      }
      raf = requestAnimationFrame(step)
    }
    const begun = performance.now()
    const finish = () => later(Math.max(0, minMs - (performance.now() - begun)), () => onDone(n))
    later(HOLD_TOP_MS, () => {
      // Measured now, once the posters have loaded.
      const over = el ? el.scrollHeight - el.clientHeight : 0
      if (still || !el || over < 8) {
        finish()
        return
      }
      const down = (over / speed) * 1000
      glide(0, over, down, () => later(HOLD_BOTTOM_MS, () => glide(over, 0, down / 1.5, () => later(1500, finish))))
    })
    return () => {
      cancelAnimationFrame(raf)
      timers.forEach((t) => window.clearTimeout(t))
    }
  }, [n, minMs, still, speed, onDone])
  return (
    <div ref={box} className="mq-scroll">
      {children}
    </div>
  )
}


// Stage shows its child, and when the key changes it keeps the one before for
// the length of the transition so the two can cross: fade, slide, flip...
const RANDOM_TRANSITIONS: Exclude<MarqueeTransition, 'random' | 'cut'>[] = ['fade', 'slide', 'push', 'zoom', 'flip', 'rotate', 'blur']
export function Stage({ k, type, ms, children }: { k: number | string; type: MarqueeTransition; ms: number; children: ReactNode }) {
  const prev = useRef<{ k: number | string; node: ReactNode } | null>(null)
  const [old, setOld] = useState<{ k: number | string; node: ReactNode; type: MarqueeTransition } | null>(null)
  // Before the new slide is painted: keep the one before, to cross out.
  useLayoutEffect(() => {
    const p = prev.current
    prev.current = { k, node: children }
    if (!p || p.k === k) return
    if (type === 'cut' || ms <= 0) setOld(null)
    else setOld({ k: p.k, node: p.node, type: type === 'random' ? RANDOM_TRANSITIONS[Math.floor(Math.random() * RANDOM_TRANSITIONS.length)] : type })
  })
  useEffect(() => {
    if (!old) return
    const id = window.setTimeout(() => setOld((o) => (o === old ? null : o)), ms + 50)
    return () => window.clearTimeout(id)
  }, [old, ms])
  const crossing = old && old.k !== k ? old : null
  return (
    <div className={`mqx-stage mqx-${crossing?.type ?? type}`} style={{ '--mqx-ms': `${ms}ms` } as CSSProperties}>
      {crossing && (
        <div key={`o${crossing.k}`} className="mqx-layer out" aria-hidden="true">
          {crossing.node}
        </div>
      )}
      <div key={`n${k}`} className={`mqx-layer${crossing ? ' in' : ''}`}>
        {children}
      </div>
    </div>
  )
}

// Sign is a look's heading: one or two lines, each kept on one line.
export function Sign({ lines, className = '' }: { lines: string[]; className?: string }) {
  if (!lines.length) return null
  return (
    <h1 className={`lk-sign${lines.length > 1 ? ' two' : ''} ${className}`} aria-live="polite">
      {lines.map((line, i) => (
        <FitLine key={i} text={line} />
      ))}
    </h1>
  )
}

// Pic is a poster, framed as set: filled, letterboxed or on a mat; with the
// slow pan and zoom when it is on.
export function Pic({ src, alt, framing, kb, seconds, blank }: { src?: string; alt?: string; framing: string; kb?: number; seconds?: number; blank?: string }) {
  const style = seconds ? ({ '--kb-s': `${seconds + 4}s` } as CSSProperties) : undefined
  return (
    <div className={`lk-pic fr-${framing}`}>
      {src ? <img className={kb !== undefined ? `kb kb-${kb % 4}` : undefined} style={style} src={src} alt={alt ?? ''} /> : <div className="lk-pic-blank">{blank}</div>}
    </div>
  )
}

// Frame puts a poster in bulbs, a gold line or neon.
export function Frame({ kind, children }: { kind: string; children: ReactNode }) {
  return (
    <div className={`lk-frame fr-${kind}`}>
      {kind === 'bulbs' && (
        <>
          <span className="bl t" aria-hidden="true" />
          <span className="bl r" aria-hidden="true" />
          <span className="bl b" aria-hidden="true" />
          <span className="bl l" aria-hidden="true" />
        </>
      )}
      {children}
    </div>
  )
}
