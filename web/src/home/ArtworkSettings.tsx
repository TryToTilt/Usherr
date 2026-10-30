// Usherr: Re-Posterr (Settings → Re-Posterr, /settings/re-posterr): what Usherr would change on
// Jellyfin's titles, to approve or skip (the side by side), what it changed
// (to put back), and how it works: ask first, fill in only or on its own; at
// night, when nobody is watching; fanart.tv's key.
import { useCallback, useEffect, useState } from 'react'
import Icon from '../components/Icon'
import Switch from '../components/Switch'
import { useConfirm } from '../components/ConfirmProvider'
import { useToast } from '../components/Toast'
import { t } from '../i18n'
import { useDocumentTitle } from '../documentTitle'
import { ART_LABEL, artApi, artImgError, candLine, changeLook, currentArt, type ArtAll, type ArtChange, type ArtSettings } from './artApi'
import ArtStyleDesigner from './ArtStyleDesigner'
import './preshowSettings.css'
import './artwork.css'

const WHY: Record<ArtChange['why'], string> = { missing: t('Missing'), small: t('Too small'), chosen: t('Chosen by hand'), styled: t('In your style') }

// How a change's picture is shown: tall for posters, wide for title cards.
const shape = (c: ArtChange) => (c.type === 'seasons' ? 'poster' : c.type === 'cards' ? 'backdrop' : c.type)
// Its picture: drawn in the style, or the one picked.
const picOf = (c: ArtChange) => (c.style ? changeLook(c.id) : c.preview || c.url)
const hour = (h: number) => new Date(2000, 0, 1, h).toLocaleTimeString(undefined, { hour: 'numeric' })

