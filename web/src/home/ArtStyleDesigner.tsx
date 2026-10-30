// Usherr: Re-Posterr's own posters (M5, and more of Posterizarr's options),
// on its page: the household's style for posters, season posters, episode
// title cards and backgrounds: a layout to start from, the font, the words'
// colour, size, outline, shadow and wording, a fade or a band behind them, a
// border, their own overlay PNGs (by resolution too); a library's own poster
// style; tried live on any title before it's saved; kept as a file to copy or
// share. The nightly look makes them only for the kinds switched on here.
import { useCallback, useEffect, useRef, useState } from 'react'
import Switch from '../components/Switch'
import { useConfirm } from '../components/ConfirmProvider'
import { useToast } from '../components/Toast'
import { t } from '../i18n'
import { artApi, stylePreview, type ArtLayer, type ArtLayerKind, type ArtRes, type ArtSample, type ArtStyle, type ArtStyles, type ArtStylesAll, type StyleWhat } from './artApi'

const WHATS: { id: StyleWhat; label: string; blurb: string }[] = [
  { id: 'poster', label: t('Posters'), blurb: t('Films, shows and collections: art without words, with the title in your style.') },
  { id: 'season', label: t('Season posters'), blurb: t('Each season of a show: the show’s art with “Season 2”.') },
  { id: 'card', label: t('Title cards'), blurb: t('Each episode: a still from it, with its name and number.') },
  { id: 'backdrop', label: t('Backgrounds'), blurb: t('The wide picture behind a title: art without words, with the title (or its logo) on it.') },
]

const SUB_LABEL: Record<StyleWhat, string> = { poster: t('Show the year'), season: t('Show the show’s name (or logo) above “Season 2”'), card: t('Show the season and episode'), backdrop: t('Show the year') }
const WORDS: Partial<Record<StyleWhat, { label: string; tip: string; def: string }>> = {
  season: { label: t('Wording'), tip: t('{n} is the season’s number: “Series {n}”, “Book {n}”'), def: 'Season {n}' },
  card: { label: t('Wording'), tip: t('{s} is the season, {e} the episode: “S{s} E{e}”'), def: 'Season {s} · Episode {e}' },
}
const RES: { id: ArtRes; label: string }[] = [
  { id: '4k', label: '4K' },
  { id: '1080', label: '1080p' },
  { id: '720', label: '720p' },
  { id: 'sd', label: t('Smaller') },
]

const ORDER: ArtLayerKind[] = ['fade', 'band', 'border', 'overlay', 'title']

const find = (st: ArtStyle, kind: ArtLayerKind) => st.layers.find((l) => l.kind === kind)

// withLayer changes (or adds, or with null takes away) a layer, keeping the
// layers in drawing order.
function withLayer(st: ArtStyle, kind: ArtLayerKind, patch: Partial<ArtLayer> | null): ArtStyle {
  const old = find(st, kind)
  let layers = st.layers.filter((l) => l.kind !== kind)
  if (patch) layers.push({ ...(old ?? { kind }), ...patch, kind })
  layers = layers.sort((a, b) => ORDER.indexOf(a.kind) - ORDER.indexOf(b.kind))
  return { ...st, layers }
}

// Layouts to start from; the title's font, colour and the rest stay as they
// are, and so does an overlay.
const LAYOUTS: { id: string; label: string; tip: string; make: (title: ArtLayer) => ArtLayer[] }[] = [
  { id: 'fade', label: t('Fade'), tip: t('The picture darkens towards the bottom, the title over it'), make: (ti) => [{ kind: 'fade', pos: 'bottom', color: '#000000', opacity: 85, size: 40 }, { ...ti, pos: 'bottom', logo: false }] },
  { id: 'band', label: t('Band'), tip: t('A solid strip along the bottom, the title in it'), make: (ti) => [{ kind: 'band', pos: 'bottom', color: '#111111', opacity: 90, size: 18 }, { ...ti, pos: 'bottom', logo: false }] },
  { id: 'logo', label: t('Logo'), tip: t('The title’s own logo instead of words, when it has one'), make: (ti) => [{ kind: 'fade', pos: 'bottom', color: '#000000', opacity: 80, size: 40 }, { ...ti, pos: 'bottom', logo: true }] },
  { id: 'centre', label: t('Centre'), tip: t('The whole picture a little darker, the title big in the middle'), make: (ti) => [{ kind: 'fade', pos: 'middle', color: '#000000', opacity: 40, size: 100 }, { ...ti, pos: 'middle', logo: false }] },
  {
    id: 'frame',
    label: t('Framed'),
    tip: t('A border all round and a band at the bottom'),
    make: (ti) => [
      { kind: 'band', pos: 'bottom', color: '#111111', opacity: 92, size: 16 },
      { kind: 'border', color: '#ffffff', size: 14 },
      { ...ti, pos: 'bottom', logo: false },
    ],
  },
]

