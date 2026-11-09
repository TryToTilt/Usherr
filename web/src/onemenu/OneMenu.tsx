// Usherr Broadcast: the one menu (broadcast-plan.md, "The one menu"). Seven
// places, the same on a computer, a TV and a phone: Home, Find, Library,
// Live TV, Movie night, You and, for administrators, Server. Library unfolds
// into the kinds of media (one order, kinds.ts); Movie night, You and Server
// unfold while you're on one of their pages. AppShell draws it in Broadcast
// only; Classic keeps its drawer and the lobby layout its LobbyNav.
import { useState, type ReactNode } from 'react'
import { Link, NavLink, useLocation, useNavigate } from 'react-router-dom'
import Icon, { type IconName } from '../components/Icon'
import KindGlyph, { type GlyphKind } from '../components/KindGlyph'
import { isAdmin } from '../api'
import { useAuth } from '../AuthContext'
import { t } from '../i18n'
import type { LibraryEntry } from '../libraryTabs'
import { settingsGroupsFor } from '../settingsNav'
import { openSmhMenu } from '../smh/engine'
import { broadcastOn } from './broadcast'
import { inYourSettings, YOUR_SETTINGS } from './broadcastNav'
import { kindInfo, sortKinds, type KindKey } from './kinds'
import { chooseMenuMode, menuMode, type MenuMode } from './menuMode'
import './oneMenu.css'

const narrowScreen = () => window.matchMedia('(max-width: 900px)').matches

// Which of the seven places a page belongs to, so its entry stays lit.
export const inFind = (p: string) => /^\/(find|search|discover|person)(\/|$)/.test(p) || p.startsWith('/manga/discover')
export const inLibraryArea = (p: string) => !inFind(p) && /^\/(library|comics|manga|nsfw|streamed|sports|calendar|wanted|following|leaving-soon|series|title|show|book|books|music|read-to-me)(\/|$)/.test(p)
export const inMovieNight = (p: string) => /^\/(showtime|crews)(\/|$)/.test(p)
export const inYouArea = (p: string) => /^\/(you|help)(\/|$)/.test(p) || inYourSettings(p)
export function inServerPlace(p: string): boolean {
  if (inYourSettings(p)) return false
  return /^\/(server|settings|queue|import|marquee\/manage|marquee\/remote|preshow\/manage)(\/|$)/.test(p)
}
const comingUp = (p: string) => /^\/(calendar|wanted|following|leaving-soon)(\/|$)/.test(p)

interface Line {
  key: string
  to: string
  label: string
  glyph?: GlyphKind
  icon?: IconName
  color?: string
  active: boolean
}

// The Library's lines: the kinds that are on, in the one order, with ebooks
// and audiobooks as one "Books" line, then Sports and Coming up; NSFW 18+ on
// a line of its own after them (never among the kinds).
export function libraryLines(entries: LibraryEntry[], pathname: string, opts: { sportsOn: boolean; admin: boolean }): { kinds: Line[]; nsfw?: Line } {
  const kinds: Line[] = []
  const sorted = sortKinds(entries.map((e) => ({ ...e, key: e.tab })))
  const books = sorted.filter((e) => e.tab === 'ebook' || e.tab === 'audiobook')
  for (const e of sorted) {
    if (e.tab === 'nsfw') continue
    if (e.tab === 'audiobook' && books.length > 1) continue
    if (e.tab === 'ebook' || e.tab === 'audiobook') {
      const info = kindInfo(e.tab)
      kinds.push({ key: 'books', to: books[0].to, label: t('Books'), glyph: info.glyph, color: info.color, active: books.some((b) => b.active) })
      continue
    }
    const info = kindInfo(e.tab as KindKey)
    kinds.push({ key: e.tab, to: e.to, label: info.label, glyph: info.glyph, color: info.color, active: e.active })
  }
  if (opts.sportsOn) kinds.push({ key: 'sports', to: '/sports', label: t('Sports'), icon: 'trophy', active: pathname.startsWith('/sports') })
  kinds.push({ key: 'coming', to: '/calendar', label: t('Coming up'), icon: 'calendar', active: comingUp(pathname) })
  if (opts.admin) kinds.push({ key: 'streamed', to: '/streamed', label: t('Streamed'), icon: 'play', active: pathname.startsWith('/streamed') })
  const n = entries.find((e) => e.tab === 'nsfw')
  const nsfw = n ? { key: 'nsfw', to: n.to, label: kindInfo('nsfw').label, glyph: kindInfo('nsfw').glyph, color: kindInfo('nsfw').color, active: n.active } : undefined
  return { kinds, nsfw }
}

