import { useState } from 'react'
import { Link } from 'react-router-dom'
import type { CastMember, SeasonInfo } from '../api'
import { plural, t } from '../i18n'
import { CAST_FIRST, firstOf, personPath } from '../people'
import './TitlePreview.css'

// The outside pages worth linking to: the title's own website, IMDb and TMDB.
export function titleLinks(kind: 'movie' | 'tv', tmdbId: number, imdbId?: string, homepage?: string): { label: string; url: string }[] {
  const links: { label: string; url: string }[] = []
  if (homepage && /^https?:\/\//i.test(homepage)) links.push({ label: t('Website'), url: homepage })
  if (imdbId && /^tt\d+$/.test(imdbId)) links.push({ label: 'IMDb', url: `https://www.imdb.com/title/${imdbId}/` })
  links.push({ label: 'TMDB', url: `https://www.themoviedb.org/${kind === 'movie' ? 'movie' : 'tv'}/${tmdbId}` })
  return links
}

function initials(name: string): string {
  return name
    .split(/\s+/)
    .filter(Boolean)
    .slice(0, 2)
    .map((w) => w.charAt(0).toUpperCase())
    .join('')
}

function Person({ p }: { p: CastMember }) {
  const [broken, setBroken] = useState(false)
  const to = personPath(p.id)
  const body = (
    <>
      <span className="preview-photo">{p.profileUrl && !broken ? <img src={p.profileUrl} alt="" loading="lazy" onError={() => setBroken(true)} /> : initials(p.name)}</span>
      <strong>{p.name}</strong>
      {p.character && <small>{p.character}</small>}
    </>
  )
  // home fork: the link keeps Usherr's look and says where it goes
  return (
    <li className="preview-person">
      {to ? (
        <Link to={to} className="preview-person-link" title={t('More with {name}', { name: p.name })}>
          {body}
        </Link>
      ) : (
        body
      )}
    </li>
  )
}

// The names of a title's directors, writers or creators, each one a link to the
// person's page when TMDB gave an id.
export function PeopleLinks({ people }: { people: { id?: number; name: string }[] }) {
  return (
    <>
      {people.map((p, i) => {
        const to = personPath(p.id)
        return (
          <span key={`${p.id ?? ''}-${p.name}`}>
            {i > 0 && ', '}
            {to ? <Link to={to}>{p.name}</Link> : p.name}
          </span>
        )
      })}
    </>
  )
}

// Who is in it, in billing order, with their photos. The first dozen show; the
// rest are one tap away.
export function CastCard({ cast }: { cast: CastMember[] }) {
  const [all, setAll] = useState(false)
  if (cast.length === 0) return null
  const { visible } = firstOf(cast, all ? cast.length : CAST_FIRST)
  return (
    <section className="card" data-walk="title-cast" style={{ marginBottom: 24 }}>
      <h2>{t('Cast')}</h2>
      <ul className="preview-cast" style={{ listStyle: 'none', padding: 0, margin: 0 }}>
        {visible.map((p) => (
          <Person key={`${p.id ?? ''}-${p.name}-${p.character ?? ''}`} p={p} />
        ))}
      </ul>
      {cast.length > CAST_FIRST && (
        <button type="button" className="btn-sm" style={{ marginTop: 12 }} aria-expanded={all} onClick={() => setAll(!all)}>
          {all ? t('Show less') : t('Show all')}
        </button>
      )}
    </section>
  )
}

// The seasons of a show and how many episodes each has.
export function SeasonsCard({ seasons }: { seasons: SeasonInfo[] }) {
  if (seasons.length === 0) return null
  return (
    <section className="card" style={{ marginBottom: 24 }}>
      <h2>{t('Seasons')}</h2>
      <ul className="preview-seasons">
        {seasons.map((s) => (
          <li key={s.number}>
            <strong>{s.name}</strong>
            <small>
              {plural(s.episodes, '{n} episode', '{n} episodes')}
              {s.airDate ? ` · ${s.airDate.slice(0, 4)}` : ''}
            </small>
          </li>
        ))}
      </ul>
    </section>
  )
}
