import { BRAND, USAGE_STATS } from '../brand' // Usherr
import TourGate from '../home/tourGate' // home fork
import SmhGate, { SmhButton } from '../smh/SmhGate' // Usherr: Show Me How
import { MENU_HIDE_EVENT, menuHideSeconds } from './menuHide' // Usherr: how soon the menu slides away
import { useMoreBelow } from './useMoreBelow'
import { usePauseWhileScrolling } from './usePauseWhileScrolling'
import { MiniPlayer, MusicPlayerProvider } from './MusicPlayer'
import { Fragment, useCallback, useEffect, useRef, useState, Suspense } from 'react'
import { Link, NavLink, Outlet, useLocation, useNavigate, useNavigationType } from 'react-router-dom'
import { scrollToTop } from '../scrollTop'
import { api, isAdmin, type AppLink, type WatchLink } from '../api'
import { useAuth } from '../AuthContext'
import { demoEnabled } from '../demo'
import { titleFor, useSpecificTitle } from '../documentTitle'
import { sectionFor, type SectionKey } from '../sections'
import { inPage, pageParts, pageTitle, settingsGroupsFor } from '../settingsNav'
import { useLive } from '../useLive'
import { useUnreadErrors } from '../useProblems'
import { useModules } from '../ModulesContext'
import { libraryEntries, type LibraryTab } from '../libraryTabs'
import { useComicsOn, useNsfwOn, useSportsOn } from '../home/libraryKinds' // home fork
import { afterClick, afterKey, type NavGroup, type OpenGroup } from '../navGroups'
import BackToTop from './BackToTop'
import BrandMark from './BrandMark'
import PageLoading from './PageLoading'
import FeedbackNudge from './FeedbackNudge'
import ImportBanner from './ImportBanner'
import Icon, { type IconName } from './Icon'
import KindGlyph from './KindGlyph'
import ProfileMenu from './ProfileMenu'
import ReportHost from './ReportHost'
import SideTools from './SideTools'
import { PlayerHost } from '../home/Player' // play hook (home fork)
import '../home/look.css' // home fork: its pages in the v3 look
import OmniBar, { type OmniPage } from '../home/OmniBar' // Usherr: the header's search is the search and AI bar
import './navDrawer.css' // Usherr: the menu as a drawer at every width
import RouletteButton from '../home/RouletteButton' // Usherr
import { TonightButton } from '../home/Tonight' // Usherr
import { ShowtimeBookingHost, ShowtimePill } from '../home/ShowtimePill' // Usherr: Showtime from any page
import { CalendarHeaderSearch } from '../home/CalendarSearch' // Usherr
import ThemeToggle from './ThemeToggle'
import UsageStatsPrompt from './UsageStats'
import WhatsNew from './WhatsNew'
import { t, tc } from '../i18n'
import { useGuideShortcut } from '../guide/shortcut.ts'
import AmbientBackdrop from './AmbientBackdrop'
import SideStack from '../home/SideStack' // Usherr: your stack, under Settings

const NAV: { to: string; label: string; icon: IconName; sec: SectionKey; end?: boolean }[] = [
  { to: '/', label: t('Dashboard'), icon: 'grid', sec: 'dashboard', end: true },
  { to: '/discover', label: t('Discover'), icon: 'compass', sec: 'discover' },
  { to: '/library', label: t('Library'), icon: 'film', sec: 'library' },
  { to: '/calendar', label: t('Upcoming'), icon: 'calendar', sec: 'calendar' },
  { to: '/showtime', label: t('Showtime'), icon: 'clock', sec: 'live' }, // Usherr: Showtimes at home, the screen and the bookings
  { to: '/sports', label: t('Sports'), icon: 'trophy', sec: 'live' }, // Usherr: Sports
  { to: '/tv', label: t('Live TV'), icon: 'tv', sec: 'live' }, // Mediarium TV (home fork): the one kind of media outside the Library
  { to: '/marquee/manage', label: t('Marquee'), icon: 'monitor', sec: 'live' }, // Usherr: the lobby display (administrators)
  { to: '/queue', label: t('Activity'), icon: 'activity', sec: 'activity' },
  { to: '/settings/media', label: t('Settings'), icon: 'sliders', sec: 'settings' },
]

