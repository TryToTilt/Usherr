// Home fork: the Library's tab row on the pages that live inside the Library
// but have pages of their own (Comics, NSFW 18+), so every kind of media is
// one click away from every other, as on the Library page itself. Drawn like
// the Library's own kind tabs (colour and icon per kind).
import { useNavigate } from 'react-router-dom'
import { KindPicker } from '../components/CompactBar' // Usherr
import { isAdmin } from '../api'
import { useAuth } from '../AuthContext'
import { t } from '../i18n'
import type { GlyphKind } from '../components/KindGlyph'
import { useModules } from '../ModulesContext'
import { useComicsOn, useNsfwOn } from './libraryKinds'
import './library.css'

type Tab = { to: string; label: string; glyph: GlyphKind; color: string; key: string }

export default function LibraryTabs({ current }: { current: 'comics' | 'nsfw' }) {
  const admin = isAdmin(useAuth().user)
  const navigate = useNavigate()
  const { on } = useModules()
  const comicsOn = useComicsOn(admin)
  const nsfwOn = useNsfwOn(admin)
  const tabs: Tab[] = [
    ...(on('movies') ? [{ key: 'movie', to: '/library?kind=movie', label: t('Movies'), glyph: 'movie' as const, color: 'var(--c-movie)' }] : []),
    ...(on('tv')
      ? [
          { key: 'tv', to: '/library?kind=tv', label: t('TV'), glyph: 'tv' as const, color: 'var(--c-tv)' },
          { key: 'anime', to: '/library?kind=anime', label: t('Anime'), glyph: 'tv' as const, color: 'var(--c-tv)' },
        ]
      : []),
    ...(on('ebooks') ? [{ key: 'ebook', to: '/library?kind=ebook', label: t('Ebooks'), glyph: 'book' as const, color: 'var(--c-book)' }] : []),
    ...(on('audiobooks') ? [{ key: 'audiobook', to: '/library?kind=audiobook', label: t('Audiobooks'), glyph: 'audiobook' as const, color: 'var(--c-audiobook)' }] : []),
    ...(on('music') ? [{ key: 'music', to: '/library?kind=music', label: t('Music'), glyph: 'music' as const, color: 'var(--c-music)' }] : []),
    ...(comicsOn ? [{ key: 'comics', to: '/comics', label: t('Comics'), glyph: 'comic' as const, color: 'var(--c-comics)' }] : []),
    ...(nsfwOn ? [{ key: 'nsfw', to: '/nsfw', label: t('NSFW 18+'), glyph: 'movie' as const, color: 'var(--c-nsfw)' }] : []),
  ]
  return (
    // Usherr: the same drop-down as the Library's (CompactBar.tsx)
    <KindPicker label={t('Library')} options={tabs.map((tab) => ({ key: tab.key, label: tab.label, glyph: tab.glyph, color: tab.color }))} value={current} onChange={(k) => navigate(tabs.find((tab) => tab.key === k)?.to ?? '/library')} />
  )
}
