// Home fork: the taste engine, from the page's side. Your taste in short,
// "For you"'s reasons, a title's match, and matches for the Library's
// "For you" order. All of it is yours only.
import { useEffect, useState } from 'react'
import { t } from '../i18n'

export type TasteKind = 'movie' | 'tv'

export interface TasteTag {
  name: string
  type: 'genre' | 'keyword' | 'cast' | 'maker' | 'decade'
}
export interface Taste {
  ready: boolean
  liked: number
  disliked: number
  watched: number
  likes: TasteTag[]
  dislikes: TasteTag[]
}
export interface TasteWhy {
  match: number
  because?: string
  via?: 'liked' | 'watched'
  tags?: string[]
}

async function getJSON<T>(path: string, init?: RequestInit): Promise<T | null> {
  try {
    const r = await fetch(path, { credentials: 'include', ...init })
    return r.ok ? ((await r.json()) as T) : null
  } catch {
    return null
  }
}

// whyText is a match and its reason in a line: "92% match · because you
// liked Knives Out".
export function whyText(w: TasteWhy): string {
  const m = t('{n}% match', { n: w.match })
  if (w.because && w.via === 'liked') return `${m} · ${t('because you liked {title}', { title: w.because })}`
  if (w.because) return `${m} · ${t('because you watched {title}', { title: w.because })}`
  if (w.tags?.length) return `${m} · ${w.tags.join(', ')}`
  return m
}

export function useTaste(kind: TasteKind): Taste | null {
  const [taste, setTaste] = useState<Taste | null>(null)
  useEffect(() => {
    setTaste(null)
    void getJSON<Taste>(`/api/home/taste?kind=${kind}`).then(setTaste)
  }, [kind])
  return taste
}

// useForYouWhy is each "For you" title's line, by TMDB id.
export function useForYouWhy(kind: TasteKind, on: boolean): Record<number, string> {
  const [why, setWhy] = useState<Record<number, string>>({})
  useEffect(() => {
    if (!on) return
    void getJSON<{ why: Record<string, TasteWhy> }>(`/api/home/taste/why?kind=${kind}`).then((d) => {
      const out: Record<number, string> = {}
      for (const [id, w] of Object.entries(d?.why ?? {})) out[Number(id)] = whyText(w)
      setWhy(out)
    })
  }, [kind, on])
  return why
}

// useTitleMatch is your match for one title (null until known, or when
// there's none: no taste yet, or you voted on it).
export function useTitleMatch(kind: TasteKind, tmdbId: number, vote: number): TasteWhy | null {
  const [why, setWhy] = useState<TasteWhy | null>(null)
  useEffect(() => {
    setWhy(null)
    if (!tmdbId || vote) return
    let live = true
    void getJSON<TasteWhy>(`/api/home/taste/title?kind=${kind}&id=${tmdbId}`).then((w) => live && setWhy(w?.match ? w : null))
    return () => {
      live = false
    }
  }, [kind, tmdbId, vote])
  return why
}

export interface TasteScores {
  scores: Record<number, number>
  seen: Set<number> // watched: part of your taste, so they always fit
}
const NO_SCORES: TasteScores = { scores: {}, seen: new Set() }

// useTasteScores is your match for each of these titles, while on.
export function useTasteScores(kind: TasteKind, ids: number[] | null): TasteScores {
  const [scores, setScores] = useState<TasteScores>(NO_SCORES)
  const key = ids ? `${kind}:${ids.length}:${ids.slice(0, 50).join(',')}` : ''
  useEffect(() => {
    if (!ids) return
    void getJSON<{ scores: Record<string, number>; seen: number[] }>('/api/home/taste/scores', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ kind, ids }),
    }).then((d) => {
      const out: Record<number, number> = {}
      for (const [id, m] of Object.entries(d?.scores ?? {})) out[Number(id)] = m
      setScores({ scores: out, seen: new Set(d?.seen ?? []) })
    })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key])
  return scores
}
