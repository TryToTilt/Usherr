// Usherr: a title page's layouts (home/filmLayout.ts), shared by films and
// shows (anime included). The page hands over its pieces; this arranges them
// as Cinema (the hero, then tabs), Side by side (the title on the left, you
// and the community on the right) or Streaming (a big backdrop, then rows).
import { useEffect, useRef, useState, type ReactNode } from 'react'
import { isAdmin } from '../api'
import { useAuth } from '../AuthContext'
import Icon from '../components/Icon'
import { t } from '../i18n'
import { openReport } from '../report'
import type { FilmLayout, FilmPageShows } from './filmLayout'
import './filmpage.css'
import './lobbyTitle.css'
import '../broadcast/title.css' // Usherr Broadcast: its page (only drawn in Broadcast)

type Tab = 'episodes' | 'about' | 'reviews' | 'cast' | 'more' | 'manage'

export interface TitleBlocks {
  layout: FilmLayout
  show: FilmPageShows
  hero: ReactNode
  facts: ReactNode // TitleFacts
  scores?: ReactNode
  where?: ReactNode // where it streams (and plays in cinemas)
  you?: ReactNode // your watches
  requests?: ReactNode
  reviews?: ReactNode
  cast?: ReactNode
  more?: ReactNode // More like this
  episodes?: ReactNode // a show's seasons and episodes
  manage?: ReactNode // false or empty: nothing to manage here
  manageSummary?: string
  manageOpen: boolean
  setManageOpen: (open: boolean) => void
  dialogs?: ReactNode
  label: string // what the tab list is about, for screen readers
}