function SubLine({ l }: { l: Line }) {
  return (
    <Link to={l.to} style={l.color ? { ['--sc' as string]: l.color } : undefined} className={l.active ? 'active' : ''} aria-current={l.active ? 'page' : undefined} title={l.label}>
      <span className="nav-ico">{l.glyph ? <KindGlyph kind={l.glyph} size={15} /> : <Icon name={l.icon ?? 'film'} size={15} />}</span>
      <span className="nav-label">{l.label}</span>
    </Link>
  )
}

function Place({ to, icon, label, active, end, count, warn, id, group, open, onHead, children }: { to: string; icon: IconName; label: string; active?: boolean; end?: boolean; count?: number; warn?: boolean; id?: string; group?: boolean; open?: boolean; onHead?: (e: React.MouseEvent | React.KeyboardEvent) => void; children?: ReactNode }) {
  return (
    <>
      <NavLink
        to={to}
        end={end}
        id={id}
        title={label}
        aria-expanded={group ? !!open : undefined}
        className={({ isActive }) => `one-place${isActive || active ? ' active' : ''}${group && open ? ' parent-open' : ''}`}
        onClick={
          onHead
            ? (e) => {
                if (e.metaKey || e.ctrlKey || e.shiftKey || e.altKey || e.button !== 0) return // a new tab as usual
                onHead(e)
              }
            : undefined
        }
        onKeyDown={
          onHead
            ? (e) => {
                if (e.key === ' ') onHead(e)
              }
            : undefined
        }
      >
        <span className="nav-ico">
          <Icon name={icon} size={18} />
        </span>
        <span className="nav-label">{label}</span>
        {count !== undefined && count > 0 && <span className={`nav-count${warn ? ' one-warn' : ''}`}>{count}</span>}
        {group && (
          <span className="nav-chev" style={{ transform: open ? 'rotate(90deg)' : 'none' }}>
            <Icon name="open" size={12} />
          </span>
        )}
      </NavLink>
      {group && open && children && (
        <div className="one-sub" role="group" aria-label={label}>
          {children}
        </div>
      )}
    </>
  )
}