export default function ArtStyleDesigner() {
  const toast = useToast()
  const confirm = useConfirm()
  const [all, setAll] = useState<ArtStylesAll | null>(null)
  const [what, setWhat] = useState<StyleWhat>('poster')
  const [lib, setLib] = useState('') // '' the house style; else a library's own (posters)
  const [draft, setDraft] = useState<ArtStyles | null>(null)
  const [on, setOn] = useState<ArtStylesAll['on']>({})
  const [samples, setSamples] = useState<ArtSample[]>([])
  // Come from a title's Re-Posterr ("Change the style"): try it on that one.
  const [from] = useState<ArtSample | null>(() => {
    const p = new URLSearchParams(window.location.search)
    const kind = p.get('kind')
    const tmdbId = Number(p.get('tmdbId'))
    return (kind === 'movie' || kind === 'tv' || kind === 'collection') && tmdbId > 0 ? { kind, tmdbId, title: p.get('title') ?? '', year: 0 } : null
  })
  const [sample, setSample] = useState<ArtSample | null>(from)
  const [q, setQ] = useState('')
  const [res, setRes] = useState<ArtRes | ''>('')
  const [pic, setPic] = useState('')
  const [err, setErr] = useState('')
  const [drawing, setDrawing] = useState(false)
  const [saving, setSaving] = useState(false)
  const importer = useRef<HTMLInputElement>(null)
  const overlayIn = useRef<HTMLInputElement>(null)

  useEffect(() => {
    artApi
      .styles()
      .then((a) => {
        setAll(a)
        setDraft(a.styles)
        setOn(a.on)
      })
      .catch((e: Error) => toast.error(e.message))
  }, [toast])

  // Titles to try it on: a show for season posters and title cards.
  const kind = what === 'season' || what === 'card' ? 'tv' : undefined
  const findSamples = useCallback(
    (text: string) => {
      artApi
        .samples(text, kind)
        .then((list) => {
          // The title come from stays first in the list (when it fits the tab).
          if (from && !text && (!kind || from.kind === kind)) list = [from, ...list.filter((x) => x.kind !== from.kind || x.tmdbId !== from.tmdbId)]
          setSamples(list)
          setSample((cur) => (!text && from && (!kind || from.kind === kind) ? from : cur && (!kind || cur.kind === kind) ? cur : (list[0] ?? null)))
        })
        .catch(() => undefined)
    },
    [kind, from],
  )
  useEffect(() => {
    const id = window.setTimeout(() => findSamples(q), q ? 350 : 0)
    return () => window.clearTimeout(id)
  }, [q, findSamples])

  // The style being changed: a library's own, or the house's.
  const own = what === 'poster' && lib ? draft?.libraries?.[lib] : undefined
  const style = what === 'poster' && lib ? own : draft?.[what]
  const shown = style ?? draft?.poster // a library on the house style shows the house's
  // The picture, drawn again a moment after each change.
  const last = useRef('')
  useEffect(() => {
    if (!shown || !sample) return
    const ctl = new AbortController()
    const id = window.setTimeout(() => {
      setDrawing(true)
      stylePreview({ what, style: shown, kind: sample.kind, tmdbId: sample.tmdbId, res }, ctl.signal)
        .then((u) => {
          if (last.current) URL.revokeObjectURL(last.current)
          last.current = u
          setPic(u)
          setErr('')
        })
        .catch((e: Error) => e.name !== 'AbortError' && setErr(e.message))
        .finally(() => setDrawing(false))
    }, 400)
    return () => {
      window.clearTimeout(id)
      ctl.abort()
    }
  }, [what, shown, sample, res])

  // Sent here from a title's Re-Posterr ("Change the style"): show it.
  const ready = !!all
  useEffect(() => {
    if (ready && window.location.hash === '#style') document.getElementById('style')?.scrollIntoView({ behavior: 'smooth' })
  }, [ready])

  if (!all || !draft || !shown) return null
  const title = find(shown, 'title') ?? { kind: 'title' as const, font: 'bebas', size: 9, color: '#ffffff' }
  const shade = find(shown, 'band') ? 'band' : find(shown, 'fade') ? 'fade' : 'none'
  const shadeLayer = find(shown, 'band') ?? find(shown, 'fade')
  const border = find(shown, 'border')
  const overlay = find(shown, 'overlay')
  const change = (st: ArtStyle) => {
    if (what === 'poster' && lib) setDraft({ ...draft, libraries: { ...draft.libraries, [lib]: st } })
    else setDraft({ ...draft, [what]: st })
  }
  const setTitle = (patch: Partial<ArtLayer>) => change(withLayer(shown, 'title', patch))
  const dirty = JSON.stringify(draft) !== JSON.stringify(all.styles) || JSON.stringify(on) !== JSON.stringify(all.on)
  const libOnHouse = what === 'poster' && !!lib && !own

  const save = async () => {
    setSaving(true)
    try {
      const a = await artApi.saveStyles({ styles: draft, on })
      setAll(a)
      setDraft(a.styles)
      setOn(a.on)
      toast.success(t('Style saved.'))
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setSaving(false)
    }
  }
  // A file of every style, to copy to another Usherr or keep.
  const exportStyles = () => {
    const blob = new Blob([JSON.stringify({ reposterr: 1, styles: draft }, null, 2)], { type: 'application/json' })
    const a = document.createElement('a')
    a.href = URL.createObjectURL(blob)
    a.download = 're-posterr-style.json'
    a.click()
    window.setTimeout(() => URL.revokeObjectURL(a.href), 1000)
  }
  const importStyles = async (f: File) => {
    try {
      const got = JSON.parse(await f.text()) as { reposterr?: number; styles?: ArtStyles }
      if (got.reposterr !== 1 || !got.styles?.poster) throw new Error(t('That isn’t a Re-Posterr style file.'))
      if (!(await confirm({ title: t('Use the styles in this file?'), body: t('They replace the ones here once you save. Overlays they name must be on this Usherr too.'), confirmLabel: t('Use them') }))) return
      setDraft({ ...draft, ...got.styles })
      toast.success(t('Loaded; save to keep them.'))
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    }
  }
  const addOverlay = async (f: File) => {
    try {
      const r = await artApi.addOverlay(f)
      setAll({ ...all, overlays: r.overlays })
      change(withLayer(shown, 'overlay', { file: r.name }))
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    }
  }
  const setOverlay = (patch: Partial<ArtLayer>) => {
    const next = { ...(overlay ?? { kind: 'overlay' as const }), ...patch }
    const empty = !next.file && !next.byRes
    change(withLayer(shown, 'overlay', empty ? null : patch))
  }
  const overlaySelect = (value: string | undefined, onPick: (v: string) => void, label: string) => (
    <label className="psm-field field">
      <span>{label}</span>
      <select value={value ?? ''} onChange={(e) => onPick(e.target.value)} title={t('One of the PNGs added here (Add an overlay…)')}>
        <option value="">{t('None')}</option>
        {all.overlays.map((o) => (
          <option key={o} value={o}>
            {o}
          </option>
        ))}
      </select>
    </label>
  )

  return (
    <section className="card art-style" id="style">
      <h2>{t('Your style')}</h2>
      <p className="hint">{t('Re-Posterr can make the pictures itself: art without words, with the title in a font and layout of your choosing, so the whole library matches. Try it on any title; nothing goes to Jellyfin until you save and use it.')}</p>
      <div className="art-tabs" role="tablist">
        {WHATS.map((w) => (
          <button key={w.id} type="button" role="tab" aria-selected={what === w.id} className={what === w.id ? 'on' : ''} onClick={() => setWhat(w.id)}>
            {w.label}
          </button>
        ))}
      </div>
      <p className="hint">{WHATS.find((w) => w.id === what)?.blurb}</p>
      <Switch
        checked={!!on[what]}
        onChange={(v) => setOn({ ...on, [what]: v })}
        label={t('Make these in the nightly look')}
        description={t('As “What it does” says: they wait for you to approve, or go on by themselves. A picture chosen by hand stays as it is.')}
      />
      {what === 'poster' && !!all.libraries?.length && (
        <div className="psm-actions art-lib">
          <label className="psm-field field">
            <span>{t('For')}</span>
            <select value={lib} onChange={(e) => setLib(e.target.value)} title={t('A library can have its own poster style: Anime unlike Movies')}>
              <option value="">{t('Every library (the house style)')}</option>
              {all.libraries.map((l) => (
                <option key={l} value={l}>
                  {draft.libraries?.[l] ? t('{lib} (its own style)', { lib: l }) : l}
                </option>
              ))}
            </select>
          </label>
          {libOnHouse && (
            <button type="button" className="btn-sm" onClick={() => change(structuredClone(draft.poster))}>
              {t('Give it its own style')}
            </button>
          )}
          {lib && own && (
            <button
              type="button"
              className="btn-sm"
              onClick={() => {
                const libraries = { ...draft.libraries }
                delete libraries[lib]
                setDraft({ ...draft, libraries })
              }}
            >
              {t('Back to the house style')}
            </button>
          )}
        </div>
      )}

      <div className="art-style-grid">
        <div>
        <fieldset className="art-style-controls" disabled={libOnHouse}>
          {libOnHouse && <p className="hint">{t('{lib} uses the house style. Give it its own to change it here.', { lib })}</p>}
          <div className="art-layouts" role="group" aria-label={t('Layout')}>
            {LAYOUTS.filter((l) => l.id !== 'logo' || what !== 'card').map((l) => (
              <button key={l.id} type="button" className="btn-sm" title={l.tip} onClick={() => change({ v: 1, layers: [...l.make(title), ...(overlay ? [overlay] : [])].sort((a, b) => ORDER.indexOf(a.kind) - ORDER.indexOf(b.kind)) })}>
                {l.label}
              </button>
            ))}
          </div>
          <div className="psm-settings">
            <label className="psm-field field">
              <span>{t('Font')}</span>
              <select value={title.font} onChange={(e) => setTitle({ font: e.target.value })} title={t('Free fonts that come with Usherr; titles in other alphabets use a font that has their letters')}>
                {all.fonts.map((f) => (
                  <option key={f.id} value={f.id}>
                    {f.name}
                  </option>
                ))}
              </select>
            </label>
            <label className="psm-field field">
              <span>{t('Where')}</span>
              <select
                value={title.pos ?? 'bottom'}
                title={t('Where the title sits; a band moves with it')}
                onChange={(e) => {
                  const pos = e.target.value as ArtLayer['pos']
                  let st = withLayer(shown, 'title', { pos })
                  if (find(st, 'band')) st = withLayer(st, 'band', { pos })
                  const f = find(st, 'fade')
                  if (f && f.pos !== 'middle') st = withLayer(st, 'fade', { pos: pos === 'top' ? 'top' : 'bottom' })
                  change(st)
                }}
              >
                <option value="top">{t('Top')}</option>
                <option value="middle">{t('Middle')}</option>
                <option value="bottom">{t('Bottom')}</option>
              </select>
            </label>
            <label className="psm-field field">
              <span>{t('Size')}</span>
              <input type="range" min={3} max={20} step={1} value={title.size ?? 9} onChange={(e) => setTitle({ size: Number(e.target.value) })} title={t('The largest the title gets; long titles shrink to fit')} />
            </label>
            <label className="psm-field field">
              <span>{t('Colour')}</span>
              <input type="color" value={title.color ?? '#ffffff'} onChange={(e) => setTitle({ color: e.target.value })} title={t('The colour of the words')} />
            </label>
            <label className="psm-field field">
              <span>{t('Outline')}</span>
              <input type="range" min={0} max={20} step={1} value={title.outline ?? 0} onChange={(e) => setTitle({ outline: Number(e.target.value) || undefined })} title={t('A dark edge round the letters, so they read on any picture (0: none)')} />
            </label>
            <label className="psm-field field">
              <span>{t('Lines at most')}</span>
              <select value={title.maxLines || 2} onChange={(e) => setTitle({ maxLines: Number(e.target.value) === 2 ? undefined : Number(e.target.value) })} title={t('A long title goes onto more lines, or gets smaller')}>
                {[1, 2, 3, 4].map((n) => (
                  <option key={n} value={n}>
                    {n}
                  </option>
                ))}
              </select>
            </label>
            {WORDS[what] && (
              <label className="psm-field field">
                <span>{WORDS[what].label}</span>
                <input type="text" maxLength={60} value={title.words ?? ''} placeholder={WORDS[what].def} onChange={(e) => setTitle({ words: e.target.value || undefined })} title={WORDS[what].tip} />
              </label>
            )}
          </div>
          <label className="art-check" title={t('The title in capital letters')}>
            <input type="checkbox" checked={!!title.caps} onChange={(e) => setTitle({ caps: e.target.checked })} /> {t('Capitals')}
          </label>
          <label className="art-check" title={t('A soft shadow under the letters')}>
            <input type="checkbox" checked={!!title.shadow} onChange={(e) => setTitle({ shadow: e.target.checked || undefined })} /> {t('Shadow')}
          </label>
          <label className="art-check" title={t('A smaller second line under the title')}>
            <input type="checkbox" checked={!!title.sub} onChange={(e) => setTitle({ sub: e.target.checked })} /> {SUB_LABEL[what]}
          </label>
          {what !== 'card' && (
            <label className="art-check" title={t('A clear logo from TMDB, fanart.tv or TheTVDB; the words when there’s none')}>
              <input type="checkbox" checked={!!title.logo} onChange={(e) => setTitle({ logo: e.target.checked })} /> {t('The title’s logo instead of words, when it has one')}
            </label>
          )}
          {what === 'poster' && (
            <label className="art-check" title={t('“Alien Collection” reads “Alien”')}>
              <input type="checkbox" checked={!!title.short} onChange={(e) => setTitle({ short: e.target.checked || undefined })} /> {t('Collections without the word “Collection”')}
            </label>
          )}

          <h3>{t('Behind the words')}</h3>
          <div className="psm-settings">
            <label className="psm-field field">
              <span>{t('Shading')}</span>
              <select
                value={shade}
                title={t('Something behind the words so they read on any picture: a fade into the edge, or a solid band')}
                onChange={(e) => {
                  let st = withLayer(withLayer(shown, 'fade', null), 'band', null)
                  const pos = title.pos ?? 'bottom'
                  if (e.target.value === 'fade') st = withLayer(st, 'fade', { pos: pos === 'top' ? 'top' : pos === 'middle' ? 'middle' : 'bottom', color: shadeLayer?.color ?? '#000000', opacity: 85, size: pos === 'middle' ? 100 : 40 })
                  if (e.target.value === 'band') st = withLayer(st, 'band', { pos, color: shadeLayer?.color ?? '#111111', opacity: 90, size: 18 })
                  change(st)
                }}
              >
                <option value="none">{t('None')}</option>
                <option value="fade">{t('A fade')}</option>
                <option value="band">{t('A band')}</option>
              </select>
            </label>
            {shadeLayer && (
              <>
                <label className="psm-field field">
                  <span>{t('Strength')}</span>
                  <input type="range" min={0} max={100} step={5} value={shadeLayer.opacity ?? 85} title={t('How dark it gets: 0 none, 100 solid')} onChange={(e) => change(withLayer(shown, shadeLayer.kind, { opacity: Number(e.target.value) }))} />
                </label>
                {shadeLayer.pos !== 'middle' && (
                  <label className="psm-field field">
                    <span>{shadeLayer.kind === 'band' ? t('Height') : t('How far up')}</span>
                    <input type="range" min={5} max={shadeLayer.kind === 'band' ? 60 : 100} step={1} value={shadeLayer.size ?? 40} title={t('How much of the picture it covers')} onChange={(e) => change(withLayer(shown, shadeLayer.kind, { size: Number(e.target.value) }))} />
                  </label>
                )}
                <label className="psm-field field">
                  <span>{t('Colour')}</span>
                  <input type="color" value={shadeLayer.color ?? '#000000'} title={t('The colour of the fade or band')} onChange={(e) => change(withLayer(shown, shadeLayer.kind, { color: e.target.value }))} />
                </label>
              </>
            )}
          </div>

          <h3>{t('Border')}</h3>
          <div className="psm-settings">
            <label className="psm-field field">
              <span>{t('Width')}</span>
              <input type="range" min={0} max={40} step={1} value={border?.size ?? 0} onChange={(e) => change(Number(e.target.value) > 0 ? withLayer(shown, 'border', { size: Number(e.target.value), color: border?.color ?? '#ffffff' }) : withLayer(shown, 'border', null))} title={t('0: no border')} />
            </label>
            {border && (
              <label className="psm-field field">
                <span>{t('Colour')}</span>
                <input type="color" value={border.color ?? '#ffffff'} title={t('The colour of the border')} onChange={(e) => change(withLayer(shown, 'border', { color: e.target.value }))} />
              </label>
            )}
          </div>

          <h3>{t('Overlay')}</h3>
          <p className="hint">{t('A PNG of your own laid over the whole picture: a frame, a fade, a badge. Parts left see-through show the picture. Shaped like a poster (2 by 3), or wide for title cards and backgrounds.')}</p>
          <div className="psm-settings">
            {overlaySelect(overlay?.file, (v) => setOverlay({ file: v || undefined }), overlay?.byRes ? t('Other copies') : t('Overlay'))}
            {overlay?.byRes &&
              RES.map((r) => (
                <div key={r.id}>
                  {overlaySelect(overlay.byRes?.[r.id], (v) => setOverlay({ byRes: { ...overlay.byRes, [r.id]: v || undefined } }), r.label)}
                </div>
              ))}
          </div>
          <div className="psm-actions">
            <button type="button" className="btn-sm" onClick={() => overlayIn.current?.click()} title={t('Kept in the artwork folder, under overlays')}>
              {t('Add an overlay…')}
            </button>
            <input ref={overlayIn} type="file" accept="image/png" hidden onChange={(e) => e.target.files?.[0] && void addOverlay(e.target.files[0])} />
            {what !== 'season' && what !== 'card' && (
              <label className="art-check" title={t('A 4K badge on the 4K copy, say: each copy in Jellyfin by the size of its picture')}>
                <input type="checkbox" checked={!!overlay?.byRes} onChange={(e) => setOverlay({ byRes: e.target.checked ? {} : undefined })} disabled={!all.overlays.length} /> {t('Another by the copy’s resolution')}
              </label>
            )}
          </div>

          </fieldset>
          <div className="psm-actions">
            <button type="button" className="btn-sm primary" disabled={!dirty || saving} onClick={() => void save()}>
              {saving ? t('Saving…') : t('Save')}
            </button>
            <button type="button" className="btn-sm" disabled={!dirty || saving} onClick={() => (setDraft(all.styles), setOn(all.on))}>
              {t('Undo changes')}
            </button>
            <button type="button" className="btn-sm" onClick={() => change(all.defaults[what])} title={t('Re-Posterr’s own style for these')}>
              {t('Start again')}
            </button>
            <button type="button" className="btn-sm" onClick={exportStyles} title={t('Every style in a file, to keep or to use on another Usherr')}>
              {t('Save as a file')}
            </button>
            <button type="button" className="btn-sm" onClick={() => importer.current?.click()} title={t('Styles from a file saved here or on another Usherr')}>
              {t('Load a file…')}
            </button>
            <input ref={importer} type="file" accept="application/json,.json" hidden onChange={(e) => e.target.files?.[0] && void importStyles(e.target.files[0])} />
          </div>
        </div>

        <div className="art-style-try">
          <div className={`art-style-pic ${what === 'card' || what === 'backdrop' ? 'wide' : ''}`}>
            {pic && <img src={pic} alt={t('How it looks')} className={drawing ? 'drawing' : ''} />}
            {!pic && !err && <span className="hint">{sample ? t('Drawing…') : t('No title to try it on yet: the nightly look finds them.')}</span>}
          </div>
          {err && <p className="art-warn">{err}</p>}
          {overlay?.byRes && (
            <label className="psm-field field">
              <span>{t('As on a copy in')}</span>
              <select value={res} onChange={(e) => setRes(e.target.value as ArtRes | '')}>
                <option value="">{t('Another size')}</option>
                {RES.map((r) => (
                  <option key={r.id} value={r.id}>
                    {r.label}
                  </option>
                ))}
              </select>
            </label>
          )}
          <label className="psm-field field">
            <span>{t('Try it on')}</span>
            <input type="search" value={q} onChange={(e) => setQ(e.target.value)} title={t('Any title Re-Posterr knows; nothing is sent to Jellyfin')} placeholder={t('Search your titles')} aria-label={t('Search your titles')} />
          </label>
          <div className="art-samples">
            {samples.map((s) => (
              <button key={`${s.kind}${s.tmdbId}`} type="button" className={`btn-sm ${sample?.tmdbId === s.tmdbId && sample.kind === s.kind ? 'primary' : ''}`} onClick={() => setSample(s)}>
                {s.title}
                {s.year ? ` (${s.year})` : ''}
              </button>
            ))}
          </div>
        </div>
      </div>
    </section>
  )
}
