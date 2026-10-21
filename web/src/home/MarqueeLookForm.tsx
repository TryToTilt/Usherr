// Usherr: the Marquee's "Look" settings (phase 2): which style a screen wears,
// and its theme, font, frame, transitions, effects and burn-in protection.
import { t } from '../i18n'
import { MARQUEE_FONT_FAMILY } from './marqueeFonts'
import type { MarqueeFont, MarqueeSettings, MarqueeStyle, MarqueeTheme, MarqueeTransition } from './marqueeData'

export const MQ_STYLES: { key: MarqueeStyle; label: string; blurb: string }[] = [
  { key: 'usherr', label: t('Usherr'), blurb: t('A lit marquee sign over one scene at a time; lists scroll.') },
  { key: 'cinema', label: t('Cinema'), blurb: t('One poster as big as it goes, with a header and a footer.') },
  { key: 'onesheet', label: t('One-sheet'), blurb: t('A framed poster under a marquee header, with the details.') },
  { key: 'posterr', label: t('Posterr'), blurb: t('A coloured banner, the poster and the title; progress while playing.') },
  { key: 'wall', label: t('Poster wall'), blurb: t('A wall of posters that turn one at a time.') },
  { key: 'screensaver', label: t('Screensaver'), blurb: t('The picture across the whole screen, with a clock.') },
  { key: 'board', label: t('Showtimes board'), blurb: t('Rows like a cinema board: now, Live TV, in theaters, coming.') },
]

const THEMES: { key: MarqueeTheme; label: string; swatch: [string, string, string] }[] = [
  { key: 'usherr', label: t('Usherr (the app’s colours)'), swatch: ['#100509', '#c99a42', '#f6d783'] },
  { key: 'deco', label: t('Art deco'), swatch: ['#0b0a08', '#c9a24a', '#ecc96a'] },
  { key: 'noir', label: t('Noir'), swatch: ['#050505', '#8d8d8d', '#ffffff'] },
  { key: 'neon', label: t('Neon'), swatch: ['#07040f', '#ff2fb3', '#39f3ff'] },
  { key: 'ivory', label: t('Ivory'), swatch: ['#f3ead8', '#9c6b2f', '#7a1f1f'] },
  { key: 'midnight', label: t('Midnight'), swatch: ['#060b1a', '#8fa8d8', '#d8e4ff'] },
  { key: 'drivein', label: t('Drive-in'), swatch: ['#0b1a1c', '#ff7a3d', '#ffd166'] },
  { key: 'velvet', label: t('Red velvet'), swatch: ['#12030a', '#d1263f', '#ffb4c0'] },
]

const FONTS: { key: MarqueeFont; label: string }[] = [
  { key: 'classic', label: t('Classic serif') },
  { key: 'deco', label: t('Art deco') },
  { key: 'neon', label: t('Neon tubes') },
  { key: 'poster', label: t('Poster capitals') },
  { key: 'roman', label: t('Roman') },
  { key: 'retro', label: t('Retro') },
  { key: 'bold', label: t('Bold serif') },
  { key: 'condensed', label: t('Condensed') },
  { key: 'elegant', label: t('Elegant') },
  { key: 'modern', label: t('Modern') },
]

const TRANSITIONS: { key: MarqueeTransition; label: string }[] = [
  { key: 'fade', label: t('Crossfade') },
  { key: 'slide', label: t('Slide in') },
  { key: 'push', label: t('Push up') },
  { key: 'zoom', label: t('Zoom') },
  { key: 'flip', label: t('Flip') },
  { key: 'rotate', label: t('Turn') },
  { key: 'blur', label: t('Blur') },
  { key: 'random', label: t('A different one each time') },
  { key: 'cut', label: t('None (a cut)') },
]

// A small drawing of each style for its card.
function Mini({ style }: { style: MarqueeStyle }) {
  switch (style) {
    case 'usherr':
      return (
        <span className="mqm mqm-usherr">
          <i className="sign" />
          <i className="poster" />
          <i className="line" />
        </span>
      )
    case 'cinema':
      return (
        <span className="mqm mqm-cinema">
          <i className="head" />
          <i className="poster" />
          <i className="line" />
        </span>
      )
    case 'onesheet':
      return (
        <span className="mqm mqm-one">
          <i className="head" />
          <i className="frame">
            <i className="poster" />
          </i>
          <i className="line" />
        </span>
      )
    case 'posterr':
      return (
        <span className="mqm mqm-posterr">
          <i className="band" />
          <i className="poster" />
          <i className="band low" />
        </span>
      )
    case 'wall':
      return (
        <span className="mqm mqm-wall">
          {Array.from({ length: 12 }, (_, i) => (
            <i key={i} />
          ))}
        </span>
      )
    case 'screensaver':
      return (
        <span className="mqm mqm-saver">
          <i className="clock" />
          <i className="card" />
        </span>
      )
    case 'board':
      return (
        <span className="mqm mqm-board">
          <i className="head" />
          {Array.from({ length: 5 }, (_, i) => (
            <i key={i} className="row" />
          ))}
        </span>
      )
  }
}