export function OneMenu({ entries, sportsOn, downloads, needs, serverMenu, tools }: { entries: LibraryEntry[]; sportsOn: boolean; downloads: number; needs: number; serverMenu: ReactNode; tools: ReactNode }) {
  const { pathname } = useLocation()
  const navigate = useNavigate()
  const { user } = useAuth()
  const admin = isAdmin(user)
  const lib = libraryLines(entries, pathname, { sportsOn, admin })
  const here = {
    library: inLibraryArea(pathname),
    night: inMovieNight(pathname),
    you: inYouArea(pathname),
    server: admin && inServerPlace(pathname),
  }
  // Library and Movie night fold open on their own pages, and their heads
  // fold them up or down; Server and You unfold only while you're there.
  const [folded, setFolded] = useState<Record<string, boolean | undefined>>({})
  const [lastPath, setLastPath] = useState(pathname)
  if (lastPath !== pathname) {
    setLastPath(pathname)
    setFolded({})
  }
  const isOpen = (k: 'library' | 'night') => folded[k] ?? here[k]
  // On its own page, a press folds the group; elsewhere it goes there (on a
  // wide screen) or only unfolds it (in a phone's menu, which stays open).
  const head = (k: 'library' | 'night', to: string) => (e: React.MouseEvent | React.KeyboardEvent) => {
    e.preventDefault()
    const open = isOpen(k)
    if (here[k] || narrowScreen()) {
      setFolded((f) => ({ ...f, [k]: !open }))
      return
    }
    navigate(to)
    setFolded((f) => ({ ...f, [k]: true }))
  }
  const yourPages = settingsGroupsFor(admin, user?.role === 'viewer').find((g) => g.key === YOUR_SETTINGS)?.pages ?? []

  return (
    <div className="one-nav">
      <Place to="/" end icon="home" label={t('Home')} />
      <Place to="/find" icon="search" label={t('Find')} active={inFind(pathname)} />
      <Place to="/library" id="sn-head-library" icon="film" label={t('Library')} active={here.library} group open={isOpen('library')} onHead={head('library', '/library')}>
        {lib.kinds.map((l) => (
          <SubLine key={l.key} l={l} />
        ))}
        {lib.nsfw && (
          <>
            <hr className="one-rule" />
            <SubLine l={lib.nsfw} />
          </>
        )}
      </Place>
      <Place to="/tv" icon="tv" label={t('Live TV')} />
      <Place to="/showtime" id="sn-head-night" icon="clock" label={t('Movie night')} active={here.night} group open={isOpen('night')} onHead={head('night', '/showtime')}>
        <SubLine l={{ key: 'showtimes', to: '/showtime', label: t('Showtimes'), icon: 'clock', active: pathname === '/showtime' || pathname.startsWith('/showtime/') }} />
        <SubLine l={{ key: 'crews', to: '/crews', label: t('Crews and votes'), icon: 'chat', active: pathname.startsWith('/crews') }} />
        {sportsOn && <SubLine l={{ key: 'sports', to: '/sports', label: t('Sports tonight'), icon: 'trophy', active: false }} />}
        <a href="/marquee" target="_blank" rel="noreferrer" title={t('Opens the lobby screen in a new tab')}>
          <span className="nav-ico">
            <Icon name="monitor" size={15} />
          </span>
          <span className="nav-label">{t('Marquee screen')}</span>
        </a>
      </Place>
      <Place to="/you" id="sn-head-you" icon="user" label={t('You')} active={here.you} group open={here.you}>
        <SubLine l={{ key: 'taste', to: '/you', label: t('Taste and votes'), icon: 'heart', active: pathname === '/you' }} />
        <SubLine l={{ key: 'diary', to: '/you/diary', label: t('Diary'), icon: 'book', active: pathname === '/you/diary' }} />
        <SubLine l={{ key: 'year', to: '/you/wrapped', label: t('Your year'), icon: 'star', active: pathname === '/you/wrapped' }} />
        <SubLine l={{ key: 'stubs', to: '/you/stubs', label: t('Ticket stubs'), icon: 'bookmark', active: pathname === '/you/stubs' }} />
        <p className="one-head">{t('Your settings')}</p>
        {yourPages.map((p) => (
          <SubLine key={p.to} l={{ key: p.to, to: p.to, label: p.label, icon: p.icon, active: pathname === p.to || pathname.startsWith(p.to + '/') }} />
        ))}
        <p className="one-head">{t('Help')}</p>
        <SubLine l={{ key: 'help', to: '/help', label: t('User guide'), icon: 'info', active: pathname.startsWith('/help') }} />
        <button type="button" className="one-sub-btn" onClick={openSmhMenu}>
          <span className="nav-ico">
            <Icon name="compass" size={15} />
          </span>
          <span className="nav-label">{t('Show me how')}</span>
        </button>
      </Place>
      {admin && (
        <Place to="/server" id="sn-head-server" icon="server" label={t('Server')} count={needs} warn active={here.server} group open={here.server}>
          <SubLine l={{ key: 'today', to: '/server', label: t('Today'), icon: 'home', active: pathname === '/server' }} />
          <Link to="/queue" className={pathname.startsWith('/queue') ? 'active' : ''} title={t('Activity')}>
            <span className="nav-ico">
              <Icon name="activity" size={15} />
            </span>
            <span className="nav-label">{t('Activity')}</span>
            {downloads > 0 && <span className="nav-count">{downloads}</span>}
          </Link>
          <div className="one-server-pages">{serverMenu}</div>
        </Place>
      )}
      <div className="one-tools">{tools}</div>
    </div>
  )
}

// The Library's top-level pages (each kind's own, and Coming up) open with
// the kinds as a row of chips, in the one order (Broadcast only).
export const kindsRowPage = (p: string) => /^\/(library|comics|manga|sports|calendar|wanted|following|leaving-soon)\/?$/.test(p)

