// Usherr: Crew Wrapped, a crew's year in review (Crews page, "<year>
// Wrapped"). Five slides stepped with dots and Next: you showed up, the
// crew's favourite, the moment, who picked best and next year. Each slide can
// be saved as a picture, drawn in the browser: Usherr never posts it
// anywhere.
import { useEffect, useRef, useState } from 'react'
import { BRAND } from '../brand'
import { locale, plural, t } from '../i18n'
import { crewsApi, type CrewWrapped as Data } from './crewsApi'
import { download } from './stubPicture'

interface Slide {
  title: string
  big: string
  line: string
  chips: string[]
  note?: string
}

// starsOutOfFive: an average out of 10 as "4.5★".
const starsOutOfFive = (s?: number) => (s ? `${(Math.round(s * 5) / 10).toLocaleString(locale(), { minimumFractionDigits: 1, maximumFractionDigits: 1 })}★` : '')

// filmClock: seconds into a film as "1:12:40".
function filmClock(secs: number): string {
  const s = Math.max(0, Math.floor(secs))
  const h = Math.floor(s / 3600)
  const m = Math.floor((s % 3600) / 60)
  const ss = String(s % 60).padStart(2, '0')
  return h ? `${h}:${String(m).padStart(2, '0')}:${ss}` : `${m}:${ss}`
}

// weekdayName: 0 = Sunday.
const weekdayName = (d: number) => new Date(2023, 0, 1 + d).toLocaleDateString(locale(), { weekday: 'long' })

function slidesOf(d: Data): Slide[] {
  const hours = Math.max(1, Math.round(d.minutes / 60))
  const days = d.weekdays
    .map((n, i) => ({ n, i }))
    .filter((x) => x.n > 0)
    .sort((a, b) => b.n - a.n)
    .slice(0, 2)
    .map((x) => t('{day}: {n}', { day: weekdayName(x.i), n: x.n }))
  const showed: Slide = {
    title: t('You showed up'),
    big: String(d.parties),
    line: [
      plural(d.parties, 'party this year with {crew}.', 'parties this year with {crew}.', { crew: d.crew }),
      d.about ? plural(hours, 'About one hour side by side.', 'About {n} hours side by side.') : plural(hours, 'One hour side by side.', '{n} hours side by side.'),
      d.houses > 1 ? t('From {n} living rooms.', { n: d.houses }) : '',
    ]
      .filter(Boolean)
      .join(' '),
    chips: [...days, ...(d.streak > 1 ? [t('Longest club streak: {n}', { n: d.streak })] : [])],
  }
  const fav: Slide = d.favourite
    ? {
        title: t('The crew’s favourite'),
        big: starsOutOfFive(d.favourite.stars),
        line: t('{title} topped the crew’s stars.', { title: d.favourite.title }),
        chips: [
          ...(d.divisive ? [t('Most divisive: {title}', { title: d.divisive.title })] : []),
          ...(d.lowest ? [t('Lowest: {title}', { title: d.lowest.title })] : []),
        ],
        note: d.starsFrom === 'crew' ? plural(d.ratings, 'From one rating the crew shared.', 'From {n} ratings the crew shared.') : plural(d.ratings, 'From one rating shared in this house only.', 'From {n} ratings shared in this house only.'),
      }
    : {
        title: t('The crew’s favourite'),
        big: '★',
        line: t('No stars shared for this year’s films yet.'),
        chips: [],
        note: t('Stars count when people turn on Share what I watch.'),
      }
  const extra = [
    ...(d.mostHa ? [t('Most HAs: {title}', { title: d.mostHa.title })] : []),
    ...(d.mostWow ? [t('Most WOWs: {title}', { title: d.mostWow.title })] : []),
  ]
  const moment: Slide = d.moment
    ? {
        title: t('The moment'),
        big: filmClock(d.moment.pos),
        line: t('{title}: {word} ×{n}, the crew’s biggest moment of the year.', { title: d.moment.title, word: d.moment.word, n: d.moment.count }),
        chips: extra,
      }
    : { title: t('The moment'), big: '…', line: t('Nobody reacted during a party this year.'), chips: extra }
  const top = d.pickers[0]
  const others = d.pickers.slice(1, 4).map((p) => plural(p.wins, '{who}: one win', '{who}: {n} wins', { who: p.you ? t('You') : p.who }))
  const picked: Slide = top
    ? {
        title: t('Who picked best'),
        big: top.you ? t('You') : top.who,
        line: top.you ? plural(top.wins, 'Your pick won the vote once.', 'Your picks won the vote {n} times.') : plural(top.wins, 'Their pick won the vote once.', 'Their picks won the vote {n} times.'),
        chips: others,
      }
    : { title: t('Who picked best'), big: '…', line: t('No vote had a winner this year.'), chips: [] }
  const next: Slide = {
    title: t('Next year'),
    big: String(d.nextYear),
    line: d.clubs.length ? t('{club} keeps its slot. Here’s to more nights together.', { club: d.clubs[0] }) : t('Here’s to more nights together.'),
    chips: [t('Save each slide as a picture'), t('Start again in January')],
  }
  return [showed, fav, moment, picked, next]
}

