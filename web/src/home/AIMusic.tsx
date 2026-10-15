// Home fork: AI for music.
//   - AIPlaylist (Playlists page): a playlist from your own music for a mood
//     or occasion.
//   - MoreLikeArtist (an artist's page): artists you have that go with this
//     one, and a few to discover.
import { useEffect, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import Icon from '../components/Icon'
import { useToast } from '../components/Toast'
import { t } from '../i18n'
import { AIButton, AIDialog, askAI, useAIOn, useAsk } from './aiShared'

export function AIPlaylist() {
  const on = useAIOn()
  const navigate = useNavigate()
  const toast = useToast()
  const [mood, setMood] = useState('')
  const [shared, setShared] = useState(false)
  const [busy, setBusy] = useState(false)
  if (!on) return null
  const make = async () => {
    if (!mood.trim()) return
    setBusy(true)
    try {
      const r = await askAI<{ id: number; name: string; note: string; tracks: number }>('/api/home/ai/playlist', { mood, shared })
      toast.success(r.note ? `${r.name}: ${r.note}` : r.name)
      navigate(`/music/playlists/${r.id}`)
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
      setBusy(false)
    }
  }
  return (
    <form
      className="ai-search-bar ai-playlist"
      onSubmit={(e) => {
        e.preventDefault()
        void make()
      }}
    >
      <Icon name="sparkles" size={18} />
      <input value={mood} onChange={(e) => setMood(e.target.value)} placeholder={t('A playlist from your music for… “Sunday morning cooking, nothing too loud”')} aria-label={t('The mood or occasion')} maxLength={200} disabled={busy} />
      <label className="mpl-share" title={t('Everyone on this server can see and play it')}>
        <input type="checkbox" checked={shared} onChange={(e) => setShared(e.target.checked)} disabled={busy} /> {t('Share with everyone')}
      </label>
      <button className="primary btn-sm" type="submit" disabled={busy || !mood.trim()}>
        {busy ? t('Picking tracks…') : t('Make it')}
      </button>
    </form>
  )
}

interface Similar {
  note: string
  have: { id: number; name: string }[]
  discover: { artist: string; album: string; why: string; to: string }[]
}

export function MoreLikeArtist({ artistId, name, autoOpen }: { artistId: number; name: string; autoOpen?: boolean }) {
  const on = useAIOn()
  const [open, setOpen] = useState(false)
  const ask = useAsk<Similar>()
  useEffect(() => {
    if (!autoOpen || !on || !artistId) return
    setOpen(true)
    void ask.run(askAI<Similar>('/api/home/ai/similar-artists', { artistId }))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [autoOpen, on, artistId])
  if (!on) return null
  return (
    <>
      <AIButton
        small
        label={t('More like this')}
        title={t('Artists you have that go with this one, and a few to discover')}
        onClick={() => {
          setOpen(true)
          if (!ask.data) void ask.run(askAI<Similar>('/api/home/ai/similar-artists', { artistId }))
        }}
      />
      {open && (
        <AIDialog title={t('More like {name}', { name })} onClose={() => setOpen(false)}>
          {ask.busy && <p className="hint">{t('Thinking…')}</p>}
          {ask.error && <p className="error-text">{ask.error}</p>}
          {ask.data && (
            <>
              {ask.data.note && <p className="recap-text">{ask.data.note}</p>}
              {ask.data.have.length > 0 && (
                <>
                  <h3 className="ai-sub">{t('In your library')}</h3>
                  <div className="news-mentions ai-chip-row">
                    {ask.data.have.map((a) => (
                      <Link key={a.id} to={`/music/artist/${a.id}`} className="news-chip have" onClick={() => setOpen(false)}>
                        <Icon name="open" size={12} /> {a.name}
                      </Link>
                    ))}
                  </div>
                </>
              )}
              {ask.data.discover.length > 0 && (
                <>
                  <h3 className="ai-sub">{t('New to you')}</h3>
                  <ul className="ai-list ai-cards">
                    {ask.data.discover.map((d) => (
                      <li key={d.artist}>
                        <Link to={d.to} className="news-chip find">
                          <Icon name="search" size={12} /> {d.artist}
                        </Link>{' '}
                        {d.album && <span className="hint">{t('start with {album}', { album: d.album })}</span>}
                        {d.why && <div className="ai-why">{d.why}</div>}
                      </li>
                    ))}
                  </ul>
                </>
              )}
            </>
          )}
        </AIDialog>
      )}
    </>
  )
}
