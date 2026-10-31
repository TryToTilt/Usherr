// Usherr SMH: the "Show me how" menu. Every task someone can do, grouped,
// with a search; tasks for administrators only, or for a kind of media that's
// switched off, aren't shown. Each starts Simple or Advanced.
import { useMemo, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { isAdmin } from '../api'
import { useAuth } from '../AuthContext'
import Icon from '../components/Icon'
import { t } from '../i18n'
import { useComicsOn } from '../home/libraryKinds'
import { useModules } from '../ModulesContext'
import { useFocusTrap } from '../useFocusTrap'
import Switch from '../components/Switch'
import { nudgeState, saveNudgeState } from './nudges'
import { closeSmhMenu, openDoctor, setSmhRun, smhDoneList, startSmh, stopSmh, useSmh } from './engine'
import { GROUPS, WALKS, walkById } from './walks'
import './smh.css'

const fold = (s: string) => s.toLowerCase().normalize('NFD').replace(/[̀-ͯ]/g, '')

export default function SmhMenu() {
  const { user } = useAuth()
  const modules = useModules()
  const { run } = useSmh()
  const box = useRef<HTMLDivElement>(null)
  useFocusTrap(box)
  const [q, setQ] = useState('')
  const done = useMemo(() => smhDoneList(), [])
  const admin = isAdmin(user)
  const comicsOn = useComicsOn(admin)
  const words = fold(q).split(/\s+/).filter(Boolean)
  const list = WALKS.filter((w) => (!w.admin || admin) && (!w.needs || (w.needs === 'comics' ? comicsOn : modules.on(w.needs)))).filter((w) => {
    const hay = fold(`${w.title} ${w.blurb} ${w.words ?? ''}`)
    return words.every((x) => hay.includes(x))
  })
  const left = run?.paused ? walkById(run.id) : undefined
  const [tips, setTips] = useState(() => (user ? !nudgeState(user.id).off : true))
  const switchTips = (on: boolean) => {
    if (!user) return
    saveNudgeState(user.id, { ...nudgeState(user.id), off: !on })
    setTips(on)
  }

  return createPortal(
    <div className="modal-backdrop" onMouseDown={(e) => e.target === e.currentTarget && closeSmhMenu()}>
      <div ref={box} tabIndex={-1} className="modal smh-menu" role="dialog" aria-modal="true" aria-label={t('Show me how')} onKeyDown={(e) => e.key === 'Escape' && closeSmhMenu()}>
        <div className="modal-head">
          <h2>
            <Icon name="compass" size={20} /> {t('Show me how')}
          </h2>
          <button className="icon-btn" onClick={closeSmhMenu} aria-label={t('Close')}>
            <Icon name="x" size={18} />
          </button>
        </div>
        <div className="modal-body">
          <p className="hint">{t('Pick what you want to do. Simple does it with you in a few taps; Advanced shows every option on the way.')}</p>
          {left && run && (
            <div className="smh-resume">
              <span>
                <b>{t('Carry on:')}</b> {left.title}
              </span>
              <button
                type="button"
                className="btn-sm primary"
                onClick={() => {
                  setSmhRun({ ...run, paused: false })
                  closeSmhMenu()
                }}
              >
                {t('Carry on')}
              </button>
              <button type="button" className="btn-link link-btn" onClick={stopSmh}>
                {t('Forget it')}
              </button>
            </div>
          )}
          <button type="button" className="smh-doctor" onClick={openDoctor}>
            <Icon name="shield" size={18} />
            <span>
              <b>{t('Something not right?')}</b>
              <small>{t('Usherr checks itself and says what to fix, in plain words.')}</small>
            </span>
            <Icon name="chevron-right" size={16} />
          </button>
          <input type="search" className="smh-search" value={q} onChange={(e) => setQ(e.target.value)} placeholder={t('I want to…')} aria-label={t('Search what you want to do')} autoFocus />
          {list.length === 0 && <p className="hint">{t('Nothing matches yet. Try other words.')}</p>}
          <div className="smh-tips">
            <Switch checked={tips} onChange={switchTips} label={t('Tips now and then')} description={t('Now and then, at most once a day, a small tip suggests something worth trying, like voting or booking a showtime.')} />
          </div>
          {GROUPS.map((g) => {
            const here = list.filter((w) => w.group === g.id)
            if (!here.length) return null
            return (
              <section key={g.id} className="smh-group">
                <h3>{g.label()}</h3>
                {here.map((w) => (
                  <div key={w.id} className="smh-item">
                    <Icon name={w.icon} size={20} />
                    <div className="smh-item-text">
                      <b>
                        {w.title} {done.has(w.id) && <span className="smh-did" title={t('You’ve done this one')}>✓</span>}
                      </b>
                      <small>{w.blurb}</small>
                    </div>
                    <div className="smh-item-go">
                      <button type="button" className="btn-sm primary" onClick={() => startSmh(w.id, 'simple')} title={t('A few questions, then Usherr does it with you')}>
                        {t('Simple')}
                      </button>
                      {w.advanced && (
                        <button type="button" className="btn-sm" onClick={() => startSmh(w.id, 'advanced')} title={t('Every option, explained on the way')}>
                          {t('Advanced')}
                        </button>
                      )}
                    </div>
                  </div>
                ))}
              </section>
            )
          })}
        </div>
      </div>
    </div>,
    document.body,
  )
}
