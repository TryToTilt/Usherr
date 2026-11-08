// Usherr: Community, step 7. You → Ticket stubs: one for each watch party you
// went to, handed out "at the door" when its film started, and stamped if
// you stayed to the end: with Stamp it (from the film's start until half an
// hour after its end), by Usherr's player at 90%, or with the button in the
// party's thread. A friend's party joined with its showtime code gets a stub
// here too, kept by this Usherr (home_community_guest.go); a linked Usherr's
// screening hands its stubs out over the link, numbered by the host, and a
// stamp here is confirmed by the host (home_link_rewards.go).
// Rewards: each stub is a collectible (the poster, the seat, its number, a
// round SEEN stamp with your stars), saved as a picture in the browser
// (stubPicture.ts); the badge cabinet sits above (Badges.tsx).
// Remove takes a stub off (asked twice); it's kept hidden, so Undo brings
// it back.
import { lazy, Suspense, useCallback, useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { useConfirm } from '../components/ConfirmProvider'
import { BRAND } from '../brand'
import { locale, plural, st, t } from '../i18n'
import { download, seatLabel, starsShort, stubNo, stubPicture } from './stubPicture'
import './stubs.css'

const Badges = lazy(() => import('./Badges')) // Usherr: rewards, the badge cabinet

interface Stub {
  showtimeId: number
  tmdbId: number
  title: string
  year?: number
  poster?: string
  host: string
  seat: number // 0: the host's seat
  guests: string[] | null
  atMs: number
  stamped: boolean
  here?: boolean // kept by this Usherr (not the bot)
  stampUntil?: number // can be stamped by hand until then
  no: number // the stub's number
  stars?: number // your stars, 1 to 10
  linked?: string // the linked Usherr that handed it out
  agreed?: boolean // ... which confirmed the stamp
  hosted?: boolean // you hosted it
}

export default function Stubs() {
  const [d, setD] = useState<{ stubs: Stub[] | null; linked: boolean; nowMs?: number } | null>(null)
  const [error, setError] = useState('')
  const [arm, setArm] = useState('') // the stub whose Remove was pressed once
  const [saving, setSaving] = useState('')
  const [removed, setRemoved] = useState<{ key: string; title: string } | null>(null)
  const confirm = useConfirm()
  const load = useCallback(() => {
    fetch('/api/home/stubs', { credentials: 'include', cache: 'no-store' })
      .then((r) => (r.ok ? r.json() : null))
      .then((x) => x && setD(x as { stubs: Stub[] | null; linked: boolean; nowMs?: number }))
      .catch(() => undefined)
  }, [])
  useEffect(load, [load])
  const keyOf = (s: Stub) => `${s.here ? 'here' : 'discord'}/${s.showtimeId}`
  async function stamp(s: Stub) {
    setError('')
    const r = await fetch(`/api/home/stubs/${keyOf(s)}/stamp`, { method: 'POST', credentials: 'include' }).catch(() => null)
    if (!r) setError(t('Couldn’t stamp it.'))
    else if (!r.ok) setError(st(((await r.json().catch(() => null)) as { error?: string } | null)?.error) || t('Couldn’t stamp it.'))
    load()
  }
  async function save(s: Stub, el: HTMLElement | null) {
    if (!el) return
    setError('')
    setSaving(keyOf(s))
    const blob = await stubPicture(
      {
        title: s.title,
        year: s.year,
        host: s.host,
        hostedByYou: !!s.hosted,
        atMs: s.atMs,
        seatLabel: seatLabel(s.seat),
        no: stubNo(s.no),
        stamped: s.stamped,
        stars: starsShort(s.stars),
        posterUrl: `/api/home/stubs/${keyOf(s)}/poster`,
      },
      el,
    ).catch(() => null)
    setSaving('')
    if (!blob) {
      setError(t('Couldn’t make the picture.'))
      return
    }
    const slug = s.title.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-|-$/g, '') || 'film'
    download(blob, `${BRAND.toLowerCase()}-stub-${slug}.png`)
  }
  async function remove(s: Stub) {
    setArm('')
    const ok = await confirm({
      title: t('Remove your {title} stub?', { title: s.title }),
      body: t('It goes from your ticket stubs here and from /stubs in Discord. You can bring it back with Undo straight after.'),
      confirmLabel: t('Remove it'),
      cancelLabel: t('Keep it'),
      danger: true,
    })
    if (!ok) return
    setError('')
    const r = await fetch(`/api/home/stubs/${keyOf(s)}`, { method: 'DELETE', credentials: 'include' }).catch(() => null)
    if (!r || !r.ok) {
      setError(st(((await r?.json().catch(() => null)) as { error?: string } | null)?.error) || t('Couldn’t remove it.'))
      return
    }
    setRemoved({ key: keyOf(s), title: s.title })
    load()
  }
  async function undo() {
    if (!removed) return
    const r = await fetch(`/api/home/stubs/${removed.key}/restore`, { method: 'POST', credentials: 'include' }).catch(() => null)
    if (r && r.ok) setRemoved(null)
    else setError(t('Couldn’t bring it back.'))
    load()
  }
  if (!d) return null
  const list = d.stubs ?? []
  const now = d.nowMs ?? 0 // the server's clock when the list was read
  const cabinet = (
    <Suspense fallback={null}>
      <Badges />
    </Suspense>
  )
  const undoBar = removed && (
    <p className="notice stub-undo" role="status">
      {t('Removed the {title} stub.', { title: removed.title })}{' '}
      <button type="button" className="btn-sm" onClick={() => void undo()}>
        {t('Undo')}
      </button>
    </p>
  )
  if (list.length === 0 && removed)
    return (
      <div className="stub-page">
        {cabinet}
        {undoBar}
      </div>
    )
  if (list.length === 0)
    return (
      <>
        {cabinet}
        <div className="hint stub-none">
          <p>{t('No ticket stubs yet. You get one at the door of each watch party you go to, and it’s stamped if you stay to the end.')}</p>
          <p>{t('A friend’s party: paste its showtime code under Showtime, Join a watch party, and your stub shows here when the film starts. A linked Usherr’s screening hands you yours over the link. The stubs a friend’s Discord bot keeps for you are under /stubs in their server.')}</p>
          {!d.linked && (
            <p>
              {t('Parties from this Usherr’s own Discord server need your Discord account linked: ')}
              <Link to="/settings/services#discord">{t('Linked services')}</Link>
            </p>
          )}
        </div>
      </>
    )
  const stamped = list.filter((s) => s.stamped).length
  return (
    <div className="stub-page" data-walk="ticket-stubs">
      {cabinet}
      <h2 className="stub-head">{t('Ticket stubs')}</h2>
      <p className="hint">
        {plural(list.length, 'One ticket stub', '{n} ticket stubs')} · {t('{n} stamped (stayed to the end)', { n: stamped })}
      </p>
      {error && <p className="error-text">{error}</p>}
      {undoBar}
      <ul className="stub-list">
        {list.map((s) => {
          const when = new Date(s.atMs)
          const canStamp = !s.stamped && !!s.stampUntil && now >= s.atMs
          const sub = s.hosted ? t('Hosted by you') : s.host ? t('Hosted by {host}', { host: s.host }) : t('A friend’s watch party')
          return (
            <li key={keyOf(s)} className="stub-item">
              <div className={`stub${s.stamped ? ' stub-stamped' : ''}`}>
                <Link to={`/title/${s.tmdbId}`} className="stub-poster" tabIndex={-1} aria-hidden="true">
                  {s.poster ? <img src={s.poster} alt="" loading="lazy" /> : <span />}
                </Link>
                <div className="stub-body">
                  <span className="stub-admit">{t('{brand} Showtime · admit one', { brand: BRAND })}</span>
                  <Link to={`/title/${s.tmdbId}`} className="stub-title">
                    {s.title}
                  </Link>
                  <span className="stub-who">
                    {s.year ? `${s.year} · ` : ''}
                    {sub}
                    {s.linked ? ` · ${t('at {usherr}', { usherr: s.linked })}` : ''}
                  </span>
                  {(s.guests ?? []).length > 0 && <span className="stub-with">{t('With {names}', { names: (s.guests ?? []).join(', ') })}</span>}
                  <span className="stub-facts">
                    <span>{when.toLocaleDateString(locale(), { weekday: 'short', day: 'numeric', month: 'short', year: 'numeric' })}</span>
                    <span>{when.toLocaleTimeString(locale(), { hour: 'numeric', minute: '2-digit' })}</span>
                    <span>{seatLabel(s.seat)}</span>
                  </span>
                </div>
                <div className="stub-tear" aria-hidden="true" />
                <div className="stub-end">
                  <span className="stub-no">{stubNo(s.no)}</span>
                  {s.stamped && (
                    <span
                      className="stub-seen"
                      role="img"
                      aria-label={s.stars ? t('Seen, {stars}', { stars: starsShort(s.stars) }) : t('Seen')}
                      title={s.linked && s.agreed ? t('Stamped, and confirmed by {usherr}', { usherr: s.linked }) : t('Stayed to the end')}
                    >
                      {t('SEEN')}
                      {s.stars ? <small>{starsShort(s.stars)}</small> : null}
                    </span>
                  )}
                  {canStamp && (
                    <button type="button" className="stub-stamp-btn" onClick={() => void stamp(s)} title={t('Stayed to the end? Stamp it until half an hour after the film ends.')}>
                      {t('Stamp it')}
                    </button>
                  )}
                </div>
              </div>
              <div className="stub-actions">
                <button type="button" className="stub-quiet-btn" disabled={saving === keyOf(s)} onClick={(e) => void save(s, e.currentTarget.closest('.stub-item')?.querySelector<HTMLElement>('.stub') ?? null)}>
                  {saving === keyOf(s) ? t('Making the picture…') : t('Save as picture')}
                </button>
                {arm === keyOf(s) ? (
                  <span className="stub-remove">
                    <button type="button" className="btn-sm danger" onClick={() => void remove(s)}>
                      {t('Remove?')}
                    </button>
                    <button type="button" className="btn-sm" onClick={() => setArm('')}>
                      {t('Keep')}
                    </button>
                  </span>
                ) : (
                  <button type="button" className="stub-quiet-btn" onClick={() => setArm(keyOf(s))} aria-label={t('Remove the {title} stub', { title: s.title })}>
                    {t('Remove')}
                  </button>
                )}
              </div>
            </li>
          )
        })}
      </ul>
    </div>
  )
}