// The lines under "Library" in the left menu: what each one is called, the glyph
// it wears and its colour (the same as the Library's tabs).
const LIBRARY_LINE: Record<LibraryTab, { label: string; glyph: 'movie' | 'tv' | 'music' | 'book' | 'audiobook' | 'comic'; color: string }> = {
  movie: { label: t('Movies'), glyph: 'movie', color: 'var(--c-movie)' },
  tv: { label: t('TV shows'), glyph: 'tv', color: 'var(--c-tv)' },
  music: { label: t('Music'), glyph: 'music', color: 'var(--c-music)' },
  ebook: { label: t('Ebooks'), glyph: 'book', color: 'var(--c-book)' },
  audiobook: { label: t('Audiobooks'), glyph: 'audiobook', color: 'var(--c-audiobook)' },
  anime: { label: t('Anime'), glyph: 'tv', color: 'var(--c-tv)' }, // Usherr
  comics: { label: t('Comics'), glyph: 'comic', color: 'var(--c-comics)' }, // Usherr
  nsfw: { label: t('NSFW 18+'), glyph: 'movie', color: 'var(--c-nsfw)' }, // Usherr
}

// Where Back goes from a detail page that was opened directly; undefined
// for pages that aren't a single title.
function backFallback(path: string): string | undefined {
  if (path.startsWith('/title/') || path.startsWith('/show/') || path.startsWith('/person/')) return '/discover'
  if (path.startsWith('/series/')) return '/library?kind=tv'
  if (path.startsWith('/book/') || path.startsWith('/books/work/')) return '/library?kind=ebook'
  if (path.startsWith('/music/artist/') || path.startsWith('/music/album/')) return '/library?kind=music'
  if (path.startsWith('/music/release/')) return '/discover?kind=music'
  if (path.startsWith('/music/playlists/')) return '/music/playlists'
  if (path === '/music/playlists') return '/library?kind=music'
  return undefined
}

// Persistent left sidebar plus an always-visible search bar that looks up
// movies and TV shows live as you type. On phones the sidebar slides in from
// the left.

// home fork: where each page was left, for the session, by its address.
const MAIN_PAGES = new Set(['/', '/discover', '/library', '/comics', '/nsfw', '/calendar', '/queue', '/music/playlists', '/upcoming', '/sports'])
const spotKey = (path: string, search: string) => `page:${path}${search}`
function spotOf(key: string): number {
  try {
    return Number(sessionStorage.getItem(key)) || 0
  } catch {
    return 0
  }
}
function keepSpot(key: string, y: number) {
  try {
    if (y > 0) sessionStorage.setItem(key, String(Math.round(y)))
    else sessionStorage.removeItem(key)
  } catch {
    // remembering is optional
  }
}

// Usherr: a moment to reach the menu after opening it, before "right away" can close it.
const MENU_GRACE_MS = 1500

export default function AppShell() {
  return (
    <MusicPlayerProvider>
      <Shell />
      <TourGate /> {/* home fork: the first-visit tour */}
      <SmhGate /> {/* Usherr: Show Me How's menu and coach card */}
    </MusicPlayerProvider>
  )
}

