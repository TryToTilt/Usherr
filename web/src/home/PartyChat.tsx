// Usherr: Community, step 5. A watch party's chat: the Discord thread under
// its card, live. It sits on the Showtime page beside the countdown, and has
// a page of its own for a phone (/showtime/chat/:id) to chat on while the
// film plays on the TV. What you type is posted in the thread under your
// linked Discord name.
//
// Usherr: linked screenings, step 4. A linked screening's chat goes over
// the link instead (internal/api/home_link_chat.go): plain text only,
// spoilers blurred until tapped, each message pinned to the second of the
// film. Its phone page is the screening's companion (PartyPhone.tsx).
import { useEffect, useRef, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { useDocumentTitle } from '../documentTitle'
import { t } from '../i18n'
import { clockLabel } from './showtimes'
import { DiscordText } from './discordText'
import { subscribeParty } from './partyStream'
import { filmClock } from './reactionMath'
import PartyPhone from './PartyPhone'
import './partychat.css'

interface ChatUser {
  id: string
  name: string
  avatar?: string
}
export interface ChatMessage {
  id: string
  user: ChatUser
  text: string
  images?: string[] | null
  at: string
  edited?: boolean
  fromBot?: boolean
  spoiler?: boolean // Usherr: a linked screening's, blurred until tapped
  pos?: number // ... the second of the film it was said at
  house?: string // ... the Usherr it was said at
}
export interface ChatAnswer {
  showtime: { id: number; title: string; posterUrl?: string; atMs: number; state: string; runtime?: number }
  messages: ChatMessage[] | null
  linked: boolean
  connected: boolean
  canPost: boolean
  discord: string
  warnings: string[] | null
  link?: boolean // Usherr: a linked screening, chatting over the link
  role?: 'host' | 'guest'
  maxText?: number
}

const base = (id: number) => `/api/home/showtimes/${id}/chat`

// Keeps the messages in order, each once (a message sent from here comes
// back from Discord too).
function merge(list: ChatMessage[], m: ChatMessage): ChatMessage[] {
  const i = list.findIndex((x) => x.id === m.id)
  if (i >= 0) {
    const next = list.slice()
    next[i] = { ...list[i], text: m.text, images: m.images, edited: m.edited }
    return next
  }
  return [...list, m]
}

// When a message was said: in a linked screening, the second of the film
// (the clock before the film starts).
export function saidAt(m: ChatMessage): string {
  return m.pos !== undefined && m.pos >= 0 ? filmClock(m.pos) : clockLabel(new Date(m.at).getTime())
}

// ChatText is a message's words: a linked screening's as plain text (a
// spoiler hidden until tapped), a Discord one with Discord's markup.
export function ChatText({ m, link }: { m: ChatMessage; link?: boolean }) {
  const [shown, setShown] = useState(false)
  if (!m.text) return null
  if (m.spoiler && !shown) {
    return (
      <button type="button" className="pc-spoiler" onClick={() => setShown(true)}>
        {t('Spoiler, tap to show')}
      </button>
    )
  }
  return <p className="pc-text">{link ? m.text : <DiscordText text={m.text} />}</p>
}

export function PartyChat({ showtimeId, tall }: { showtimeId: number; tall?: boolean }) {
  const [a, setA] = useState<ChatAnswer | null>(null)
  const [msgs, setMsgs] = useState<ChatMessage[]>([])
  const [text, setText] = useState('')
  const [spoiler, setSpoiler] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const end = useRef<HTMLDivElement>(null)

  useEffect(() => {
    let live = true
    const load = () =>
      fetch(base(showtimeId), { credentials: 'include', cache: 'no-store' })
        .then((r) => (r.ok ? (r.json() as Promise<ChatAnswer>) : null))
        .then((d) => {
          if (!live || !d) return
          setA(d)
          setMsgs(d.messages ?? [])
        })
        .catch(() => undefined)
    void load()
    // After a lost connection, read it all again: nothing said meanwhile is missed.
    const off = subscribeParty(
      showtimeId,
      (ev) => {
        if (ev.kind === 'react') return // Usherr: a linked screening's reaction (Reactions.tsx)
        const m = ev.message as ChatMessage
        setMsgs((list) => (ev.kind === 'delete' ? list.filter((x) => x.id !== m.id) : merge(list, m)))
      },
      () => void load(),
    )
    return () => {
      live = false
      off()
    }
  }, [showtimeId])

  useEffect(() => {
    if (!tall || !a?.link) end.current?.scrollIntoView({ block: 'end' })
  }, [msgs.length, tall, a?.link])

  if (!a) return null

  // say sends a message (throws what went wrong).
  const say = async (words: string, spoilerToo: boolean) => {
    const res = await fetch(base(showtimeId), {
      method: 'POST',
      credentials: 'include',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(a.link ? { text: words, spoiler: spoilerToo } : { text: words }),
    })
    const d = (await res.json().catch(() => null)) as { message?: ChatMessage; error?: string } | null
    if (!res.ok) throw new Error(d?.error ?? t('Something went wrong (error {status}).', { status: res.status }))
    if (d?.message) setMsgs((list) => merge(list, d.message as ChatMessage))
  }

  if (tall && a.link) return <PartyPhone showtimeId={showtimeId} answer={a} msgs={msgs} say={say} />

  const send = async () => {
    const words = text.trim()
    if (!words) return
    setBusy(true)
    setError('')
    try {
      await say(words, spoiler)
      setText('')
      setSpoiler(false)
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  return (
    <section className={`card pc-chat${tall ? ' pc-tall' : ''}`} data-walk="party-chat">
      <div className="pc-head">
        <h2>{a.link ? t('Chat') : t('Watch party chat')}</h2>
        {a.link && <span className="pc-sub">{t('With everyone at the screening · spoilers blurred')}</span>}
        <span className="pc-links">
          {!tall && (
            <Link data-walk="party-phone-link" to={`/showtime/chat/${showtimeId}`} title={t('A page for your phone, to chat while the film plays on the TV')}>
              {t('On your phone')}
            </Link>
          )}
          {a.discord && (
            <a href={a.discord} target="_blank" rel="noreferrer">
              {t('Open in Discord')}
            </a>
          )}
        </span>
      </div>
      {(a.warnings ?? []).length > 0 && <p className="notice notice-warn">{t('The bot can’t read what people write (Message Content is off), so the chat shows only Usherr’s own messages. Settings, Movie night, Community says how to switch it on.')}</p>}
      <div className="pc-list" role="log" aria-live="polite">
        {msgs.length === 0 && <p className="hint">{t('Nothing said yet. Say hello!')}</p>}
        {msgs.map((m) => (
          <div key={m.id} className={`pc-msg${m.fromBot ? ' pc-bot' : ''}`}>
            {m.user.avatar ? <img className="pc-av" src={m.user.avatar} alt="" /> : <span className="pc-av pc-noav" aria-hidden="true">{m.user.name.slice(0, 1)}</span>}
            <div>
              <span className="pc-who">
                {m.user.name} <time>{saidAt(m)}</time>
                {m.house && <span className="pc-house">{m.house}</span>}
                {m.edited && <span className="pc-edited">{t('(edited)')}</span>}
              </span>
              <ChatText m={m} link={a.link} />
              {(m.images ?? []).map((src) => (
                <img key={src} className="pc-img" src={src} alt="" loading="lazy" />
              ))}
            </div>
          </div>
        ))}
        <div ref={end} />
      </div>
      {a.canPost ? (
        <form
          className="pc-say"
          onSubmit={(e) => {
            e.preventDefault()
            void send()
          }}
        >
          <input value={text} maxLength={a.link ? (a.maxText ?? 500) : 2000} onChange={(e) => setText(e.target.value)} placeholder={t('Say something…')} aria-label={t('Message')} enterKeyHint="send" />
          {a.link && (
            <label className="cx-check pc-spoil" title={t('Blurred until someone taps it')}>
              <input type="checkbox" checked={spoiler} onChange={(e) => setSpoiler(e.target.checked)} /> {t('Spoiler')}
            </label>
          )}
          <button className="primary btn-sm" type="submit" disabled={busy || !text.trim()}>
            {t('Send')}
          </button>
        </form>
      ) : (
        <p className="hint">
          {a.link ? (
            t('Switch on Link with other Usherrs to chat here.')
          ) : !a.linked ? (
            <>
              {t('To write here, link your Discord account: ')}
              <Link to="/settings/services#discord">{t('Linked services')}</Link>
            </>
          ) : (
            t('Community isn’t connected right now, so messages can’t be sent from here.')
          )}
        </p>
      )}
      {error && <p className="notice notice-warn">{error}</p>}
    </section>
  )
}

// PartyChatPage is the chat on its own, for a phone.
export default function PartyChatPage() {
  const { id } = useParams()
  const n = Number(id)
  useDocumentTitle(t('Watch party chat'))
  if (!Number.isFinite(n) || n <= 0) return <p className="hint">{t('That watch party isn’t known.')}</p>
  return (
    <div className="page pc-page">
      <PartyChat showtimeId={n} tall />
      <p className="hint pc-back">
        <Link to="/showtime">{t('The Showtime page')}</Link>
      </p>
    </div>
  )
}
