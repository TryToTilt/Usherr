import { Suspense, useRef } from 'react'
import { useFoldCards } from './useFoldCards' // Usherr
import { Outlet, useLocation } from 'react-router-dom'
import { isAdmin } from '../../api'
import { useAuth } from '../../AuthContext'
import TipLabel from '../../components/TipLabel'
import { t } from '../../i18n'
import { pageBlurb, pageHelp, pageParts } from '../../settingsNav'
import '../../settings.css'

// Holds the open settings page. Pages that share a sidebar entry are listed
// under it in the sidebar, and the top bar names the group and the page. On
// phones the top bar has no room for the name, so it is repeated here.
export default function SettingsLayout() {
  const { pathname } = useLocation()
  const { user } = useAuth()
  const admin = isAdmin(user)
  const parts = pageParts(pathname, admin)
  const blurb = pageBlurb(pathname, admin)
  const help = pageHelp(pathname, admin)
  // Usherr: cards fold to their title row on pages with several of them.
  const box = useRef<HTMLDivElement>(null)
  const fold = useFoldCards(box, pathname)
  return (
    <div className="settings-content" ref={box}>
      <h2 className="settings-phone-title">
        {parts.group && <small>{parts.group}</small>}
        {parts.page}
      </h2>
      {blurb && (
        <p className="settings-blurb">
          {help ? <TipLabel help={help} label={t('Open the user guide for this page')}>{blurb}</TipLabel> : blurb}
        </p>
      )}
      {fold.count > 0 && (
        <p className="fold-all">
          <button type="button" className="link-btn" onClick={fold.openAll}>
            {t('Open all')}
          </button>
          {' · '}
          <button type="button" className="link-btn" onClick={fold.foldAll}>
            {t('Fold all')}
          </button>
        </p>
      )}
      <Suspense fallback={null}>
        <Outlet />
      </Suspense>
    </div>
  )
}
