import { useCallback, useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { api, type DashboardData } from '../api'
import Icon, { type IconName } from '../components/Icon'
import Loading from '../components/Loading'
import { t } from '../i18n'
import { settingsGroupsFor } from '../settingsNav'
import { useLive } from '../useLive'
import { useUnreadErrors } from '../useProblems'
import '../neu/lobbyHome.css'

// Usherr: the Server area of the new layout (administrators only). The whole
// management side in one place, so it never sits in the way of watching:
// what needs you first, honestly (warnings and errors, not "All good" while
// something is broken), then the day-to-day pages, then every settings group.

function Tile({ to, icon, title, sub, count, color }: { to: string; icon: IconName; title: string; sub: string; count?: number; color?: string }) {
  return (
    <Link to={to} className="server-tile" style={color ? { ['--sc' as string]: color } : undefined}>
      <span className="ico">
        <Icon name={icon} size={19} />
      </span>
      <span>
        <b>{title}</b>
        <small>{sub}</small>
      </span>
      {count !== undefined && count > 0 && <span className="count">{count}</span>}
    </Link>
  )
}

export default function ServerHub() {
  const [data, setData] = useState<DashboardData | null>(null)
  const [downloads, setDownloads] = useState(0)
  const unreadErrors = useUnreadErrors(true)
  const load = useCallback(() => {
    api.dashboard().then(setData).catch(() => undefined)
    api
      .queueCount()
      .then((c) => setDownloads(c.active))
      .catch(() => undefined)
  }, [])
  useEffect(load, [load])
  useLive(load, 15_000)

  const needs = (data?.health ?? []).filter((h) => h.level !== 'info')
  const wanted = (data?.library.wanted.movies ?? 0) + (data?.library.wanted.episodes ?? 0)
  const groups = settingsGroupsFor(true)
  return (
    <div className="server-hub">
      <h1 style={{ margin: 0 }}>{t('Server')}</h1>
      <section>
        <h2>{t('Needs you')}</h2>
        {!data ? (
          <Loading height={80}>
            <div className="skeleton" style={{ height: 80, borderRadius: 22 }} />
          </Loading>
        ) : needs.length === 0 && unreadErrors === 0 ? (
          <p className="server-ok">{t('Nothing needs you right now.')}</p>
        ) : (
          <ul className="server-needs">
            {needs.map((h) => (
              <li key={h.id} className={`lv-${h.level}`}>
                <span className="grow">
                  <b>{h.title}</b>
                  <small>{h.impact}</small>
                </span>
                {h.action && (
                  <Link to={h.action.path} className="lobby-btn">
                    {h.action.label}
                  </Link>
                )}
              </li>
            ))}
            {unreadErrors > 0 && (
              <li className="lv-error">
                <span className="grow">
                  <b>{unreadErrors === 1 ? t('1 new error') : t('{n} new errors', { n: unreadErrors })}</b>
                  <small>{t('From the last day, not marked as read yet.')}</small>
                </span>
                <Link to="/settings/logs" className="lobby-btn">
                  {t('See them')}
                </Link>
              </li>
            )}
          </ul>
        )}
      </section>
      <section>
        <h2>{t('Day to day')}</h2>
        <div className="server-tiles">
          <Tile to="/queue" icon="download" title={t('Downloads')} sub={t('What’s downloading, what failed, and the history')} count={downloads} />
          <Tile to="/wanted" icon="bookmark" title={t('Wanted')} sub={t('Films and episodes still to find')} count={wanted} />
          <Tile to="/server/overview" icon="grid" title={t('Overview')} sub={t('The full dashboard: library size, folders, recent downloads')} />
          <Tile to="/settings/accounts" icon="user" title={t('Accounts')} sub={t('Who can watch, ask for films, book movie nights, and delete')} />
          <Tile to="/marquee/manage" icon="monitor" title={t('Marquee')} sub={t('The lobby screen by the TV')} />
          <Tile to="/preshow/manage" icon="film" title={t('Pre-show')} sub={t('What plays before a showtime')} />
        </div>
      </section>
      <section>
        <h2>{t('Settings')}</h2>
        <div className="server-tiles">
          {groups.map((g) => (
            <Tile key={g.key} to={g.pages[0].to} icon={g.icon} color={g.color} title={g.label} sub={g.pages.map((p) => p.label).join(' · ')} />
          ))}
        </div>
      </section>
    </div>
  )
}
