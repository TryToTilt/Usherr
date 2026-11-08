// Usherr: linked screenings, step 4. The screening on your phone
// (/showtime/chat/:id, for a linked screening): what's playing and where
// it's got to, the chat (spoilers blurred until tapped), the five reactions,
// and for the host a remote: pause or play for everyone, an intermission,
// back 10 seconds for everyone. Guests see no host controls.
import { useEffect, useRef, useState } from 'react'
import Icon from '../components/Icon'
import { t } from '../i18n'
import { ChatText, saidAt, type ChatAnswer, type ChatMessage } from './PartyChat'
import { filmClock, type Reaction } from './reactionMath'
import { HeatStrip, ReactButtons, useReactions } from './Reactions'
import { useRoomLink } from './roomLink'
import { HostPartyButtons, Intermission, Verdict } from './Party' // Usherr: the party: intermission, verdict
import { useParty } from './partyLink'
import { inLabel } from './showtimes'

const errText = (e: unknown) => (e instanceof Error ? e.message : String(e))

type Item = { key: string; at: number; m?: ChatMessage; r?: Reaction }

export default function PartyPhone({ showtimeId, answer: a, msgs, say }: { showtimeId: number; answer: ChatAnswer; msgs: ChatMessage[]; say: (text: string, spoiler: boolean) => Promise<void> }) {
  const { link, room } = useRoomLink(showtimeId)
  const { list, runtime } = useReactions({ showtimeId })
  const { link: pl, party } = useParty(showtimeId)
  const [text, setText] = useState('')
  const [spoiler, setSpoiler] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [asked, setAsked] = useState(false)
  const end = useRef<HTMLDivElement>(null)

  const at = link?.at() ?? null
  const state = at?.state
  const pos = at?.position ?? 0
  const playing = state === 'film'
  const length = Math.max(runtime, (room?.runtime ?? a.showtime.runtime ?? 0) * 60)

  // What was said and sent, in the order it happened.
  const items: Item[] = [
    ...msgs.map((m) => ({ key: `m${m.id}`, at: new Date(m.at).getTime(), m })),
    ...list.filter((r) => r.atMs).map((r) => ({ key: `r${r.id}`, at: r.atMs ?? 0, r })),
  ]
    .sort((x, y) => x.at - y.at)
    .slice(-150)

  useEffect(() => {
    end.current?.scrollIntoView({ block: 'end' })
  }, [items.length])

  const run = (p: Promise<unknown> | undefined) => {
    setError('')
    p?.catch((e: unknown) => setError(errText(e)))
  }
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
      setError(errText(e))
    } finally {
      setBusy(false)
    }
  }

  const host = room ? (room.hostName ? t('{name} ({usherr})', { name: room.hostName, usherr: room.host }) : room.host) : ''
  const where =
    state === 'waiting'
      ? t('Starts {when}', { when: inLabel(room?.atMs ?? a.showtime.atMs, link?.serverNow() ?? Date.now()) })
      : state === 'preshow'
        ? t('The pre-show is on')
        : state === 'paused'
          ? room?.canControl
            ? t('Paused for everyone')
            : t('Host paused')
          : state === 'ended'
            ? t('That’s the end. Thanks for coming!')
            : t('Playing, in step with everyone')
  const canControl = !!room?.canControl && state !== 'ended' && state !== 'waiting'

  return (
    <section className="card pp-phone" data-walk="party-phone">
      <header className="pp-head">
        <div className="pp-film">
          {a.showtime.posterUrl ? <img className="pp-poster" src={a.showtime.posterUrl} alt="" /> : <span className="pp-poster" aria-hidden="true" />}
          <div className="pp-title">
            <strong>{a.showtime.title}</strong>
            <span>{where}</span>
            {room?.role === 'guest' && <span>{t('Hosted by {host}', { host })}</span>}
          </div>
          {(state === 'film' || state === 'paused') && (
            <time className="pp-clock" aria-label={t('Where the film is')}>
              {filmClock(pos)}
            </time>
          )}
        </div>
        <HeatStrip list={list} length={length} position={state === 'film' || state === 'paused' ? () => link?.at()?.position ?? 0 : undefined} />
        {canControl && (
          <div className="pp-remote" role="group" aria-label={t('Host controls')}>
            <button type="button" className="btn-with-icon pp-main" onClick={() => run(link?.control(playing ? 'paused' : 'film', pos))}>
              <Icon name={playing ? 'pause' : 'play'} size={16} /> {playing ? t('Pause for everyone') : t('Play for everyone')}
            </button>
            <HostPartyButtons showtimeId={showtimeId} className="pp-inter" />
            <button type="button" className="pp-back" onClick={() => run(link?.control(playing ? 'film' : 'paused', Math.max(0, pos - 10)))} aria-label={t('Back 10 seconds, for everyone')} title={t('Back 10 seconds, for everyone')}>
              <Icon name="skip-prev" size={18} />
            </button>
          </div>
        )}
        {!room?.canControl && room?.canPause && playing && (
          <button
            type="button"
            className="btn-sm btn-with-icon pp-ask"
            disabled={asked}
            onClick={() => {
              setAsked(true)
              window.setTimeout(() => setAsked(false), 5000)
              run(link?.pause())
            }}
          >
            <Icon name="pause" size={14} /> {asked ? t('Asked') : t('Ask to pause')}
          </button>
        )}
      </header>
      {pl && party?.intermission.on && <Intermission link={pl} party={party} pausedAt={state === 'paused' ? pos : undefined} compact />}
      {pl && party?.verdict.open && !party.intermission.on && <Verdict key={party.showtimeId} link={pl} party={party} title={a.showtime.title} compact />}
      <div className="pp-list" role="log" aria-live="polite">
        {items.length === 0 && <p className="hint">{t('Nothing said yet. Say hello!')}</p>}
        {items.map((it) =>
          it.r ? (
            <p key={it.key} className="pp-react">
              <b className={`rx-${it.r.word}`}>{it.r.name}</b> {t('reacted {word} at {time}', { word: it.r.word, time: filmClock(it.r.pos) })}
            </p>
          ) : it.m ? (
            <div key={it.key} className="pc-msg">
              <span className="pc-av pc-noav" aria-hidden="true">
                {it.m.user.name.slice(0, 1)}
              </span>
              <div>
                <span className="pc-who">
                  {it.m.user.name} <time>{saidAt(it.m)}</time>
                </span>
                <ChatText m={it.m} link />
              </div>
            </div>
          ) : null,
        )}
        <div ref={end} />
      </div>
      <ReactButtons showtimeId={showtimeId} big />
      {a.canPost ? (
        <form
          className="pc-say pp-say"
          onSubmit={(e) => {
            e.preventDefault()
            void send()
          }}
        >
          <input value={text} maxLength={a.maxText ?? 500} onChange={(e) => setText(e.target.value)} placeholder={t('Message the crew')} aria-label={t('Message')} enterKeyHint="send" />
          <button className="primary" type="submit" disabled={busy || !text.trim()}>
            {t('Send')}
          </button>
          <label className="cx-check pc-spoil" title={t('Blurred until someone taps it')}>
            <input type="checkbox" checked={spoiler} onChange={(e) => setSpoiler(e.target.checked)} /> {t('Spoiler')}
          </label>
        </form>
      ) : (
        <p className="hint">{t('Switch on Link with other Usherrs to chat here.')}</p>
      )}
      {(error || link?.error) && <p className="notice notice-warn">{error || link?.error}</p>}
    </section>
  )
}
