// Usherr: send a film to a friend, with a few words of your own. It goes as
// a film card in a crew you share (crewsApi.send): their Usherr tells them,
// and the card says whether they have it, with Get it when they don't.
import { useEffect, useMemo, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { Link } from 'react-router-dom'
import Icon from '../components/Icon'
import { useToast } from '../components/Toast'
import { t } from '../i18n'
import { useFocusTrap } from '../useFocusTrap'
import { crewsApi, type CrewList } from './crewsApi'
import './crews.css'

export const NOTE_MAX = 280 // internal/link/crew: Card.Note

interface Friend {
  crew: number
  crewName: string
  h: number
  who: string
  house: string
}

// friendsOf: everyone in a crew you've joined, once each (the first crew they share with you).
function friendsOf(list: CrewList | null): Friend[] {
  const seen = new Set<string>()
  const out: Friend[] = []
  for (const c of list?.crews ?? []) {
    if (!c.joined || c.state !== 'joined') continue
    for (const h of c.houses) {
      if (h.me || h.state !== 'joined') continue
      for (const who of h.people.length > 0 ? h.people : [h.name]) {
        const key = `${h.name}|${who}`
        if (seen.has(key)) continue
        seen.add(key)
        out.push({ crew: c.id, crewName: c.name, h: h.h, who, house: h.name })
      }
    }
  }
  return out
}

export default function SendToFriend({ film, onClose }: { film: { tmdbId: number; title: string; year?: number; posterUrl?: string }; onClose: () => void }) {
  const toast = useToast()
  const box = useRef<HTMLDivElement>(null)
  useFocusTrap(box)
  const [list, setList] = useState<CrewList | null>(null)
  const [error, setError] = useState('')
  const [pick, setPick] = useState(0)
  const [note, setNote] = useState('')
  const [sending, setSending] = useState(false)
  useEffect(() => {
    crewsApi
      .list()
      .then(setList)
      .catch((e: unknown) => setError(e instanceof Error ? e.message : String(e)))
  }, [])
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape' && !sending) onClose()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose, sending])
  const friends = useMemo(() => friendsOf(list), [list])
  const to = friends[pick]
  const send = () => {
    if (!to) return
    setSending(true)
    crewsApi
      .send(to.crew, { tmdbId: film.tmdbId, title: film.title, year: film.year, poster: film.posterUrl }, to.h, to.who, note.trim())
      .then(() => {
        toast.success(t('{title} is sent to {who}.', { title: film.title, who: to.who }))
        onClose()
      })
      .catch((e: unknown) => {
        toast.error(e instanceof Error ? e.message : String(e))
        setSending(false)
      })
  }
  return createPortal(
    <div className="modal-backdrop" onMouseDown={(e) => e.target === e.currentTarget && !sending && onClose()}>
      <div ref={box} tabIndex={-1} className="modal send-friend" role="dialog" aria-modal="true" aria-label={t('Send {title} to a friend', { title: film.title })}>
        <div className="share-head">
          <span className="share-ico">
            <Icon name="share" size={20} />
          </span>
          <div>
            <h2>{t('Send {title} to a friend', { title: film.title })}</h2>
            <p>{t('It shows up in your crew’s chat with your note, and their Usherr lets them know.')}</p>
          </div>
        </div>
        {error && <p className="notice notice-warn">{error}</p>}
        {!list && !error && <p className="hint">{t('Loading…')}</p>}
        {list && friends.length === 0 && (
          <p className="hint">
            {t('Friends are people on linked Usherrs who are in a crew with you.')} <Link to="/crews" onClick={onClose}>{t('Make a crew')}</Link>
          </p>
        )}
        {friends.length > 0 && (
          <>
            <label className="send-friend-field">
              {t('To')}
              <select value={pick} onChange={(e) => setPick(Number(e.target.value))} data-autofocus="">
                {friends.map((f, i) => (
                  <option key={`${f.house}|${f.who}`} value={i}>
                    {f.who} · {f.house}
                  </option>
                ))}
              </select>
            </label>
            <label className="send-friend-field">
              {t('Say something about it (if you like)')}
              <textarea
                value={note}
                maxLength={NOTE_MAX}
                rows={3}
                placeholder={t('Hey, this is a cool movie, you should check it out!')}
                onChange={(e) => setNote(e.target.value)}
              />
              <span className="send-friend-count">{t('{n} of {max}', { n: note.length, max: NOTE_MAX })}</span>
            </label>
          </>
        )}
        <div className="send-friend-actions">
          <button type="button" className="btn-sm" onClick={onClose} disabled={sending}>
            {t('Cancel')}
          </button>
          {friends.length > 0 && (
            <button type="button" className="primary btn-sm" onClick={send} disabled={sending || !to}>
              {sending ? t('Sending…') : t('Send to {who}', { who: to?.who ?? '' })}
            </button>
          )}
        </div>
      </div>
    </div>,
    document.body,
  )
}
