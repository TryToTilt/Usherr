// Usherr Broadcast: Find, one of the menu's seven places. Everything for
// finding something in one page: search and Ask (the OmniBar), Discover by
// kind (Manga's Discover too), Roulette and Tonight (they were in the top
// bar), people, and "things you can do": the Show me how walks matching what
// you type. /search and /discover still open as their own pages.
import { useEffect, useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import { isAdmin } from '../api'
import { useAuth } from '../AuthContext'
import Icon from '../components/Icon'
import KindGlyph from '../components/KindGlyph'
import OmniBar from '../home/OmniBar'
import RouletteButton from '../home/RouletteButton'
import Tonight from '../home/Tonight'
import { useDiscoverTabs } from '../home/DiscoverTabs'
import { useComicsOn } from '../home/libraryKinds'
import { t } from '../i18n'
import { useModules } from '../ModulesContext'
import { startSmh } from '../smh/engine'
import type { Walkthrough } from '../smh/types'
import { walkInLook } from '../smh/look'
import { kindInfo, sortKinds, type KindKey } from './kinds'
import './find.css'

const fold = (s: string) => s.toLowerCase().normalize('NFD').replace(/[̀-ͯ]/g, '')

// The walks that fit what's typed: every word found in the title, blurb or
// extra words. With nothing typed, the first few everyday ones.
export function matchWalks(walks: readonly Walkthrough[], q: string, limit = 8): Walkthrough[] {
  const words = fold(q).split(/[^a-z0-9]+/).filter((w) => w.length > 1)
  if (words.length === 0) return walks.slice(0, limit)
  return walks.filter((w) => {
    const hay = fold(`${w.title} ${w.blurb} ${w.words ?? ''}`)
    return words.every((x) => hay.includes(x))
  }).slice(0, limit)
}

export default function Find() {
  const { user } = useAuth()
  const admin = isAdmin(user)
  const { on } = useModules()
  const comicsOn = useComicsOn(admin)
  const dtabs = useDiscoverTabs(admin)
  const kinds = sortKinds([...dtabs.map((k) => ({ key: k as string, to: `/discover?kind=${k}` })), ...(on('manga') ? [{ key: 'manga', to: '/manga/discover' }] : [])]).filter((k) => k.key !== 'nsfw')

  const [walks, setWalks] = useState<Walkthrough[]>([])
  useEffect(() => {
    void import('../smh/walks').then((m) => setWalks(m.WALKS))
  }, [])
  const [q, setQ] = useState('')
  // A few everyday things first (title and one short line); "More things you
  // can do" opens the rest with their own filter box.
  const [more, setMore] = useState(false)
  const can = (w: Walkthrough) => walkInLook(w, true) && (!w.admin || admin) && (!w.needs || (w.needs === 'comics' ? comicsOn : on(w.needs)))
  const shown = useMemo(() => matchWalks(walks.filter(can), more ? q : '', more ? 12 : 4), [walks, q, more, admin, comicsOn]) // eslint-disable-line react-hooks/exhaustive-deps

  return (
    <div className="page-pad find-page">
      <h1>{t('Find')}</h1>
      <section className="find-ask" aria-label={t('Search or ask')}>
        <OmniBar page="dashboard" />
      </section>

      <section className="find-section">
        <h2>{t('Discover')}</h2>
        <div className="find-kinds">
          {kinds.map((k) => {
            const info = kindInfo(k.key as KindKey)
            return (
              <Link key={k.key} to={k.to} className="find-kind" style={{ ['--kc' as string]: info.color }}>
                <KindGlyph kind={info.glyph} size={22} />
                <span>{info.label}</span>
              </Link>
            )
          })}
          <Link to="/discover/complete" className="find-kind">
            <Icon name="check" size={22} />
            <span>{t('Complete your library')}</span>
          </Link>
        </div>
      </section>

      <section className="find-section">
        <h2>{t('Not sure what to watch?')}</h2>
        <div className="find-row">
          <RouletteButton className="btn-with-icon" />
          <Link to="/search" className="btn-with-icon find-btn">
            <Icon name="search" size={14} /> {t('Search titles, books and music')}
          </Link>
          <Link to="/following" className="btn-with-icon find-btn">
            <Icon name="user" size={14} /> {t('People you follow')}
          </Link>
          {admin && (
            <Link to="/search/releases" className="btn-with-icon find-btn">
              <Icon name="list" size={14} /> {t('Search releases')}
            </Link>
          )}
        </div>
        <Tonight />
      </section>

      <section className="find-section">
        <h2>{t('Things you can do')}</h2>
        {more && (
          <label className="find-filter">
            <Icon name="search" size={16} />
            <input type="search" value={q} onChange={(e) => setQ(e.target.value)} placeholder={t('What do you want to do? (book a movie night, read a comic…)')} aria-label={t('Things you can do')} />
          </label>
        )}
        {shown.length === 0 ? (
          <p className="hint">{t('Nothing matches. Try other words, or open Show me how at the top.')}</p>
        ) : (
          <ul className="find-walks">
            {shown.map((w) => (
              <li key={w.id}>
                <button type="button" onClick={() => startSmh(w.id, 'simple')} title={w.blurb}>
                  <Icon name={w.icon} size={18} />
                  <span>
                    <b>{w.title}</b>
                    <small>{w.blurb}</small>
                  </span>
                </button>
              </li>
            ))}
          </ul>
        )}
        {!more && walks.length > 0 && (
          <button type="button" className="find-more" onClick={() => setMore(true)}>
            {t('More things you can do')} <Icon name="chevron-right" size={16} />
          </button>
        )}
      </section>
    </div>
  )
}
