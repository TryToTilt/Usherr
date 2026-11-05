// Usherr: the profile editor's Advanced options added on top of Mediarium's:
// sizes per quality, upgrade limits, TRaSH score sets, release groups, the
// profile's own language, where it searches, and export as JSON.
import { useEffect, useState } from 'react'
import { api, type IndexerConfig, type QualityProfileInput } from '../api'
import { AUDIO_LANGUAGES } from '../languages'
import type { ExtrasDraft, SizeRow } from '../profileExtras'
import { plural, t } from '../i18n'
import Icon from './Icon'
import { useToast } from './Toast'

// Groups of TRaSH Guides formats, by their names in the guides. Picking one
// imports the formats that are missing and gives each TRaSH's own score.
const SCORE_SETS: { key: string; label: () => string; names: string[] }[] = [
  { key: 'hdr', label: () => t('HDR formats'), names: ['DV', 'DV HDR10', 'DV HDR10+', 'DV (WEBDL)', 'HDR10+', 'HDR10', 'HDR', 'HDR (undefined)', 'PQ', 'HLG'] },
  { key: 'audio', label: () => t('Audio formats'), names: ['TrueHD ATMOS', 'DTS X', 'ATMOS (undefined)', 'DD+ ATMOS', 'TrueHD', 'DTS-HD MA', 'FLAC', 'PCM', 'DTS-HD HRA', 'DD+', 'DTS-ES', 'DTS', 'AAC', 'DD'] },
  { key: 'unwanted', label: () => t('Unwanted (fakes, LQ, upscales)'), names: ['BR-DISK', 'LQ', 'LQ (Release Title)', '3D', 'Upscaled', 'Extras', 'x265 (HD)', 'AV1', 'Bad Dual Groups', 'No-RlsGroup', 'Obfuscated', 'Retags', 'Scene'] },
  { key: 'streaming', label: () => t('Streaming services'), names: ['AMZN', 'ATVP', 'BCORE', 'CRAV', 'DSNP', 'HBO', 'HMAX', 'Hulu', 'MAX', 'NF', 'PCOK', 'PMTP', 'STAN', 'iT', 'MA', 'CRiT', 'ROKU', 'SHO'] },
  { key: 'editions', label: () => t('Editions (IMAX, Criterion…)'), names: ['IMAX', 'IMAX Enhanced', 'Criterion Collection', 'Remaster', '4K Remaster', 'Special Edition', 'Theatrical Cut', 'Open Matte', 'Hybrid', 'Masters of Cinema', 'Vinegar Syndrome'] },
  { key: 'fixes', label: () => t('Release fixes (Repack, Proper)'), names: ['Repack/Proper', 'Repack v2', 'Repack v3', 'Repack2', 'Repack3'] },
  { key: 'tiers', label: () => t('Release group tiers'), names: ['Remux Tier 01', 'Remux Tier 02', 'Remux Tier 03', 'UHD Bluray Tier 01', 'UHD Bluray Tier 02', 'UHD Bluray Tier 03', 'HD Bluray Tier 01', 'HD Bluray Tier 02', 'HD Bluray Tier 03', 'WEB Tier 01', 'WEB Tier 02', 'WEB Tier 03'] },
]

export interface AdvancedExtrasProps {
  x: ExtrasDraft
  onX: (x: ExtrasDraft) => void
  // The profile's qualities, best first, for the sizes table.
  qualities: string[]
  tierLabel: (t: string) => string
  // Adds custom format scores to the form (format id -> score as typed).
  onScores: (scores: Record<number, string>) => void
  // Formats were imported: the form's format list must be read again.
  onFormatsChanged: () => void
  body: QualityProfileInput
  problem: string | null
}

