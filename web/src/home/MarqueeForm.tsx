// Usherr: the Marquee's settings form, for the shared settings (on
// the Marquee's page) and for a screen's own (the Marquee remote).
import Icon from '../components/Icon'
import { t } from '../i18n'
import type { MarqueeSettings, SceneKind } from './marqueeData'
import MarqueeLookForm from './MarqueeLookForm'
import MarqueeShowsForm from './MarqueeShowsForm'
import './cinema.css'

const row = { display: 'flex', gap: 8, flexWrap: 'wrap', alignItems: 'center' } as const

export const MQ_SCENE_LABEL: Record<SceneKind, string> = {
  playing: t('Now showing (what is playing on the TV)'),
  new: t('New on the server (one film)'),
  trailer: t('Trailer'),
  'theaters-film': t('In theaters now (one film)'),
  arrivals: t('Just added (posters)'),
  'coming-film': t('Coming soon (one film)'),
  theaters: t('In theaters now (posters)'),
  coming: t('Coming up (the week)'),
  tvnow: t('On Live TV (what each channel has on)'),
  tonight: t('Tonight on Usherr TV (the channels’ next hours)'),
  spotlight: t('In the spotlight (a director or star, with their films)'),
  trivia: t('Did you know? (facts about the films)'),
  slides: t('Your slides (pictures, videos, pages, words)'),
  preshow: t('The pre-show (ads, countdown, feature presentation)'),
}

