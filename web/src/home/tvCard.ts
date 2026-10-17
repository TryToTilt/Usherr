// Usherr: the "Up next" card a channel shows between programs, drawn on a
// canvas and played in the player's own video (so full screen carries on).
import { locale, t } from '../i18n'

export interface CardInfo {
  channel: string
  next?: { title: string; subtitle?: string; start: string }
  seconds: number
}

const W = 1280
const H = 720

function fit(ctx: CanvasRenderingContext2D, text: string, max: number, size: number, weight = 700): number {
  let s = size
  ctx.font = `${weight} ${s}px system-ui, sans-serif`
  while (s > 20 && ctx.measureText(text).width > max) {
    s -= 2
    ctx.font = `${weight} ${s}px system-ui, sans-serif`
  }
  return s
}

// playCard draws the card into video until it has run for its seconds, then
// calls done. It returns a function that stops it early.
export function playCard(video: HTMLVideoElement, card: CardInfo, done: () => void): () => void {
  const canvas = document.createElement('canvas')
  canvas.width = W
  canvas.height = H
  const ctx = canvas.getContext('2d')
  if (!ctx || typeof canvas.captureStream !== 'function') {
    const timer = window.setTimeout(done, Math.max(1, card.seconds) * 1000)
    return () => window.clearTimeout(timer)
  }
  const accent = getComputedStyle(document.documentElement).getPropertyValue('--accent').trim() || '#e8b04b'
  const ends = Date.now() + Math.max(1, card.seconds) * 1000
  const at = card.next ? new Date(card.next.start).toLocaleTimeString(locale(), { hour: 'numeric', minute: '2-digit' }) : ''
  const draw = () => {
    const now = Date.now()
    const g = ctx.createLinearGradient(0, 0, W, H)
    g.addColorStop(0, '#0e1a33')
    g.addColorStop(1, '#241238')
    ctx.fillStyle = g
    ctx.fillRect(0, 0, W, H)
    // A slow sheen across the card.
    const x = ((now / 12) % (W * 2)) - W / 2
    const sheen = ctx.createLinearGradient(x - 200, 0, x + 200, H)
    sheen.addColorStop(0, 'rgba(255,255,255,0)')
    sheen.addColorStop(0.5, 'rgba(255,255,255,0.05)')
    sheen.addColorStop(1, 'rgba(255,255,255,0)')
    ctx.fillStyle = sheen
    ctx.fillRect(0, 0, W, H)
    // The channel along the top.
    ctx.fillStyle = 'rgba(0,0,0,0.35)'
    ctx.fillRect(0, 0, W, 110)
    ctx.fillStyle = '#f4e3b0'
    fit(ctx, card.channel, W - 300, 40)
    ctx.fillText(card.channel, 60, 70)
    const left = Math.max(0, Math.ceil((ends - now) / 1000))
    ctx.font = '500 32px ui-monospace, monospace'
    ctx.fillStyle = 'rgba(255,255,255,0.7)'
    const mm = `${Math.floor(left / 60)}:${String(left % 60).padStart(2, '0')}`
    ctx.fillText(mm, W - 60 - ctx.measureText(mm).width, 70)
    if (card.next) {
      ctx.fillStyle = accent
      ctx.font = '800 44px system-ui, sans-serif'
      ctx.fillText(t('UP NEXT'), 60, 290)
      ctx.fillStyle = '#ffffff'
      fit(ctx, card.next.title, W - 120, 80, 800)
      ctx.fillText(card.next.title, 60, 380)
      if (card.next.subtitle) {
        ctx.fillStyle = '#d8d8d8'
        fit(ctx, card.next.subtitle, W - 120, 38, 500)
        ctx.fillText(card.next.subtitle, 60, 440)
      }
      ctx.fillStyle = accent
      ctx.font = '700 40px system-ui, sans-serif'
      ctx.fillText(at, 60, 510)
    } else {
      ctx.fillStyle = '#ffffff'
      const s = fit(ctx, t("We'll be right back"), W - 120, 64, 800)
      const w = ctx.measureText(t("We'll be right back")).width
      ctx.fillText(t("We'll be right back"), (W - w) / 2, H / 2 + s / 3)
    }
  }
  draw()
  const timer = window.setInterval(draw, 200)
  video.srcObject = canvas.captureStream(10)
  void video.play().catch(() => undefined)
  const end = window.setTimeout(done, Math.max(1, card.seconds) * 1000)
  return () => {
    window.clearInterval(timer)
    window.clearTimeout(end)
    if (video.srcObject) video.srcObject = null
  }
}
