// Usherr: rewards. "Save as picture": a ticket stub drawn into a PNG in the
// browser and downloaded. Nothing leaves this computer: Usherr never posts
// it anywhere. The colours and fonts are read from the stub on the page, so
// the picture looks like it.
import { BRAND } from '../brand'
import { locale, t } from '../i18n'

// seatLabel: eight seats a row, the host's seat first ("Row A · Seat 1").
export function seatLabel(seat: number): string {
  const row = String.fromCharCode(65 + (Math.floor(Math.max(seat, 0) / 8) % 26))
  return t('Row {row} · Seat {n}', { row, n: (Math.max(seat, 0) % 8) + 1 })
}

// starsShort: stars 1 to 10 (half stars out of five) as "4½★".
export function starsShort(stars?: number): string {
  if (!stars || stars < 1) return ''
  const s = Math.min(stars, 10)
  return `${Math.floor(s / 2) || ''}${s % 2 ? '½' : ''}★`
}

// stubNo: "No. 0047".
export const stubNo = (n: number) => t('No. {n}', { n: String(Math.max(n, 0)).padStart(4, '0') })

export interface StubPictureData {
  title: string
  year?: number
  host: string
  hostedByYou: boolean
  atMs: number
  seatLabel: string
  no: string
  stamped: boolean
  stars: string // "4½★", or ''
  posterUrl: string // same origin, so the canvas can be saved
}

const W = 1200
const H = 520

function loadImage(src: string): Promise<HTMLImageElement | null> {
  return new Promise((resolve) => {
    if (!src) return resolve(null)
    const img = new Image()
    img.onload = () => resolve(img)
    img.onerror = () => resolve(null)
    img.src = src
  })
}

function fit(ctx: CanvasRenderingContext2D, text: string, max: number, size: number, family: string, weight: number): number {
  let s = size
  for (; s > 26; s -= 2) {
    ctx.font = `${weight} ${s}px ${family}`
    if (ctx.measureText(text).width <= max) break
  }
  return s
}

function cut(ctx: CanvasRenderingContext2D, text: string, max: number): string {
  if (ctx.measureText(text).width <= max) return text
  let s = text
  while (s.length > 1 && ctx.measureText(s + '…').width > max) s = s.slice(0, -1)
  return s + '…'
}

// stubPicture draws the stub; from is the stub element on the page (its
// colours and fonts are used).
export async function stubPicture(d: StubPictureData, from: HTMLElement): Promise<Blob | null> {
  const css = getComputedStyle(from)
  const v = (name: string, fallback: string) => css.getPropertyValue(name).trim() || fallback
  const paper = v('--stub-paper', '#f3ead8')
  const paper2 = v('--stub-paper-2', paper)
  const ink = v('--stub-ink', '#2a2116')
  const soft = v('--stub-soft', '#6b5a3d')
  const line = v('--stub-line', '#c9b894')
  const red = v('--stub-red', '#8a1e2e')
  const display = v('--font-display', 'sans-serif')
  const body = v('--font-body', 'sans-serif')
  const mono = v('--font-mono', 'monospace')

  const canvas = document.createElement('canvas')
  canvas.width = W
  canvas.height = H
  const ctx = canvas.getContext('2d')
  if (!ctx) return null

  // The paper, with a notch each side of the tear.
  const tear = W - 300
  const grad = ctx.createLinearGradient(0, 0, W, H)
  grad.addColorStop(0, paper)
  grad.addColorStop(1, paper2)
  ctx.fillStyle = grad
  ctx.beginPath()
  ctx.roundRect(0, 0, W, H, 28)
  ctx.fill()
  ctx.globalCompositeOperation = 'destination-out'
  for (const y of [0, H]) {
    ctx.beginPath()
    ctx.arc(tear, y, 22, 0, Math.PI * 2)
    ctx.fill()
  }
  ctx.globalCompositeOperation = 'source-over'

  // The poster.
  const posterW = 300
  const img = await loadImage(d.posterUrl)
  ctx.save()
  ctx.beginPath()
  ctx.roundRect(0, 0, posterW, H, [28, 0, 0, 28])
  ctx.clip()
  ctx.fillStyle = ink
  ctx.fillRect(0, 0, posterW, H)
  if (img) {
    const scale = Math.max(posterW / img.width, H / img.height)
    const w = img.width * scale
    const h = img.height * scale
    ctx.drawImage(img, (posterW - w) / 2, (H - h) / 2, w, h)
  }
  ctx.restore()

  // The words.
  const left = posterW + 48
  const room = tear - left - 40
  ctx.fillStyle = red
  ctx.font = `700 22px ${body}`
  ctx.fillText(t('{brand} Showtime · admit one', { brand: BRAND }).toUpperCase(), left, 72)
  const title = d.title.toUpperCase()
  const size = fit(ctx, title, room, 64, display, 700)
  ctx.fillStyle = ink
  ctx.font = `700 ${size}px ${display}`
  ctx.fillText(cut(ctx, title, room), left, 72 + 30 + size)
  ctx.fillStyle = soft
  ctx.font = `500 28px ${body}`
  const sub = [d.year ? String(d.year) : '', d.hostedByYou ? t('Hosted by you') : d.host ? t('Hosted by {host}', { host: d.host }) : ''].filter(Boolean).join(' · ')
  ctx.fillText(cut(ctx, sub, room), left, 72 + 30 + size + 50)
  const when = new Date(d.atMs)
  const date = when.toLocaleDateString(locale(), { weekday: 'short', day: 'numeric', month: 'short', year: 'numeric' }).toUpperCase()
  const time = when.toLocaleTimeString(locale(), { hour: 'numeric', minute: '2-digit' }).toUpperCase()
  ctx.fillStyle = ink
  ctx.font = `600 26px ${mono}`
  ctx.fillText(cut(ctx, `${date}   ${time}   ${d.seatLabel.toUpperCase()}`, room), left, H - 56)

  // The tear.
  ctx.strokeStyle = line
  ctx.lineWidth = 4
  ctx.setLineDash([14, 12])
  ctx.beginPath()
  ctx.moveTo(tear, 34)
  ctx.lineTo(tear, H - 34)
  ctx.stroke()
  ctx.setLineDash([])

  // The end: the number, and the stamp.
  const cx = tear + (W - tear) / 2
  ctx.textAlign = 'center'
  ctx.fillStyle = soft
  ctx.font = `600 26px ${mono}`
  ctx.fillText(d.no, cx, 110)
  if (d.stamped) {
    ctx.save()
    ctx.translate(cx, H / 2 + 50)
    ctx.rotate((-14 * Math.PI) / 180)
    ctx.strokeStyle = red
    ctx.lineWidth = 7
    ctx.beginPath()
    ctx.arc(0, 0, 100, 0, Math.PI * 2)
    ctx.stroke()
    ctx.lineWidth = 2
    ctx.beginPath()
    ctx.arc(0, 0, 86, 0, Math.PI * 2)
    ctx.stroke()
    ctx.fillStyle = red
    ctx.font = `800 46px ${display}`
    ctx.fillText(t('SEEN'), 0, d.stars ? -2 : 16)
    if (d.stars) {
      ctx.font = `700 34px ${display}`
      ctx.fillText(d.stars, 0, 44)
    }
    ctx.restore()
  }
  return new Promise((resolve) => canvas.toBlob(resolve, 'image/png'))
}

// download saves a picture under a name.
export function download(blob: Blob, name: string) {
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = name
  document.body.appendChild(a)
  a.click()
  a.remove()
  setTimeout(() => URL.revokeObjectURL(url), 10_000)
}
