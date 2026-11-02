// Usherr: what the heart button says before it opens Ryan's Ko-fi page.
// Usherr is built on Mediarium, so the support goes to its developer; then a
// word about sharing Usherr and sending feedback (the bug button does that).
import { createPortal } from 'react-dom'
import { useEffect, useRef } from 'react'
import { useFocusTrap } from '../useFocusTrap'
import Icon from '../components/Icon'
import { copyText } from '../components/CopyBox'
import { useToast } from '../components/Toast'
import { REPO_URL, UPSTREAM } from '../brand'
import { t } from '../i18n'
import './support.css'

export const KOFI_URL = 'https://ko-fi.com/ryanborg'

// Mediarium's name goes in as {upstream}: t() turns the word Mediarium into
// Usherr (brand.ts), and here it really means Ryan's project.
const up = { upstream: UPSTREAM }

export default function SupportDialog({ onClose }: { onClose: () => void }) {
  const box = useRef<HTMLDivElement>(null)
  const toast = useToast()
  useFocusTrap(box)
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && onClose()
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose])

  // The phone's own share sheet where there is one (https only); otherwise the link is copied.
  async function share() {
    const data = { title: 'Usherr', text: t('Usherr: your own home cinema, for films, shows, music and books.'), url: REPO_URL }
    if (navigator.share && window.isSecureContext) {
      try {
        await navigator.share(data)
        return
      } catch (e) {
        if (e instanceof DOMException && e.name === 'AbortError') return
      }
    }
    if (await copyText(REPO_URL)) toast.success(t('Usherr’s link is copied: {url}', { url: REPO_URL }))
    else toast.info(t('Usherr’s link: {url}', { url: REPO_URL }))
  }

  return createPortal(
    <div className="modal-backdrop" onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      <div ref={box} tabIndex={-1} className="modal confirm-dialog support-dialog" role="dialog" aria-modal="true" aria-label={t('Support {upstream}', up)}>
        <div className="confirm-head">
          <span className="confirm-ico support-ico">
            <Icon name="heart" size={20} />
          </span>
          <h2>{t('Support Ryan and {upstream}', up)}</h2>
          <button className="icon-btn support-x" onClick={onClose} aria-label={t('Close')}>
            <Icon name="x" size={18} />
          </button>
        </div>
        <div className="confirm-body">
          <p>{t('Usherr is built on {upstream}, made by Ryan. I want to support Ryan in his efforts to develop {upstream}: without it, Usherr could not exist. The first button below goes to his Ko-fi page.', up)}</p>
          <p>{t('Please share Usherr too, so it can grow. Feedback and bug reports are always appreciated: the bug button next to this one sends them.')}</p>
        </div>
        <div className="support-actions">
          <a className="btn-link primary-look" href={KOFI_URL} target="_blank" rel="noreferrer" onClick={onClose}>
            <Icon name="heart" size={16} /> {t('Support Ryan on Ko-fi')}
          </a>
          <button className="btn-with-icon" onClick={() => void share()}>
            <Icon name="share" size={16} /> {t('Share Usherr')}
          </button>
        </div>
      </div>
    </div>,
    document.body,
  )
}
