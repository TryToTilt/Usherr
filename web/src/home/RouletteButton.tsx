// Home fork: the button that opens Roulette (the dialog loads only when
// pressed, so the pages it sits on stay light).
import { lazy, Suspense, useState } from 'react'
import { createPortal } from 'react-dom'
import Icon from '../components/Icon'
import { t } from '../i18n'

const Roulette = lazy(() => import('./Roulette'))

export default function RouletteButton({ className = 'btn-sm btn-with-icon' }: { className?: string }) {
  const [open, setOpen] = useState(false)
  return (
    <>
      <button className={className} onClick={() => setOpen(true)} title={t('Spin for something to watch')}>
        <Icon name="shuffle" size={14} /> {t('Roulette')}
      </button>
      {/* Drawn on the page's top level: a parent with a transform (a page's
          entrance animation) would otherwise hold the "fixed" dialog inside it. */}
      {open &&
        createPortal(
          <Suspense fallback={null}>
            <Roulette onClose={() => setOpen(false)} />
          </Suspense>,
          document.body,
        )}
    </>
  )
}
