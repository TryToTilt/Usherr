// Usherr: Multiview (four channels at once, one with sound) and the links
// that put the channels in other apps (IPTV apps such as TiviMate, Plex).
import { useEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import Icon from '../components/Icon'
import { t } from '../i18n'
import { useFocusTrap } from '../useFocusTrap'
import { ChannelPreview, playChannel } from './Player'

interface Ch {
  id: number
  number: number
  name: string
}

export function MultiviewButton({ channels }: { channels: Ch[] }) {
  const [open, setOpen] = useState(false)
  if (channels.length < 2) return null
  return (
    <>
      <button type="button" className="btn-with-icon" onClick={() => setOpen(true)} title={t('Four channels at once')}>
        <Icon name="grid" size={16} /> {t('Multiview')}
      </button>
      {open && createPortal(<Multiview channels={channels} onClose={() => setOpen(false)} />, document.body)}
    </>
  )
}

function Multiview({ channels, onClose }: { channels: Ch[]; onClose: () => void }) {
  const [picks, setPicks] = useState<number[]>(() => channels.slice(0, 4).map((c) => c.id))
  const [loud, setLoud] = useState(0)
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && onClose()
    document.addEventListener('keydown', onKey)
    const overflow = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    return () => {
      document.removeEventListener('keydown', onKey)
      document.body.style.overflow = overflow
    }
  }, [onClose])
  return (
    <div className="tv-multi" role="dialog" aria-modal="true" aria-label={t('Multiview')}>
      <div className="tv-multi-head">
        <strong>{t('Multiview')}</strong>
        <span className="hint">{t('Tap a picture for its sound; double-tap to watch it full size.')}</span>
        <button className="btn-sm" onClick={onClose} aria-label={t('Close')}>
          <Icon name="x" size={16} />
        </button>
      </div>
      <div className="tv-multi-grid">
        {picks.map((id, i) => {
          const ch = channels.find((c) => c.id === id)
          return (
            <div key={i} className={`tv-multi-cell${loud === i ? ' loud' : ''}`} onClick={() => setLoud(i)} onDoubleClick={() => ch && (onClose(), playChannel(ch.id, ch.name))}>
              {ch && <ChannelPreview key={ch.id} channelId={ch.id} name={ch.name} muted={loud !== i} />}
              <select value={id} onClick={(e) => e.stopPropagation()} onChange={(e) => setPicks((p) => p.map((x, j) => (j === i ? Number(e.target.value) : x)))} aria-label={t('Channel')}>
                {channels.map((c) => (
                  <option key={c.id} value={c.id}>
                    {c.number} {c.name}
                  </option>
                ))}
              </select>
              {loud === i && (
                <span className="tv-multi-sound">
                  <Icon name="volume" size={14} />
                </span>
              )}
            </div>
          )
        })}
      </div>
    </div>
  )
}

export function OtherAppsButton() {
  const [open, setOpen] = useState(false)
  const [links, setLinks] = useState<{ m3u: string; xmltv: string; hdhr: string } | null>(null)
  const [copied, setCopied] = useState('')
  const box = useRef<HTMLDivElement>(null)
  useEffect(() => {
    if (!open || links) return
    fetch('/api/tv/tuner', { credentials: 'include' })
      .then((r) => (r.ok ? r.json() : null))
      .then((d) => d && setLinks(d))
      .catch(() => undefined)
  }, [open, links])
  const copy = (v: string) => {
    void navigator.clipboard?.writeText(v).then(() => setCopied(v))
  }
  const row = (label: string, v: string | undefined, help: string) => (
    <div className="tv-apps-row">
      <strong>{label}</strong>
      <div className="tv-apps-link">
        <code>{v ?? '…'}</code>
        {v && (
          <button className="btn-sm" onClick={() => copy(v)}>
            {copied === v ? t('Copied') : t('Copy')}
          </button>
        )}
      </div>
      <small className="hint">{help}</small>
    </div>
  )
  return (
    <>
      <button type="button" className="btn-with-icon" onClick={() => setOpen(true)} title={t('Watch these channels in Plex, TiviMate and other apps')}>
        <Icon name="share" size={16} /> {t('Other apps')}
      </button>
      {open &&
        createPortal(
          <div className="modal-backdrop" onMouseDown={(e) => e.target === e.currentTarget && setOpen(false)}>
            <OtherAppsDialog box={box} onClose={() => setOpen(false)}>
              {row(t('Channel list (M3U)'), links?.m3u, t('For IPTV apps such as TiviMate (on the Shield), IPTV Smarters or VLC: add a playlist with this address.'))}
              {row(t('Guide (XMLTV)'), links?.xmltv, t('The guide for the same apps (in TiviMate: EPG sources), and for Plex below.'))}
              {row(t('Tuner for Plex'), links?.hdhr, t('In Plex: Settings, Live TV & DVR, Set up Plex DVR, “Don’t see your HDHomeRun device? Enter its network address manually”, then this address. Give Plex the guide address above when it asks for one.'))}
              <p className="hint">{t('The addresses carry a private key: share them only with apps you use. Jellyfin needs none of this: use Add to Jellyfin.')}</p>
            </OtherAppsDialog>
          </div>,
          document.body,
        )}
    </>
  )
}

function OtherAppsDialog({ box, onClose, children }: { box: React.RefObject<HTMLDivElement | null>; onClose: () => void; children: React.ReactNode }) {
  useFocusTrap(box)
  return (
    <div ref={box} tabIndex={-1} className="modal tv-apps" role="dialog" aria-modal="true" aria-label={t('Other apps')}>
      <div className="modal-head">
        <h2>{t('Watch in other apps')}</h2>
        <button className="icon-btn" onClick={onClose} aria-label={t('Close')}>
          <Icon name="x" />
        </button>
      </div>
      {children}
    </div>
  )
}