function Shell() {
  const [menuOpen, setMenuOpen] = useState(false)
  // A small, hideable link to support the project (administrators only). Hidden per browser.
  const [showSupport] = useState(() => {
    try {
      return localStorage.getItem('mediarium-hide-support') !== '1'
    } catch {
      return true
    }
  })
  const [active, setActive] = useState(0)
  const location = useLocation()
  const [homes, setHomes] = useState<WatchLink[]>([])
  const [apps, setApps] = useState<AppLink[]>([])
  useEffect(() => {
    api.mediaServerHomes().then(setHomes).catch(() => setHomes([]))
    api.listApps().then(setApps).catch(() => setApps([]))
  }, [])
  // A new page opens at the top. Back puts the page where it was left (a long list you were scrolling), and a change that only
  // rewrites the address in place (picking a filter, typing in a search box) leaves the page where it is.
  const navType = useNavigationType()
  const scrolled = useRef(new Map<string, number>())
  const lastPath = useRef(location.pathname)
  useEffect(() => {
    const el = document.querySelector('.content')
    if (!el) return
    const key = location.key
    // home fork: the spot is also kept for the session by address, so a main page opens where you left it
    const spot = spotKey(location.pathname, location.search)
    let pending = 0
    const onScroll = () => {
      scrolled.current.set(key, el.scrollTop)
      window.clearTimeout(pending)
      pending = window.setTimeout(() => {
        keepSpot(spot, el.scrollTop)
        keepSpot(spotKey(location.pathname, ''), el.scrollTop) // the menu opens a page without its tab in the address
      }, 150)
    }
    el.addEventListener('scroll', onScroll, { passive: true })
    return () => {
      window.clearTimeout(pending)
      el.removeEventListener('scroll', onScroll)
    }
  }, [location.key, location.pathname, location.search])
  useEffect(() => {
    const samePath = lastPath.current === location.pathname
    lastPath.current = location.pathname
    // home fork: main pages (Dashboard, Discover, Library…) also come back to their spot when opened from the menu
    if (navType === 'POP' || (!samePath && MAIN_PAGES.has(location.pathname))) {
      const y = (navType === 'POP' ? scrolled.current.get(location.key) : undefined) ?? (spotOf(spotKey(location.pathname, location.search)) || spotOf(spotKey(location.pathname, '')))
      if (y === 0) return scrollToTop()
      // The list may still be loading, so keep trying for a moment until the page is tall enough.
      const el = document.querySelector('.content')
      let tries = 0
      let timer = 0
      const tick = () => {
        el?.scrollTo({ top: y, behavior: 'auto' })
        if (el && Math.abs(el.scrollTop - y) > 2 && tries++ < 25) timer = window.setTimeout(tick, 120)
      }
      tick()
      return () => window.clearTimeout(timer)
    }
    if (navType === 'PUSH' || !samePath) scrollToTop()
  }, [location.key, location.pathname, navType])
  // On a phone the kind tabs scroll sideways: the open one is brought into view (sideways only, the page stays where it is).
  useEffect(() => {
    const timer = window.setTimeout(() => {
      for (const tabs of document.querySelectorAll<HTMLElement>('.kind-tabs')) {
        const on = tabs.querySelector<HTMLElement>(':scope > .active')
        if (!on || tabs.scrollWidth <= tabs.clientWidth + 2) continue
        const mine = on.getBoundingClientRect()
        const box = tabs.getBoundingClientRect()
        tabs.scrollLeft += mine.left - box.left - (tabs.clientWidth - mine.width) / 2
      }
    }, 150)
    return () => window.clearTimeout(timer)
  }, [location.key, location.search])
  const { user } = useAuth()
  const admin = isAdmin(user)
  // Members only have their own profile and About under Settings.
  const settingsGroups = settingsGroupsFor(admin, user?.role === 'viewer')
  const sportsOn = useSportsOn(admin) // Usherr
  const nav = NAV.filter((n) => (admin || n.to !== '/marquee/manage') && (sportsOn || n.to !== '/sports')).map((n) => (n.sec === 'settings' ? { ...n, to: settingsGroups[0].pages[0].to } : n)) // Usherr: the Marquee is for administrators
  const { on: moduleOn } = useModules()
  const ebooksOn = moduleOn('ebooks')
  const audiobooksOn = moduleOn('audiobooks')
  const musicOn = moduleOn('music')
  const comicsOn = useComicsOn(admin) // home fork
  const nsfwOn = useNsfwOn(admin) // home fork
  // home fork: the streamed sheet is inside the Library.
  const inLibraryPages = /^\/streamed(\/|$)/.test(location.pathname)
  // The lines under "Library": each kind that is switched on opens the Library on its tab.
  const libraryLines = libraryEntries(moduleOn, location.pathname, location.search, { comics: comicsOn, nsfw: nsfwOn })
  const inUpcoming = location.pathname.startsWith('/wanted') || location.pathname.startsWith('/calendar') || location.pathname.startsWith('/following') /* home fork */
  // Errors from the last day that nobody has marked as read (administrators only).
  const unreadErrors = useUnreadErrors(admin)
  const errorDot = <span className="nav-dot" role="img" aria-label={t('New errors')} title={t('New errors. See Settings, System, Logs and errors.')} />

  // Close the mobile drawer whenever the page changes (a Library line only changes the address's query).
  // Usherr: on a wider screen it stays open while you go from page to page.
  useEffect(() => {
    if (window.matchMedia('(max-width: 900px)').matches) setMenuOpen(false)
  }, [location.pathname, location.search])
  // Usherr: on a wider screen the open menu sits over the page without
  // dimming it. It stays while the mouse is over it, and slides away the
  // chosen time after the mouse leaves (Settings, General: right away to 10
  // seconds), or with the hamburger, or Escape.
  const [hideS, setHideS] = useState(menuHideSeconds)
  useEffect(() => {
    const read = () => setHideS(menuHideSeconds())
    window.addEventListener(MENU_HIDE_EVENT, read)
    return () => window.removeEventListener(MENU_HIDE_EVENT, read)
  }, [])
  useEffect(() => {
    if (!menuOpen || window.matchMedia('(max-width: 900px)').matches) return
    const nav = navRef.current
    if (!nav) return
    let timer = 0
    const arm = (ms: number) => {
      window.clearTimeout(timer)
      timer = window.setTimeout(() => setMenuOpen(false), ms)
    }
    const stay = () => window.clearTimeout(timer)
    const leave = () => arm(hideS * 1000)
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setMenuOpen(false)
    }
    // Opened from the hamburger, the mouse isn't on the menu yet.
    if (!nav.matches(':hover')) arm(Math.max(hideS * 1000, MENU_GRACE_MS))
    nav.addEventListener('pointerenter', stay)
    nav.addEventListener('pointerleave', leave)
    nav.addEventListener('focusin', stay)
    document.addEventListener('keydown', onKey)
    return () => {
      window.clearTimeout(timer)
      nav.removeEventListener('pointerenter', stay)
      nav.removeEventListener('pointerleave', leave)
      nav.removeEventListener('focusin', stay)
      document.removeEventListener('keydown', onKey)
    }
  }, [menuOpen, hideS])

  // On a phone the search is an icon that opens a bar over the header.
  const [searchOpen, setSearchOpen] = useState(false)
  useEffect(() => {
    if (!searchOpen) return
    document.querySelector<HTMLInputElement>('.top-search input')?.focus()
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && setSearchOpen(false)
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [searchOpen])
  useEffect(() => {
    setSearchOpen(false)
  }, [location.pathname])

  // The Settings entry always lists its groups, and one group at a time shows
  // its pages (an accordion). The group that holds the open settings page is
  // the open one; pressing another group opens it and closes the one before.
  // On any other page the open group folds up again; the list of groups stays.
  // Usherr: a settings page can live outside /settings (the Marquee, the pre-show): its group counts all the same.
  const currentGroup = settingsGroups.find((g) => g.pages.length > 1 && g.pages.some((p) => inPage(location.pathname, p.to)))?.key ?? null
  const [openKey, setOpenKey] = useState<string | null>(() => currentGroup)
  // Moving to a page of another group closes the one that was open: also to a page
  // that has no pages under it (Indexers & Search), and to any page outside Settings.
  useEffect(() => {
    setOpenKey(currentGroup)
  }, [currentGroup, location.pathname])

  const inSettings = location.pathname.startsWith('/settings') || currentGroup !== null // Usherr: and its pages elsewhere
  // Usherr: what the top bar's AI expects, from the page: the kind on Discover and the Library, Comics on its pages.
  const omniPage: OmniPage = (() => {
    if (location.pathname.startsWith('/comics')) return 'comics'
    if (location.pathname === '/discover' || location.pathname === '/library') {
      const k = new URLSearchParams(location.search).get('kind') ?? 'movie'
      return (['movie', 'tv', 'anime', 'ebook', 'audiobook', 'music', 'comics'] as const).find((p) => p === k) ?? 'dashboard'
    }
    return 'dashboard'
  })()
  const inLibrary = /^\/(library|comics|nsfw|streamed)(\/|$)/.test(location.pathname) // Usherr: its own Library pages too

  // The sub menus under Library and under Settings. Only a click on the group
  // opens one (and only one is open at a time); being on one of its pages or
  // changing page never does, and a reload starts with both closed. While a
  // group is closed, its row names the page you are on.
  const [openGroup, setOpenGroup] = useState<OpenGroup>(inLibrary ? 'library' : inSettings ? 'settings' : null)
  // Usherr: a group stays open on its own pages and folds up elsewhere, as before.
  useEffect(() => {
    setOpenGroup(inLibrary ? 'library' : inSettings ? 'settings' : null)
  }, [inLibrary, inSettings, location.pathname])
  const hereLine = libraryLines.find((l) => l.active)
  const here: Record<NavGroup, string> = {
    library: hereLine ? LIBRARY_LINE[hereLine.tab].label : '',
    settings: inSettings ? (settingsGroups.flatMap((g) => g.pages).find((p) => inPage(location.pathname, p.to))?.label ?? '') : '',
  }
  // Arrow keys inside an open sub menu: up and down move, Escape folds it up and goes back to its row.
  const subMenuKeys = (group: NavGroup) => (e: React.KeyboardEvent<HTMLElement>) => {
    const items = [...e.currentTarget.querySelectorAll<HTMLElement>('a[href], [role="button"]')].filter((el) => el.getClientRects().length > 0)
    const at = items.indexOf(document.activeElement as HTMLElement)
    const toHead = () => document.getElementById(`sn-head-${group}`)?.focus()
    if (e.key === 'ArrowDown' && at >= 0 && at < items.length - 1) {
      e.preventDefault()
      items[at + 1].focus()
    } else if (e.key === 'ArrowUp' && at > 0) {
      e.preventDefault()
      items[at - 1].focus()
    } else if (e.key === 'ArrowUp' && at === 0) {
      e.preventDefault()
      toHead()
    } else if (e.key === 'Escape') {
      e.preventDefault()
      e.stopPropagation()
      setOpenGroup(null)
      toHead()
    }
  }

  // The page name in the top bar, in the browser tab and (when the page has no
  // heading of its own) as the page's one <h1>.
  const heading = location.pathname.startsWith('/help') ? t('User guide') : pageTitle(location.pathname, admin)
  const parts = pageParts(location.pathname, admin)
  const headingText = parts.group ? `${parts.group} · ${parts.page}` : heading
  const headingNode = (
    <span className="top-text">
      {parts.group ? (
        <>
          <span className="top-crumb">{parts.group}</span>
          <span className="top-sep" aria-hidden="true"> · </span>
          {parts.page}
        </>
      ) : (
        heading
      )}
    </span>
  )
  useGuideShortcut()
  const specificTitle = useSpecificTitle()
  useEffect(() => {
    document.title = titleFor(specificTitle || heading)
  }, [specificTitle, heading])
  const mainRef = useRef<HTMLElement>(null)
  const navRef = useRef<HTMLElement>(null)
  const scrollRef = useRef<HTMLDivElement>(null)
  const moreBelow = useMoreBelow(scrollRef)
  usePauseWhileScrolling(mainRef)
  const menuButton = useRef<HTMLButtonElement>(null)
  // The open drawer (phones and tablets): Escape closes it, Tab stays inside
  // it, and focus goes back to the menu button when it closes.
  useEffect(() => {
    if (!menuOpen) return
    const nav = navRef.current
    if (!nav || !window.matchMedia('(max-width: 900px)').matches) return
    const items = () => [...nav.querySelectorAll<HTMLElement>('a[href], button:not([disabled]), input:not([disabled]), select:not([disabled])')].filter((el) => el.getClientRects().length > 0)
    // The first menu item, not the menu's own close button.
    ;(items().find((el) => el.matches('a[href]')) ?? items()[0])?.focus({ preventScroll: true })
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        setMenuOpen(false)
        menuButton.current?.focus({ preventScroll: true })
        return
      }
      if (e.key !== 'Tab') return
      const list = items()
      if (list.length === 0) return
      const first = list[0]
      const last = list[list.length - 1]
      const at = document.activeElement
      if (e.shiftKey && (at === first || !nav.contains(at))) {
        e.preventDefault()
        last.focus()
      } else if (!e.shiftKey && (at === last || !nav.contains(at))) {
        e.preventDefault()
        first.focus()
      }
    }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [menuOpen])
  // Usherr: a click or touch anywhere outside the open menu puts it away
  // (on a wide screen there's no backdrop to catch it). The hamburger
  // toggles it itself.
  useEffect(() => {
    if (!menuOpen) return
    const away = (e: PointerEvent) => {
      const at = e.target as Node | null
      if (!at || navRef.current?.contains(at) || menuButton.current?.contains(at)) return
      setMenuOpen(false)
    }
    document.addEventListener('pointerdown', away)
    return () => document.removeEventListener('pointerdown', away)
  }, [menuOpen])

  const [pageHasHeading, setPageHasHeading] = useState(false)
  // Title, show, book and artist pages get a Back button: it returns to
  // wherever the page was opened from, or to its section when it was opened
  // directly (a link from outside, a fresh tab).
  const navigate = useNavigate()
  const backTo = backFallback(location.pathname)
  const goBack = () => {
    const idx = (window.history.state as { idx?: number } | null)?.idx ?? 0
    if (idx > 0) navigate(-1)
    else if (backTo) navigate(backTo)
  }
  useEffect(() => {
    const el = mainRef.current
    if (!el) return
    const check = () => setPageHasHeading(el.querySelector('h1') !== null)
    check()
    const watch = new MutationObserver(check)
    watch.observe(el, { childList: true, subtree: true })
    return () => watch.disconnect()
  }, [location.pathname])
  const [version, setVersion] = useState('')
  // What's new goes first; the usage-numbers question waits until it is closed
  // (or has nothing to show), so the two never open on top of each other.
  const [notesDone, setNotesDone] = useState(false)
  const onNotesDone = useCallback(() => setNotesDone(true), [])
  useEffect(() => {
    api
      .version()
      .then((v) => setVersion(v.version))
      .catch(() => setNotesDone(true))
  }, [])
  // A dot on the version when a newer release is waiting (administrators only).
  const [updateReady, setUpdateReady] = useState(false)
  const checkUpdate = useCallback(() => {
    if (!admin) return
    api
      .updateLatest()
      .then((n) => setUpdateReady(n.available))
      .catch(() => undefined)
  }, [admin])
  useEffect(checkUpdate, [checkUpdate])
  useLive(checkUpdate, 10 * 60 * 1000, admin)

  // A small count of downloads in flight next to "Activity".
  const loadActive = useCallback(() => {
    api
      .queueCount()
      .then((c) => setActive(c.active))
      .catch(() => undefined)
  }, [])
  useEffect(loadActive, [loadActive])
  useLive(loadActive, 5000)

  return (
    <div className="app-shell nav-drawer" data-section={sectionFor(location.pathname)}>
      <a
        className="skip-link"
        href="#main"
        onClick={(e) => {
          e.preventDefault()
          mainRef.current?.focus()
        }}
      >
        {t('Skip to content')}
      </a>
      <PlayerHost /> {/* play hook (home fork) */}
      <ShowtimeBookingHost /> {/* Usherr: "Book a showtime" from a card */}
      <div className={`sidebar-backdrop${menuOpen ? ' open' : ''}`} onClick={() => setMenuOpen(false)} />
      <nav id="main-menu" ref={navRef} className={`sidebar${menuOpen ? ' open' : ''}`} aria-label={tc('menu', 'Main')}>
        <div className="brand">
          {/* Usherr: the open menu covers the top bar's hamburger, so it has its own, in the same place */}
          <button type="button" className="menu-button menu-close" onClick={() => setMenuOpen(false)} aria-controls="main-menu" aria-expanded={menuOpen} aria-label={t('Close menu')}>
            <Icon name="menu" size={20} />
          </button>
          <BrandMark />
          <span className="brand-text">
            <span className="brand-name">{BRAND}</span>
          </span>
        </div>
        <div className={`side-scroll${moreBelow ? ' more-below' : ''}`} ref={scrollRef}>
        {nav.map((n) => {
          const group: NavGroup | null = n.sec === 'library' || n.sec === 'settings' ? n.sec : null
          const isOpen = group !== null && openGroup === group
          const inside = group === 'library' ? inLibrary : inSettings
          // From another page a click goes there and opens the sub menu; on one of its pages it only folds it up or down.
          const press = () => {
            if (!group) return
            setOpenGroup(afterClick(openGroup, group, inside))
            if (!inside) navigate(n.to)
          }
          return (
          <Fragment key={n.to}>
          <NavLink
            to={n.to}
            end={n.end}
            id={group ? `sn-head-${group}` : undefined}
            aria-expanded={group ? isOpen : undefined}
            aria-controls={group ? `sn-${group}` : undefined}
            style={{ ['--nc' as string]: `var(--c-${n.sec})` }}
            className={({ isActive }) => (group === 'settings' ? (inSettings ? (isOpen ? 'active parent-open' : 'active') : '') : group === 'library' ? (isActive || inLibraryPages || libraryLines.some((l) => l.active) ? (isOpen ? 'active parent-open' : 'active') : '') : isActive || (n.sec === 'calendar' && inUpcoming) ? 'active' : '')}
            onClick={group ? (e) => {
              if (e.metaKey || e.ctrlKey || e.shiftKey || e.altKey || e.button !== 0) return // open in a new tab as usual
              if (inside) e.preventDefault()
              setOpenGroup(afterClick(openGroup, group, inside))
            } : undefined}
            onKeyDown={group ? (e) => {
              // A link answers Enter on its own; Space and the arrow keys are added here.
              if (e.key === ' ') {
                e.preventDefault()
                press()
                return
              }
              const next = afterKey(openGroup, group, e.key)
              if (next !== undefined) {
                e.preventDefault()
                setOpenGroup(next)
              } else if (e.key === 'ArrowDown' && isOpen) {
                e.preventDefault()
                document.querySelector<HTMLElement>(`#sn-${group} a[href]`)?.focus()
              }
            } : undefined}
          >
            <span className="nav-ico">
              <Icon name={n.icon} size={17} />
            </span>
            <span className="nav-label">{n.label}</span>
            {n.to === '/queue' && active > 0 && <span className="nav-count">{active}</span>}
            {group && !isOpen && here[group] && <span className="nav-here">{here[group]}</span>}
            {group && (
              <span className="nav-chev" style={{ transform: isOpen ? 'rotate(90deg)' : 'none' }}>
                <Icon name="open" size={12} />
              </span>
            )}
          </NavLink>
          {n.sec === 'library' && (
            <div className={`sn-collapse${isOpen ? ' open' : ''}`} id="sn-library" onKeyDown={subMenuKeys('library')}>
            <div className="sn-collapse-inner">
            <div className="subnav subnav-library" aria-label={t('Library')} role="group">
              {libraryLines.map((l) => {
                const line = LIBRARY_LINE[l.tab]
                return (
                  <Link key={l.tab} to={l.to} style={{ ['--sc' as string]: line.color }} className={l.active ? 'active' : ''} aria-current={l.active ? 'page' : undefined}>
                    <span className="nav-ico">
                      <KindGlyph kind={line.glyph} size={15} />
                    </span>
                    <span className="nav-label">{line.label}</span>
                  </Link>
                )
              })}
            </div>
            </div>
            </div>
          )}
          </Fragment>
          )
        })}
        {(
        <div className={`sn-collapse${openGroup === 'settings' ? ' open' : ''}`} id="sn-settings" onKeyDown={subMenuKeys('settings')}>
        <div className="sn-collapse-inner">
        <div className="subnav" style={{ ['--nc' as string]: 'var(--c-settings)' }}>
          {settingsGroups.map((g) => {
            const current = g.pages.some((p) => inPage(location.pathname, p.to))
            if (g.pages.length === 1) {
              return (
                <NavLink key={g.key} to={g.pages[0].to} style={{ ['--sc' as string]: g.color }} className={() => (current ? 'active' : '')}>
                  <span className="nav-ico">
                    <Icon name={g.icon} size={15} />
                  </span>
                  <span className="nav-label">{g.label}</span>
                </NavLink>
              )
            }
            const open = openKey === g.key
            // Opening a group goes to its first page (on a wide screen); pressing an open one folds it up, unless you're on one of its pages.
            const toggleGroup = () => {
              // Usherr: the group of the page you're on stays open, so its pages can be picked.
              if (open && current) return
              if (open) {
                setOpenKey(null)
                return
              }
              setOpenKey(g.key)
              // Usherr: on a phone the menu closes on a new page, so opening a group only shows its pages there.
              if (!g.pages.some((p) => inPage(location.pathname, p.to)) && !window.matchMedia('(max-width: 900px)').matches) navigate(g.pages[0].to)
            }
            return (
              <div key={g.key} className={`sn-group${open ? ' open' : ''}`} style={{ ['--sc' as string]: g.color }}>
                <NavLink
                  to={g.pages[0].to}
                  role="button"
                  aria-expanded={open}
                  aria-controls={`sn-pages-${g.key}`}
                  className={() => (current ? 'sn-head current' : 'sn-head')}
                  onClick={(e) => {
                    e.preventDefault()
                    toggleGroup()
                  }}
                  onKeyDown={(e) => {
                    // A link answers Enter on its own; a button also answers Space.
                    if (e.key === ' ') {
                      e.preventDefault()
                      toggleGroup()
                    }
                  }}
                >
                  <span className="nav-ico">
                    <Icon name={g.icon} size={15} />
                  </span>
                  <span className="nav-label">{g.label}</span>
                  {g.key === 'system' && unreadErrors > 0 && errorDot}
                  <span className="nav-chev" style={{ transform: open ? 'rotate(90deg)' : 'none' }}>
                    <Icon name="open" size={12} />
                  </span>
                </NavLink>
                {open && (
                  <div className="sn-pages" id={`sn-pages-${g.key}`}>
                    {g.pages.map((p) => (
                      <NavLink key={p.to} to={p.to} title={p.label} style={{ ['--sc' as string]: g.color }} className={() => (inPage(location.pathname, p.to) ? 'sn-page active' : 'sn-page')}>
                        <span className="sn-ico">
                          <Icon name={p.icon} size={13} />
                        </span>
                        <span className="nav-label">{p.label}</span>
                        {p.to === '/settings/logs' && unreadErrors > 0 && <span className="sn-count">{unreadErrors}</span>}
                      </NavLink>
                    ))}
                  </div>
                )}
              </div>
            )
          })}
        </div>
        </div>
        </div>
        )}
        <SideStack />
        <SideTools ebooksOn={ebooksOn} audiobooksOn={audiobooksOn} musicOn={musicOn} homes={homes} apps={apps} showSupport={admin && showSupport} variant="side">
          <ThemeToggle />
        </SideTools>
        </div>
        {version && (
          <div className="side-version">
            {admin ? (
              <NavLink to="/settings/system" aria-label={t('Version {version}', { version: `v${version}` })} title={updateReady ? t('Mediarium {version}. A newer version is available.', { version }) : `Mediarium ${version}`}>
                <BrandMark className="ver-mark" />
                v{version}
                {updateReady && <span className="version-dot" aria-label={t('A newer version is available')} />}
              </NavLink>
            ) : (
              <span aria-label={t('Version {version}', { version: `v${version}` })} title={`Mediarium ${version}`}>
                <BrandMark className="ver-mark" />
                v{version}
              </span>
            )}
          </div>
        )}
      </nav>
      <div className="main-area">
        <AmbientBackdrop />
        <header className="topbar">
          <button ref={menuButton} className="menu-button" aria-controls="main-menu" onClick={() => setMenuOpen((o) => !o)} aria-label={t('Open menu')} aria-expanded={menuOpen}>
            <Icon name="menu" size={20} />
          </button>
          {backTo && (
            <button type="button" className="back-btn" onClick={goBack} aria-label={t('Back')} title={t('Back')}>
              <Icon name="chevron-left" size={18} />
              <span>{t('Back')}</span>
            </button>
          )}
          {pageHasHeading ? <p className="top-title" title={headingText}>{headingNode}</p> : <h1 className="top-title" title={headingText}>{headingNode}</h1>}
          <div className={`top-search${searchOpen ? ' open' : ''}`}>
            {location.pathname === '/calendar' ? <CalendarHeaderSearch /> : <OmniBar key={omniPage} page={omniPage} header />} {/* Usherr: search or ask the AI, from any page, shaped by the page you are on; on the Calendar it searches the calendar */}
            <button type="button" className="icon-btn top-search-close" onClick={() => setSearchOpen(false)} aria-label={t('Close search')}>
              <Icon name="x" size={18} />
            </button>
          </div>
          <RouletteButton iconOnly className="icon-btn top-dice" /> {/* Usherr: Roulette */}
          <SmhButton /> {/* Usherr: Show Me How */}
          <ShowtimePill /> {/* Usherr: Showtime, counting down, from any page */}
          <TonightButton /> {/* Usherr: what to watch tonight */}
          <button type="button" className="icon-btn search-toggle" onClick={() => setSearchOpen(true)} aria-label={t('Search')}>
            <Icon name="search" size={19} />
          </button>
          <div className="top-actions">
            {demoEnabled() && (
              <span className="demo-chip" title={t('Sample data is showing. Turn it off from your profile menu.')}>
                <Icon name="flask" size={14} /> <span className="demo-chip-label">{t('Demo data')}</span>
              </span>
            )}
            <SideTools ebooksOn={ebooksOn} audiobooksOn={audiobooksOn} musicOn={musicOn} homes={homes} apps={apps} showSupport={admin && showSupport} variant="top" />
            <span className="top-theme">
              <ThemeToggle />
            </span>
            <ProfileMenu />
          </div>
        </header>
        <ImportBanner />
        <main id="main" className="content" ref={mainRef} tabIndex={-1}>
          <Suspense fallback={<PageLoading />}>
            <Outlet />
          </Suspense>
        </main>
        <BackToTop target={mainRef} />
        <MiniPlayer />
        {!demoEnabled() && <WhatsNew version={version} onDone={onNotesDone} />}
        {!demoEnabled() && admin && USAGE_STATS && notesDone && <UsageStatsPrompt />}
        <FeedbackNudge />
        <ReportHost />
      </div>
    </div>
  )
}
