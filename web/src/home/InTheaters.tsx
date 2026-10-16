// Usherr: "In theaters now", a sideways row of posters (Dashboard, and the top
// of Library → Movies). Each poster opens the film's page, where its showtimes
// and the other cinemas showing it are linked.
import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import Icon from '../components/Icon'
import { t } from '../i18n'
import { loadCinema, theatreUrl, whereElseUrl, type Cinema } from './cinema'
import './cinema.css'

export default function InTheaters() {
  const [c, setC] = useState<Cinema | null>(null)
  useEffect(() => {
    let live = true
    void loadCinema().then((d) => live && setC(d))
    return () => {
      live = false
    }
  }, [])
  if (!c?.on || c.results.length === 0) return null
  return (
    <section className="card cinema-row" aria-labelledby="cinema-row-title">
      <div className="cinema-head">
        <Icon name="film" size={18} />
        <h2 id="cinema-row-title">{t('In theaters now')}</h2>
        <span className="hint cinema-where">{c.source === 'amc' && c.theatre ? c.theatre : t('In cinemas across the country')}</span>
      </div>
      <ul className="cinema-strip">
        {c.results.map((m) => (
          <li key={m.tmdbId} className="cinema-item">
            <Link to={`/title/${m.tmdbId}`} className="cinema-poster" title={m.title}>
              {m.posterUrl ? <img src={m.posterUrl} alt="" loading="lazy" draggable={false} /> : <span className="cinema-noposter">{m.title}</span>}
              {m.inLibrary && (
                <span className="cinema-have" title={t('In your library')}>
                  <Icon name="check" size={11} />
                </span>
              )}
            </Link>
            <Link to={`/title/${m.tmdbId}`} className="cinema-title">
              {m.title}
            </Link>
            <a className="cinema-times" href={whereElseUrl(m.title, m.year, c.zip)} target="_blank" rel="noopener noreferrer">
              <Icon name="external" size={11} /> {t('Showtimes')}
            </a>
          </li>
        ))}
      </ul>
    </section>
  )
}

// On a film's page while it's in cinemas: its showtimes at the household's
// theater, and everywhere else near home that's showing it.
export function InTheatersPanel({ tmdbId, title, year }: { tmdbId: number; title: string; year?: number }) {
  const [c, setC] = useState<Cinema | null>(null)
  useEffect(() => {
    let live = true
    void loadCinema().then((d) => live && setC(d))
    return () => {
      live = false
    }
  }, [])
  if (!c?.on || !c.results.some((m) => m.tmdbId === tmdbId)) return null
  return (
    <div className="cinema-panel">
      <strong>
        <Icon name="film" size={15} /> {t('In theaters now')}
      </strong>
      {c.theatre && (
        <a href={theatreUrl(title, c.theatre)} target="_blank" rel="noopener noreferrer">
          {t('Showtimes at {theater}', { theater: c.theatre })} <Icon name="external" size={12} />
        </a>
      )}
      <a href={whereElseUrl(title, year, c.zip)} target="_blank" rel="noopener noreferrer">
        {t('Where else is it playing?')} <Icon name="external" size={12} />
      </a>
    </div>
  )
}
