// Usherr: Community, step 6. "From the community" on a film's or show's
// page: the reviews posted in Discord (from Usherr, with /review, or as
// #review in a watch party's thread), and yours to write, change or remove.
// Yours goes to Discord under your linked Discord name.
import { useCallback, useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { useToast } from '../components/Toast'
import { t } from '../i18n'
import { DiscordText } from './discordText'
import Stars from './Stars'
import './partychat.css'

interface Review {
  name: string
  avatar?: string
  username?: string
  stars: number
  text: string
  source: 'usherr' | 'command' | 'typed'
  updatedMs: number
  link?: string
  mine?: boolean
}
interface Answer {
  reviews: Review[] | null
  mine: Review | null
  linked: boolean
  connected: boolean
}

export function postReview(kind: 'movie' | 'tv', tmdbId: number, stars: number, text: string) {
  return fetch('/api/home/reviews', { method: 'POST', credentials: 'include', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ kind, tmdbId, stars, text }) }).then(async (r) => {
    if (!r.ok) {
      const d = (await r.json().catch(() => null)) as { error?: string } | null
      throw new Error(d?.error ?? t('Something went wrong (error {status}).', { status: r.status }))
    }
  })
}

export default function CommunityReviews({ kind, tmdbId }: { kind: 'movie' | 'tv'; tmdbId: number }) {
  const toast = useToast()
  const [a, setA] = useState<Answer | null>(null)
  const [writing, setWriting] = useState(false)
  const [stars, setStars] = useState(0)
  const [text, setText] = useState('')
  const [busy, setBusy] = useState(false)
  const load = useCallback(() => {
    fetch(`/api/home/reviews?kind=${kind}&tmdbId=${tmdbId}`, { credentials: 'include', cache: 'no-store' })
      .then((r) => (r.ok ? (r.json() as Promise<Answer>) : null))
      .then((d) => d && setA(d))
      .catch(() => undefined)
  }, [kind, tmdbId])
  useEffect(load, [load])
  if (!a) return null
  const list = a.reviews ?? []
  const canWrite = a.linked && a.connected
  if (list.length === 0 && !canWrite) return null

  const open = () => {
    setStars(a.mine?.stars ?? 0)
    setText(a.mine?.text ?? '')
    setWriting(true)
  }
  const post = async () => {
    setBusy(true)
    try {
      await postReview(kind, tmdbId, stars, text)
      toast.success(t('Posted to Discord.'))
      setWriting(false)
      load()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }
  const remove = async () => {
    setBusy(true)
    try {
      await fetch(`/api/home/reviews?kind=${kind}&tmdbId=${tmdbId}`, { method: 'DELETE', credentials: 'include' })
      toast.success(t('Your review is removed, in Discord too.'))
      setWriting(false)
      load()
    } finally {
      setBusy(false)
    }
  }

  return (
    <section className="card cr-reviews" data-walk="community-reviews" style={{ marginBottom: 24 }}>
      <div className="pc-head">
        <h2>{t('From the community')}</h2>
        {canWrite && !writing && (
          <button className="btn-sm" onClick={open}>
            {a.mine ? t('Change your review') : t('Write a review')}
          </button>
        )}
      </div>
      {writing && (
        <div className="cr-write">
          <Stars value={stars} onChange={setStars} size={22} />
          <textarea value={text} onChange={(e) => setText(e.target.value)} rows={3} maxLength={1500} placeholder={t('What did you think? ||Like this|| hides a spoiler.')} />
          <div className="cr-btns">
            <button className="primary btn-sm" disabled={busy || (stars === 0 && !text.trim())} onClick={() => void post()}>
              {a.mine ? t('Update in Discord') : t('Post to Discord')}
            </button>
            {a.mine && (
              <button className="btn-sm" disabled={busy} onClick={() => void remove()}>
                {t('Remove it')}
              </button>
            )}
            <button className="btn-sm" onClick={() => setWriting(false)}>
              {t('Cancel')}
            </button>
          </div>
          <p className="hint">{t('One review each: posting again replaces yours. It goes to your Discord server under your Discord name.')}</p>
        </div>
      )}
      {list.length === 0 ? (
        <p className="hint">{t('No reviews yet. Be the first!')}</p>
      ) : (
        <div className="pc-list cr-list">
          {list.map((r) => (
            <div key={`${r.name}-${r.updatedMs}`} className="pc-msg">
              {r.avatar ? <img className="pc-av" src={r.avatar} alt="" /> : <span className="pc-av pc-noav" aria-hidden="true">{r.name.slice(0, 1)}</span>}
              <div>
                <span className="pc-who">
                  {r.name}
                  {r.stars > 0 && <span className="cr-stars"> {'★'.repeat(Math.floor(r.stars / 2))}{r.stars % 2 ? '½' : ''}</span>}
                  <time>{new Date(r.updatedMs).toLocaleDateString()}</time>
                  {r.link && (
                    <a className="cr-link" href={r.link} target="_blank" rel="noreferrer">
                      {t('In Discord')}
                    </a>
                  )}
                </span>
                {r.text && (
                  <p className="pc-text">
                    <DiscordText text={r.text} />
                  </p>
                )}
              </div>
            </div>
          ))}
        </div>
      )}
      {!a.linked && a.connected && (
        <p className="hint">
          {t('To write one, link your Discord account: ')}
          <Link to="/settings/services#discord">{t('Linked services')}</Link>
        </p>
      )}
    </section>
  )
}
