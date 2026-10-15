// Home fork: the Library's tab row on the pages that live inside the Library
// but have pages of their own (Comics, NSFW 18+), so every kind of media is
// one click away from every other, as on the Library page itself. Drawn like
// the Library's own kind tabs (colour and icon per kind).
import { Link } from 'react-router-dom'
import { isAdmin } from '../api'
import { useAuth } from '../AuthContext'
import { t } from '../i18n'
import KindGlyph, { type GlyphKind } from '../components/KindGlyph'
import { useModules } from '../ModulesContext'
import { useComicsOn, useScenesOn } from './libraryKinds'
import './library.css'

type Tab = { to: string; label: string; glyph: GlyphKind; color: string; key: string }

export default function LibraryTabs({ current }: { current: 'comics' | 'nsfw' }) {
  const admin = isAdmin(useAuth().user)
  const { on } = useModules()
  const comicsOn = useComicsOn(admin)
  const nsfwOn = useScenesOn(admin)
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
    ...(comicsOn ? [{ key: 'comics', to: '/comics', label: t('Comics'), glyph: 'book' as const, color: 'var(--c-comics)' }] : []),
    ...(nsfwOn ? [{ key: 'nsfw', to: '/scenes', label: t('NSFW 18+'), glyph: 'movie' as const, color: 'var(--c-nsfw)' }] : []),
  ]
  return (
    <nav className="seg kind-tabs" aria-label={t('Library')}>
      {tabs.map((tab) => (
        <Link key={tab.key} to={tab.to} className={tab.key === current ? 'active' : ''} aria-current={tab.key === current ? 'page' : undefined} style={{ ['--kc' as string]: tab.color }}>
          <KindGlyph kind={tab.glyph} size={17} />
          {tab.label}
        </Link>
      ))}
    </nav>
  )
}
