// Usherr: the Marquee's own page (/marquee/manage). The lobby display for a
// tablet or a screen by the TV, all in one place: the screens and their
// remote, the shared look and scenes, your slides and pre-show, and Home
// Assistant and the API.
import { useEffect, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import Icon, { type IconName } from '../components/Icon'
import { useToast } from '../components/Toast'
import { useDocumentTitle } from '../documentTitle'
import { t } from '../i18n'
import { marqueeCall } from './marqueeCall'
import { ownLabel, type MarqueeSettings } from './marqueeData'
import MarqueeConnect from './MarqueeConnect'
import MarqueeForm from './MarqueeForm'
import MarqueeMedia from './MarqueeMedia'
import { MarqueeScreens } from './MarqueeRemote'
import './cinema.css'

type Tab = 'screens' | 'look' | 'media' | 'connect'
const TABS: { key: Tab; label: string; icon: IconName }[] = [
  { key: 'screens', label: t('Screens'), icon: 'monitor' },
  { key: 'look', label: t('Look and scenes'), icon: 'sliders' },
  { key: 'media', label: t('Slides and pre-show'), icon: 'film' },
  { key: 'connect', label: t('Home Assistant and API'), icon: 'server' },
]

export default function MarqueeHub() {
  useDocumentTitle(t('Marquee'))
  const [params, setParams] = useSearchParams()
  const tab = (TABS.find((x) => x.key === params.get('tab'))?.key ?? 'screens') as Tab
  const address = `${window.location.origin}/marquee`
  return (
    <div className="page-pad mqh">
      <div className="page-header">
        <h1>{t('Marquee')}</h1>
      </div>
      <section className="card mqh-head" data-walk="mq-head">
        <p>{t('A lobby display for a tablet or a screen next to the TV: what’s playing, new and coming films, trailers, Live TV, your own slides and a pre-show, in the look you choose. Open {address} on the screen, full-screen; it shows up under Screens by itself.', { address })}</p>
        <div className="mqh-actions">
          <a className="btn-sm btn-with-icon" href="/marquee" target="_blank" rel="noreferrer">
            <Icon name="external" size={14} /> {t('Open the Marquee')}
          </a>
          <Link className="btn-sm btn-with-icon" to="/marquee/remote">
            <Icon name="play" size={14} /> {t('The phone remote')}
          </Link>
          <Link className="btn-sm btn-with-icon" to="/showtime">
            <Icon name="clock" size={14} /> {t('Showtimes')}
          </Link>
        </div>
      </section>
      <nav className="seg mqh-tabs" data-walk="mq-tabs" aria-label={t('Marquee')}>
        {TABS.map((x) => (
          <button key={x.key} className={tab === x.key ? 'active' : ''} aria-current={tab === x.key ? 'page' : undefined} onClick={() => setParams(x.key === 'screens' ? {} : { tab: x.key }, { replace: true })}>
            <Icon name={x.icon} size={15} /> {x.label}
          </button>
        ))}
      </nav>
      {tab === 'screens' && <MarqueeScreens />}
      {tab === 'look' && <SharedLook />}
      {tab === 'media' && (
        <section className="card mqh-card">
          <MarqueeMedia />
        </section>
      )}
      {tab === 'connect' && (
        <section className="card mqh-card">
          <MarqueeConnect />
        </section>
      )}
    </div>
  )
}

// SharedLook is the shared settings: every screen without its own shows them.
function SharedLook() {
  const toast = useToast()
  const [m, setM] = useState<MarqueeSettings | null>(null)
  const [profiles, setProfiles] = useState<string[]>([])
  const [dirty, setDirty] = useState(false)
  // Screens with settings of their own don't follow these: said plainly,
  // with a way to put them back on the shared ones.
  const [own, setOwn] = useState<{ id: string; name: string; what: string }[]>([])
  const loadOwn = () =>
    marqueeCall<{ screens: { id: string; name: string; own: boolean; ownKeys?: string[] }[] }>('GET', '/api/home/marquee/screens')
      .then((d) => setOwn((d.screens ?? []).filter((sc) => sc.own).map((sc) => ({ id: sc.id, name: sc.name, what: ownLabel(sc.ownKeys) }))))
      .catch(() => undefined)
  useEffect(() => {
    marqueeCall<{ settings: MarqueeSettings; profiles: string[] }>('GET', '/api/home/marquee/settings')
      .then((d) => {
        setM(d.settings)
        setProfiles(d.profiles ?? [])
      })
      .catch(() => undefined)
    void loadOwn()
  }, [])
  if (!m) return null
  const backToShared = async (sc: { id: string; name: string; what: string }) => {
    try {
      await marqueeCall('PUT', `/api/home/marquee/screens/${sc.id}`, { settings: null })
      toast.success(t('{name} shows these settings now.', { name: sc.name }))
      void loadOwn()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    }
  }
  const change = (patch: Partial<MarqueeSettings>) => {
    setM({ ...m, ...patch })
    setDirty(true)
  }
  const save = async () => {
    try {
      const d = await marqueeCall<{ settings: MarqueeSettings }>('PUT', '/api/home/marquee/settings', m)
      setM(d.settings)
      setDirty(false)
      toast.success(
        own.length
          ? t('Saved. {names} keep some of their own (see above); the rest of these apply there too.', { names: own.map((sc) => sc.name).join(', ') })
          : t('Marquee settings saved. The screens show them now.'),
      )
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    }
  }
  return (
    <section className="card mqh-card">
      <p className="hint">{t('These settings apply to every screen that has none of its own. Under Screens, any screen can get its own.')}</p>
      {own.length > 0 && (
        <div className="mqh-own" role="status">
          <Icon name="warning" size={18} />
          <div>
            <strong>{t('Some screens keep a few settings of their own; everything else here applies to them too.')}</strong>
            <ul className="mqh-own-list">
              {own.map((sc) => (
                <li key={sc.id}>
                  <span>{t('{name} keeps its own {what}.', { name: sc.name, what: sc.what })}</span>
                  <button className="btn-sm" onClick={() => void backToShared(sc)}>
                    {t('Use these on {name} too', { name: sc.name })}
                  </button>
                </li>
              ))}
            </ul>
          </div>
        </div>
      )}
      <div className="mq-set">
        <MarqueeForm m={m} profiles={profiles} change={change} name="mq-shared" />
      </div>
      <div className="mqh-save">
        <button className="primary" onClick={() => void save()} disabled={!dirty}>
          {t('Save')}
        </button>
        {dirty && <span className="hint">{t('Not saved yet')}</span>}
      </div>
    </section>
  )
}
