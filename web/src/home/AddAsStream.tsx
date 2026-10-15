// Home fork: "Add as stream" in the add dialog (Search, Discover and the
// title pages all use it). The title goes into the streamed library instead
// of being downloaded: it plays through StreamNZB, and shows in the Library
// and the media server like any other title.
import { useEffect, useState } from 'react'
import { isAdmin } from '../api'
import { useAuth } from '../AuthContext'
import Icon from '../components/Icon'
import { useToast } from '../components/Toast'
import { streamingOn } from './streamApi'
import { t } from '../i18n'

export default function AddAsStream({ kind, tmdbId, title, disabled, onDone }: { kind: 'movie' | 'tv'; tmdbId: number; title: string; disabled?: boolean; onDone: () => void }) {
  const admin = isAdmin(useAuth().user)
  const toast = useToast()
  const [on, setOn] = useState(false)
  const [have, setHave] = useState(0)
  const [busy, setBusy] = useState(false)
  useEffect(() => {
    if (!admin) return
    let live = true
    void streamingOn().then((v) => live && setOn(v))
    fetch(`/api/streamed/by-tmdb?kind=${kind}&tmdbId=${tmdbId}`, { credentials: 'include' })
      .then((r) => (r.ok ? r.json() : { id: 0 }))
      .then((d: { id: number }) => live && setHave(d.id))
      .catch(() => undefined)
    return () => {
      live = false
    }
  }, [admin, kind, tmdbId])
  if (!admin || !on || (have && kind === 'movie')) return null

  async function add() {
    setBusy(true)
    try {
      const res = await fetch('/api/streamed/add', {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ kind, tmdbId }),
      })
      const d = await res.json().catch(() => null)
      if (!res.ok) throw new Error(d?.error ?? t("Couldn't add it (error {status}).", { status: res.status }))
      toast.success(`${title}: ${d.message}`)
      onDone()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  return (
    <button className="btn-with-icon" onClick={() => void add()} disabled={busy || disabled} title={t('Play it through StreamNZB instead of downloading it')}>
      <Icon name="play" size={16} /> {busy ? t('Adding…') : have ? t('Add missing episodes as streams') : t('Add as stream')}
    </button>
  )
}
