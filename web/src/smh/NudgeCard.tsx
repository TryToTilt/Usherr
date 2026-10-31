// Usherr SMH: a nudge on screen. Loaded a couple of minutes after the app
// opens, shown only when nothing else is going on (no walkthrough, no menu,
// nothing playing), bottom left, out of the coach card's way.
import { useEffect, useState } from 'react'
import { createPortal } from 'react-dom'
import { useLocation } from 'react-router-dom'
import { isAdmin } from '../api'
import { useAuth } from '../AuthContext'
import Icon from '../components/Icon'
import { t } from '../i18n'
import { startSmh, useSmh } from './engine'
import { nextNudge, nudgeState, saveNudgeState, type Nudge } from './nudges'
import './smh.css'

const WEEK = 7 * 24 * 60 * 60_000

// Pages where a tip would get in the way: watching, the TV screens.
const BUSY_PAGES = ['/showtime', '/marquee', '/tv', '/scenes']

const playing = () => [...document.querySelectorAll('video, audio')].some((m) => !(m as HTMLMediaElement).paused)

export default function NudgeCard() {
  const { user } = useAuth()
  const { run, menuOpen, doctorOpen } = useSmh()
  const location = useLocation()
  const [nudge, setNudge] = useState<Nudge | null>(null)
  const busy = !!run || menuOpen || doctorOpen || BUSY_PAGES.some((p) => location.pathname.startsWith(p))

  useEffect(() => {
    if (!user || nudge || busy) return
    let gone = false
    const id = window.setTimeout(() => {
      if (playing()) return
      void nextNudge(user.id, isAdmin(user)).then((n) => {
        if (gone || !n) return
        saveNudgeState(user.id, { ...nudgeState(user.id), lastShown: Date.now() })
        setNudge(n)
      })
    }, 1500)
    return () => {
      gone = true
      window.clearTimeout(id)
    }
  }, [user, nudge, busy])

  if (!user || !nudge || busy) return null
  const s = nudgeState(user.id)
  const snooze = (ms: number) => {
    saveNudgeState(user.id, { ...s, snooze: { ...s.snooze, [nudge.id]: Date.now() + ms } })
    setNudge(null)
  }
  const never = () => {
    saveNudgeState(user.id, { ...s, never: [...(s.never ?? []), nudge.id] })
    setNudge(null)
  }
  return createPortal(
    <aside className="smh-nudge card" role="status" aria-label={t('A tip from Show me how')}>
      <div className="smh-nudge-head">
        <Icon name="compass" size={15} />
        <span>{t('A tip')}</span>
        <button type="button" className="icon-btn smh-x" onClick={() => snooze(WEEK)} aria-label={t('Not now')} title={t('Not now')}>
          <Icon name="x" size={15} />
        </button>
      </div>
      <strong>{nudge.title}</strong>
      <p>{nudge.why}</p>
      <div className="smh-nav">
        <button
          type="button"
          className="btn-sm primary"
          onClick={() => {
            snooze(4 * WEEK)
            startSmh(nudge.walk, 'simple')
          }}
        >
          <Icon name="compass" size={14} /> {t('Show me how')}
        </button>
        <button type="button" className="btn-sm" onClick={() => snooze(WEEK)}>
          {t('Not now')}
        </button>
        <span className="spacer" />
        <button type="button" className="btn-link link-btn smh-never" onClick={never}>
          {t('Don’t remind me')}
        </button>
      </div>
    </aside>,
    document.body,
  )
}
