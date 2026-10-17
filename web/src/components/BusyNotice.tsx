import { useBusyNotice } from '../useBusyNotice'
import Icon from './Icon'
import { t } from '../i18n'

// A toast in the bottom right corner (inside the toast stack, see Toast.tsx)
// while Mediarium is slow to answer: a page has been waiting a good while, or
// the app said it is busy. It floats over the page, so nothing moves, and it
// shows only once the problem has lasted a few seconds. It stays while the
// problem lasts and goes away by itself when the app answers again.
export default function BusyNotice() {
  if (!useBusyNotice()) return null
  return (
    <div className="toast toast-warn">
      <span className="toast-icon" aria-hidden="true">
        <Icon name="warning" size={14} />
      </span>
      <span className="toast-text">
        <strong>{t('Mediarium is slow to answer right now.')}</strong> {t('It may be busy with a download or import.')}
      </span>
      <button type="button" className="btn-sm toast-action" onClick={() => window.location.reload()}>
        {t('Try again')}
      </button>
    </div>
  )
}