export default function MarqueeForm({ m, profiles, change, name }: { m: MarqueeSettings; profiles: string[]; change: (patch: Partial<MarqueeSettings>) => void; name: string }) {
  const move = (i: number, by: number) => {
    const list = [...m.scenes]
    const j = i + by
    if (j < 0 || j >= list.length) return
    ;[list[i], list[j]] = [list[j], list[i]]
    change({ scenes: list })
  }
  const scene = (i: number, patch: Partial<MarqueeSettings['scenes'][number]>) => change({ scenes: m.scenes.map((s, j) => (j === i ? { ...s, ...patch } : s)) })
  const toggleProfile = (p: string, on: boolean) => change({ profiles: on ? [...m.profiles, p] : m.profiles.filter((x) => x !== p) })
  return (
    <>
      <MarqueeLookForm m={m} change={change} name={name} />

      <h3>{t('Scenes, in order')}</h3>
      <ol className="mq-set-scenes">
        {m.scenes.map((s, i) => (
          <li key={s.kind} className={s.on ? '' : 'off'}>
            <label className="cx-check">
              <input type="checkbox" checked={s.on} onChange={(e) => scene(i, { on: e.target.checked })} /> {MQ_SCENE_LABEL[s.kind]}
            </label>
            <label className="mq-set-secs">
              <input type="number" min={5} max={600} value={s.seconds} onChange={(e) => scene(i, { seconds: Number(e.target.value) || 5 })} aria-label={t('Seconds for {scene}', { scene: MQ_SCENE_LABEL[s.kind] })} /> {t('sec')}
            </label>
            <button className="icon-btn" onClick={() => move(i, -1)} disabled={i === 0} aria-label={t('Move {scene} up', { scene: MQ_SCENE_LABEL[s.kind] })}>
              <Icon name="chevron-up" size={15} />
            </button>
            <button className="icon-btn" onClick={() => move(i, 1)} disabled={i === m.scenes.length - 1} aria-label={t('Move {scene} down', { scene: MQ_SCENE_LABEL[s.kind] })}>
              <Icon name="chevron-down" size={15} />
            </button>
          </li>
        ))}
      </ol>
      <p className="hint">{t('Scenes with nothing to show are skipped. A scene taller than the screen scrolls slowly down and back, and takes the time it needs.')}</p>

      <MarqueeShowsForm m={m} change={change} />

      <h3>{t('The sign')}</h3>
      <div style={row}>
        <label className="cx-check">
          <input type="radio" name={`${name}-sign`} checked={m.signMode === 'auto'} onChange={() => change({ signMode: 'auto' })} /> {t('The name of each scene')}
        </label>
        <label className="cx-check">
          <input type="radio" name={`${name}-sign`} checked={m.signMode === 'custom'} onChange={() => change({ signMode: 'custom' })} /> {t('My own words')}
        </label>
      </div>
      {m.signMode === 'custom' && (
        <div className="grid-form">
          <label>
            {t('Line 1')}
            <input value={m.signLine1} maxLength={40} onChange={(e) => change({ signLine1: e.target.value })} placeholder={t('Now Showing')} />
          </label>
          <label>
            {t('Line 2 (optional)')}
            <input value={m.signLine2} maxLength={40} onChange={(e) => change({ signLine2: e.target.value })} placeholder={t('The Living Room')} />
          </label>
          <span className="hint">{t('Each line stays on one line and gets smaller to fit. Leave both empty to hide the sign.')}</span>
        </div>
      )}

      <h3>{t('Top bar')}</h3>
      <div style={row}>
        <label className="cx-check">
          <input type="checkbox" checked={m.showIcon} onChange={(e) => change({ showIcon: e.target.checked })} /> {t('Icon')}
        </label>
        <label className="cx-check">
          <input type="checkbox" checked={m.showName} onChange={(e) => change({ showName: e.target.checked })} /> {t('Name')}
        </label>
        <label className="cx-check">
          <input type="checkbox" checked={m.showClock} onChange={(e) => change({ showClock: e.target.checked })} /> {t('Clock')}
        </label>
      </div>

      <h3>{t('Sizes')}</h3>
      <div className="grid-form">
        <label>
          {t('Text size: {n}%', { n: m.textScale })}
          <input type="range" min={70} max={140} step={5} value={m.textScale} onChange={(e) => change({ textScale: Number(e.target.value) })} />
        </label>
        <label>
          {t('Poster and trailer size: {n}%', { n: m.posterScale })}
          <input type="range" min={60} max={120} step={5} value={m.posterScale} onChange={(e) => change({ posterScale: Number(e.target.value) })} />
        </label>
        <span className="hint">{t('Posters and the trailer also grow to fill the room left when the top bar or a sign line is switched off.')}</span>
        <label>
          {t('Scroll speed')}
          <select value={m.scrollSpeed} onChange={(e) => change({ scrollSpeed: e.target.value as MarqueeSettings['scrollSpeed'] })}>
            <option value="slow">{t('Slow')}</option>
            <option value="normal">{t('Normal')}</option>
            <option value="fast">{t('Fast')}</option>
          </select>
        </label>
        <label className="cx-check">
          <input type="checkbox" checked={m.trailerSound} onChange={(e) => change({ trailerSound: e.target.checked })} /> {t('Play trailers with sound (the tablet’s browser must allow it)')}
        </label>
      </div>

      <h3>{t('Now showing comes from')}</h3>
      <div style={row}>
        <label className="cx-check">
          <input type="checkbox" checked={m.fromJellyfin} onChange={(e) => change({ fromJellyfin: e.target.checked })} /> {t('The media server (Jellyfin or Emby)')}
        </label>
        <label className="cx-check">
          <input type="checkbox" checked={m.fromStreamNZB} onChange={(e) => change({ fromStreamNZB: e.target.checked })} /> {t('StreamNZB (Nuvio, Stremio)')}
        </label>
      </div>
      {m.fromStreamNZB && profiles.length > 0 && (
        <div style={row}>
          <span className="hint">{t('StreamNZB profiles to show (none ticked shows them all):')}</span>
          {profiles.map((p) => (
            <label key={p} className="cx-check">
              <input type="checkbox" checked={m.profiles.includes(p)} onChange={(e) => toggleProfile(p, e.target.checked)} /> {p}
            </label>
          ))}
        </div>
      )}

      <h3>{t('Quiet hours')}</h3>
      <div style={row}>
        <label className="cx-check">
          <input type="checkbox" checked={m.quietOn} onChange={(e) => change({ quietOn: e.target.checked })} /> {t('Rest between')}
        </label>
        <input type="time" value={m.quietFrom} onChange={(e) => change({ quietFrom: e.target.value })} aria-label={t('Quiet from')} />
        {t('and')}
        <input type="time" value={m.quietTo} onChange={(e) => change({ quietTo: e.target.value })} aria-label={t('Quiet until')} />
        <select value={m.quietMode} onChange={(e) => change({ quietMode: e.target.value as MarqueeSettings['quietMode'] })} aria-label={t('While quiet')}>
          <option value="clock">{t('showing a dim clock')}</option>
          <option value="black">{t('with a black screen')}</option>
        </select>
      </div>

      {m.theme === 'usherr' && (
        <>
          <h3>{t('Colours of the Usherr theme')}</h3>
          <select value={m.palette} onChange={(e) => change({ palette: e.target.value })} aria-label={t('Marquee colours')}>
            <option value="">{t('As chosen on the tablet')}</option>
            {['wine', 'cherry', 'amber', 'emerald', 'teal', 'blue', 'violet', 'rose', 'graphite'].map((p) => (
              <option key={p} value={p}>
                {p[0].toUpperCase() + p.slice(1)}
              </option>
            ))}
          </select>
        </>
      )}
    </>
  )
}
