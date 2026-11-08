// Usherr: Community, step 5. The watch party's chat over the film: a new
// message slides in at a corner of the picture for a few seconds and fades.
// Read-only (people type on their phone), off until switched on with the
// Chat button on the player, and drawn inside the player's stage, so it shows
// in fullscreen too. Usherr: a linked screening's messages too, as plain
// text, a spoiler never shown here.
import { useEffect, useState } from 'react'
import { t } from '../i18n'
import { DiscordText } from './discordText'
import type { ChatMessage } from './PartyChat'
import { subscribeParty } from './partyStream'
import './partychat.css'

const SHOWN_MS = 9000 // each message stays this long
const MOST = 3 // and no more than this many at once

export default function PartyOverlay({ showtimeId }: { showtimeId: number }) {
  const [shown, setShown] = useState<(ChatMessage & { until: number })[]>([])
  useEffect(() => {
    const off = subscribeParty(showtimeId, (ev) => {
      if (ev.kind === 'react') return // floated by Reactions.tsx
      const m = ev.message as ChatMessage
      setShown((list) => {
        if (ev.kind === 'delete') return list.filter((x) => x.id !== m.id)
        if (ev.kind !== 'new' || list.some((x) => x.id === m.id)) return list
        return [...list, { ...m, until: Date.now() + SHOWN_MS }].slice(-MOST)
      })
    })
    const tick = window.setInterval(() => setShown((list) => (list.some((m) => m.until <= Date.now()) ? list.filter((m) => m.until > Date.now()) : list)), 1000)
    return () => {
      off()
      window.clearInterval(tick)
    }
  }, [showtimeId])
  if (shown.length === 0) return null
  return (
    <div className="po-stack" aria-live="polite">
      {shown.map((m) => (
        <div key={m.id} className={`po-msg${m.fromBot ? ' pc-bot' : ''}`}>
          {m.user.avatar ? <img className="pc-av" src={m.user.avatar} alt="" /> : <span className="pc-av pc-noav" aria-hidden="true">{m.user.name.slice(0, 1)}</span>}
          <div>
            <span className="pc-who">{m.user.name}</span>
            {m.text && (m.spoiler ? <p className="pc-text po-spoiler">{t('A spoiler: read it on your phone')}</p> : <p className="pc-text">{m.pos !== undefined ? m.text : <DiscordText text={m.text} />}</p>)}
            {(m.images ?? []).slice(0, 1).map((src) => (
              <img key={src} className="po-img" src={src} alt="" />
            ))}
          </div>
        </div>
      ))}
    </div>
  )
}
