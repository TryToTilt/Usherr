// Usherr: Settings, General, Film and show pages: the title page's layout for this
// browser (home/filmLayout.ts), shown as small drawings to pick from, and the
// switches for what the page shows.
import { useState } from 'react'
import { useAuth } from '../AuthContext'
import { isAdmin } from '../api'
import Icon from '../components/Icon'
import Switch from '../components/Switch'
import { t } from '../i18n'
import { autoLayout, loadFilmPage, saveFilmPage, type FilmLayoutChoice, type FilmPagePrefs, type FilmPageShows } from './filmLayout'
import './filmpage.css'

const LAYOUTS: { id: FilmLayoutChoice; name: () => string; hint: () => string }[] = [
  { id: 'auto', name: () => t('Automatic'), hint: () => t('By the screen: Streaming on a TV, Side by side on a wide screen, Cinema on a phone or laptop.') },
  { id: 'cinema', name: () => t('Cinema'), hint: () => t('The title up top, the rest in tabs: Episodes (for a show), About, Reviews, Cast, More like this, Manage.') },
  { id: 'side', name: () => t('Side by side'), hint: () => t('The title on the left; you, the reviews and where it streams on the right. A show’s episodes below.') },
  { id: 'stream', name: () => t('Streaming'), hint: () => t('A big backdrop with the title, then rows: a show’s episodes first. Easy with a TV remote.') },
]

// A small drawing of each layout.
function Sketch({ id }: { id: FilmLayoutChoice }) {
  if (id === 'auto')
    return (
      <span className="flc-sketch flc-auto" aria-hidden="true">
        <Icon name="monitor" size={26} />
      </span>
    )
  return (
    <span className={`flc-sketch flc-${id}`} aria-hidden="true">
      <i className="a" />
      <i className="b" />
      <i className="c" />
      <i className="d" />
    </span>
  )
}

const SHOWS: { key: keyof FilmPageShows; label: () => string; admin?: boolean }[] = [
  { key: 'scores', label: () => t('Scores (IMDb, Rotten Tomatoes, Letterboxd…)') },
  { key: 'streaming', label: () => t('Where it streams, and in cinemas') },
  { key: 'you', label: () => t('You: how often you watched it') },
  { key: 'reviews', label: () => t('Reviews from the community') },
  { key: 'cast', label: () => t('Cast') },
  { key: 'similar', label: () => t('More like this') },
  { key: 'fullPlot', label: () => t('The whole plot (not three lines)') },
  { key: 'libraryOpen', label: () => t('Manage open, not folded (Side by side, Streaming)'), admin: true },
]

export default function FilmLayoutChoice() {
  const admin = isAdmin(useAuth().user)
  const [prefs, setPrefs] = useState<FilmPagePrefs>(loadFilmPage)
  const change = (p: FilmPagePrefs) => {
    setPrefs(p)
    saveFilmPage(p)
  }
  const now = LAYOUTS.find((l) => l.id === autoLayout(window.innerWidth))
  return (
    <fieldset className="group" data-walk="film-layout">
      <legend>
        <Icon name="film" size={14} /> {t('Film and show pages')}
      </legend>
      <p style={{ margin: '0 0 10px', color: 'var(--text-dim)' }}>{t('How the page of a film or a show (anime too) is laid out, kept in this browser: the TV and your laptop can each have their own.')}</p>
      <div className="flc-grid" role="radiogroup" aria-label={t('Film page layout')}>
        {LAYOUTS.map((l) => (
          <button key={l.id} type="button" role="radio" aria-checked={prefs.layout === l.id} className={`flc-card${prefs.layout === l.id ? ' on' : ''}`} onClick={() => change({ ...prefs, layout: l.id })}>
            <Sketch id={l.id} />
            <strong>{l.name()}</strong>
            <small>{l.hint()}</small>
          </button>
        ))}
      </div>
      {prefs.layout === 'auto' && now && <p className="hint" style={{ margin: '8px 0 0' }}>{t('On this screen: {name}.', { name: now.name() })}</p>}
      <div className="flc-shows">
        <strong>{t('Show on the page')}</strong>
        {SHOWS.filter((s) => !s.admin || admin).map((s) => (
          <Switch key={s.key} checked={prefs.show[s.key]} label={s.label()} onChange={(v) => change({ ...prefs, show: { ...prefs.show, [s.key]: v } })} />
        ))}
      </div>
    </fieldset>
  )
}
