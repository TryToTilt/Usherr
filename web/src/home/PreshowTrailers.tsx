// Usherr: the coming-soon trailers on the Pre-show page: what's downloaded,
// what's waiting, the space they take, and when they come and go
// (home_preshow_trailers.go).
import { useCallback, useEffect, useState } from 'react'
import Icon from '../components/Icon'
import Switch from '../components/Switch'
import { useToast } from '../components/Toast'
import { t } from '../i18n'
import { preshowApi, soonLine, type PreshowTrailer, type TrailerSettings, type TrailersAll } from './preshowApi'

const gb = (bytes: number) => (bytes >= 1 << 30 ? t('{n} GB', { n: (bytes / (1 << 30)).toFixed(1) }) : t('{n} MB', { n: Math.round(bytes / (1 << 20)) }))
const mins = (secs: number) => `${Math.floor(secs / 60)}:${String(Math.round(secs % 60)).padStart(2, '0')}`

const WHY: Record<PreshowTrailer['why'], string> = { wanted: t('Wanted'), cinema: t('In cinemas'), season: t('New season') }

export default function PreshowTrailers() {
  const toast = useToast()
  const [all, setAll] = useState<TrailersAll | null>(null)
  const [showAll, setShowAll] = useState(false)
  const load = useCallback(() => {
    preshowApi
      .trailers()
      .then(setAll)
      .catch((e: Error) => toast.error(e.message))
  }, [toast])
  useEffect(() => load(), [load])
  // While it's downloading, look again now and then.
  useEffect(() => {
    if (!all?.busy) return
    const id = window.setInterval(load, 8000)
    return () => window.clearInterval(id)
  }, [all?.busy, load])
  if (!all) return null

  const act = async (what: () => Promise<unknown>) => {
    try {
      await what()
      load()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    }
  }
  const set = all.settings
  const save = (patch: Partial<TrailerSettings>) => void act(() => preshowApi.saveTrailerSettings({ ...set, ...patch }))
  const ready = all.trailers.filter((x) => x.state === 'ready').length
  const waiting = all.trailers.filter((x) => x.state === 'waiting').length

  return (
    <section className="card psm-tr">
      <h2>{t('Coming-soon trailers')}</h2>
      <p>
        {t('Usherr downloads the trailers of what’s on its way: films you want, films coming to cinemas and your shows’ new seasons. They play at once, in the Jellyfin app too, and go again after the release.')}
      </p>
      {!all.ytdlp && <p className="psm-warn">{t('This Usherr has no yt-dlp, so trailers can’t be downloaded. The pre-show plays them from YouTube instead.')}</p>}
      <Switch checked={set.on} onChange={(on) => save({ on })} label={t('Download coming-soon trailers')} />
      <div className="psm-actions">
        <button type="button" className="btn-with-icon" disabled={!set.on || all.busy} title={t('Looks for trailers and downloads a few now, instead of waiting for the next round')} onClick={() => void act(async () => {
          await preshowApi.fetchTrailers()
          toast.success(t('Looking for trailers now.'))
          window.setTimeout(load, 1500)
        })}>
          <Icon name="download" size={16} /> {all.busy ? t('Downloading…') : t('Look now')}
        </button>
        <span className="hint">
          {t('{ready} ready, {waiting} waiting · {used} of {cap} GB', { ready, waiting, used: gb(all.used), cap: set.capGB })}
        </span>
      </div>

      {all.trailers.length > 0 && (
        <ul className="psm-trs">
          {(showAll ? all.trailers : all.trailers.slice(0, 8)).map((x) => (
            <li key={x.id} className={`psm-tr-${x.state}`}>
              {x.posterUrl ? <img src={x.posterUrl} alt="" loading="lazy" /> : <span className="psm-tr-ph" />}
              <div className="psm-tr-main">
                <strong>
                  {x.title}
                  {x.season ? ` · ${t('Season {n}', { n: x.season })}` : ''}
                </strong>
                <span className="hint">
                  {soonLine(x.releaseDate, x.why, x.season)} · {WHY[x.why] ?? x.why}
                </span>
                <span className="hint">
                  {x.state === 'ready' && `${t('Ready')} · ${mins(x.seconds)} · ${gb(x.size)}`}
                  {x.state === 'waiting' && t('Waiting to download')}
                  {x.state === 'failed' && `${t('Not downloaded')}: ${x.error ?? ''}`}
                </span>
              </div>
              <div className="psm-tr-acts">
                {x.state === 'ready' && x.fileUrl && (
                  <a className="btn-sm" href={x.fileUrl} target="_blank" rel="noreferrer" title={t('Plays the downloaded trailer in a new tab')}>
                    {t('Watch')}
                  </a>
                )}
                {x.state === 'failed' && (
                  <button type="button" className="btn-sm" title={x.error ?? t('Downloads it again in the next round')} onClick={() => void act(() => preshowApi.saveTrailer(x.id, { again: true }))}>
                    {t('Try again')}
                  </button>
                )}
                <label className="cx-check" title={t('Kept past its release and the space limit')}>
                  <input type="checkbox" checked={x.keep} onChange={(e) => void act(() => preshowApi.saveTrailer(x.id, { keep: e.target.checked }))} /> {t('Keep')}
                </label>
                <button type="button" className="icon-btn" aria-label={t('Remove')} title={t('Remove (it isn’t downloaded again)')} onClick={() => void act(() => preshowApi.removeTrailer(x.id))}>
                  <Icon name="x" size={16} />
                </button>
              </div>
            </li>
          ))}
        </ul>
      )}
      {all.trailers.length > 8 && (
        <button type="button" className="btn-sm" onClick={() => setShowAll((v) => !v)}>
          {showAll ? t('Show fewer') : t('Show all {n}', { n: all.trailers.length })}
        </button>
      )}
      {all.trailers.length === 0 && set.on && <p className="psm-none">{t('Nothing yet. Usherr looks every few hours, or press Look now.')}</p>}

      <details className="psm-jf-more">
        <summary>{t('When they come and go')}</summary>
        <div className="psm-settings">
          <label className="psm-field">
            <span>{t('Downloaded')}</span>
            <select value={set.days} title={t('How long before its release a trailer is downloaded')} onChange={(e) => save({ days: Number(e.target.value) })}>
              {[30, 45, 60, 90, 120].map((d) => (
                <option key={d} value={d}>
                  {t('{n} days before the release', { n: d })}
                </option>
              ))}
            </select>
          </label>
          <label className="psm-field">
            <span>{t('Removed')}</span>
            <select value={set.after} title={t('Kept trailers stay past this')} onChange={(e) => save({ after: Number(e.target.value) })}>
              {[0, 7, 14, 30, 60].map((d) => (
                <option key={d} value={d}>
                  {d === 0 ? t('On the release day') : t('{n} days after the release', { n: d })}
                </option>
              ))}
            </select>
            <small className="hint">{t('Or as soon as the film is in your library.')}</small>
          </label>
          <label className="psm-field">
            <span>{t('Space, at most')}</span>
            <select value={set.capGB} title={t('Over this, the earliest releases go first; kept ones stay')} onChange={(e) => save({ capGB: Number(e.target.value) })}>
              {[2, 5, 10, 20, 50].map((g) => (
                <option key={g} value={g}>
                  {t('{n} GB', { n: g })}
                </option>
              ))}
            </select>
          </label>
          <label className="psm-field">
            <span>{t('Quality')}</span>
            <select value={set.quality} title={t('A 4K trailer is about 200 MB, a 1080p one about 50 MB')} onChange={(e) => save({ quality: Number(e.target.value) as TrailerSettings['quality'] })}>
              <option value={2160}>{t('4K, or the best each trailer has')}</option>
              <option value={1440}>1440p</option>
              <option value={1080}>{t('1080p (smallest that plays everywhere as it is)')}</option>
              <option value={720}>720p</option>
            </select>
          </label>
          <label className="psm-field">
            <span>{t('Folder')}</span>
            <input type="text" defaultValue={set.folder} title={t('Empty: /data/trailers when it is mapped, otherwise trailers in Usherr’s config folder')} onBlur={(e) => e.target.value.trim() !== set.folder && save({ folder: e.target.value.trim() })} />
            <small className="hint">{t('As Usherr sees it.')}</small>
          </label>
        </div>
        <Switch checked={set.card} onChange={(card) => save({ card })} label={t('A coming-soon card before each trailer')} description={t('Its poster, its name and when it comes.')} />
      </details>
    </section>
  )
}
