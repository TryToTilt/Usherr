// Usherr: Upcoming → Leaving soon. What the clean-up rule sets will remove,
// and when. Everyone can see it; "Keep it" (when an administrator allows it)
// means the rules never touch that movie or show.
import { useCallback, useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { api, type LeavingEntry } from '../api'
import Icon from '../components/Icon'
import { useToast } from '../components/Toast'
import { locale, plural, t, st as serverText } from '../i18n'

function date(ms: number): string {
  return new Date(ms).toLocaleDateString(locale(), { weekday: 'short', day: 'numeric', month: 'short' })
}

function link(e: LeavingEntry): string {
  if (e.kind === 'movie') return e.tmdbId ? `/title/${e.tmdbId}` : '/library'
  return `/series/${e.titleId}`
}

export default function LeavingSoon() {
  const toast = useToast()
  const [items, setItems] = useState<LeavingEntry[] | null>(null)
  const [canKeep, setCanKeep] = useState(false)
  const [busy, setBusy] = useState('')
  const [error, setError] = useState('')

  const load = useCallback(() => {
    api
      .leavingSoon()
      .then((r) => {
        setItems(r.items)
        setCanKeep(r.canKeep)
      })
      .catch((e: Error) => setError(e.message))
  }, [])
  useEffect(load, [load])

  async function keep(e: LeavingEntry) {
    setBusy(e.unit)
    try {
      await api.keepLeaving(e.kind === 'movie' ? 'movie' : 'show', e.titleId)
      toast.success(t("{title} stays. The clean-up rules won't touch it.", { title: e.title }))
      load()
    } catch (err) {
      toast.error(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy('')
    }
  }

  if (error) return <p className="error-text">{error}</p>
  if (!items) return null
  return (
    <section className="card wide">
      <h2>{t('Leaving soon')}</h2>
      <p style={{ color: 'var(--text-dim)' }}>
        {t('These titles match the clean-up rules and will be removed on the date shown, if they still match then. Watching one again can keep it.')}
        {canKeep ? ` ${t('Press Keep it to keep a title for good.')}` : ''}
      </p>
      {items.length === 0 ? (
        <p className="hint">{t('Nothing is leaving soon.')}</p>
      ) : (
        <>
          <p className="hint">{plural(items.length, '{n} title is leaving soon.', '{n} titles are leaving soon.')}</p>
          <ul style={{ listStyle: 'none', padding: 0, margin: 0, display: 'grid', gap: 6 }}>
            {items.map((e) => (
              <li key={e.unit} className="cleanup-rule">
                <span style={{ flex: 1, minWidth: 200 }}>
                  <Icon name={e.kind === 'movie' ? 'film' : 'tv'} size={14} /> <Link to={link(e)}>{e.title}</Link>{' '}
                  <span className="hint">· {serverText(e.reason)}</span>
                </span>
                <span className="hint">{t('Leaves {date}', { date: date(e.actMs) })}</span>
                {canKeep && (
                  <button className="btn-sm btn-with-icon" disabled={!!busy} onClick={() => void keep(e)}>
                    <Icon name="heart" size={13} /> {busy === e.unit ? t('Keeping…') : t('Keep it')}
                  </button>
                )}
              </li>
            ))}
          </ul>
        </>
      )}
    </section>
  )
}
