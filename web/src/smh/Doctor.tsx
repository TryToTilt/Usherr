// Usherr SMH: the Doctor ("Something's not right?"). Runs Usherr's own
// health checks on demand and shows what was checked, not only what's
// wrong: each part with a tick, each problem in plain words with what stops
// working, a button to fix it and, where there is one, the walkthrough that
// helps. The checks are the server's (GET /api/health), for administrators;
// anyone else gets the things they can do themselves.
import { useCallback, useEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { Link } from 'react-router-dom'
import { api, isAdmin, type HealthItem } from '../api'
import { useAuth } from '../AuthContext'
import Icon from '../components/Icon'
import { t } from '../i18n'
import { openReport } from '../report'
import { useFocusTrap } from '../useFocusTrap'
import { closeDoctor, startSmh } from './engine'
import './smh.css'

// The parts that are checked, and which checks belong to each.
const PARTS: { key: string; label: () => string; has: (id: string) => boolean }[] = [
  { key: 'core', label: () => t('Usherr itself'), has: (id) => /^(database|safe-mode|secret-key|automation)/.test(id) },
  { key: 'tmdb', label: () => t('Film and show details (TMDB)'), has: (id) => id.startsWith('tmdb') },
  { key: 'usenet', label: () => t('Usenet provider'), has: (id) => id.startsWith('no-usenet') },
  { key: 'indexers', label: () => t('Indexers'), has: (id) => id.startsWith('indexer') || id === 'vpn-indexers-none' || id === 'flaresolverr-down' },
  { key: 'vpn', label: () => t('VPN'), has: (id) => /^(no-vpn|vpn-down|vpn-required)/.test(id) },
  { key: 'folders', label: () => t('Folders and disk space'), has: (id) => id.includes('folder') || id === 'no-hardlinks' },
  { key: 'downloads', label: () => t('Downloads lately'), has: (id) => id === 'recent-failures' },
  { key: 'subtitles', label: () => t('Subtitles'), has: (id) => id.includes('subtitles') },
  { key: 'lists', label: () => t('Lists and tracking'), has: (id) => /^(trakt|simkl)/.test(id) },
  { key: 'drive', label: () => t('Virtual drive (Plex)'), has: (id) => id.startsWith('drive-') },
]

// The walkthrough that helps with a problem, when there is one.
function walkFor(id: string): string | null {
  if (id.startsWith('no-usenet')) return 'usenet-provider'
  if (id.startsWith('indexer') || id === 'vpn-indexers-none') return 'indexer'
  if (/^(no-vpn|vpn-down|vpn-required)/.test(id)) return 'vpn'
  if (id === 'recent-failures') return 'why-failed'
  if (id.includes('subtitles')) return 'subtitles'
  if (id.startsWith('drive-')) return 'plex-drive'
  return null
}

const ICON = { error: 'warning', warn: 'info', info: 'info' } as const

export default function Doctor() {
  const { user } = useAuth()
  const admin = isAdmin(user)
  const box = useRef<HTMLDivElement>(null)
  useFocusTrap(box)
  const [items, setItems] = useState<HealthItem[] | null>(null)
  const [error, setError] = useState('')
  const [at, setAt] = useState(0)
  const check = useCallback(() => {
    setItems(null)
    setError('')
    api
      .health()
      .then((d) => {
        setItems(d.items)
        setAt(Date.now())
      })
      .catch((e: Error) => setError(e.message))
  }, [])
  useEffect(() => {
    if (admin) check()
  }, [admin, check])

  const problems = (items ?? []).filter((i) => i.level !== 'info')
  const optional = (items ?? []).filter((i) => i.level === 'info')
  const help = (i: HealthItem) => {
    const w = walkFor(i.id)
    return (
      <li key={i.id} className={`doc-item level-${i.level}`}>
        <Icon name={ICON[i.level]} size={16} />
        <div>
          <strong>{i.title}</strong>
          <p>{i.impact}</p>
          <div className="doc-acts">
            {i.action && (
              <Link className="btn-sm primary" to={i.action.path} onClick={closeDoctor}>
                {i.action.label}
              </Link>
            )}
            {w && (
              <button type="button" className="btn-sm" onClick={() => startSmh(w, 'simple')}>
                <Icon name="compass" size={13} /> {t('Show me how')}
              </button>
            )}
          </div>
        </div>
      </li>
    )
  }

  return createPortal(
    <div className="modal-backdrop" onMouseDown={(e) => e.target === e.currentTarget && closeDoctor()}>
      <div ref={box} tabIndex={-1} className="modal doc" role="dialog" aria-modal="true" aria-label={t('Check Usherr')} onKeyDown={(e) => e.key === 'Escape' && closeDoctor()}>
        <div className="modal-head">
          <h2>
            <Icon name="shield" size={20} /> {t('Something not right?')}
          </h2>
          <button className="icon-btn" onClick={closeDoctor} aria-label={t('Close')}>
            <Icon name="x" size={18} />
          </button>
        </div>
        <div className="modal-body">
          {!admin ? (
            <>
              <p>{t('Usherr’s own checks are for an administrator, who can see the server’s settings. What you can do:')}</p>
              <div className="doc-acts">
                <button type="button" className="btn-sm primary" onClick={() => startSmh('why-failed', 'simple')}>
                  <Icon name="compass" size={13} /> {t('Why didn’t my download work?')}
                </button>
                <button
                  type="button"
                  className="btn-sm"
                  onClick={() => {
                    closeDoctor()
                    openReport('problem')
                  }}
                >
                  <Icon name="bug" size={13} /> {t('Tell an administrator')}
                </button>
              </div>
            </>
          ) : error ? (
            <p className="error-text">{error}</p>
          ) : !items ? (
            <p className="smh-busy">{t('Checking everything Usherr depends on…')}</p>
          ) : (
            <>
              <p className={`doc-sum ${problems.length ? 'bad' : 'ok'}`}>
                <Icon name={problems.length ? 'warning' : 'check'} size={18} />{' '}
                {problems.length === 0
                  ? t('Everything checked out.')
                  : problems.length === 1
                    ? t('One thing needs fixing.')
                    : t('{n} things need fixing.', { n: problems.length })}
              </p>
              <ul className="doc-parts">
                {PARTS.map((p) => {
                  const mine = (items ?? []).filter((i) => p.has(i.id) && i.level !== 'info')
                  return (
                    <li key={p.key} className={mine.length ? (mine.some((i) => i.level === 'error') ? 'bad' : 'warn') : 'ok'}>
                      <Icon name={mine.length ? 'warning' : 'check'} size={14} /> {p.label()}
                    </li>
                  )
                })}
              </ul>
              {problems.length > 0 && <ul className="doc-list">{problems.map(help)}</ul>}
              {optional.length > 0 && (
                <details className="doc-optional">
                  <summary>{t('Worth knowing ({n})', { n: optional.length })}</summary>
                  <ul className="doc-list">{optional.map(help)}</ul>
                </details>
              )}
              <div className="doc-foot">
                <span className="hint">{at ? t('Checked at {time}', { time: new Date(at).toLocaleTimeString([], { hour: 'numeric', minute: '2-digit' }) }) : ''}</span>
                <span className="spacer" />
                <button type="button" className="btn-sm" onClick={check}>
                  <Icon name="refresh" size={13} /> {t('Check again')}
                </button>
              </div>
              <p className="hint">{t('Still stuck? Settings, Logs and errors has the technical detail, and Report a problem sends it to the people who make Usherr.')}</p>
            </>
          )}
        </div>
      </div>
    </div>,
    document.body,
  )
}
