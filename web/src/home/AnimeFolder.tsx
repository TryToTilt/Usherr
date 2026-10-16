// Usherr: the anime folder, next to the TV folder in Settings > Library >
// Folders. New anime shows go there unless another folder is picked, and the
// Library's Anime tab finds anime by it. Saved on its own.
import { useEffect, useState } from 'react'
import FolderStatus from '../components/FolderStatus'
import { useToast } from '../components/Toast'
import { t } from '../i18n'

interface AnimeFolderState {
  path: string
  env: string
  inUse: string
}

export default function AnimeFolder() {
  const toast = useToast()
  const [st, setSt] = useState<AnimeFolderState | null>(null)
  const [path, setPath] = useState('')
  useEffect(() => {
    fetch('/api/home/anime-folder', { credentials: 'include' })
      .then((r) => (r.ok ? (r.json() as Promise<AnimeFolderState>) : null))
      .then((d) => {
        if (!d) return
        setSt(d)
        setPath(d.path)
      })
      .catch(() => undefined)
  }, [])
  if (!st) return null
  const save = async () => {
    const r = await fetch('/api/home/anime-folder', { method: 'PUT', credentials: 'include', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ path }) })
    const d = (await r.json().catch(() => ({}))) as AnimeFolderState & { error?: string }
    if (!r.ok) {
      toast.error(d.error ?? t('The anime folder couldn’t be saved.'))
      return
    }
    setSt(d)
    setPath(d.path)
    toast.success(t('Anime folder saved.'))
  }
  return (
    <div className="path-col">
      <label>
        {t('Anime folder')}
        <input value={path} onChange={(e) => setPath(e.target.value)} placeholder={st.env || '/data/anime'} spellCheck={false} />
      </label>
      {path.trim() ? (
        <FolderStatus path={path} />
      ) : (
        <small className="path-note">{st.env ? t('Using {path} from ANIME_DIR.', { path: st.env }) : t('Optional. Without one, anime goes in the TV folder.')}</small>
      )}
      <div>
        <button className="btn-sm" onClick={() => void save()} disabled={path.trim() === st.path}>
          {t('Save the anime folder')}
        </button>
      </div>
      <small className="path-note">{t('New anime shows go here. Add it to Jellyfin, Emby or Plex as its own library.')}</small>
    </div>
  )
}
