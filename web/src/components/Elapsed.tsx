// Home fork: a live countdown to the end of a download's current step,
// ticking every second: "12:05 left", "1:03:20 left". Until there is an
// estimate it says "estimating…"; past it, "almost done".
import { useEffect, useState } from 'react'
import { stageName } from '../home/stageName'
import { locale, t } from '../i18n'

export function formatClock(ms: number): string {
  const t = Math.max(0, Math.ceil(ms / 1000))
  const h = Math.floor(t / 3600)
  const m = Math.floor((t % 3600) / 60)
  const ss = String(t % 60).padStart(2, '0')
  return h > 0 ? `${h}:${String(m).padStart(2, '0')}:${ss}` : `${m}:${ss}`
}

export default function Countdown({ ends, prefix = '' }: { ends?: string; prefix?: string }) {
  const end = ends ? Date.parse(ends) : NaN
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    if (Number.isNaN(end)) return
    const t = window.setInterval(() => setNow(Date.now()), 1000)
    return () => window.clearInterval(t)
  }, [end])
  const text = Number.isNaN(end) ? t('estimating…') : end - now <= 0 ? t('almost done') : t('{time} left', { time: formatClock(end - now) })
  return (
    <span className="elapsed" title={Number.isNaN(end) ? t('Working out how long this step will take') : t('Expected by {time}', { time: new Date(end).toLocaleTimeString(locale()) })}>
      {prefix}
      {text}
    </span>
  )
}

// timeLeft is "12:05 left" for a step with an estimated end, else "".
export function timeLeft(ends?: string): string {
  const end = ends ? Date.parse(ends) : NaN
  if (Number.isNaN(end)) return ''
  const ms = end - Date.now()
  return ms <= 0 ? t('almost done') : t('{time} left', { time: formatClock(ms) })
}

// stepLabel is what a running download shows: its current step, or the
// download progress while it is still downloading.
export function stepLabel(q: { status: string; stage?: string; progressPct: number }, size: string): string {
  if (q.stage && q.stage !== 'Downloading' && q.stage !== 'Starting') return `${stageName(q.stage)}…`
  const pct = Math.floor(q.progressPct)
  const done = size ? t('{pct}% of {size}', { pct, size }) : `${pct}%`
  return q.stage === 'Starting' ? `${t('Starting')} · ${done}` : done
}

