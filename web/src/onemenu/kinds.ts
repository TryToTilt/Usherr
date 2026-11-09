// Usherr Broadcast: the kinds of media in ONE order with ONE set of names,
// for every place that lists them (the menu's Library, the Library's kind
// picker, the picker on the Comics, Manga and NSFW pages, Discover). Before,
// there were three orders and "TV" next to "TV shows". Classic and Neu keep
// their own lists exactly as they were: broadcastKinds hands them back as is.
//
// No React in this file, so it can be tested on its own.

import type { GlyphKind } from '../components/KindGlyph'
import { t } from '../i18n.ts'
import { broadcastOn } from './broadcast.ts'

export type KindKey = 'movie' | 'tv' | 'anime' | 'music' | 'ebook' | 'audiobook' | 'manga' | 'comics' | 'nsfw'

// Movies, TV shows, Anime, Music, Books (ebooks, then audiobooks), Manga,
// Comics; NSFW 18+ last, only where it's allowed and only on its own page.
export const KIND_ORDER: readonly KindKey[] = ['movie', 'tv', 'anime', 'music', 'ebook', 'audiobook', 'manga', 'comics', 'nsfw']

export interface KindInfo {
  label: string
  glyph: GlyphKind
  color: string
}

export function kindInfo(k: KindKey): KindInfo {
  switch (k) {
    case 'movie':
      return { label: t('Movies'), glyph: 'movie', color: 'var(--c-movie)' }
    case 'tv':
      return { label: t('TV shows'), glyph: 'tv', color: 'var(--c-tv)' }
    case 'anime':
      return { label: t('Anime'), glyph: 'tv', color: 'var(--c-tv)' }
    case 'music':
      return { label: t('Music'), glyph: 'music', color: 'var(--c-music)' }
    case 'ebook':
      return { label: t('Ebooks'), glyph: 'book', color: 'var(--c-book)' }
    case 'audiobook':
      return { label: t('Audiobooks'), glyph: 'audiobook', color: 'var(--c-audiobook)' }
    case 'manga':
      return { label: t('Manga'), glyph: 'manga', color: 'var(--c-manga)' }
    case 'comics':
      return { label: t('Comics'), glyph: 'comic', color: 'var(--c-comics)' }
    case 'nsfw':
      return { label: t('NSFW 18+'), glyph: 'movie', color: 'var(--c-nsfw)' }
  }
}

const rank = (key: string) => {
  const i = (KIND_ORDER as readonly string[]).indexOf(key)
  return i < 0 ? KIND_ORDER.length : i
}

// sortKinds puts kinds in the one order; anything unknown keeps its place at the end.
export function sortKinds<T extends { key: string }>(list: readonly T[]): T[] {
  return list
    .map((o, i) => ({ o, i }))
    .sort((a, b) => rank(a.o.key) - rank(b.o.key) || a.i - b.i)
    .map((x) => x.o)
}

// broadcastKinds: in Broadcast, a picker's options in the one order with the
// one set of names (each keeps its count). In any other look, unchanged.
export function broadcastKinds<T extends { key: string; label: string; glyph: GlyphKind; color: string }>(options: readonly T[], on = broadcastOn()): T[] {
  if (!on) return [...options]
  return sortKinds(options).map((o) => ((KIND_ORDER as readonly string[]).includes(o.key) ? { ...o, ...kindInfo(o.key as KindKey) } : o))
}
