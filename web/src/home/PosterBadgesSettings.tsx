// Usherr: Settings > Re-Posterr > Poster badges (/settings/poster-badges). Small badges on the
// posters Jellyfin and Plex show: 4K / 1080p, HDR or Dolby Vision, Atmos and the like, the
// rating, "New" (home_badges.go). Kometa's overlays job. Always drawn on the original poster,
// which is kept, so turning badges off puts the originals back.
import { useCallback, useEffect, useState } from 'react'
import Switch from '../components/Switch'
import { useToast } from '../components/Toast'
import { plural, t } from '../i18n'
import { useDocumentTitle } from '../documentTitle'
import { badgeApi, type BadgeAll, type BadgeCorner, type BadgeSettings } from './collectionsApi'
import './collections.css'

const CORNERS: { value: BadgeCorner; label: string }[] = [
  { value: 'top-left', label: t('Top left') },
  { value: 'top-right', label: t('Top right') },
  { value: 'bottom-left', label: t('Bottom left') },
  { value: 'bottom-right', label: t('Bottom right') },
]

export default function PosterBadgesSettings() {
  useDocumentTitle(t('Poster badges'))
  const toast = useToast()
  const [all, setAll] = useState<BadgeAll | null>(null)
  const [form, setForm] = useState<BadgeSettings | null>(null)
  const [copy, setCopy] = useState('')
  const [pic, setPic] = useState('')
  const [picErr, setPicErr] = useState('')
  const [busy, setBusy] = useState(false)
  const load = useCallback(() => {
    badgeApi
      .all()
      .then((a) => {
        setAll(a)
        setForm((f) => f ?? a.settings)
        setCopy((c) => c || a.samples[0]?.copy || '')
      })
      .catch((e: Error) => toast.error(e.message))
  }, [toast])
  useEffect(load, [load])
  useEffect(() => {
    if (!all?.status.running) return
    const id = window.setInterval(load, 5000)
    return () => window.clearInterval(id)
  }, [all?.status.running, load])
  // The preview follows the settings as they're changed (a moment after).
  useEffect(() => {
    if (!form || !copy) return
    const ctl = new AbortController()
    const id = window.setTimeout(() => {
      badgeApi
        .preview(form, copy, ctl.signal)
        .then((url) => {
          setPic((old) => {
            if (old) URL.revokeObjectURL(old)
            return url
          })
          setPicErr('')
        })
        .catch((e: Error) => {
          if (e.name !== 'AbortError') setPicErr(e.message)
        })
    }, 350)
    return () => {
      window.clearTimeout(id)
      ctl.abort()
    }
  }, [form, copy])
  if (!all || !form) return <div>{t('Loading…')}</div>

  const set = (patch: Partial<BadgeSettings>) => setForm({ ...form, ...patch })
  const changed = JSON.stringify(form) !== JSON.stringify(all.settings)
  const st = all.status

  async function save() {
    if (!form) return
    setBusy(true)
    try {
      const out = await badgeApi.save(form)
      setForm(out)
      setAll((a) => (a ? { ...a, settings: out, status: { ...a.status, running: true } } : a))
      toast.success(out.on ? t('Saved. Usherr draws the badges now; it takes a while for a big library.') : t('Saved. Usherr puts the original posters back now.'))
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  const box = (key: 'res' | 'hdr' | 'audio' | 'channels' | 'rating' | 'new', label: string) => (
    <label>
      <input type="checkbox" checked={form[key]} onChange={(e) => set({ [key]: e.target.checked })} />
      {label}
    </label>
  )

  return (
    <div className="settings-stack">
      <section className="card wide">
        <h2>{t('Poster badges')}</h2>
        <p style={{ color: 'var(--text-dim)', marginTop: 0 }}>
          {t('Small badges on the posters Jellyfin and Plex show: the picture (4K, 1080p), HDR or Dolby Vision, the best sound (Atmos, DTS:X, TrueHD, DTS-HD MA), the rating, and "New". Usherr keeps each original poster and always draws on it, so badges never pile up, and turning them off puts the originals back. A poster that changes some other way (by hand, or by Re-Posterr) becomes the new original.')}
        </p>
        {!all.servers && <p className="pcol-warn">{t('Connect Jellyfin or Plex first (Settings → Media servers).')}</p>}
        {all.note && <p className="pcol-warn">{all.note}</p>}
        <Switch checked={form.on} onChange={(v) => set({ on: v })} showState label={t('Badges on posters')} description={t('Off by default. Only the libraries ticked below get them.')} />
        <div className="badge-grid">
          <div className="grid-form">
            <fieldset className="pcol-checks">
              <legend>{t('Libraries')}</legend>
              <div className="badge-libs">
                {all.libraries.length === 0 && <span className="field-hint">{t('No libraries found on your media servers.')}</span>}
                {all.libraries.map((l) => (
                  <label key={l}>
                    <input type="checkbox" checked={form.libraries.includes(l)} onChange={(e) => set({ libraries: e.target.checked ? [...form.libraries, l] : form.libraries.filter((x) => x !== l) })} />
                    {l}
                  </label>
                ))}
              </div>
            </fieldset>
            <fieldset className="pcol-checks">
              <legend>{t('Badges')}</legend>
              {box('res', t('4K / 1080p'))}
              {box('hdr', t('HDR / Dolby Vision'))}
              {box('audio', t('Atmos, DTS:X, TrueHD'))}
              {box('channels', t('5.1 / 7.1'))}
              {box('rating', t('Rating'))}
              {box('new', t('New'))}
            </fieldset>
            <p className="field-hint">{t('Picture and sound badges are for films; shows get the rating and New.')}</p>
            {form.rating && (
              <label>
                {t('Rating from')}
                <select value={form.ratingFrom} onChange={(e) => set({ ratingFrom: e.target.value as BadgeSettings['ratingFrom'] })}>
                  <option value="server">{t('Your media server (TMDB’s, usually)')}</option>
                  <option value="imdb">{t('IMDb (from MDBList scores Usherr already has)')}</option>
                </select>
              </label>
            )}
            {form.new && (
              <label>
                {t('New means added in the last (days)')}
                <input type="number" min={1} max={365} value={form.newDays} onChange={(e) => set({ newDays: Number(e.target.value) || 14 })} />
              </label>
            )}
            <label>
              {t('Picture and sound badges')}
              <select value={form.qualityAt} onChange={(e) => set({ qualityAt: e.target.value as BadgeCorner })}>
                {CORNERS.map((c) => (
                  <option key={c.value} value={c.value}>
                    {c.label}
                  </option>
                ))}
              </select>
            </label>
            <label>
              {t('Rating and New')}
              <select value={form.ratingAt} onChange={(e) => set({ ratingAt: e.target.value as BadgeCorner })}>
                {CORNERS.map((c) => (
                  <option key={c.value} value={c.value}>
                    {c.label}
                  </option>
                ))}
              </select>
            </label>
            <label>
              {t('Look')}
              <select value={form.look} onChange={(e) => set({ look: e.target.value as BadgeSettings['look'] })}>
                <option value="dark">{t('Dark, see-through')}</option>
                <option value="color">{t('Colours')}</option>
              </select>
            </label>
            <label>
              {t('Size')}
              <select value={form.size} onChange={(e) => set({ size: e.target.value as BadgeSettings['size'] })}>
                <option value="small">{t('Small')}</option>
                <option value="medium">{t('Medium')}</option>
                <option value="large">{t('Large')}</option>
              </select>
            </label>
          </div>
          <div className="badge-preview">
            {all.samples.length > 0 ? (
              <>
                <select value={copy} onChange={(e) => setCopy(e.target.value)} aria-label={t('Title to preview')}>
                  {all.samples.map((s) => (
                    <option key={s.copy} value={s.copy}>
                      {s.title}
                      {s.year ? ` (${s.year})` : ''} · {s.library}
                    </option>
                  ))}
                </select>
                {pic && <img src={pic} alt={t('Preview of the poster with badges')} />}
                {picErr && <p className="field-hint">{picErr}</p>}
              </>
            ) : (
              <p className="field-hint">{t('A preview shows here once Usherr can read a film from your media servers.')}</p>
            )}
          </div>
        </div>
        <div className="row-actions">
          <button type="button" className="primary" disabled={busy || !changed} onClick={() => void save()}>
            {t('Save')}
          </button>
          <button
            type="button"
            className="btn-sm"
            disabled={st.running || !all.settings.on}
            onClick={() =>
              void badgeApi
                .apply()
                .then(() => load())
                .catch((e: Error) => toast.error(e.message))
            }
          >
            {st.running ? t('Drawing…') : t('Draw now')}
          </button>
        </div>
        <p className="field-hint">
          {st.lastMs ? t('Last pass {when}: {d} drawn, {r} put back.', { when: new Date(st.lastMs).toLocaleString(), d: st.drawn, r: st.restored }) : t('Usherr checks every hour and draws again when a file or a rating changes.')}{' '}
          {st.badged > 0 && plural(st.badged, '{n} poster has badges now.', '{n} posters have badges now.')}
          {st.failed > 0 && ` ${plural(st.failed, '{n} poster couldn’t be drawn.', '{n} posters couldn’t be drawn.')}`}
        </p>
        {st.note && <p className="pcol-warn">{st.note}</p>}
      </section>
    </div>
  )
}
