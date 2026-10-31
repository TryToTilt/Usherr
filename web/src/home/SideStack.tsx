// Usherr: your Stack, in the sidebar under Settings. The films and shows you
// pulled out to watch next, as a stack of tickets: swipe (or use the arrows)
// to go through it, move one up or down, take it off, or play it. Tapping a
// poster offers its page or Play now. Titles go on it from the Stack button on
// any poster card and on title pages.
import { useEffect, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import Icon from '../components/Icon'
import PosterCard from '../components/PosterCard'
import { describeState } from '../components/state'
import { useToast } from '../components/Toast'
import { t } from '../i18n'
import { playTitle } from './Player'
import { moveInStack, removeFromStack, stackPath, useStack, type StackItem } from './stack'
import { ShowtimeDialog } from './ShowtimeButton'
import StreamButton from './StreamButton'
import { useStreamed } from './useHave'
import { useVotes } from './useVotes'
import { voteActions } from './VoteButtons'
import './stack.css'

const SWIPE = 40 // pixels a swipe must travel to change the card

export default function SideStack() {
  const stack = useStack()
  const [at, setAt] = useState(0)
  const [menu, setMenu] = useState(false)
  const [booking, setBooking] = useState(false) // Usherr: Showtimes at home
  const [drag, setDrag] = useState(0)
  const start = useRef<{ x: number; y: number } | null>(null)
  const dx = useRef(0)
  const navigate = useNavigate()
  const toast = useToast()
  const votes = useVotes()
  const movieStreamed = useStreamed('movie')
  const showStreamed = useStreamed('tv')
  const n = stack?.length ?? 0
  // The card shown stays in range as titles come and go.
  useEffect(() => {
    if (at > 0 && at >= n) setAt(Math.max(0, n - 1))
  }, [at, n])
  if (!stack) return null

  const fail = (e: unknown) => toast.error(e instanceof Error ? e.message : String(e))
  const go = (by: number) => {
    setMenu(false)
    if (n > 0) setAt((i) => (i + by + n) % n)
  }
  const it: StackItem | undefined = stack[Math.min(at, n - 1)]

  const playNow = (x: StackItem) => {
    setMenu(false)
    if (x.kind === 'movie' && x.downloaded && x.libraryId) playTitle('movie', x.libraryId, x.title)
    else navigate(stackPath(x)) // a show: pick the episode on its page
  }

  return (
    <section className="side-stack" data-walk="side-stack" aria-label={t('Your stack')}>
      <div className="ss-head">
        <Icon name="stack" size={15} />
        <span className="ss-title">{t('Your stack')}</span>
        {n > 0 && <span className="ss-count">{t('{at} of {n}', { at: Math.min(at, n - 1) + 1, n })}</span>}
      </div>

      {n === 0 || !it ? (
        <p className="ss-empty">
          <Icon name="stack" size={22} />
          {t('Put films and shows here to watch next: the stack button on any poster, or on a title’s page.')}
        </p>
      ) : (
        <>
          <div
            className="ss-deck"
            tabIndex={0}
            aria-roledescription={t('stack')}
            aria-label={t('{title}, {at} of {n}. Swipe or use the arrow keys.', { title: it.title, at: at + 1, n })}
            onKeyDown={(e) => {
              if (e.key === 'ArrowRight') go(1)
              else if (e.key === 'ArrowLeft') go(-1)
            }}
            onPointerDown={(e) => {
              start.current = { x: e.clientX, y: e.clientY }
              dx.current = 0
            }}
            onPointerMove={(e) => {
              if (!start.current) return
              const d = e.clientX - start.current.x
              if (Math.abs(d) > Math.abs(e.clientY - start.current.y) && Math.abs(d) > 4) {
                // Keep the swipe even when the finger leaves the card.
                if (!e.currentTarget.hasPointerCapture(e.pointerId)) e.currentTarget.setPointerCapture(e.pointerId)
                dx.current = d
                setDrag(d)
              }
            }}
            onPointerUp={() => {
              const d = dx.current
              start.current = null
              setDrag(0)
              if (d <= -SWIPE) go(1)
              else if (d >= SWIPE) go(-1)
            }}
            onPointerCancel={() => {
              start.current = null
              dx.current = 0
              setDrag(0)
            }}
            // A poster picture dragged off would end the swipe.
            onDragStart={(e) => e.preventDefault()}
            onClickCapture={(e) => {
              // A swipe isn't a tap on the poster.
              if (Math.abs(dx.current) > 6) {
                e.stopPropagation()
                e.preventDefault()
                dx.current = 0
              }
            }}
          >
            {[2, 1].map((d) => {
              const b = n > d ? stack[(at + d) % n] : undefined
              return b ? (
                <div key={d} className={`ss-behind b${d}`} aria-hidden="true">
                  {b.posterUrl ? <img src={b.posterUrl} alt="" loading="lazy" /> : <span>{b.title}</span>}
                </div>
              ) : null
            })}
            <div className="ss-front" key={`${it.kind}-${it.tmdbId}`} style={drag ? { transform: `translateX(${drag}px) rotate(${drag / 20}deg)` } : undefined}>
              <PosterCard
                to={stackPath(it)}
                onArt={() => setMenu((m) => !m)}
                poster={it.posterUrl}
                title={it.title}
                meta={it.year ? String(it.year) : undefined}
                kind={it.kind}
                state={it.downloaded ? describeState('downloaded') : (it.kind === 'movie' ? movieStreamed(it.tmdbId) : showStreamed(it.tmdbId)) ? describeState('streams') : undefined}
                actions={voteActions(votes, it.kind, it.tmdbId, { title: it.title, year: it.year, image: it.posterUrl })}
              />
            </div>
          </div>

          {menu && (
            <div className="ss-menu" role="menu">
              <button role="menuitem" className="btn-sm btn-with-icon" onClick={() => navigate(stackPath(it))}>
                <Icon name="open" size={14} /> {t('Its page')}
              </button>
              {it.kind === 'movie' && (
                <button role="menuitem" className="btn-sm btn-with-icon" onClick={() => setBooking(true)}>
                  <Icon name="clock" size={14} /> {t('Showtime')}
                </button>
              )}
              {it.kind === 'movie' && !it.downloaded ? (
                <StreamButton small target={{ kind: 'movie', tmdbId: it.tmdbId, title: it.title }} />
              ) : (
                <button role="menuitem" className="primary btn-sm btn-with-icon" onClick={() => playNow(it)}>
                  <Icon name="play" size={14} /> {it.kind === 'movie' ? t('Play now') : t('Pick an episode')}
                </button>
              )}
            </div>
          )}

          <div className="ss-nav">
            <button className="icon-btn" onClick={() => go(-1)} disabled={n < 2} aria-label={t('The one before')}>
              <Icon name="chevron-left" size={16} />
            </button>
            <div className="ss-tools">
              <button className="icon-btn" onClick={() => void moveInStack(at, -1).then(() => setAt((i) => Math.max(0, i - 1))).catch(fail)} disabled={at === 0} aria-label={t('Move it up the stack')} title={t('Move it up the stack')}>
                <Icon name="chevron-up" size={16} />
              </button>
              <button className="icon-btn" onClick={() => void moveInStack(at, 1).then(() => setAt((i) => Math.min(n - 1, i + 1))).catch(fail)} disabled={at >= n - 1} aria-label={t('Move it down the stack')} title={t('Move it down the stack')}>
                <Icon name="chevron-down" size={16} />
              </button>
              <button className="icon-btn" onClick={() => void removeFromStack(it.kind, it.tmdbId).catch(fail)} aria-label={t('Take it off the stack')} title={t('Take it off the stack')}>
                <Icon name="x" size={16} />
              </button>
              <button className="icon-btn ss-play" onClick={() => setMenu((m) => !m)} aria-label={t('Play {title}', { title: it.title })} title={t('Play')}>
                <Icon name="play" size={16} />
              </button>
            </div>
            <button className="icon-btn" onClick={() => go(1)} disabled={n < 2} aria-label={t('The next one')}>
              <Icon name="chevron-right" size={16} />
            </button>
          </div>
        </>
      )}
      {booking && it && <ShowtimeDialog film={{ tmdbId: it.tmdbId, title: it.title, year: it.year, posterUrl: it.posterUrl }} onClose={() => setBooking(false)} />}
    </section>
  )
}
