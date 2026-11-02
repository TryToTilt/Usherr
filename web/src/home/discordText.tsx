// Usherr: Community. Discord's own marks in a message, drawn the way Discord
// draws them: **bold**, *italic*, `code`, ||spoilers|| (hidden until tapped,
// which matters in a film's chat), its times (<t:1700000000:t>, shown in this
// reader's time zone), and mentions and custom emoji as plain words. Nothing
// is ever put in as HTML.
import { useState, type ReactNode } from 'react'
import { locale } from '../i18n'

function Spoiler({ children }: { children: ReactNode }) {
  const [shown, setShown] = useState(false)
  return (
    <span className={`dt-spoiler${shown ? ' shown' : ''}`} role="button" tabIndex={0} onClick={() => setShown(true)} onKeyDown={(e) => e.key === 'Enter' && setShown(true)} aria-label={shown ? undefined : 'Spoiler'}>
      {children}
    </span>
  )
}

function discordTime(sec: number, style: string): string {
  const d = new Date(sec * 1000)
  const loc = locale()
  switch (style) {
    case 't':
      return d.toLocaleTimeString(loc, { hour: 'numeric', minute: '2-digit' })
    case 'T':
      return d.toLocaleTimeString(loc)
    case 'd':
      return d.toLocaleDateString(loc)
    case 'D':
      return d.toLocaleDateString(loc, { dateStyle: 'long' })
    case 'R': {
      const mins = Math.round((sec * 1000 - Date.now()) / 60_000)
      const rtf = new Intl.RelativeTimeFormat(loc, { numeric: 'auto' })
      return Math.abs(mins) < 60 ? rtf.format(mins, 'minute') : Math.abs(mins) < 1440 ? rtf.format(Math.round(mins / 60), 'hour') : rtf.format(Math.round(mins / 1440), 'day')
    }
    default:
      return d.toLocaleString(loc, { dateStyle: 'full', timeStyle: 'short' })
  }
}

// One token at a time, the first that matches.
const TOKEN = /(\|\|[\s\S]+?\|\||\*\*[\s\S]+?\*\*|`[^`]+`|\*[^*\s][^*]*\*|_[^_\s][^_]*_|<t:(-?\d+)(?::([tTdDfFR]))?>|<a?:(\w+):\d+>|<@!?\d+>|<@&\d+>|<#\d+>)/

export function DiscordText({ text }: { text: string }): ReactNode {
  const out: ReactNode[] = []
  let rest = text
  let key = 0
  while (rest) {
    const m = TOKEN.exec(rest)
    if (!m) {
      out.push(rest)
      break
    }
    if (m.index > 0) out.push(rest.slice(0, m.index))
    const tok = m[0]
    if (tok.startsWith('||')) out.push(<Spoiler key={key++}>{<DiscordText text={tok.slice(2, -2)} />}</Spoiler>)
    else if (tok.startsWith('**')) out.push(<strong key={key++}>{<DiscordText text={tok.slice(2, -2)} />}</strong>)
    else if (tok.startsWith('`')) out.push(<code key={key++}>{tok.slice(1, -1)}</code>)
    else if (tok.startsWith('*') || tok.startsWith('_')) out.push(<em key={key++}>{<DiscordText text={tok.slice(1, -1)} />}</em>)
    else if (m[2] !== undefined) out.push(<time key={key++}>{discordTime(Number(m[2]), m[3] ?? 'f')}</time>)
    else if (m[4] !== undefined) out.push(`:${m[4]}:`)
    else if (tok.startsWith('<#')) out.push('#channel')
    else out.push('@someone')
    rest = rest.slice(m.index + tok.length)
  }
  return <>{out}</>
}