// wrapText splits text into lines that fit max.
function wrapText(ctx: CanvasRenderingContext2D, text: string, max: number): string[] {
  const lines: string[] = []
  let line = ''
  for (const word of text.split(' ')) {
    const tryLine = line ? `${line} ${word}` : word
    if (line && ctx.measureText(tryLine).width > max) {
      lines.push(line)
      line = word
    } else line = tryLine
  }
  if (line) lines.push(line)
  return lines
}

// slidePicture draws a slide; from is the slide on the page (its colours and
// fonts are used).
async function slidePicture(s: Slide, label: string, from: HTMLElement): Promise<Blob | null> {
  const css = getComputedStyle(from)
  const v = (name: string, fallback: string) => css.getPropertyValue(name).trim() || fallback
  const bg = v('--cw-bg', '#22192a')
  const ink = v('--text', '#e8eaf2')
  const dim = v('--cw-dim', '#cdb8dd')
  const big = v('--cw-big', '#f2b84b')
  const chip = v('--cw-chip', '#2f2238')
  const display = v('--font-display', 'sans-serif')
  const body = v('--font-body', 'sans-serif')
  const W = 1200
  const H = 1200
  const canvas = document.createElement('canvas')
  canvas.width = W
  canvas.height = H
  const ctx = canvas.getContext('2d')
  if (!ctx) return null
  ctx.fillStyle = bg
  ctx.beginPath()
  ctx.roundRect(0, 0, W, H, 40)
  ctx.fill()
  const left = 90
  const room = W - left * 2
  ctx.fillStyle = dim
  ctx.font = `600 30px ${body}`
  ctx.fillText(label.toUpperCase(), left, 140)
  ctx.fillStyle = ink
  ctx.font = `700 64px ${display}`
  ctx.fillText(s.title, left, 230)
  let size = 260
  ctx.font = `700 ${size}px ${display}`
  while (size > 80 && ctx.measureText(s.big).width > room) {
    size -= 10
    ctx.font = `700 ${size}px ${display}`
  }
  ctx.fillStyle = big
  ctx.fillText(s.big, left, 300 + size * 0.8)
  let y = 300 + size * 0.8 + 100
  ctx.fillStyle = ink
  ctx.font = `500 44px ${body}`
  for (const l of wrapText(ctx, s.line, room).slice(0, 4)) {
    ctx.fillText(l, left, y)
    y += 60
  }
  y += 30
  let x = left
  ctx.font = `500 32px ${body}`
  for (const c of s.chips) {
    const w = ctx.measureText(c).width + 48
    if (x + w > W - left) {
      x = left
      y += 76
    }
    ctx.fillStyle = chip
    ctx.beginPath()
    ctx.roundRect(x, y - 44, w, 62, 24)
    ctx.fill()
    ctx.fillStyle = ink
    ctx.fillText(c, x + 24, y)
    x += w + 16
  }
  ctx.fillStyle = dim
  ctx.font = `500 28px ${body}`
  if (s.note) ctx.fillText(s.note, left, H - 140)
  ctx.textAlign = 'right'
  ctx.font = `700 32px ${display}`
  ctx.fillText(BRAND, W - left, H - 80)
  return new Promise((resolve) => canvas.toBlob(resolve, 'image/png'))
}

