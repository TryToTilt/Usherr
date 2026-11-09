// Usherr Broadcast: Server > Today, as in the mock-up: three big tiles, what's
// downloading, what's wanted and what needs you, each one stop for the remote
// (the first has focus when the page opens). Server's other parts follow it on
// the page (pages/ServerHub.tsx).
import { Link } from 'react-router-dom'
import { plural, t } from '../i18n'
import './screens.css'

interface Props {
  downloads: number
  wanted: number
  needs: number // warnings and errors, and unread errors
  needsTo: string // where Needs you leads: the first thing's own page
  needsLine?: string // the first thing that needs you, in a few words
  loading: boolean
}

function TodayTile({ to, label, sub, tone, first }: { to: string; label: string; sub: string; tone: 'ok' | 'live' | 'warn'; first?: boolean }) {
  return (
    <Link to={to} className={`b-today-tile tone-${tone}`} data-autofocus={first ? '' : undefined}>
      <span className="b-today-label">
        <span className="b-dot" aria-hidden="true" />
        {label}
      </span>
      <span className="b-today-sub">{sub}</span>
    </Link>
  )
}

export default function ServerToday({ downloads, wanted, needs, needsTo, needsLine, loading }: Props) {
  const wait = t('Looking…')
  return (
    <div className="b-today" role="group" aria-label={t('Today')}>
      <TodayTile first to="/queue" tone="ok" label={t('Downloads')} sub={loading ? wait : downloads > 0 ? plural(downloads, '{n} going', '{n} going') : t('Nothing downloading')} />
      <TodayTile to="/wanted" tone="live" label={t('Wanted')} sub={loading ? wait : wanted > 0 ? plural(wanted, 'One film or episode to find', '{n} films and episodes to find') : t('Nothing to find')} />
      <TodayTile
        to={needs > 0 ? needsTo : '/settings/service-checks'}
        tone={needs > 0 ? 'warn' : 'ok'}
        label={t('Needs you')}
        sub={loading ? wait : needs > 0 ? (needsLine ?? plural(needs, 'One thing', '{n} things')) : t('Nothing needs you right now.')}
      />
    </div>
  )
}
