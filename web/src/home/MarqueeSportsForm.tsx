// Usherr: the Marquee's Sports settings — the ticker, the countdown, scores
// (off: nothing that gives a result away), followed teams only, the leagues.
import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { t } from '../i18n'
import type { MarqueeSettings } from './marqueeData'
import { sportsApi } from './sportsApi'

const row = { display: 'flex', gap: 8, flexWrap: 'wrap', alignItems: 'center' } as const

export default function MarqueeSportsForm({ m, change }: { m: MarqueeSettings; change: (patch: Partial<MarqueeSettings>) => void }) {
  const [leagues, setLeagues] = useState<string[] | null>(null)
  const [off, setOff] = useState(false)
  useEffect(() => {
    sportsApi
      .home()
      .then((h) => {
        setOff(!h.on)
        setLeagues(h.leagues.map((l) => l.name))
      })
      .catch(() => setLeagues([]))
  }, [])
  const chosen = m.sportsLeagues ?? []
  const toggle = (name: string, on: boolean) => change({ sportsLeagues: on ? [...chosen, name] : chosen.filter((x) => x !== name) })
  return (
    <>
      <h3>{t('Sports')}</h3>
      {off ? (
        <p className="hint">
          {t('Sports isn’t set up.')} <Link to="/settings/extras#sports">{t('Set up Sports')}</Link>
        </p>
      ) : (
        <>
          <div style={row}>
            <label className="cx-check">
              <input type="checkbox" checked={!!m.sportsTicker} onChange={(e) => change({ sportsTicker: e.target.checked })} /> {t('Score ticker along the bottom')}
            </label>
            <label className="cx-check">
              <input type="checkbox" checked={!!m.sportsCountdown} onChange={(e) => change({ sportsCountdown: e.target.checked })} /> {t('Countdown to the next kick-off')}
            </label>
            <label className="cx-check">
              <input type="checkbox" checked={!!m.sportsTeamsOnly} onChange={(e) => change({ sportsTeamsOnly: e.target.checked })} /> {t('Only the teams we follow')}
            </label>
          </div>
          <label className="cx-check">
            <input type="checkbox" checked={!!m.sportsScores} onChange={(e) => change({ sportsScores: e.target.checked })} /> {t('Show scores')}
          </label>
          <p className="hint">{t('Off, this screen never shows a score or a result, so it won’t spoil a game you’ll watch later. Live scores update every minute; a TheSportsDB key (Settings > Extras > Sports) adds the clock and the teams’ crests.')}</p>
          {leagues && leagues.length > 0 && (
            <div style={row}>
              <span className="hint">{t('Leagues to show (none ticked shows them all):')}</span>
              {leagues.map((l) => (
                <label key={l} className="cx-check">
                  <input type="checkbox" checked={chosen.includes(l)} onChange={(e) => toggle(l, e.target.checked)} /> {l}
                </label>
              ))}
            </div>
          )}
          <p className="hint">{t('The Scoreboard and Tonight’s games scenes are in the list of scenes above.')}</p>
        </>
      )}
    </>
  )
}
