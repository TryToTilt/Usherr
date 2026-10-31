// Home fork: the button that opens Rate 20 (loaded only when pressed).
import { lazy, Suspense, useState } from 'react'
import { createPortal } from 'react-dom'
import Icon from '../components/Icon'
import { t } from '../i18n'

const RateDeck = lazy(() => import('./RateDeck'))

export default function RateButton({ className = 'btn-sm btn-with-icon' }: { className?: string }) {
  const [open, setOpen] = useState(false)
  return (
    <>
      <button className={className} data-walk="rate20" onClick={() => setOpen(true)} title={t('Thumb up or down 20 titles, so recommendations learn your taste')}>
        <Icon name="thumb-up" size={14} /> {t('Rate 20')}
      </button>
      {/* On the page's top level, like Roulette: a transformed parent would hold the dialog. */}
      {open &&
        createPortal(
          <Suspense fallback={null}>
            <RateDeck onClose={() => setOpen(false)} />
          </Suspense>,
          document.body,
        )}
    </>
  )
}
