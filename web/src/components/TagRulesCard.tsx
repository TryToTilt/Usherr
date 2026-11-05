// Usherr: tags that do something, as in Radarr and Sonarr (internal/api/tag_rules.go).
// Auto-tagging rules tag titles by genre, years, original language, type or
// library folder; tag rules make tagged titles search only some indexers, or
// only Usenet or torrents.
import { useEffect, useState } from 'react'
import { api, type AutoTagRule, type IndexerConfig, type TagRule, type TagRulesConfig } from '../api'
import { plural, t } from '../i18n'
import Icon from './Icon'
import TipLabel from './TipLabel'
import { useToast } from './Toast'

const GENRES = ['Action', 'Adventure', 'Animation', 'Comedy', 'Crime', 'Documentary', 'Drama', 'Family', 'Fantasy', 'History', 'Horror', 'Kids', 'Music', 'Mystery', 'Reality', 'Romance', 'Science Fiction', 'Sci-Fi & Fantasy', 'Thriller', 'War', 'Western']
const LANGUAGES: [string, string][] = [
  ['en', 'English'], ['ja', 'Japanese'], ['ko', 'Korean'], ['zh', 'Chinese'], ['cn', 'Cantonese'], ['hi', 'Hindi'], ['fr', 'French'], ['de', 'German'], ['es', 'Spanish'],
  ['it', 'Italian'], ['pt', 'Portuguese'], ['ru', 'Russian'], ['sv', 'Swedish'], ['da', 'Danish'], ['no', 'Norwegian'], ['nl', 'Dutch'], ['pl', 'Polish'], ['tr', 'Turkish'], ['th', 'Thai'], ['ta', 'Tamil'], ['te', 'Telugu'],
]
const langName = (code: string) => LANGUAGES.find(([c]) => c === code)?.[1] ?? code

const blankAuto = (): AutoTagRule => ({ tag: '', kind: '', genres: [], languages: [], yearFrom: 0, yearTo: 0, rootPath: '', remove: false, enabled: true })
const blankRule = (): TagRule => ({ tag: '', indexers: [], protocol: '' })

function Picks({ label, options, value, onChange, name = (v: string) => v }: { label: string; options: string[]; value: string[]; onChange: (v: string[]) => void; name?: (v: string) => string }) {
  return (
    <div className="tr-picks">
      <span>{label}</span>
      <div className="tr-chips">
        {value.map((v) => (
          <button key={v} type="button" className="chip active" aria-label={t('Remove {name}', { name: name(v) })} onClick={() => onChange(value.filter((x) => x !== v))}>
            {name(v)} <Icon name="x" size={11} />
          </button>
        ))}
        <select value="" aria-label={label} onChange={(e) => e.target.value && onChange([...value, e.target.value])}>
          <option value="">{t('Add…')}</option>
          {options.filter((o) => !value.includes(o)).map((o) => (
            <option key={o} value={o}>
              {name(o)}
            </option>
          ))}
        </select>
      </div>
    </div>
  )
}

