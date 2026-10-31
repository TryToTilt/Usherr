// Home fork: a person's page, opened from a cast photo: who they are, and the
// films and shows they're best known for, as rows that scroll sideways; and
// a Follow button (their new work shows on Discover and Upcoming → Following).
import HelpLink from '../components/HelpLink'
import { useEffect, useState } from 'react'
import { useParams } from 'react-router-dom'
import type { DiscoverMovie } from '../api'
import AddDialog, { type AddTarget } from '../components/AddDialog'
import DiscoverRail from '../components/DiscoverRail'
import Icon from '../components/Icon'
import { t } from '../i18n'
import { useOwned } from '../useOwned'
import { follow, unfollow } from './follows'
import './news.css'

interface PersonInfo {
  id: number
  name: string
  biography: string
  birthday: string
  deathday: string
  place: string
  known: string
  image: string
}

export default function PersonPage() {
  const { id } = useParams()
  const owned = useOwned()
  const [adding, setAdding] = useState<AddTarget | null>(null)
  const [data, setData] = useState<{ person: PersonInfo; following: boolean; movies: DiscoverMovie[]; shows: DiscoverMovie[] } | null>(null)
  const [error, setError] = useState('')
  const [more, setMore] = useState(false)
  const [busy, setBusy] = useState(false)
  useEffect(() => {
    setData(null)
    setError('')
    fetch(`/api/home/person/${id}`, { credentials: 'include' })
      .then(async (r) => {
        const out = await r.json()
        if (!r.ok) throw new Error(out.error ?? r.statusText)
        return out
      })
      .then(setData)
      .catch((e: Error) => setError(e.message))
  }, [id])
  if (error) return <p className="error-text page-pad">{error}</p>
  if (!data) return <div className="page-pad"><div className="skeleton" style={{ height: 260 }} /></div>
  const p = data.person
  const life = [p.birthday ? p.birthday.slice(0, 4) : '', p.deathday ? `– ${p.deathday.slice(0, 4)}` : ''].join(' ').trim()
  const bio = p.biography.trim()
  const toggle = () => {
    setBusy(true)
    const done = data.following ? unfollow(p.id) : follow({ id: p.id, name: p.name, image: p.image, known: p.known })
    done
      .then(() => setData({ ...data, following: !data.following }))
      .catch((e: Error) => setError(e.message))
      .finally(() => setBusy(false))
  }
  return (
    <div className="page-pad person-page">
      <div className="person-head">
        {p.image ? <img src={p.image} alt="" className="person-photo" /> : <span className="person-photo person-nophoto"><Icon name="user" size={40} /></span>}
        <div>
          <h1>{p.name}</h1>
          <p className="hint">{[p.known === 'Directing' ? t('Director') : p.known === 'Acting' ? t('Actor') : p.known, life, p.place].filter(Boolean).join(' · ')}</p>
          <button data-walk="person-follow" className={`btn-sm person-follow${data.following ? ' on' : ''}`} onClick={toggle} disabled={busy} aria-pressed={data.following}>
            <Icon name={data.following ? 'check' : 'plus'} size={14} /> {data.following ? t('Following') : t('Follow')}
          </button>{' '}
          <HelpLink topic="home-finding#people-and-following" />
          {bio && (
            <p className={`person-bio${more ? ' open' : ''}`}>
              {bio}
            </p>
          )}
          {bio.length > 320 && (
            <button className="btn-link" onClick={() => setMore((m) => !m)}>
              {more ? t('Less') : t('More')}
            </button>
          )}
        </div>
      </div>
      {data.movies.length > 0 && <DiscoverRail title={t('Films')} hint={t('best known first')} items={data.movies} kind="movie" owned={owned} onAdd={setAdding} rows={1} keepOwned scroll />}
      {data.shows.length > 0 && <DiscoverRail title={t('Shows')} hint={t('best known first')} items={data.shows} kind="tv" owned={owned} onAdd={setAdding} rows={1} keepOwned scroll />}
      {data.movies.length === 0 && data.shows.length === 0 && <p className="hint">{t('TMDB lists no other films or shows for them.')}</p>}
      {adding && (
        <AddDialog
          target={adding}
          onClose={() => setAdding(null)}
          onAdded={() => {
            setAdding(null)
            owned.reload()
          }}
        />
      )}
    </div>
  )
}
