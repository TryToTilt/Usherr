// Usherr: the folders the base app doesn't have (anime, comics, NSFW 18+),
// next to the TV folder in Settings > Library > Folders. Each is saved on its
// own; left empty, the *_DIR variable or the default is used, and the field
// says which.
import { useEffect, useState } from 'react'
import FolderStatus from '../components/FolderStatus'
import { useToast } from '../components/Toast'
import { t } from '../i18n'

export type ExtraKind = 'anime' | 'comics' | 'nsfw'

interface FolderState {
  path: string // saved
  env: string // from the *_DIR variable
  envName?: string // which variable (an old name is still read)
  fallback: string // the default when neither is set
  inUse: string
}

const LABEL: Record<ExtraKind, () => string> = {
  anime: () => t('Anime folder'),
  comics: () => t('Comics folder'),
  nsfw: () => t('NSFW 18+ folder'),
}
const NOTE: Record<ExtraKind, () => string> = {
  anime: () => t('New anime shows go here. Add it to Jellyfin, Emby or Plex as its own library.'),
  comics: () => t('Downloaded and imported comics go here, a folder per series. Point Komga or Kavita at it.'),
  nsfw: () => t('NSFW 18+ downloads go here. Files in it are never overwritten or deleted.'),
}
const EXAMPLE: Record<ExtraKind, string> = { anime: '/data/anime', comics: '/data/comics', nsfw: '/data/nsfw' }
const VARIABLE: Record<ExtraKind, string> = { anime: 'ANIME_DIR', comics: 'COMICS_DIR', nsfw: 'NSFW_DIR' }

export default function ExtraFolder({ kind }: { kind: ExtraKind }) {
  const toast = useToast()
  const [st, setSt] = useState<FolderState | null>(null)
  const [path, setPath] = useState('')
  useEffect(() => {
    fetch(`/api/home/folders/${kind}`, { credentials: 'include' })
      .then((r) => (r.ok ? (r.json() as Promise<FolderState>) : null))
      .then((d) => {
        if (!d) return
        setSt(d)
        setPath(d.path)
      })
      .catch(() => undefined)
  }, [kind])
  if (!st) return null
  const save = async () => {
    const r = await fetch(`/api/home/folders/${kind}`, { method: 'PUT', credentials: 'include', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ path }) })
    const d = (await r.json().catch(() => ({}))) as FolderState & { error?: string }
    if (!r.ok) {
      toast.error(d.error ?? t('The folder couldn’t be saved.'))
      return
    }
    setSt(d)
    setPath(d.path)
    toast.success(t('{name} saved.', { name: LABEL[kind]() }))
  }
  // Left empty: say what is used instead.
  const unsetNote = st.env
    ? t('Using {path}, from {variable}.', { path: st.env, variable: st.envName || VARIABLE[kind] })
    : st.fallback
      ? t('Using {path}. Type another folder to change it.', { path: st.fallback })
      : kind === 'anime'
        ? t('Optional. Without one, anime goes in the TV folder.')
        : t('Not set yet. Type the folder inside the container, such as {example}.', { example: EXAMPLE[kind] })
  return (
    <div className="path-col">
      <label>
        {LABEL[kind]()}
        <input value={path} onChange={(e) => setPath(e.target.value)} placeholder={st.env || st.fallback || EXAMPLE[kind]} spellCheck={false} />
      </label>
      {path.trim() ? <FolderStatus path={path} /> : st.inUse ? <FolderStatus path={st.inUse} /> : null}
      {!path.trim() && <small className="path-note">{unsetNote}</small>}
      <div>
        <button className="btn-sm" onClick={() => void save()} disabled={path.trim() === st.path}>
          {t('Save')}
        </button>
      </div>
      <small className="path-note">{NOTE[kind]()}</small>
    </div>
  )
}
