// Usherr: the Marquee's "What it shows" settings (phase 3): badges, the info
// panel, QR codes, theme music and the pre-show.
import { t } from '../i18n'
import { PRESHOW_LABEL } from './MarqueeExtras'
import type { MarqueeSettings, PreShowPiece } from './marqueeData'

const row = { display: 'flex', gap: 8, flexWrap: 'wrap', alignItems: 'center' } as const
const PIECES: PreShowPiece[] = ['popcorn', 'silence', 'coming', 'countdown', 'feature']

export default function MarqueeShowsForm({ m, change }: { m: MarqueeSettings; change: (patch: Partial<MarqueeSettings>) => void }) {
  const piece = (p: PreShowPiece, on: boolean) => change({ preShowBuiltins: on ? PIECES.filter((x) => x === p || m.preShowBuiltins.includes(x)) : m.preShowBuiltins.filter((x) => x !== p) })
  return (
    <>
      <h3>{t('On a film')}</h3>
      <div className="grid-form">
        <label className="cx-check">
          <input type="checkbox" checked={m.badges === 'on'} onChange={(e) => change({ badges: e.target.checked ? 'on' : 'off' })} /> {t('Show 4K, HDR, Dolby Vision and Atmos on what’s playing')}
        </label>
        <label>
          {t('The info panel (plot, cast, director)')}
          <select value={m.infoPanel} onChange={(e) => change({ infoPanel: e.target.value as MarqueeSettings['infoPanel'] })}>
            <option value="held">{t('When paused, or held on the remote')}</option>
            <option value="always">{t('Always')}</option>
            <option value="off">{t('Only from the remote')}</option>
          </select>
        </label>
        <label>
          {t('A QR code in the corner')}
          <select value={m.qr} onChange={(e) => change({ qr: e.target.value as MarqueeSettings['qr'] })}>
            <option value="off">{t('None')}</option>
            <option value="trailer">{t('The trailer, to watch on a phone')}</option>
            <option value="usherr">{t('The film in Usherr')}</option>
          </select>
        </label>
      </div>

      <h3>{t('Theme music')}</h3>
      <div style={row}>
        <label className="cx-check">
          <input type="checkbox" checked={m.themeMusic} onChange={(e) => change({ themeMusic: e.target.checked })} /> {t('Play a film’s or show’s theme while it is on screen')}
        </label>
      </div>
      {m.themeMusic && (
        <label className="mqf-range">
          {t('Volume: {n}%', { n: m.themeVolume })}
          <input type="range" min={5} max={100} step={5} value={m.themeVolume} onChange={(e) => change({ themeVolume: Number(e.target.value) })} />
        </label>
      )}
      <p className="hint">{t('A theme.mp3 in the film’s or show’s folder (or a theme-music folder) plays, the way Jellyfin’s Theme Songs plugin stores them. The screen’s browser must allow sound.')}</p>

      <h3>{t('Pre-show')}</h3>
      <p className="hint">{t('Like at the cinema: a few pieces, your own pictures and videos, trailers, a countdown and the feature presentation. Put the Pre-show scene in the order above, start it from the remote, or let it start when something begins playing.')}</p>
      <div style={row}>
        <label className="cx-check">
          <input type="checkbox" checked={m.preShowOnStart} onChange={(e) => change({ preShowOnStart: e.target.checked })} /> {t('Play it when something starts playing')}
        </label>
      </div>
      <div style={row}>
        {PIECES.map((p) => (
          <label key={p} className="cx-check">
            <input type="checkbox" checked={m.preShowBuiltins.includes(p)} onChange={(e) => piece(p, e.target.checked)} /> {PRESHOW_LABEL[p]}
          </label>
        ))}
      </div>
      <label className="mqf-range">
        {t('Trailers in the pre-show')}
        <select value={m.preShowTrailers} onChange={(e) => change({ preShowTrailers: Number(e.target.value) })}>
          {[0, 1, 2, 3].map((n) => (
            <option key={n} value={n}>
              {n}
            </option>
          ))}
        </select>
      </label>
    </>
  )
}
