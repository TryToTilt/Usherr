// Usherr: ready-made channels. A pack is a channel's rules built from
// genres and decades, so it fills itself from whatever the library has;
// the AI builder makes one from a description. Either opens in the channel
// editor to look over before it's saved.
import { useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import Icon from '../components/Icon'
import { t } from '../i18n'
import { useFocusTrap } from '../useFocusTrap'
import { askAI, useAIOn } from './aiShared'

// The rules a draft carries (the channel editor's Rules).
export type DraftRules = Record<string, unknown>
export interface Draft {
  name: string
  rules: DraftRules
  note?: string
}

const PACKS: { id: string; name: () => string; blurb: () => string; rules: DraftRules }[] = [
  { id: 'saturday', name: () => t('Saturday Morning'), blurb: () => t('Cartoons and kids’ shows, half-hour slots, family hours'), rules: { genres: ['Animation', 'Kids', 'Family'], order: 'rotate', grid: 30, familyHours: true, holidays: true } },
  { id: 'sitcoms', name: () => t('Sitcom Gold'), blurb: () => t('Comedies back to back on the half hour, with a Friday-night line-up'), rules: { genres: ['Comedy'], order: 'rotate', grid: 30, holidays: true, premieres: true } },
  { id: 'tgif', name: () => t('TGIF'), blurb: () => t('’80s and ’90s family comedies'), rules: { genres: ['Comedy', 'Family'], decades: [1980, 1990], order: 'rotate', grid: 30, familyHours: true, holidays: true } },
  { id: 'scifi', name: () => t('Sci-Fi Channel'), blurb: () => t('Science fiction and fantasy, on the hour, with a movie at night'), rules: { genres: ['Sci-Fi & Fantasy', 'Science Fiction'], allMovies: true, maxMoviesPerDay: 1, order: 'rotate', grid: 60 } },
  { id: 'crime', name: () => t('Crime & Mystery'), blurb: () => t('Detectives, cases and whodunits'), rules: { genres: ['Crime', 'Mystery'], order: 'rotate', grid: 60, premieres: true } },
  { id: 'movies', name: () => t('Movie Channel'), blurb: () => t('Films on the hour, with a Sunday Night Movie'), rules: { allMovies: true, order: 'shuffle', grid: 60, slots: [{ days: [0], at: '20:00', movie: true, name: 'Sunday Night Movie' }] } },
  { id: 'horror', name: () => t('Creature Feature'), blurb: () => t('Horror after dark, cartoons-free'), rules: { genres: ['Horror'], allMovies: true, order: 'shuffle', grid: 30, holidays: true } },
  { id: 'holiday', name: () => t('Holiday Channel'), blurb: () => t('Family shows that turn festive in each season'), rules: { genres: ['Family', 'Animation', 'Comedy'], order: 'rotate', grid: 30, holidays: true, familyHours: true } },
  { id: 'retro', name: () => t('Rewind ’70s–’90s'), blurb: () => t('Shows and movies from the decades you grew up with'), rules: { decades: [1970, 1980, 1990], allMovies: true, maxMoviesPerDay: 1, order: 'rotate', grid: 30 } },
  { id: 'docs', name: () => t('Discovery Hour'), blurb: () => t('Documentaries and reality, on the hour'), rules: { genres: ['Documentary', 'Reality'], order: 'rotate', grid: 60 } },
]

export function PacksButton({ onPick }: { onPick: (d: Draft) => void }) {
  const [open, setOpen] = useState(false)
  const box = useRef<HTMLDivElement>(null)
  return (
    <>
      <button type="button" className="btn-with-icon" data-walk="tv-packs" onClick={() => setOpen(true)} title={t('Ready-made channels that fill themselves from your library')}>
        <Icon name="grid" size={16} /> {t('Channel packs')}
      </button>
      {open &&
        createPortal(
          <div className="modal-backdrop" onMouseDown={(e) => e.target === e.currentTarget && setOpen(false)}>
            <PacksDialog box={box} onClose={() => setOpen(false)} onPick={(d) => (setOpen(false), onPick(d))} />
          </div>,
          document.body,
        )}
    </>
  )
}

function PacksDialog({ box, onClose, onPick }: { box: React.RefObject<HTMLDivElement | null>; onClose: () => void; onPick: (d: Draft) => void }) {
  useFocusTrap(box)
  return (
    <div ref={box} tabIndex={-1} className="modal tv-packs" role="dialog" aria-modal="true" aria-label={t('Channel packs')}>
      <div className="modal-head">
        <h2>{t('Channel packs')}</h2>
        <button className="icon-btn" onClick={onClose} aria-label={t('Close')}>
          <Icon name="x" />
        </button>
      </div>
      <p className="hint">{t('Each pack fills itself from the shows and movies in your library. It opens in the editor first, so you can change anything.')}</p>
      <div className="tv-packs-grid">
        {PACKS.map((p) => (
          <button key={p.id} type="button" className="tv-pack" onClick={() => onPick({ name: p.name(), rules: p.rules })}>
            <strong>{p.name()}</strong>
            <span>{p.blurb()}</span>
          </button>
        ))}
      </div>
    </div>
  )
}

export function AIBuildButton({ onPick }: { onPick: (d: Draft) => void }) {
  const aiOn = useAIOn()
  const [open, setOpen] = useState(false)
  const [prompt, setPrompt] = useState('')
  const [streaming, setStreaming] = useState<'library' | 'mix' | 'stream'>('mix') // Usherr
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const box = useRef<HTMLDivElement>(null)
  if (!aiOn) return null
  const build = () => {
    if (!prompt.trim()) return
    setBusy(true)
    setError('')
    askAI<Draft>('/api/tv/ai-build', { prompt, streaming })
      .then((d) => {
        setOpen(false)
        onPick(d)
      })
      .catch((e: Error) => setError(e.message))
      .finally(() => setBusy(false))
  }
  return (
    <>
      <button type="button" className="btn-with-icon" data-walk="tv-ai" onClick={() => setOpen(true)}>
        <Icon name="sparkles" size={16} /> {t('Build with AI')}
      </button>
      {open &&
        createPortal(
          <div className="modal-backdrop" onMouseDown={(e) => e.target === e.currentTarget && !busy && setOpen(false)}>
            <div ref={box} tabIndex={-1} className="modal tv-aibuild" role="dialog" aria-modal="true" aria-label={t('Build a channel with AI')}>
              <div className="modal-head">
                <h2>{t('Build a channel with AI')}</h2>
                <button className="icon-btn" onClick={() => setOpen(false)} aria-label={t('Close')} disabled={busy}>
                  <Icon name="x" />
                </button>
              </div>
              <textarea rows={3} value={prompt} onChange={(e) => setPrompt(e.target.value)} placeholder={t('A 1995 TGIF channel · Horror for October nights · Saturday-morning cartoons from the ’80s')} maxLength={400} autoFocus />
              <div className="room-row">
                {[t('A 1995 TGIF channel'), t('Horror for October nights'), t('Saturday-morning cartoons from the ’80s'), t('A cosy British mystery channel')].map((ex) => (
                  <button key={ex} type="button" className="room-chip" onClick={() => setPrompt(ex)}>
                    {ex}
                  </button>
                ))}
              </div>
              <div className="seg" role="group" aria-label={t('Streamed shows')}>
                {(
                  [
                    ['library', t('My library only')],
                    ['mix', t('Library + streamed')],
                    ['stream', t('Mostly streamed')],
                  ] as const
                ).map(([k, label]) => (
                  <button key={k} type="button" className={streaming === k ? 'active' : ''} aria-pressed={streaming === k} onClick={() => setStreaming(k)}>
                    {label}
                  </button>
                ))}
              </div>
              {error && <p className="error-text">{error}</p>}
              <div className="modal-foot">
                <button className="primary btn-with-icon" onClick={build} disabled={busy || !prompt.trim()}>
                  <Icon name="sparkles" size={16} /> {busy ? t('Building…') : t('Build it')}
                </button>
              </div>
            </div>
          </div>,
          document.body,
        )}
    </>
  )
}