export default function ArtworkSettings() {
  useDocumentTitle('Re-Posterr')
  const toast = useToast()
  const confirm = useConfirm()
  const [all, setAll] = useState<ArtAll | null>(null)
  const [waiting, setWaiting] = useState<ArtChange[]>([])
  const [done, setDone] = useState<ArtChange[]>([])
  const [key, setKey] = useState('')
  const [tvdbKey, setTvdbKey] = useState('')
  const load = useCallback(() => {
    artApi
      .all()
      .then(setAll)
      .catch((e: Error) => toast.error(e.message))
    artApi.changes('waiting', 60).then(setWaiting).catch(() => undefined)
    artApi.changes('done', 30).then(setDone).catch(() => undefined)
  }, [toast])
  useEffect(() => load(), [load])
  // While a look goes on, follow it.
  useEffect(() => {
    if (!all?.status.running) return
    const id = window.setInterval(load, 6000)
    return () => window.clearInterval(id)
  }, [all?.status.running, load])
  if (!all) return <div>{t('Loading…')}</div>

  const act = async (what: () => Promise<unknown>, ok?: string) => {
    try {
      await what()
      if (ok) toast.success(ok)
      load()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    }
  }
  const set = all.settings
  const save = (patch: Partial<ArtSettings> & { fanartKey?: string; removeFanartKey?: boolean; tvdbKey?: string; removeTvdbKey?: boolean }) => void act(() => artApi.saveSettings({ ...set, ...patch }))
  const c = all.counts
  const st = all.status

  return (
    <div className="art-page">
      <div className="page-header">
        <h1>Re-Posterr</h1>
      </div>

      <section className="card">
        <p>{t('Re-Posterr picks the poster, background and logo for each film, show and collection in Jellyfin, from TMDB and fanart.tv: your language first, backgrounds without words, the best liked, big enough. Or it makes them in your style (below). Only pictures change, never a file, and every change can be put back.')}</p>
        {!all.jellyfin && <p className="art-warn">{t('Connect Jellyfin first (Settings → Media servers).')}</p>}
        {!all.tmdb && <p className="art-warn">{t('A TMDB key is needed (Settings → Metadata).')}</p>}
        <div className="art-counts">
          <span title={t('Films, shows and collections in Jellyfin with a TMDB id; a film in two libraries counts once')}>
            <strong>{c.titles}</strong> {t('titles')}
          </span>
          <span title={t('Looked at for artwork so far')}>
            <strong>{c.looked}</strong> {t('looked at')}
          </span>
          <span title={t('Titles with no poster in Jellyfin')}>
            <strong>{c.missingPoster}</strong> {t('without a poster')}
          </span>
          <span title={t('Titles with no background in Jellyfin')}>
            <strong>{c.missingBackdrop}</strong> {t('without a background')}
          </span>
          <span title={t('Titles with no logo in Jellyfin')}>
            <strong>{c.missingLogo}</strong> {t('without a logo')}
          </span>
        </div>
        <div className="psm-actions">
          <button type="button" className="btn-with-icon" disabled={st.running || !all.jellyfin} onClick={() => void act(artApi.look, t('Looking now.'))} title={t('Looks now instead of tonight; it still stops if someone starts watching')}>
            <Icon name="search" size={16} /> {st.running ? t('Looking… {n} so far', { n: st.looked }) : t('Look now')}
          </button>
          <span className="hint">
            {st.lastMs ? t('Last look: {when}, {n} titles, {p} proposed, {a} changed.', { when: new Date(st.lastMs).toLocaleString(), n: st.looked, p: st.proposed, a: st.applied }) : t('It looks every night between {from} and {to}.', { from: hour(set.from), to: hour(set.to) })}
            {st.freeGB > 0 && ` · ${t('Jellyfin’s disk: {n} GB free', { n: Math.round(st.freeGB) })}`}
          </span>
        </div>
        {st.note && <p className="art-warn">{st.note}</p>}
      </section>

      <ArtStyleDesigner />

      <section className="card">
        <div className="art-sec-head">
          <h2>{t('Waiting for you')}</h2>
          {c.waiting > 0 && (
            <div className="psm-actions">
              <button
                type="button"
                className="btn-sm primary"
                title={t('Sends every waiting picture to Jellyfin, a picture a second; each can be put back')}
                onClick={async () => {
                  if (await confirm({ title: t('Use all {n}?', { n: c.waiting }), body: t('Every waiting picture goes to Jellyfin, a picture a second. The pictures they replace are kept, so each can be put back.'), confirmLabel: t('Use all') })) void act(() => artApi.apply({ all: true }), t('Sending them to Jellyfin.'))
                }}
              >
                {t('Use all {n}', { n: c.waiting })}
              </button>
              <button type="button" className="btn-sm" title={t('None of them; they aren’t proposed again')} onClick={() => void act(() => artApi.skip({ all: true }))}>
                {t('Skip all')}
              </button>
            </div>
          )}
        </div>
        <p className="hint">{set.mode === 'ask' ? t('Usherr’s picks, beside what Jellyfin has now. Nothing changes until you say so.') : t('With Usherr putting pictures on by itself, only what it can’t decide waits here.')}</p>
        {waiting.length === 0 ? (
          <p className="psm-none">{t('Nothing waiting.')}</p>
        ) : (
          <ul className="art-review">
            {waiting.map((w) => (
              <li key={w.id}>
                <div className="art-pair">
                  {w.type !== 'seasons' && w.type !== 'cards' && (
                    <>
                      <figure className={`art-${w.type}`}>
                        {w.current && w.why !== 'missing' ? <img src={currentArt(w.current, w.type)} alt="" loading="lazy" /> : <span className="art-none">{t('None')}</span>}
                        <figcaption>{t('Now')}</figcaption>
                      </figure>
                      <Icon name="chevron-right" size={18} />
                    </>
                  )}
                  <figure className={`art-${shape(w)}`}>
                    <img src={picOf(w)} alt="" loading="lazy" onError={(e) => artImgError(e, w.style ? '' : w.url, () => (e.currentTarget.style.visibility = 'hidden'))} />
                    <figcaption>{w.style ? (w.type === 'poster' ? t('In your style') : t('In your style; the first of them')) : candLine(w)}</figcaption>
                  </figure>
                </div>
                <div className="art-review-main">
                  <strong>{w.title}</strong>
                  <span className="hint">
                    {ART_LABEL[w.type]} · {WHY[w.why]}
                  </span>
                </div>
                <div className="psm-actions">
                  <button type="button" className="btn-sm primary" title={t('Sends it to Jellyfin; the one there now is kept')} onClick={() => void act(() => artApi.apply({ ids: [w.id] }), t('Sent to Jellyfin.'))}>
                    {t('Use it')}
                  </button>
                  <button type="button" className="btn-sm" title={t('Keep what Jellyfin has; it isn’t proposed again')} onClick={() => void act(() => artApi.skip({ ids: [w.id] }))}>
                    {t('Skip')}
                  </button>
                </div>
              </li>
            ))}
          </ul>
        )}
        {c.waiting > waiting.length && <p className="hint">{t('{n} more after these.', { n: c.waiting - waiting.length })}</p>}
      </section>

      <section className="card">
        <h2>{t('How it works')}</h2>
        <Switch checked={set.on} onChange={(on) => save({ on })} label={t('Look every night')} description={t('A title every few seconds, between the hours below, stopping when someone starts watching on Jellyfin.')} />
        <div className="psm-settings">
          <label className="psm-field field">
            <span>{t('What it does')}</span>
            <select value={set.mode} onChange={(e) => save({ mode: e.target.value as ArtSettings['mode'] })} title={t('Ask first is the safest: nothing changes until you approve it')}>
              <option value="ask">{t('Ask me first (picks wait here)')}</option>
              <option value="fill">{t('Fill in what’s missing, by itself')}</option>
              <option value="auto">{t('Fill in and replace pictures that are too small, by itself')}</option>
            </select>
          </label>
          <label className="psm-field field">
            <span>{t('From')}</span>
            <select value={set.from} onChange={(e) => save({ from: Number(e.target.value) })} title={t('When the nightly look may start')}>
              {Array.from({ length: 24 }, (_, h) => (
                <option key={h} value={h}>
                  {hour(h)}
                </option>
              ))}
            </select>
          </label>
          <label className="psm-field field">
            <span>{t('To')}</span>
            <select value={set.to} onChange={(e) => save({ to: Number(e.target.value) })} title={t('When it stops for the day; it carries on the next night')}>
              {Array.from({ length: 24 }, (_, h) => (
                <option key={h} value={h}>
                  {hour(h)}
                </option>
              ))}
            </select>
          </label>
          <label className="psm-field field">
            <span>{t('Keep free on Jellyfin’s disk')}</span>
            <select value={set.minFreeGB} onChange={(e) => save({ minFreeGB: Number(e.target.value) })} title={t('Pictures go to Jellyfin’s metadata folder; with less room than this, nothing is sent in bulk')}>
              {[5, 10, 20, 30, 50].map((g) => (
                <option key={g} value={g}>
                  {t('{n} GB', { n: g })}
                </option>
              ))}
            </select>
          </label>
        </div>
        <Switch checked={set.textless} onChange={(textless) => save({ textless })} label={t('Backgrounds without words')} description={t('A clean picture, so the Marquee and the pre-show can put the title on it themselves.')} />

        <h3>fanart.tv</h3>
        <p className="hint">{t('Optional. More logos, textless posters and backgrounds, above all for older films and smaller shows. A free personal key from fanart.tv (your profile, API keys).')}</p>
        <div className="psm-actions">
          {set.fanartKeySet ? (
            <>
              <span className="psm-tag on">{t('Key saved')}</span>
              <button type="button" className="btn-sm" title={t('Asks fanart.tv for a well-known film’s art with the key')} onClick={() => void act(async () => toast.success(t('fanart.tv answered with {n} pictures.', { n: (await artApi.testFanart()).pictures })))}>
                {t('Test it')}
              </button>
              <button type="button" className="btn-sm" title={t('Usherr goes back to TMDB alone')} onClick={() => save({ removeFanartKey: true })}>
                {t('Remove the key')}
              </button>
            </>
          ) : (
            <>
              <input type="password" value={key} onChange={(e) => setKey(e.target.value)} placeholder={t('fanart.tv key')} aria-label={t('fanart.tv key')} autoComplete="off" />
              <button
                type="button"
                className="btn-sm primary"
                disabled={!key.trim()}
                title={t('Kept encrypted with Usherr’s other keys')}
                onClick={() => {
                  save({ fanartKey: key.trim() })
                  setKey('')
                }}
              >
                {t('Save')}
              </button>
            </>
          )}
        </div>
        <h3>TheTVDB</h3>
        <p className="hint">{t('Optional, for shows: more posters, backgrounds and logos, above all for anime and older shows. A TheTVDB API key (thetvdb.com, your account, API keys).')}</p>
        <div className="psm-actions">
          {set.tvdbKeySet ? (
            <>
              <span className="psm-tag on">{t('Key saved')}</span>
              <button type="button" className="btn-sm" title={t('Asks TheTVDB for a well-known show’s art with the key')} onClick={() => void act(async () => toast.success(t('TheTVDB answered with {n} pictures.', { n: (await artApi.testTvdb()).pictures })))}>
                {t('Test it')}
              </button>
              <button type="button" className="btn-sm" title={t('Usherr stops asking TheTVDB')} onClick={() => save({ removeTvdbKey: true })}>
                {t('Remove the key')}
              </button>
            </>
          ) : (
            <>
              <input type="password" value={tvdbKey} onChange={(e) => setTvdbKey(e.target.value)} placeholder={t('TheTVDB key')} aria-label={t('TheTVDB key')} autoComplete="off" />
              <button
                type="button"
                className="btn-sm primary"
                disabled={!tvdbKey.trim()}
                title={t('Kept encrypted with Usherr’s other keys')}
                onClick={() => {
                  save({ tvdbKey: tvdbKey.trim() })
                  setTvdbKey('')
                }}
              >
                {t('Save')}
              </button>
            </>
          )}
        </div>

        <h3>Posterizarr</h3>
        <p className="hint">{t('Optional. The full Posterizarr app, for anything Re-Posterr doesn’t do. Usherr links to it, and it can take over making the posters.')}</p>
        <div className="psm-actions">
          <input type="url" defaultValue={set.posterizarr} placeholder="http://host.docker.internal:8000" aria-label={t('Posterizarr’s address')} onBlur={(e) => e.target.value.trim() !== set.posterizarr && save({ posterizarr: e.target.value.trim() })} title={t('As Usherr reaches it')} />
          {set.posterizarr && (
            <>
              <button type="button" className="btn-sm" onClick={() => void act(async () => (await artApi.testPosterizarr(), toast.success(t('Posterizarr answered.'))))}>
                {t('Test it')}
              </button>
              <a className="btn-sm" href={set.posterizarr} target="_blank" rel="noreferrer">
                {t('Open Posterizarr')}
              </a>
            </>
          )}
        </div>
        {set.posterizarr && (
          <Switch
            checked={set.posterizarrOwns}
            onChange={(posterizarrOwns) => save({ posterizarrOwns })}
            label={t('Let Posterizarr make the posters')}
            description={t('Re-Posterr’s style stands aside in the nightly look; it still fills in what’s missing, and by hand everything works as before.')}
          />
        )}

        <details className="psm-jf-more">
          <summary>{t('Where replaced pictures are kept')}</summary>
          <label className="psm-field field">
            <span>{t('Folder')}</span>
            <input type="text" defaultValue={set.folder} placeholder={all.folder} onBlur={(e) => e.target.value.trim() !== set.folder && save({ folder: e.target.value.trim() })} title={t('Empty: /data/artwork when it is mapped, otherwise artwork in Usherr’s config folder')} />
            <small className="hint">{t('As Usherr sees it. Each picture Usherr replaces is kept here first, so it can be put back.')}</small>
          </label>
        </details>
      </section>

      <section className="card">
        <h2>{t('Changed lately')}</h2>
        {done.length === 0 ? (
          <p className="psm-none">{t('Nothing changed yet.')}</p>
        ) : (
          <ul className="art-done">
            {done.map((d) => (
              <li key={d.id}>
                <img className={`art-${shape(d)}`} src={picOf(d)} alt="" loading="lazy" onError={(e) => artImgError(e, d.style ? '' : d.url, () => (e.currentTarget.style.visibility = 'hidden'))} />
                <div className="art-review-main">
                  <strong>{d.title}</strong>
                  <span className="hint">
                    {ART_LABEL[d.type]} · {WHY[d.why]} · {d.doneMs ? new Date(d.doneMs).toLocaleDateString() : ''}
                  </span>
                </div>
                <button type="button" className="btn-sm" title={t('Puts back the picture Jellyfin had before')} onClick={() => void act(() => artApi.undo(d.id), t('The old picture is back.'))}>
                  {t('Put the old one back')}
                </button>
              </li>
            ))}
          </ul>
        )}
        {c.failed > 0 && <p className="hint">{t('{n} couldn’t be sent; they’re proposed again on a later look.', { n: c.failed })}</p>}
      </section>
    </div>
  )
}
