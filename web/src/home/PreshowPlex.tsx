// Usherr: Settings > Movie night > Pre-show > In the Plex app. Plex plays
// the same videos before every film (its pre-roll, a Plex Pass feature), so
// Usherr puts a pre-show together every half hour (the theme's clips, the
// downloaded trailers, the cards that don't need the film), puts it in the
// virtual drive's Preshow folder and points Plex's pre-roll at it. The theme:
// the same as everywhere, or Plex's own (home_preshow_plex.go).
import { useCallback, useEffect, useState } from 'react'
import Icon from '../components/Icon'
import Switch from '../components/Switch'
import { useToast } from '../components/Toast'
import { locale, t } from '../i18n'
import { preshowApi, type PreshowPlex } from './preshowApi'

export default function PreshowPlex({ themes }: { themes: { id: number; name: string }[] }) {
  const toast = useToast()
  const [p, setP] = useState<PreshowPlex | null>(null)
  const load = useCallback(() => {
    preshowApi
      .plex()
      .then(setP)
      .catch(() => setP(null))
  }, [])
  useEffect(load, [load])
  if (!p || !p.plex) return null
  const save = (patch: Partial<Pick<PreshowPlex, 'on' | 'themeId'>>) =>
    preshowApi
      .savePlex({ on: p.on, themeId: p.themeId, ...patch })
      .then((d) => {
        setP(d)
        toast.success(t('Saved. The pre-show for Plex is being put together.'))
        setTimeout(load, 120_000) // it's in Plex a couple of minutes later
      })
      .catch((e: Error) => toast.error(e.message))
  return (
    <section className="card psm-jf" data-walk="preshow-plex">
      <h2>{t('In the Plex app')}</h2>
      <p>
        {t('Plex plays the same videos before every film (its pre-roll, with Plex Pass). Usherr puts a pre-show together for it every half hour, so it keeps changing: the theme’s clips, the trailers downloaded and the cards. The rating card is left out, as Plex doesn’t say which film is next.')}
      </p>
      {!p.drive && <p className="notice notice-warn">{t('Plex reads the pre-show from the virtual drive: switch it on first (Settings, Extras, Virtual drive).')}</p>}
      <div className="psm-jf-opts">
        <Switch checked={p.on} onChange={(on) => save({ on })} label={t('Before films in Plex')} />
        <label className="psm-field">
          <span>{t('Theme')}</span>
          <select value={p.themeId} onChange={(e) => save({ themeId: Number(e.target.value) })} title={t('The theme played in the Plex app: the same as everywhere, or its own')}>
            <option value={0}>{t('The same as everywhere (schedules, then the default)')}</option>
            {themes.map((th) => (
              <option key={th.id} value={th.id}>
                {t('Its own: {theme}', { theme: th.name })}
              </option>
            ))}
          </select>
        </label>
      </div>
      {p.on && (
        <p className="hint">
          <Icon name={p.note ? 'info' : 'check'} size={13} />{' '}
          {p.note
            ? p.note
            : p.videos > 0
              ? t('{n} videos in Plex’s pre-roll, from {theme}, put together {when}. Plex reads them from {where}.', {
                  n: p.videos,
                  theme: p.theme || t('the default theme'),
                  when: new Date(p.builtMs).toLocaleTimeString(locale(), { hour: 'numeric', minute: '2-digit' }),
                  where: p.where,
                })
              : t('Being put together.')}
        </p>
      )}
    </section>
  )
}
