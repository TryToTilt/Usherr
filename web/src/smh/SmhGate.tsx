// Usherr: Show Me How's place in the app: the top bar's button, and the menu
// and coach card, which load only when they're used.
import { lazy, Suspense, useEffect } from 'react'
import { useAuth } from '../AuthContext'
import Icon from '../components/Icon'
import { t } from '../i18n'
import { openSmhMenu, smhUser, useSmh } from './engine'
import './smh.css'

const Coach = lazy(() => import('./Coach'))
const SmhMenu = lazy(() => import('./SmhMenu'))

export default function SmhGate() {
  const { user } = useAuth()
  const { run, menuOpen } = useSmh()
  useEffect(() => smhUser(user?.id ?? null), [user])
  return (
    <Suspense fallback={null}>
      {run && !run.paused && <Coach run={run} />}
      {menuOpen && <SmhMenu />}
    </Suspense>
  )
}

// The top bar's "Show me how" button (the compass alone on a phone). A dot
// says a walkthrough is waiting to carry on.
export function SmhButton() {
  const { run } = useSmh()
  return (
    <button type="button" className="icon-btn top-smh" data-walk="smh-button" onClick={openSmhMenu} title={t('Show me how: pick what you want to do, and Usherr walks you through it')} aria-label={t('Show me how')}>
      <Icon name="compass" size={18} />
      <span className="top-smh-label">{t('Show me how')}</span>
      {run?.paused && <span className="top-smh-dot" aria-label={t('A walkthrough is waiting')} />}
    </button>
  )
}