export default function ProfileAdvancedExtras({ x, onX, qualities, tierLabel, onScores, onFormatsChanged, body, problem }: AdvancedExtrasProps) {
  const toast = useToast()
  const [indexers, setIndexers] = useState<IndexerConfig[]>([])
  const [setBusy, setSetBusy] = useState(false)
  useEffect(() => {
    api
      .listIndexers()
      .then((list) => setIndexers(list))
      .catch(() => setIndexers([]))
  }, [])

  const size = (tier: string): SizeRow => x.sizes[tier] ?? { min: '', pref: '', max: '' }
  const setSize = (tier: string, row: SizeRow) => onX({ ...x, sizes: { ...x.sizes, [tier]: row } })
  const langs = x.audioLanguage
    .split(',')
    .map((l) => l.trim())
    .filter(Boolean)
  const langName = (l: string) => (l === 'Original' ? t('Original language of the title') : l)

  async function addSet(key: string) {
    const set = SCORE_SETS.find((s) => s.key === key)
    if (!set) return
    const app = x.kind === 'tv' || x.kind === 'anime' ? 'sonarr' : 'radarr'
    setSetBusy(true)
    try {
      const guide = await api.trashGuides(app)
      const wanted = new Set(set.names.map((n) => n.toLowerCase()))
      const picked = guide.formats.filter((f) => wanted.has(f.name.toLowerCase()))
      if (picked.length === 0) {
        toast.info(t('The TRaSH Guides have none of these formats for {app} right now.', { app: app === 'sonarr' ? t('shows') : t('movies') }))
        return
      }
      const missing = picked.filter((f) => !f.importedId).map((f) => f.trashId)
      if (missing.length > 0) await api.importTrashFormats(app, missing)
      const stored = await api.listCustomFormats()
      const byTrash = new Map(stored.formats.filter((f) => f.trashId).map((f) => [f.trashId as string, f.id]))
      const scores: Record<number, string> = {}
      for (const f of picked) {
        const id = f.importedId || byTrash.get(f.trashId)
        if (id) scores[id] = String(f.defaultScore)
      }
      onScores(scores)
      onFormatsChanged()
      toast.success(plural(Object.keys(scores).length, 'Added {n} format with the TRaSH score. Save the profile to keep it.', 'Added {n} formats with the TRaSH scores. Save the profile to keep them.'))
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setSetBusy(false)
    }
  }

  function exportJSON() {
    const blob = new Blob([JSON.stringify(body, null, 2) + '\n'], { type: 'application/json' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `${(body.name || 'profile').replace(/[^\w .-]+/g, '_')}.json`
    document.body.appendChild(a)
    a.click()
    a.remove()
    setTimeout(() => URL.revokeObjectURL(url), 1000)
  }

  return (
    <>
      <label>
        {t('Used for')}
        <select value={x.kind} onChange={(e) => onX({ ...x, kind: e.target.value as ExtrasDraft['kind'] })}>
          <option value="">{t('Movies and TV')}</option>
          <option value="movies">{t('Movies')}</option>
          <option value="tv">{t('TV')}</option>
          <option value="anime">{t('Anime')}</option>
        </select>
        <small className="pa-hint">{t('A label for the profile lists. It also picks which TRaSH formats the score sets use.')}</small>
      </label>

      <fieldset className="profile-fieldset">
        <legend>{t('Sizes per quality')}</legend>
        <p className="pa-hint">{t('In GB, for this profile only: per file for a movie, per episode for a show. Empty means no limit of its own; the largest download above and the size limits under Settings, Quality still apply.')}</p>
        <div className="pa-scroll">
          <table className="pa-sizes">
            <thead>
              <tr>
                <th>{t('Quality')}</th>
                <th>{t('Smallest')}</th>
                <th>{t('Preferred')}</th>
                <th>{t('Largest')}</th>
              </tr>
            </thead>
            <tbody>
              {qualities.map((q) => {
                const r = size(q)
                return (
                  <tr key={q}>
                    <td>{tierLabel(q)}</td>
                    <td>
                      <input inputMode="decimal" aria-label={t('Smallest {q} size in GB', { q })} value={r.min} placeholder="–" onChange={(e) => setSize(q, { ...r, min: e.target.value })} />
                    </td>
                    <td>
                      <input inputMode="decimal" aria-label={t('Preferred {q} size in GB', { q })} value={r.pref} placeholder="–" onChange={(e) => setSize(q, { ...r, pref: e.target.value })} />
                    </td>
                    <td>
                      <input inputMode="decimal" aria-label={t('Largest {q} size in GB', { q })} value={r.max} placeholder={t('no limit')} onChange={(e) => setSize(q, { ...r, max: e.target.value })} />
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        </div>
        <label className="check-hint">
          <input type="checkbox" checked={x.preferSmaller} onChange={(e) => onX({ ...x, preferSmaller: e.target.checked })} />
          <span>
            {t('Pick the smaller file when two are equally good')}
            <small>{t('A preferred size above wins over this: the release nearest it is picked.')}</small>
          </span>
        </label>
        <label className="check-hint">
          <input type="checkbox" checked={x.preferHevc} onChange={(e) => onX({ ...x, preferHevc: e.target.checked })} />
          <span>
            {t('Prefer x265/HEVC at the same quality')}
            <small>{t('Smaller files that look the same.')}</small>
          </span>
        </label>
      </fieldset>

      <fieldset className="profile-fieldset">
        <legend>{t('Upgrade limits')}</legend>
        <div className="pa-two">
          <label>
            {t('Smallest score gain worth an upgrade')}
            <input inputMode="numeric" value={x.minUpgradeGain} placeholder={t('any gain')} onChange={(e) => onX({ ...x, minUpgradeGain: e.target.value })} />
            <small className="pa-hint">{t('Stops swapping a file for one of the same quality that scores only a little higher.')}</small>
          </label>
          <label>
            {t('Stop upgrading after the release (days)')}
            <input inputMode="numeric" value={x.upgradeDays} placeholder={t('never stop')} onChange={(e) => onX({ ...x, upgradeDays: e.target.value })} />
            <small className="pa-hint">{t('Counted from the film’s release or the episode’s air date. A file from a fallback is still replaced.')}</small>
          </label>
        </div>
      </fieldset>

      <fieldset className="profile-fieldset">
        <legend>{t('Score sets from the TRaSH Guides')}</legend>
        <p className="pa-hint">{t('Adds a group of custom formats with the TRaSH Guides’ scores in one step, importing any that are missing. Change any score afterwards.')}</p>
        <select value="" disabled={setBusy} aria-label={t('Add a score set')} onChange={(e) => void addSet(e.target.value)}>
          <option value="">{setBusy ? t('Adding…') : t('Add a score set…')}</option>
          {SCORE_SETS.map((s) => (
            <option key={s.key} value={s.key}>
              {s.label()}
            </option>
          ))}
        </select>
      </fieldset>

      <fieldset className="profile-fieldset">
        <legend>{t('Release groups')}</legend>
        <div className="pa-two">
          <label>
            {t('Prefer')}
            <input value={x.preferGroups} placeholder="FraMeSToR, HONE" onChange={(e) => onX({ ...x, preferGroups: e.target.value })} />
          </label>
          <label>
            {t('Never take')}
            <input value={x.neverGroups} placeholder="YIFY, RARBG" onChange={(e) => onX({ ...x, neverGroups: e.target.value })} />
          </label>
        </div>
        <small className="pa-hint">{t('Separate names with commas. A preferred group wins between releases of the same quality; a never group is skipped.')}</small>
      </fieldset>

      <fieldset className="profile-fieldset">
        <legend>{t('Language')}</legend>
        <p className="pa-hint">{langs.length === 0 ? t('Uses the audio languages under Settings, Quality. Add languages here to give this profile its own, best first.') : t('This profile’s own audio languages, best first, in place of the ones under Settings, Quality.')}</p>
        {langs.length > 0 && (
          <ol className="pa-langs">
            {langs.map((l) => (
              <li key={l}>
                {langName(l)}
                <button type="button" className="icon-btn" aria-label={t('Remove {name}', { name: langName(l) })} onClick={() => onX({ ...x, audioLanguage: langs.filter((y) => y !== l).join(',') })}>
                  <Icon name="x" size={13} />
                </button>
              </li>
            ))}
          </ol>
        )}
        <select value="" aria-label={t('Add a language')} onChange={(e) => e.target.value && onX({ ...x, audioLanguage: [...langs, e.target.value].join(',') })}>
          <option value="">{t('Add a language…')}</option>
          {['Original', ...AUDIO_LANGUAGES].filter((l) => !langs.includes(l)).map((l) => (
            <option key={l} value={l}>
              {langName(l)}
            </option>
          ))}
        </select>
        <label className="check-hint">
          <input type="checkbox" checked={x.wantSubs} onChange={(e) => onX({ ...x, wantSubs: e.target.checked })} />
          <span>
            {t('Prefer releases with English subtitles inside')}
            <small>{t('For foreign-language titles. Between equal releases, the one that says it has English subs wins.')}</small>
          </span>
        </label>
      </fieldset>

      <fieldset className="profile-fieldset">
        <legend>{t('Where it searches')}</legend>
        <p className="pa-hint">{t('For automatic searches. A search by hand still lists everything.')}</p>
        <div className="pa-two">
          <label>
            {t('Usenet or torrents')}
            <select value={x.protocol} onChange={(e) => onX({ ...x, protocol: e.target.value as ExtrasDraft['protocol'] })}>
              <option value="">{t('Either')}</option>
              <option value="usenet">{t('Usenet only')}</option>
              <option value="torrent">{t('Torrents only')}</option>
            </select>
          </label>
          <label>
            {t('Skip releases newer than (minutes)')}
            <input inputMode="numeric" value={x.minAge} placeholder="0" onChange={(e) => onX({ ...x, minAge: e.target.value })} />
            <small className="pa-hint">{t('Gives a fresh Usenet post time to finish uploading.')}</small>
          </label>
          <label>
            {t('Skip releases older than (days)')}
            <input inputMode="numeric" value={x.maxAge} placeholder={t('no limit')} onChange={(e) => onX({ ...x, maxAge: e.target.value })} />
          </label>
        </div>
        {indexers.length > 0 && (
          <div className="pa-indexers">
            <span>{x.indexers.length === 0 ? t('Indexers: all of them. Tick some to search only those.') : plural(x.indexers.length, 'Indexers: only the {n} ticked.', 'Indexers: only the {n} ticked.')}</span>
            <div className="profile-tiers">
              {indexers.map((ix) => (
                <label key={ix.id}>
                  <input
                    type="checkbox"
                    checked={x.indexers.includes(ix.id)}
                    onChange={(e) => onX({ ...x, indexers: e.target.checked ? [...x.indexers, ix.id] : x.indexers.filter((id) => id !== ix.id) })}
                  />
                  {ix.name}
                  {!ix.enabled && <small> ({t('off')})</small>}
                </label>
              ))}
            </div>
          </div>
        )}
      </fieldset>

      {problem && <p className="error-text">{problem}</p>}
      <div>
        <button type="button" onClick={exportJSON}>
          <Icon name="download" size={14} /> {t('Export as JSON')}
        </button>
      </div>
    </>
  )
}