export default function TagRulesCard() {
  const toast = useToast()
  const [cfg, setCfg] = useState<TagRulesConfig | null>(null)
  const [saved, setSaved] = useState('')
  const [indexers, setIndexers] = useState<IndexerConfig[]>([])
  const [tags, setTags] = useState<string[]>([])
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    api.getTagRules().then((c) => { setCfg(c); setSaved(JSON.stringify(c)) }).catch((e) => toast.error(e instanceof Error ? e.message : String(e)))
    api.listIndexers().then(setIndexers).catch(() => setIndexers([]))
    api.listTags().then((l) => setTags(l.map((x) => x.name))).catch(() => setTags([]))
  }, [toast])

  if (!cfg) return null
  const changed = JSON.stringify(cfg) !== saved
  const setAuto = (i: number, r: AutoTagRule) => setCfg({ ...cfg, autoTags: cfg.autoTags.map((x, j) => (j === i ? r : x)) })
  const setRule = (i: number, r: TagRule) => setCfg({ ...cfg, tagRules: cfg.tagRules.map((x, j) => (j === i ? r : x)) })
  const year = (s: string) => (/^\d{4}$/.test(s.trim()) ? Number(s) : 0)

  async function save() {
    if (!cfg) return
    setBusy(true)
    try {
      const c = await api.putTagRules(cfg)
      setCfg(c)
      setSaved(JSON.stringify(c))
      toast.success(c.autoTags.some((r) => r.enabled) ? t('Saved. The library is being tagged now.') : t('Saved.'))
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  async function applyNow() {
    try {
      const r = await api.applyAutoTags()
      toast.info(r.started ? t('Tagging the library now. Tags appear on titles as it goes.') : t('Tagging is already running.'))
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    }
  }

  return (
    <section className="card wide tag-rules">
      <h2><TipLabel text={t('Tags sort a library and become collections in Plex, Jellyfin and Emby. Here they can also be given automatically, and decide where tagged titles search.')}>{t('Tags')}</TipLabel></h2>

      <fieldset className="profile-fieldset">
        <legend>{t('Auto-tagging')}</legend>
        <p className="pa-hint">{t('Each rule gives its tag to every movie or show that matches all of its conditions. Rules run when you save, and every few hours for new titles.')}</p>
        <datalist id="tr-tags">
          {tags.map((tg) => <option key={tg} value={tg} />)}
        </datalist>
        {cfg.autoTags.length === 0 && <p className="pa-hint">{t('No rules yet.')}</p>}
        {cfg.autoTags.map((r, i) => (
          <div key={i} className="tr-rule">
            <div className="tr-row">
              <label>
                {t('Tag')}
                <input list="tr-tags" value={r.tag} placeholder={t('Anime')} onChange={(e) => setAuto(i, { ...r, tag: e.target.value })} />
              </label>
              <label>
                {t('For')}
                <select value={r.kind} onChange={(e) => setAuto(i, { ...r, kind: e.target.value as AutoTagRule['kind'] })}>
                  <option value="">{t('Movies and shows')}</option>
                  <option value="movie">{t('Movies')}</option>
                  <option value="tv">{t('Shows')}</option>
                </select>
              </label>
              <label>
                {t('From year')}
                <input inputMode="numeric" value={r.yearFrom || ''} placeholder={t('any')} onChange={(e) => setAuto(i, { ...r, yearFrom: year(e.target.value) || (e.target.value === '' ? 0 : r.yearFrom) })} />
              </label>
              <label>
                {t('To year')}
                <input inputMode="numeric" value={r.yearTo || ''} placeholder={t('any')} onChange={(e) => setAuto(i, { ...r, yearTo: year(e.target.value) || (e.target.value === '' ? 0 : r.yearTo) })} />
              </label>
            </div>
            <Picks label={t('Any of these genres')} options={GENRES} value={r.genres} onChange={(genres) => setAuto(i, { ...r, genres })} />
            <Picks label={t('Made in')} options={LANGUAGES.map(([c]) => c)} value={r.languages} onChange={(languages) => setAuto(i, { ...r, languages })} name={langName} />
            <label className="tr-wide">
              {t('Only titles in this library folder (optional)')}
              <input value={r.rootPath} placeholder="/data/anime" onChange={(e) => setAuto(i, { ...r, rootPath: e.target.value })} />
            </label>
            <div className="tr-row tr-foot">
              <label className="check-hint">
                <input type="checkbox" checked={r.enabled} onChange={(e) => setAuto(i, { ...r, enabled: e.target.checked })} />
                <span>{t('On')}</span>
              </label>
              <label className="check-hint">
                <input type="checkbox" checked={r.remove} onChange={(e) => setAuto(i, { ...r, remove: e.target.checked })} />
                <span>{t('Take the tag off titles that stop matching')}</span>
              </label>
              <button type="button" className="icon-btn" aria-label={t('Remove this rule')} title={t('Remove this rule')} onClick={() => setCfg({ ...cfg, autoTags: cfg.autoTags.filter((_, j) => j !== i) })}>
                <Icon name="trash" size={15} />
              </button>
            </div>
          </div>
        ))}
        <div className="tr-actions">
          <button type="button" onClick={() => setCfg({ ...cfg, autoTags: [...cfg.autoTags, blankAuto()] })}>{t('Add a rule')}</button>
          {cfg.autoTags.some((r) => r.enabled) && !changed && <button type="button" onClick={() => void applyNow()}>{t('Tag the library now')}</button>}
        </div>
      </fieldset>

      <fieldset className="profile-fieldset">
        <legend>{t('Tag rules')}</legend>
        <p className="pa-hint">{t('Titles with the tag search only the indexers ticked here, or only Usenet or torrents, in automatic searches. With a profile that limits indexers too, only indexers both allow are used.')}</p>
        {cfg.tagRules.length === 0 && <p className="pa-hint">{t('No tag rules yet.')}</p>}
        {cfg.tagRules.map((r, i) => (
          <div key={i} className="tr-rule">
            <div className="tr-row">
              <label>
                {t('Tag')}
                <input list="tr-tags" value={r.tag} onChange={(e) => setRule(i, { ...r, tag: e.target.value })} />
              </label>
              <label>
                {t('Usenet or torrents')}
                <select value={r.protocol} onChange={(e) => setRule(i, { ...r, protocol: e.target.value as TagRule['protocol'] })}>
                  <option value="">{t('Either')}</option>
                  <option value="usenet">{t('Usenet only')}</option>
                  <option value="torrent">{t('Torrents only')}</option>
                </select>
              </label>
              <button type="button" className="icon-btn" aria-label={t('Remove this rule')} title={t('Remove this rule')} onClick={() => setCfg({ ...cfg, tagRules: cfg.tagRules.filter((_, j) => j !== i) })}>
                <Icon name="trash" size={15} />
              </button>
            </div>
            {indexers.length > 0 && (
              <div className="pa-indexers">
                <span>{r.indexers.length === 0 ? t('Indexers: all of them. Tick some to search only those.') : plural(r.indexers.length, 'Indexers: only the {n} ticked.', 'Indexers: only the {n} ticked.')}</span>
                <div className="profile-tiers">
                  {indexers.map((ix) => (
                    <label key={ix.id}>
                      <input type="checkbox" checked={r.indexers.includes(ix.id)} onChange={(e) => setRule(i, { ...r, indexers: e.target.checked ? [...r.indexers, ix.id] : r.indexers.filter((id) => id !== ix.id) })} />
                      {ix.name}
                    </label>
                  ))}
                </div>
              </div>
            )}
          </div>
        ))}
        <div className="tr-actions">
          <button type="button" onClick={() => setCfg({ ...cfg, tagRules: [...cfg.tagRules, blankRule()] })}>{t('Add a tag rule')}</button>
        </div>
      </fieldset>

      <div className="tr-actions">
        <button className="primary" type="button" disabled={!changed || busy} onClick={() => void save()}>
          {busy ? t('Saving…') : t('Save tags')}
        </button>
        {changed && <button type="button" onClick={() => setCfg(JSON.parse(saved))}>{t('Undo changes')}</button>}
      </div>
    </section>
  )
}
