// Usherr: Community, step 5. A watch party's chat: the Discord thread under
// its card, live. It sits on the Showtime page beside the countdown, and has
// a page of its own for a phone (/showtime/chat/:id) to chat on while the
// film plays on the TV. What you type is posted in the thread under your
// linked Discord name.
import { useEffect, useRef, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { useDocumentTitle } from '../documentTitle'
import { t } from '../i18n'
import { clockLabel } from './showtimes'
import { DiscordText } from './discordText'
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
}
interface ChatAnswer {
  showtime: { id: number; title: string; posterUrl?: string; atMs: number; state: string }
  messages: ChatMessage[] | null
  linked: boolean
  connected: boolean
  canPost: boolean
  discord: string
  warnings: string[] | null
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

export function PartyChat({ showtimeId, tall }: { showtimeId: number; tall?: boolean }) {
  const [a, setA] = useState<ChatAnswer | null>(null)
  const [msgs, setMsgs] = useState<ChatMessage[]>([])
  const [text, setText] = useState('')
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
    const es = new EventSource(`${base(showtimeId)}/events`, { withCredentials: true })
    es.onmessage = (e) => {
      const ev = JSON.parse(e.data as string) as { kind: string; message: ChatMessage }
      setMsgs((list) => (ev.kind === 'delete' ? list.filter((m) => m.id !== ev.message.id) : merge(list, ev.message)))
    }
    // After a lost connection, read it all again: nothing said meanwhile is missed.
    es.onopen = () => void load()
    return () => {
      live = false
      es.close()
    }
  }, [showtimeId])

  useEffect(() => {
    end.current?.scrollIntoView({ block: 'end' })
  }, [msgs.length])

  if (!a) return null

  const send = async () => {
    const say = text.trim()
    if (!say) return
    setBusy(true)
    setError('')
    try {
      const res = await fetch(base(showtimeId), { method: 'POST', credentials: 'include', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ text: say }) })
      const d = (await res.json().catch(() => null)) as { message?: ChatMessage; error?: string } | null
      if (!res.ok) throw new Error(d?.error ?? t('Something went wrong (error {status}).', { status: res.status }))
      if (d?.message) setMsgs((list) => merge(list, d.message as ChatMessage))
      setText('')
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  return (
    <section className={`card pc-chat${tall ? ' pc-tall' : ''}`} data-walk="party-chat">
      <div className="pc-head">
        <h2>{t('Watch party chat')}</h2>
        <span className="pc-links">
          {!tall && (
            <Link to={`/showtime/chat/${showtimeId}`} title={t('A page for your phone, to chat while the film plays on the TV')}>
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
                {m.user.name} <time>{clockLabel(new Date(m.at).getTime())}</time>
                {m.edited && <span className="pc-edited">{t('(edited)')}</span>}
              </span>
              {m.text && (
                <p className="pc-text">
                  <DiscordText text={m.text} />
                </p>
              )}
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
          <input value={text} maxLength={2000} onChange={(e) => setText(e.target.value)} placeholder={t('Say something…')} aria-label={t('Message')} enterKeyHint="send" />
          <button className="primary btn-sm" type="submit" disabled={busy || !text.trim()}>
            {t('Send')}
          </button>
        </form>
      ) : (
        <p className="hint">
          {!a.linked ? (
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
