// Home fork: Discover has the same tabs as the Library, in the same order:
// Movies, TV, Anime, Ebooks, Audiobooks, Music, Comics, NSFW 18+ (no All tab).
// Anime gets its own rails from TMDB; Comics shows the week's new issues and
// NSFW the Scenes "Find" view (administrators only).
import { lazy, Suspense } from 'react'
import RecipeRails from './Recipes'
import type { AddTarget } from '../components/AddDialog'
import DiscoverRail from '../components/DiscoverRail'
import type { GlyphKind } from '../components/KindGlyph'
import { sourceTitle, sourceToQuery, type ListName, type Source } from '../discoverSources'
import { t } from '../i18n'
import { useModules } from '../ModulesContext'
import type { Owned } from '../useOwned'
import { useComicsOn, useScenesOn } from './libraryKinds'

export type DTab = 'movie' | 'tv' | 'anime' | 'ebook' | 'audiobook' | 'music' | 'comics' | 'nsfw'

export const DTAB_INFO: Record<DTab, { label: () => string; glyph: GlyphKind; color: string }> = {
  movie: { label: () => t('Movies'), glyph: 'movie', color: 'var(--c-movie)' },
  tv: { label: () => t('TV'), glyph: 'tv', color: 'var(--c-tv)' },
  anime: { label: () => t('Anime'), glyph: 'tv', color: 'var(--c-tv)' },
  ebook: { label: () => t('Ebooks'), glyph: 'book', color: 'var(--c-book)' },
  audiobook: { label: () => t('Audiobooks'), glyph: 'audiobook', color: 'var(--c-audiobook)' },
  music: { label: () => t('Music'), glyph: 'music', color: 'var(--c-music)' },
  comics: { label: () => t('Comics'), glyph: 'book', color: 'var(--c-comics)' },
  nsfw: { label: () => t('NSFW 18+'), glyph: 'movie', color: 'var(--c-nsfw)' },
}

// useDiscoverTabs is the tabs to show, in the Library's order.
export function useDiscoverTabs(admin: boolean): DTab[] {
  const { on } = useModules()
  const comicsOn = useComicsOn(admin)
  const nsfwOn = useScenesOn(admin)
  const tabs: DTab[] = []
  if (on('movies')) tabs.push('movie')
  if (on('tv')) tabs.push('tv', 'anime')
  if (on('ebooks')) tabs.push('ebook')
  if (on('audiobooks')) tabs.push('audiobook')
  if (on('music')) tabs.push('music')
  if (comicsOn) tabs.push('comics')
  if (nsfwOn) tabs.push('nsfw')
  if (tabs.length === 0) tabs.push('movie')
  return tabs
}

// asTab reads a tab from the address; older addresses said "book" and "all".
export function asTab(v: string | null | undefined, tabs: DTab[]): DTab {
  const k = v === 'book' ? 'ebook' : v
  return tabs.includes(k as DTab) ? (k as DTab) : tabs[0]
}

const ANIME_LISTS: ListName[] = ['anime-airing', 'anime-popular', 'anime-new', 'anime-top']
const ANIME_HINT: Partial<Record<ListName, string>> = { 'anime-airing': 'new episodes this week', 'anime-new': 'started in the last year' }

export function AnimeRails({ owned, onAdd }: { owned: Owned; onAdd: (t: AddTarget) => void }) {
  return (
    <>
      <RecipeRails tab="anime" owned={owned} onAdd={onAdd} />
      {ANIME_LISTS.map((list) => {
        const src: Source = { kind: 'tv', list }
        const hint = list === 'anime-airing' ? t('new episodes this week') : list === 'anime-new' ? t('started in the last year') : ANIME_HINT[list]
        return <DiscoverRail key={sourceToQuery(src)} title={sourceTitle(src)} hint={hint} source={src} kind="tv" owned={owned} onAdd={onAdd} rows={2} />
      })}
    </>
  )
}

const ComicWeek = lazy(() => import('./ComicWeek'))
const SceneFind = lazy(() => import('./Scenes').then((m) => ({ default: m.FindTab })))

export function ComicsDiscover({ admin }: { admin: boolean }) {
  return (
    <div className="discover-comics">
      <Suspense fallback={<div className="skeleton" style={{ height: 200 }} />}>
        <ComicWeek admin={admin} />
      </Suspense>
    </div>
  )
}

export function ScenesDiscover() {
  return (
    <Suspense fallback={<div className="skeleton" style={{ height: 200 }} />}>
      <SceneFind />
    </Suspense>
  )
}