const row = { display: 'flex', gap: 8, flexWrap: 'wrap', alignItems: 'center' } as const

export default function MarqueeLookForm({ m, change, name }: { m: MarqueeSettings; change: (patch: Partial<MarqueeSettings>) => void; name: string }) {
  const single = m.style === 'cinema' || m.style === 'onesheet' || m.style === 'posterr' || m.style === 'screensaver'
  const framed = m.style === 'usherr' || m.style === 'onesheet'
  return (
    <>
      <h3>{t('Look')}</h3>
      <div className="mqf-looks" role="radiogroup" aria-label={t('Look')}>
        {MQ_STYLES.map((s) => (
          <button key={s.key} type="button" role="radio" aria-checked={m.style === s.key} className={`mqf-look${m.style === s.key ? ' on' : ''}`} onClick={() => change({ style: s.key })}>
            <Mini style={s.key} />
            <strong>{s.label}</strong>
            <small>{s.blurb}</small>
          </button>
        ))}
      </div>

      <div className="grid-form">
        <label>
          {t('Theme')}
          <select value={m.theme} onChange={(e) => change({ theme: e.target.value as MarqueeTheme })}>
            {THEMES.map((x) => (
              <option key={x.key} value={x.key}>
                {x.label}
              </option>
            ))}
          </select>
        </label>
        <div className="mqf-swatch" aria-hidden="true">
          {(THEMES.find((x) => x.key === m.theme)?.swatch ?? []).map((c, i) => (
            <span key={i} style={{ background: i === 0 && m.background ? m.background : i === 1 && m.accent ? m.accent : c }} />
          ))}
        </div>
        <div style={row}>
          <label className="cx-check">
            <input type="checkbox" checked={!!m.accent} onChange={(e) => change({ accent: e.target.checked ? '#e0a33a' : '' })} /> {t('My own accent colour')}
          </label>
          {m.accent && <input type="color" value={m.accent} onChange={(e) => change({ accent: e.target.value })} aria-label={t('Accent colour')} />}
          <label className="cx-check">
            <input type="checkbox" checked={!!m.background} onChange={(e) => change({ background: e.target.checked ? '#0a0a0a' : '' })} /> {t('My own background')}
          </label>
          {m.background && <input type="color" value={m.background} onChange={(e) => change({ background: e.target.value })} aria-label={t('Background colour')} />}
        </div>
        <label>
          {t('Font for signs and titles')}
          <select value={m.font} onChange={(e) => change({ font: e.target.value as MarqueeFont })} style={{ fontFamily: MARQUEE_FONT_FAMILY[m.font] }}>
            {FONTS.map((x) => (
              <option key={x.key} value={x.key} style={{ fontFamily: MARQUEE_FONT_FAMILY[x.key] }}>
                {x.label}
              </option>
            ))}
          </select>
        </label>
        <p className="mqf-sample" style={{ fontFamily: MARQUEE_FONT_FAMILY[m.font] }}>
          {t('Now Showing')}
        </p>
        <label>
          {t('Corners')}
          <select value={m.corners} onChange={(e) => change({ corners: e.target.value as MarqueeSettings['corners'] })}>
            <option value="square">{t('Square')}</option>
            <option value="soft">{t('Soft')}</option>
            <option value="round">{t('Round')}</option>
          </select>
        </label>
        {framed && (
          <label>
            {m.style === 'usherr' ? t('The sign’s frame') : t('The poster’s frame')}
            <select value={m.frame} onChange={(e) => change({ frame: e.target.value as MarqueeSettings['frame'] })}>
              <option value="bulbs">{t('Chasing bulbs')}</option>
              <option value="gold">{t('Gold line')}</option>
              <option value="neon">{t('Neon')}</option>
              <option value="none">{t('None')}</option>
            </select>
          </label>
        )}
        {framed && m.frame === 'bulbs' && (
          <label>
            {t('Bulb size: {n}%', { n: m.bulbSize })}
            <input type="range" min={60} max={160} step={10} value={m.bulbSize} onChange={(e) => change({ bulbSize: Number(e.target.value) })} />
          </label>
        )}
        {single && m.style !== 'screensaver' && (
          <label>
            {t('Poster framing')}
            <select value={m.framing} onChange={(e) => change({ framing: e.target.value as MarqueeSettings['framing'] })}>
              <option value="fill">{t('Fill the space')}</option>
              <option value="letterbox">{t('Whole poster (letterbox)')}</option>
              <option value="matted">{t('On a mat')}</option>
            </select>
          </label>
        )}
        {single && (
          <label>
            {t('Under the poster')}
            <select value={m.footer} onChange={(e) => change({ footer: e.target.value as MarqueeSettings['footer'] })}>
              <option value="details">{t('Title and details')}</option>
              <option value="title">{t('The title')}</option>
              <option value="tagline">{t('The tagline')}</option>
              <option value="none">{t('Nothing')}</option>
            </select>
          </label>
        )}
        {m.style === 'wall' && (
          <>
            <label>
              {t('Wall layout')}
              <select value={m.wallLayout} onChange={(e) => change({ wallLayout: e.target.value as MarqueeSettings['wallLayout'] })}>
                <option value="grid">{t('Posters only')}</option>
                <option value="hero">{t('One film big, the wall below')}</option>
              </select>
            </label>
            <label>
              {t('Posters across: {n}', { n: m.wallColumns })}
              <input type="range" min={2} max={8} step={1} value={m.wallColumns} onChange={(e) => change({ wallColumns: Number(e.target.value) })} />
            </label>
            <label>
              {t('A poster turns every {n} seconds', { n: m.wallSwap })}
              <input type="range" min={2} max={60} step={1} value={m.wallSwap} onChange={(e) => change({ wallSwap: Number(e.target.value) })} />
            </label>
          </>
        )}
        {m.style !== 'board' && (
          <label>
            {m.style === 'wall' ? t('How a poster turns') : t('Between slides')}
            <select value={m.transition} onChange={(e) => change({ transition: e.target.value as MarqueeTransition, transitionMs: e.target.value === 'cut' ? 0 : m.transitionMs || 900 })}>
              {TRANSITIONS.map((x) => (
                <option key={x.key} value={x.key}>
                  {x.label}
                </option>
              ))}
            </select>
          </label>
        )}
        {m.style !== 'board' && m.style !== 'wall' && m.transition !== 'cut' && (
          <label>
            {t('Transition length: {n} s', { n: (m.transitionMs / 1000).toFixed(1) })}
            <input type="range" min={200} max={4000} step={100} value={m.transitionMs} onChange={(e) => change({ transitionMs: Number(e.target.value) })} />
          </label>
        )}
      </div>

      <h3>{t('Effects')}</h3>
      <div style={row}>
        {(single || m.style === 'wall') && (
          <label className="cx-check">
            <input type="checkbox" checked={m.kenBurns} onChange={(e) => change({ kenBurns: e.target.checked })} /> {t('Slow pan and zoom')}
          </label>
        )}
        {m.style === 'cinema' && (
          <label className="cx-check">
            <input type="checkbox" checked={m.ambilight} onChange={(e) => change({ ambilight: e.target.checked })} /> {t('Poster glow')}
          </label>
        )}
        <label className="cx-check">
          <input type="checkbox" checked={m.grain} onChange={(e) => change({ grain: e.target.checked })} /> {t('Film grain')}
        </label>
        <label className="cx-check">
          <input type="checkbox" checked={m.vintage} onChange={(e) => change({ vintage: e.target.checked })} /> {t('Vintage colour')}
        </label>
        <label className="cx-check">
          <input type="checkbox" checked={m.playingFirst} onChange={(e) => change({ playingFirst: e.target.checked })} /> {t('While something plays, show only that')}
        </label>
      </div>

      <h3>{t('Screen care')}</h3>
      <div style={row}>
        <label className="cx-check">
          <input type="checkbox" checked={m.nudge} onChange={(e) => change({ nudge: e.target.checked })} /> {t('Move everything a few pixels every few minutes (against burn-in)')}
        </label>
      </div>
      <div style={row}>
        <label className="cx-check">
          <input type="checkbox" checked={m.nightDimOn} onChange={(e) => change({ nightDimOn: e.target.checked })} /> {t('Dim at night, between')}
        </label>
        <input type="time" value={m.nightFrom} onChange={(e) => change({ nightFrom: e.target.value })} aria-label={t('Dim from')} />
        {t('and')}
        <input type="time" value={m.nightTo} onChange={(e) => change({ nightTo: e.target.value })} aria-label={t('Dim until')} />
      </div>
      {m.nightDimOn && (
        <label className="mqf-range">
          {t('Brightness at night: {n}%', { n: m.nightLevel })}
          <input type="range" name={`${name}-night`} min={15} max={90} step={5} value={m.nightLevel} onChange={(e) => change({ nightLevel: Number(e.target.value) })} />
        </label>
      )}
    </>
  )
}