export default function CrewWrapped({ crewId, year: first, onClose }: { crewId: number; year: number; onClose: () => void }) {
  const [year, setYear] = useState(first)
  const [data, setData] = useState<Data | null>(null)
  const [error, setError] = useState('')
  const [at, setAt] = useState(0)
  const [saving, setSaving] = useState(false)
  const box = useRef<HTMLElement>(null)
  useEffect(() => {
    let live = true
    setData(null)
    setError('')
    setAt(0)
    crewsApi
      .wrapped(crewId, year || undefined)
      .then((d) => live && setData(d))
      .catch((e: unknown) => live && setError(e instanceof Error ? e.message : String(e)))
    return () => {
      live = false
    }
  }, [crewId, year])
  const label = data ? t('{crew} · {year} Wrapped', { crew: data.crew, year: data.year }) : ''
  const years = data ? Array.from(new Set([...data.years, data.year])).sort((a, b) => b - a) : []
  const head = (
    <div className="cw-top">
      <div className="cw-label">{label}</div>
      {years.length > 1 && (
        <label className="cw-years">
          <span className="sr-only">{t('Year')}</span>
          <select value={data?.year} onChange={(e) => setYear(Number(e.target.value))}>
            {years.map((y) => (
              <option key={y} value={y}>
                {y}
              </option>
            ))}
          </select>
        </label>
      )}
      <button type="button" className="btn-sm link-btn cw-close" onClick={onClose}>
        {t('Close')}
      </button>
    </div>
  )
  if (error)
    return (
      <section className="crw-wrapped" ref={box}>
        {head}
        <p className="notice notice-warn">{error}</p>
      </section>
    )
  if (!data)
    return (
      <section className="crw-wrapped" ref={box}>
        <p className="hint">{t('Adding up the crew’s year…')}</p>
      </section>
    )
  if (data.empty)
    return (
      <section className="crw-wrapped" ref={box} aria-label={label}>
        {head}
        <div className="cw-body">
          <h2>{t('Not enough parties yet')}</h2>
          <p className="cw-line">{t('Wrapped fills up once the crew has watched a film together that year. Start a vote or a film club.')}</p>
        </div>
      </section>
    )
  const slides = slidesOf(data)
  const s = slides[Math.min(at, slides.length - 1)]
  const save = async () => {
    if (!box.current) return
    setSaving(true)
    try {
      const blob = await slidePicture(s, label, box.current)
      if (blob) download(blob, `${BRAND.toLowerCase()}-wrapped-${data.year}-${at + 1}.png`)
    } finally {
      setSaving(false)
    }
  }
  return (
    <section className="crw-wrapped" ref={box} aria-label={label}>
      {head}
      <h2 className="cw-title">{s.title}</h2>
      <div className="cw-body" aria-live="polite">
        <div className="cw-big">{s.big}</div>
        <p className="cw-line">{s.line}</p>
        {s.chips.length > 0 && (
          <div className="cw-chips">
            {s.chips.map((c) => (
              <span key={c} className="cw-chip">
                {c}
              </span>
            ))}
          </div>
        )}
        {s.note && <p className="cw-note">{s.note}</p>}
      </div>
      <div className="cw-foot">
        {slides.map((x, i) => (
          <button key={x.title} type="button" className={`cw-dot${i === at ? ' on' : ''}`} aria-label={t('Slide {n}: {title}', { n: i + 1, title: x.title })} aria-current={i === at ? 'step' : undefined} onClick={() => setAt(i)} />
        ))}
        <button type="button" className="btn-sm cw-save" disabled={saving} onClick={() => void save()}>
          {t('Save as picture')}
        </button>
        <button type="button" className="primary cw-next" onClick={() => setAt((at + 1) % slides.length)}>
          {at === slides.length - 1 ? t('Back to the start') : t('Next')}
        </button>
      </div>
    </section>
  )
}