// LibraryKinds: the kinds row at the top of those pages; the one you're on is selected.
export function LibraryKinds({ entries, sportsOn }: { entries: LibraryEntry[]; sportsOn: boolean }) {
  const { pathname } = useLocation()
  const admin = isAdmin(useAuth().user)
  if (!broadcastOn() || !kindsRowPage(pathname)) return null
  const { kinds } = libraryLines(entries, pathname, { sportsOn, admin })
  return (
    <nav className="b-kinds" aria-label={t('Kinds of media')}>
      {kinds
        .filter((l) => l.key !== 'streamed')
        .map((l) => (
          <Link key={l.key} to={l.to} className={`chip b-kind${l.active ? ' active' : ''}`} aria-current={l.active ? 'page' : undefined}>
            {l.glyph ? <KindGlyph kind={l.glyph} size={18} /> : <Icon name={l.icon ?? 'film'} size={18} />}
            <span>{l.label}</span>
          </Link>
        ))}
    </nav>
  )
}

// The bar along the bottom of a phone: six places; Server is reached from You.
export function OneTabs({ needs }: { needs: number }) {
  const { pathname } = useLocation()
  const admin = isAdmin(useAuth().user)
  const tabs: { to: string; icon: IconName; label: string; end?: boolean; on?: boolean; dot?: boolean }[] = [
    { to: '/', icon: 'home', label: t('Home'), end: true },
    { to: '/find', icon: 'search', label: t('Find'), on: inFind(pathname) },
    { to: '/library', icon: 'film', label: t('Library'), on: inLibraryArea(pathname) },
    { to: '/tv', icon: 'tv', label: t('Live TV') },
    { to: '/showtime', icon: 'clock', label: t('Movie night'), on: inMovieNight(pathname) },
    { to: '/you', icon: 'user', label: t('You'), on: inYouArea(pathname) || (admin && inServerPlace(pathname)), dot: admin && needs > 0 },
  ]
  return (
    <nav className="one-tabs" aria-label={t('Places')}>
      {tabs.map((tb) => (
        <NavLink key={tb.to} to={tb.to} end={tb.end} className={({ isActive }) => (isActive || tb.on ? 'active' : '')}>
          <span className="one-tab-ico">
            <Icon name={tb.icon} size={21} />
            {tb.dot && <span className="one-tab-dot" aria-label={t('{n} need you', { n: needs })} />}
          </span>
          <span>{tb.label}</span>
        </NavLink>
      ))}
    </nav>
  )
}

// On You (Broadcast): Your settings, Server for administrators, and help.
export function YouPlaces() {
  const admin = isAdmin(useAuth().user)
  if (!broadcastOn()) return null
  return (
    <nav className="room-row you-places" aria-label={t('More of you')}>
      <Link to="/settings/general" className="room-chip">
        <Icon name="sliders" size={14} /> {t('Your settings')}
      </Link>
      {admin && (
        <Link to="/server" className="room-chip">
          <Icon name="server" size={14} /> {t('Server')}
        </Link>
      )}
      <Link to="/help" className="room-chip">
        <Icon name="info" size={14} /> {t('Help')}
      </Link>
    </nav>
  )
}

const MODES: { mode: MenuMode; label: () => string; hint: () => string }[] = [
  { mode: 'open', label: () => t('Always open'), hint: () => t('The whole menu stays in sight.') },
  { mode: 'icons', label: () => t('Icons'), hint: () => t('A narrow row of icons that opens when you point at it or move to it.') },
  { mode: 'hidden', label: () => t('Hidden'), hint: () => t('Out of the way until the menu button or the left edge of the screen opens it.') },
]

// You > Your settings > Look and language: how the menu sits on a computer or a TV.
export function MenuModeChoice() {
  const [mode, setMode] = useState(menuMode)
  return (
    <div className="one-mode-choice" data-walk="menu-mode" role="radiogroup" aria-label={t('The menu')}>
      <span>{t('The menu')}</span>
      {MODES.map((m) => (
        <label key={m.mode} title={m.hint()}>
          <input
            type="radio"
            name="one-menu-mode"
            checked={mode === m.mode}
            onChange={() => {
              setMode(m.mode)
              chooseMenuMode(m.mode)
            }}
          />{' '}
          {m.label()}
        </label>
      ))}
      <span style={{ color: 'var(--text-dim)' }}>{MODES.find((m) => m.mode === mode)?.hint()} {t('Kept in this browser. Phones always have the bar along the bottom.')}</span>
    </div>
  )
}
