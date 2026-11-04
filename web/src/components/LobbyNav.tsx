import { useCallback, useEffect, useState, type ReactNode } from 'react'
import { NavLink, useLocation } from 'react-router-dom'
import { api } from '../api'
import { t } from '../i18n'
import { useLive } from '../useLive'
import Icon, { type IconName } from './Icon'
import KindGlyph from './KindGlyph'

// Usherr: the new layout's menu (the "lobby" look, neu/look.ts). The same
// pages as the drawer, grouped the way people look for them: watching first,
// the libraries always in sight, the evening together, and the whole
// management side under one "Server" entry that only administrators see.
// On a phone it gives way to the tabs along the bottom (LobbyTabs).

export interface LobbyLine {
  to: string
  label: string
  glyph: 'movie' | 'tv' | 'music' | 'book' | 'audiobook' | 'comic'
  color: string
  active: boolean
}

// What the administrator should look at: the dashboard's warnings and errors
// plus the errors nobody has read. Counted once a minute, administrators only.
export function useServerNeeds(admin: boolean, unreadErrors: number): number {
  const [health, setHealth] = useState(0)
  const load = useCallback(() => {
    if (!admin) return
    api
      .dashboard()
      .then((d) => setHealth(d.health.filter((h) => h.level !== 'info').length))
      .catch(() => undefined)
  }, [admin])
  useEffect(load, [load])
  useLive(load, 60_000, admin)
  return health + (unreadErrors > 0 ? 1 : 0)
}

// The pages that belong to the Server area, so its entry stays lit on them.
export function inServerArea(path: string): boolean {
  return /^\/(server|settings(?!\/(profile|security|services|about))|queue|wanted|import|marquee\/manage|marquee\/remote|preshow\/manage)(\/|$)/.test(path)
}

function Item({ to, icon, label, end, count, warn, active }: { to: string; icon: IconName; label: string; end?: boolean; count?: number; warn?: boolean; active?: boolean }) {
  return (
    <NavLink to={to} end={end} className={({ isActive }) => (isActive || active ? 'active' : '')}>
      <span className="nav-ico">
        <Icon name={icon} size={17} />
      </span>
      <span className="nav-label">{label}</span>
      {count !== undefined && count > 0 && <span className={`nav-count${warn ? ' lobby-warn' : ''}`}>{count}</span>}
    </NavLink>
  )
}

export function LobbyNav({ lines, admin, sportsOn, downloads, needs, settingsMenu, tools }: { lines: LobbyLine[]; admin: boolean; sportsOn: boolean; downloads: number; needs: number; settingsMenu: ReactNode; tools: ReactNode }) {
  const { pathname } = useLocation()
  const inUpcoming = /^\/(calendar|wanted|following)(\/|$)/.test(pathname)
  const inServer = admin && inServerArea(pathname)
  const mine = /^\/settings\/(profile|security|services|about)/.test(pathname)
  return (
    <div className="lobby-nav">
      <Item to="/" end icon="home" label={t('Home')} />
      <Item to="/search" icon="search" label={t('Search')} />
      <Item to="/discover" icon="compass" label={t('Discover')} />
      <p className="lobby-head">{t('Libraries')}</p>
      {lines.map((l) => (
        <NavLink key={l.to} to={l.to} style={{ ['--sc' as string]: l.color }} className={() => (l.active ? 'active' : '')} aria-current={l.active ? 'page' : undefined}>
          <span className="nav-ico">
            <KindGlyph kind={l.glyph} size={16} />
          </span>
          <span className="nav-label">{l.label}</span>
        </NavLink>
      ))}
      <Item to="/tv" icon="tv" label={t('Live TV')} />
      {sportsOn && <Item to="/sports" icon="trophy" label={t('Sports')} />}
      <p className="lobby-head">{t('Together')}</p>
      <Item to="/showtime" icon="clock" label={t('Movie night')} />
      <Item to="/calendar" icon="calendar" label={t('Coming up')} active={inUpcoming && !inServer} />
      <Item to="/you" icon="user" label={t('You')} active={mine} />
      <Item to="/help" icon="info" label={t('Help')} />
      {admin && (
        <div className="lobby-admin">
          <p className="lobby-head">{t('Admins only')}</p>
          <Item to="/server" icon="server" label={t('Server')} count={needs} warn active={inServer} />
          {inServer && (
            <div className="lobby-server-pages">
              <Item to="/queue" icon="activity" label={t('Downloads')} count={downloads} />
              <Item to="/wanted" icon="bookmark" label={t('Wanted')} />
              {settingsMenu}
            </div>
          )}
        </div>
      )}
      {tools}
    </div>
  )
}

// The tabs along the bottom of a phone: the five places people go, and Server
// for administrators.
export function LobbyTabs({ admin, needs }: { admin: boolean; needs: number }) {
  const { pathname } = useLocation()
  const library = /^\/(library|series|title|show|streamed|comics|nsfw|person|book|music)(\/|$)/.test(pathname)
  const tabs: { to: string; icon: IconName; label: string; end?: boolean; on?: boolean; count?: number }[] = [
    { to: '/', icon: 'home', label: t('Home'), end: true },
    { to: '/search', icon: 'search', label: t('Search'), on: pathname.startsWith('/discover') },
    { to: '/library', icon: 'film', label: t('Library'), on: library },
    { to: '/showtime', icon: 'clock', label: t('Movie night') },
    { to: '/you', icon: 'user', label: t('You') },
  ]
  if (admin) tabs.push({ to: '/server', icon: 'server', label: t('Server'), on: inServerArea(pathname), count: needs })
  return (
    <nav className="lobby-tabs" aria-label={t('Main')}>
      {tabs.map((tb) => (
        <NavLink key={tb.to} to={tb.to} end={tb.end} className={({ isActive }) => (isActive || tb.on ? 'active' : '')}>
          <span className="lobby-tab-ico">
            <Icon name={tb.icon} size={21} />
            {tb.count !== undefined && tb.count > 0 && <span className="lobby-tab-dot" aria-label={t('{n} need you', { n: tb.count })} />}
          </span>
          <span>{tb.label}</span>
        </NavLink>
      ))}
    </nav>
  )
}
