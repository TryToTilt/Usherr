import { NavLink } from 'react-router-dom'
import Icon from '../components/Icon'
import Calendar from './Calendar'
import Wanted from './Wanted'
import Following from '../home/Following' // home fork
import { t } from '../i18n'

// Upcoming: what is coming (the calendar) and what is still missing or could
// be better (wanted), as two tabs of one page.
export default function Upcoming({ tab }: { tab: 'calendar' | 'wanted' | 'following' }) {
  return (
    <div>
      {/* Usherr: the calendar puts its kind picker and its menu at the end of the tabs (its search is in the top bar). */}
      <nav className="page-tabs" data-walk="upcoming-tabs" aria-label={t('Upcoming')} style={{ ['--tc' as string]: 'var(--c-calendar)' }}>
        <NavLink to="/calendar" className={({ isActive }) => (isActive ? 'active' : '')}>
          <Icon name="calendar" size={15} /> {t('Calendar')}
        </NavLink>
        <NavLink to="/wanted" className={({ isActive }) => (isActive ? 'active' : '')}>
          <Icon name="bookmark" size={15} /> {t('Wanted')}
        </NavLink>
        {/* home fork: the people you follow */}
        <NavLink to="/following" className={({ isActive }) => (isActive ? 'active' : '')}>
          <Icon name="user" size={15} /> {t('Following')}
        </NavLink>
        <span id="upcoming-tabs-end" className="page-tabs-end" />
      </nav>
      {tab === 'calendar' ? <Calendar /> : tab === 'following' ? <Following /> : <Wanted />}
    </div>
  )
}
