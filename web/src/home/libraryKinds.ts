// Home fork: whether the Comics and NSFW 18+ kinds show in the Library. Kept
// apart from their pages so the Library doesn't load those pages to ask.
import { useEffect, useState } from 'react'
import { comicsApi } from './comicsApi'
import { scenesApi } from './scenesApi'

export function useComicsOn(admin: boolean): boolean {
  const [on, setOn] = useState(false)
  useEffect(() => {
    if (admin) {
      setOn(true)
      return
    }
    comicsApi
      .enabled()
      .then((r) => setOn(r.enabled))
      .catch(() => setOn(false))
  }, [admin])
  return on
}

export function useScenesOn(admin: boolean): boolean {
  const [on, setOn] = useState(false)
  useEffect(() => {
    if (!admin) {
      // An account given the NSFW 18+ switch (the server answers 403 otherwise).
      fetch('/api/scenes/enabled', { credentials: 'include' })
        .then((r) => (r.ok ? r.json() : { enabled: false }))
        .then((d: { enabled: boolean }) => setOn(d.enabled))
        .catch(() => setOn(false))
      return
    }
    scenesApi.settings().then((s) => setOn(s.enabled)).catch(() => setOn(false))
  }, [admin])
  return on
}