export default function TitleLayout(b: TitleBlocks) {
  const [tab, setTab] = useState<Tab>(b.episodes ? 'episodes' : 'about')
  const { layout, show, manage, manageOpen, setManageOpen } = b
  const admin = isAdmin(useAuth().user)
  // Usherr Broadcast: Manage, opened from the More sheet, comes into view with the focus in it.
  const manageRef = useRef<HTMLElement>(null)
  const broadcastManaging = layout === 'broadcast' && !!manage && manageOpen
  useEffect(() => {
    if (!broadcastManaging) return
    const box = manageRef.current
    if (!box) return
    box.scrollIntoView({ block: 'start' })
    box.querySelector<HTMLElement>('button:not([disabled]), a[href], input, select')?.focus({ preventScroll: true })
  }, [broadcastManaging])

  // Usherr Broadcast: the page from the mock-up. The title, its one main
  // button and three more (the rest is in the More sheet), the seasons and
  // episodes, More like this, then the people, reviews and the details.
  // Manage (administrators) opens under the title from the More sheet.
  if (layout === 'broadcast') {
    return (
      <div className="fp fp-broadcast">
        {b.hero}
        {broadcastManaging && (
          <section ref={manageRef} className="card fp-library bc-manage" aria-label={t('Manage')}>
            <div className="fp-manage-head">
              <h2>{t('Manage')}</h2>
              {b.manageSummary && <p className="fp-sum">{b.manageSummary}</p>}
              <button type="button" className="btn-sm" onClick={() => setManageOpen(false)}>
                {t('Close')}
              </button>
            </div>
            {manage}
          </section>
        )}
        {b.episodes}
        {b.more}
        {b.cast}
        {b.reviews}
        <section className="card fp-about">
          <h2>{t('Details')}</h2>
          {b.facts}
          {b.scores}
          {b.where}
          {b.you}
          {b.requests}
        </section>
        {b.dialogs}
      </div>
    )
  }

  // Usherr: the new layout's page (neu/look.ts). The title and one button up
  // top, then the episodes, about and the people; Manage opens beside it.
  if (layout === 'lobby') {
    const managing = !!manage && manageOpen
    return (
      <div className={`fp fp-lobby${managing ? ' managing' : ''}`}>
        <div className="fp-lobby-hero">{b.hero}</div>
        {managing && (
          <aside className="card fp-side-manage" aria-label={t('Manage')}>
            <div className="fp-manage-head">
              <h2>{t('Manage')}</h2>
              {b.manageSummary && <p className="fp-sum">{b.manageSummary}</p>}
              <button type="button" className="btn-sm" onClick={() => setManageOpen(false)}>
                {t('Close')}
              </button>
            </div>
            {manage}
          </aside>
        )}
        <div className="fp-lobby-rest">
          {b.episodes}
          <div className="fp-lobby-pair">
            <section className="card fp-about">
              <h2>{t('About')}</h2>
              {b.facts}
              {b.scores}
              {b.where}
            </section>
            {(b.you || b.requests) && (
              <section className="card fp-you">
                <h2>{t('You and your friends')}</h2>
                {b.you}
                {b.requests}
              </section>
            )}
          </div>
          {b.reviews}
          {b.cast}
          {b.more}
          {!admin && (
            <p className="fp-problem">
              {t('Something wrong with it, like no sound or the wrong title?')}{' '}
              <button type="button" className="link-button" onClick={() => openReport('problem')}>
                {t('Tell an admin')}
              </button>
            </p>
          )}
        </div>
        {b.dialogs}
      </div>
    )
  }

  if (layout === 'side') {
    return (
      <div className="fp fp-side">
        <div className="fp-cols">
          <div className="fp-main">
            {b.hero}
            <section className="card fp-about">
              {b.facts}
              {b.scores}
            </section>
          </div>
          <aside className="fp-aside">
            {b.you && <section className="card fp-you">{b.you}</section>}
            {b.reviews}
            {b.where && <section className="card">{b.where}</section>}
            {b.requests}
          </aside>
        </div>
        {b.episodes}
        {manage && (
          <details className="card fp-library" open={show.libraryOpen || manageOpen} onToggle={(e) => setManageOpen((e.target as HTMLDetailsElement).open)}>
            <summary>
              <Icon name="sliders" size={15} /> {t('Manage')} {b.manageSummary && <span className="fp-sum">{b.manageSummary}</span>}
            </summary>
            {manage}
          </details>
        )}
        {b.cast}
        {b.more}
        {b.dialogs}
      </div>
    )
  }

  if (layout === 'stream') {
    return (
      <div className="fp fp-stream">
        {b.hero}
        {manage && manageOpen && <section className="card fp-library">{manage}</section>}
        {b.episodes}
        {b.reviews}
        {b.cast}
        {b.more}
        <section className="card fp-about">
          <h2>{t('Details')}</h2>
          {b.facts}
          {b.scores}
          {b.where}
          {b.you}
          {b.requests}
        </section>
        {b.dialogs}
      </div>
    )
  }

  // Cinema: the hero, then tabs. A show opens on its episodes.
  const tabs: { key: Tab; label: string }[] = [
    ...(b.episodes ? [{ key: 'episodes' as const, label: t('Episodes') }] : []),
    { key: 'about', label: t('About') },
    ...(b.reviews ? [{ key: 'reviews' as const, label: t('Reviews') }] : []),
    ...(b.cast ? [{ key: 'cast' as const, label: t('Cast') }] : []),
    ...(b.more ? [{ key: 'more' as const, label: t('More like this') }] : []),
    ...(manage ? [{ key: 'manage' as const, label: t('Manage') }] : []),
  ]
  // Manage (the button up top) is a tab here.
  const current: Tab = manageOpen ? 'manage' : tab
  const shown = tabs.some((x) => x.key === current) ? current : tabs[0].key
  return (
    <div className="fp fp-cinema">
      {b.hero}
      <nav className="fp-tabs" role="tablist" aria-label={b.label}>
        {tabs.map((x) => (
          <button
            key={x.key}
            type="button"
            role="tab"
            aria-selected={shown === x.key}
            className={shown === x.key ? 'on' : ''}
            onClick={() => {
              setTab(x.key)
              setManageOpen(x.key === 'manage')
            }}
          >
            {x.label}
          </button>
        ))}
      </nav>
      <div className="fp-tab" role="tabpanel">
        {shown === 'episodes' && b.episodes}
        {shown === 'about' && (
          <section className="card fp-about">
            {b.facts}
            {b.scores}
            {b.where}
            {b.you}
            {b.requests}
          </section>
        )}
        {shown === 'reviews' && b.reviews}
        {shown === 'cast' && b.cast}
        {shown === 'more' && b.more}
        {shown === 'manage' && <section className="card fp-library">{manage}</section>}
      </div>
      {b.dialogs}
    </div>
  )
}
