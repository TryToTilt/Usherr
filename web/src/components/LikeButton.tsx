import { useEffect, useState } from 'react'
import { api } from '../api'
import Icon from './Icon'
import { t } from '../i18n'

// Usherr: the heart for a track, album or artist. The account's likes are read
// once and shared by every heart on the page.

type Kind = 'track' | 'album' | 'artist'

let likes: Promise<Record<Kind, Set<number>>> | null = null
const listeners = new Set<() => void>()

function load(): Promise<Record<Kind, Set<number>>> {
  if (!likes) {
    likes = api
      .musicFavorites()
      .then((f) => ({ track: new Set(f.tracks), album: new Set(f.albums), artist: new Set(f.artists) }))
      .catch(() => ({ track: new Set<number>(), album: new Set<number>(), artist: new Set<number>() }))
  }
  return likes
}

export default function LikeButton({ kind, id, className = 'mp-btn', size = 17 }: { kind: Kind; id: number; className?: string; size?: number }) {
  const [liked, setLiked] = useState(false)
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    let live = true
    const read = () => void load().then((l) => live && setLiked(l[kind].has(id)))
    read()
    listeners.add(read)
    return () => {
      live = false
      listeners.delete(read)
    }
  }, [kind, id])

  async function toggle() {
    const next = !liked
    setBusy(true)
    setLiked(next)
    try {
      await api.setMusicFavorite(kind, id, next)
      const l = await load()
      if (next) l[kind].add(id)
      else l[kind].delete(id)
      listeners.forEach((f) => f())
    } catch {
      setLiked(!next)
    } finally {
      setBusy(false)
    }
  }

  const label = liked ? t('Unlike') : t('Like')
  return (
    <button type="button" className={`${className}${liked ? ' on' : ''}`} onClick={() => void toggle()} disabled={busy} aria-pressed={liked} aria-label={label} title={label}>
      <Icon name="heart" size={size} />
    </button>
  )
}
