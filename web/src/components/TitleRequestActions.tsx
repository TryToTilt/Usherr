import { createPortal } from 'react-dom'
import { useEffect, useRef, useState } from 'react'
import { api, isAdmin, isRequested, isViewer, type IssueCategory, type RequestStatus } from '../api'
import { useAuth } from '../AuthContext'
import { st as serverText, t } from '../i18n'
import { useFocusTrap } from '../useFocusTrap'
import { ISSUE_CATEGORY } from './issueCategories'
import Icon from './Icon'
import { useToast } from './Toast'
import './RequestPortal.css'

// On a movie or show that is in the library: "Ask for a 4K copy" (for an
// account that has to ask, when an administrator allows it), "Ask for more
// seasons" (Usherr: a show with seasons nobody asked for yet) and "Report a
// problem" (for anyone signed in, when an administrator allows it).
export default function TitleRequestActions({
  kind,
  tmdbId,
  libraryId,
  title,
  openSeasons = [],
}: {
  kind: 'movie' | 'tv'
  tmdbId: number
  libraryId: number
  title: string
  // Usherr: seasons here that are neither downloaded nor wanted yet.
  openSeasons?: number[]
}) {
  const toast = useToast()
  const me = useAuth().user
  const admin = isAdmin(me)
  // A viewer can only look: asking for a copy or reporting a problem both change something.
  const viewer = isViewer(me)
  const [st, setSt] = useState<RequestStatus | null>(null)
  const [reporting, setReporting] = useState(false)
  const [askingSeasons, setAskingSeasons] = useState(false)
  const [busy, setBusy] = useState(false)
  // The title already has a second copy in the 4K quality.
  const [hasFourK, setHasFourK] = useState(false)
  useEffect(() => {
    if (viewer) return
    api.requestStatus().then(setSt).catch(() => setSt(null))
  }, [viewer])
  useEffect(() => {
    if (viewer || !st || admin || !st.asksToAdd || st.fourKProfileId <= 0) return
    api
      .titleCopies(kind === 'movie' ? 'movie' : 'series', libraryId)
      .then((c) => setHasFourK(c.items.some((x) => x.profileId === st.fourKProfileId)))
      .catch(() => undefined)
  }, [st, admin, viewer, kind, libraryId])

  if (viewer || !st) return null
  const canAskFourK = !admin && st.asksToAdd && (kind === 'movie' ? st.fourK.movie : st.fourK.tv) && !hasFourK
  const canAskSeasons = !admin && kind === 'tv' && openSeasons.length > 0
  if (!canAskFourK && !st.reports && !canAskSeasons) return null

  async function askFourK() {
    setBusy(true)
    try {
      const res = kind === 'movie' ? await api.addMovie(tmdbId, { fourK: true }) : await api.addSeries(tmdbId, { fourK: true })
      toast.success(isRequested(res) ? serverText(res.message) : t('Sent.'))
      api.requestStatus().then(setSt).catch(() => undefined)
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  return (
    <>
      <div className="title-actions">
        {canAskFourK && (
          <button className="btn-with-icon" disabled={busy} onClick={() => void askFourK()} title={st.fourKLabel ? t('A second copy in {quality}', { quality: st.fourKLabel }) : undefined}>
            <Icon name="disc" size={15} /> {t('Ask for a 4K copy')}
          </button>
        )}
        {canAskSeasons && (
          <button className="btn-with-icon" onClick={() => setAskingSeasons(true)}>
            <Icon name="plus" size={15} /> {t('Ask for more seasons')}
          </button>
        )}
        {st.reports && (
          <button className="btn-with-icon" onClick={() => setReporting(true)}>
            <Icon name="bug" size={15} /> {t('Report a problem')}
          </button>
        )}
      </div>
      {reporting && <ProblemDialog kind={kind} libraryId={libraryId} title={title} onClose={() => setReporting(false)} />}
      {askingSeasons && (
        <SeasonsDialog
          tmdbId={tmdbId}
          title={title}
          seasons={openSeasons}
          onClose={() => setAskingSeasons(false)}
          onDone={() => {
            setAskingSeasons(false)
            api.requestStatus().then(setSt).catch(() => undefined)
          }}
        />
      )}
    </>
  )
}

// Usherr: pick the seasons to ask for. A member who has to ask files a
// request; one who may add switches the seasons on straight away.
function SeasonsDialog({ tmdbId, title, seasons, onClose, onDone }: { tmdbId: number; title: string; seasons: number[]; onClose: () => void; onDone: () => void }) {
  const toast = useToast()
  const [picked, setPicked] = useState<number[]>([])
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const box = useRef<HTMLDivElement>(null)
  useFocusTrap(box)
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && !busy && onClose()
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose, busy])
  const toggle = (n: number) => setPicked((p) => (p.includes(n) ? p.filter((x) => x !== n) : [...p, n].sort((a, b) => a - b)))

  async function send() {
    setBusy(true)
    setError('')
    try {
      const res = await api.addSeries(tmdbId, { seasons: picked })
      toast.success(isRequested(res) ? serverText(res.message) : t('Those seasons are on their way.'))
      onDone()
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  return createPortal(
    <div className="modal-backdrop" onMouseDown={(e) => e.target === e.currentTarget && !busy && onClose()}>
      <div ref={box} tabIndex={-1} className="modal" role="dialog" aria-modal="true" aria-label={t('Ask for more seasons of {name}', { name: title })}>
        <div className="modal-head">
          <div style={{ flex: 1, minWidth: 0 }}>
            <h2 style={{ margin: '0 0 4px' }}>{t('Ask for more seasons')}</h2>
            <span style={{ color: 'var(--text-dim)' }}>{title}</span>
          </div>
          <button className="icon-btn" onClick={onClose} disabled={busy} aria-label={t('Close')}>
            <Icon name="x" />
          </button>
        </div>
        <div className="modal-body problem-form">
          <p style={{ margin: 0, color: 'var(--text-dim)' }}>{t('These seasons aren’t here or on their way yet. Pick the ones you want.')}</p>
          <div className="tr-chips">
            {seasons.length > 1 && (
              <button type="button" className={`chip${picked.length === seasons.length ? ' active' : ''}`} aria-pressed={picked.length === seasons.length} onClick={() => setPicked(picked.length === seasons.length ? [] : [...seasons])}>
                {t('All')}
              </button>
            )}
            {seasons.map((n) => (
              <button key={n} type="button" className={`chip${picked.includes(n) ? ' active' : ''}`} aria-pressed={picked.includes(n)} onClick={() => toggle(n)}>
                {n === 0 ? t('Specials') : t('Season {n}', { n })}
              </button>
            ))}
          </div>
          {error && <p className="error-text">{error}</p>}
        </div>
        <div className="modal-foot">
          <button onClick={onClose} disabled={busy}>
            {t('Cancel')}
          </button>
          <button className="primary" onClick={() => void send()} disabled={busy || picked.length === 0}>
            {busy ? t('Sending…') : t('Ask for them')}
          </button>
        </div>
      </div>
    </div>,
    document.body,
  )
}

function ProblemDialog({ kind, libraryId, title, onClose }: { kind: 'movie' | 'tv'; libraryId: number; title: string; onClose: () => void }) {
  const toast = useToast()
  const [category, setCategory] = useState<IssueCategory>('wrong-file')
  const [episode, setEpisode] = useState('')
  const [message, setMessage] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const box = useRef<HTMLDivElement>(null)
  useFocusTrap(box)
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && !busy && onClose()
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose, busy])

  async function send() {
    setBusy(true)
    setError('')
    try {
      await api.createIssue({ kind, itemId: libraryId, category, episode: episode.trim() || undefined, message: message.trim() || undefined })
      toast.success(t('Thanks. An administrator will look at it. You can follow it under Activity > Reports.'))
      onClose()
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  return createPortal(
    <div className="modal-backdrop" onMouseDown={(e) => e.target === e.currentTarget && !busy && onClose()}>
      <div ref={box} tabIndex={-1} className="modal" role="dialog" aria-modal="true" aria-label={t('Report a problem with {name}', { name: title })}>
        <div className="modal-head">
          <div style={{ flex: 1, minWidth: 0 }}>
            <h2 style={{ margin: '0 0 4px' }}>{t('Report a problem')}</h2>
            <span style={{ color: 'var(--text-dim)' }}>{title}</span>
          </div>
          <button className="icon-btn" onClick={onClose} disabled={busy} aria-label={t('Close')}>
            <Icon name="x" />
          </button>
        </div>
        <div className="modal-body problem-form">
          <label>
            {t("What's wrong?")}
            <select value={category} onChange={(e) => setCategory(e.target.value as IssueCategory)}>
              {(Object.keys(ISSUE_CATEGORY) as IssueCategory[]).map((c) => (
                <option key={c} value={c}>
                  {ISSUE_CATEGORY[c]}
                </option>
              ))}
            </select>
          </label>
          {kind === 'tv' && (
            <label>
              {t('Which episode? (optional)')}
              <input value={episode} onChange={(e) => setEpisode(e.target.value)} placeholder="S01E03" maxLength={40} />
            </label>
          )}
          <label>
            {category === 'other' ? t('Tell us what is wrong') : t('More detail (optional)')}
            <textarea value={message} onChange={(e) => setMessage(e.target.value)} maxLength={500} placeholder={t('For example: the voices drop out after an hour.')} />
          </label>
          {error && <p className="error-text">{error}</p>}
        </div>
        <div className="modal-foot">
          <button onClick={onClose} disabled={busy}>
            {t('Cancel')}
          </button>
          <button className="primary" onClick={() => void send()} disabled={busy || (category === 'other' && !message.trim())}>
            {busy ? t('Sending…') : t('Send report')}
          </button>
        </div>
      </div>
    </div>,
    document.body,
  )
}
