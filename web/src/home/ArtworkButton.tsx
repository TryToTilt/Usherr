// Usherr: the Re-Posterr button on a film's or show's page (administrators): what
// Jellyfin shows now, and every poster, background and logo TMDB and
// fanart.tv have, side by side. A picture chosen here goes on at once, on
// every copy in Jellyfin, and is kept: no nightly pass replaces it. The
// "Your style" tab makes the poster in the household's style instead (and a
// show's season posters and title cards).
import { useEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { Link } from 'react-router-dom'
import Icon from '../components/Icon'
import { useConfirm } from '../components/ConfirmProvider'
import { useToast } from '../components/Toast'
import { t } from '../i18n'
import { useFocusTrap } from '../useFocusTrap'
import { ART_LABEL, artApi, artImgError, candLine, currentArt, stylePreview, type ArtCand, type ArtKind, type ArtTitle, type ArtType, type StyleWhat } from './artApi'
import './artwork.css'

type Tab = ArtType | 'style'

export default function ArtworkButton({ kind, tmdbId, title }: { kind: ArtKind; tmdbId: number; title: string }) {
  const [open, setOpen] = useState(false)
  return (
    <>
      <button type="button" className="btn-with-icon" data-walk="title-art" onClick={() => setOpen(true)} title={t('Pick the poster, background and logo Jellyfin shows for this title')}>
        <Icon name="image" size={16} /> Re-Posterr
      </button>
      {open && <ArtworkDialog kind={kind} tmdbId={tmdbId} title={title} onClose={() => setOpen(false)} />}
    </>
  )
}

function ArtworkDialog({ kind, tmdbId, title, onClose }: { kind: ArtKind; tmdbId: number; title: string; onClose: () => void }) {
  const toast = useToast()
  const confirm = useConfirm()
  const box = useRef<HTMLDivElement>(null)
  useFocusTrap(box)
  const [tab, setTab] = useState<Tab>('poster')
  const [data, setData] = useState<ArtTitle | null>(null)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState('')
  const [stamp, setStamp] = useState(0) // a new picture: the current one is asked for again
  const [gone, setGone] = useState<Set<string>>(new Set()) // pictures that wouldn't load
  useEffect(() => {
    let live = true
    artApi
      .title(kind, tmdbId)
      .then((d) => live && setData(d))
      .catch((e: Error) => live && setError(e.message))
    return () => {
      live = false
    }
  }, [kind, tmdbId, stamp])
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && !busy && onClose()
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [busy, onClose])

  const type: ArtType = tab === 'style' ? 'poster' : tab
  const choose = async (url: string) => {
    if (!data) return
    const copies = data.copies > 1 ? t('All {n} copies in Jellyfin get it.', { n: data.copies }) : ''
    if (!(await confirm({ title: t('Use this {what}?', { what: ART_LABEL[type].toLowerCase() }), body: `${t('It goes on at once and stays: the nightly pass won’t replace it. The one there now is kept, so it can be put back.')} ${copies}`, confirmLabel: t('Use it') }))) return
    setBusy(url)
    try {
      await artApi.choose({ kind, tmdbId, type, url })
      toast.success(t('Jellyfin has the new {what}.', { what: ART_LABEL[type].toLowerCase() }))
      setStamp((n) => n + 1)
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy('')
    }
  }
  const undo = async (id: number) => {
    setBusy('undo')
    try {
      await artApi.undo(id)
      toast.success(t('The old picture is back.'))
      setStamp((n) => n + 1)
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy('')
    }
  }
  const list = (data?.candidates[type] ?? []).filter((c) => !gone.has(c.url))
  const last = (data?.changes ?? []).find((c) => c.type === type && c.state === 'done')

  return createPortal(
    <div className="modal-backdrop" onMouseDown={(e) => e.target === e.currentTarget && !busy && onClose()}>
      <div ref={box} tabIndex={-1} className="modal art-dialog" data-walk="art-dialog" role="dialog" aria-modal="true" aria-label="Re-Posterr">
        <div className="art-head">
          <h2>{t('Re-Posterr: {title}', { title })}</h2>
          <button type="button" className="icon-btn" onClick={onClose} aria-label={t('Close')} title={t('Close')} disabled={!!busy}>
            <Icon name="x" size={18} />
          </button>
        </div>
        <div className="art-tabs" role="tablist">
          {(['poster', 'backdrop', 'logo'] as const).map((ty) => (
            <button key={ty} type="button" role="tab" aria-selected={tab === ty} className={tab === ty ? 'on' : ''} onClick={() => setTab(ty)}>
              {ART_LABEL[ty]}
              {data?.chosen[ty] && <Icon name="check" size={13} />}
            </button>
          ))}
          <button type="button" role="tab" aria-selected={tab === 'style'} className={tab === 'style' ? 'on' : ''} onClick={() => setTab('style')} title={t('A poster made in your style, from art without words')}>
            {t('Your style')}
          </button>
        </div>
        {error && <p className="error">{error}</p>}
        {!data && !error && <p className="hint">{t('Looking at TMDB and fanart.tv…')}</p>}
        {data && !data.inJellyfin && <p className="art-warn">{t('Jellyfin doesn’t have this title yet, so there’s nothing to change. Once it’s in a Jellyfin library, its artwork can be picked here.')}</p>}
        {data && tab === 'style' && <StyleTab kind={kind} tmdbId={tmdbId} title={title} data={data} busy={busy} setBusy={setBusy} onDone={() => setStamp((n) => n + 1)} onUndo={undo} />}
        {data && tab !== 'style' && (
          <>
            {data.inJellyfin && (
              <div className="art-now">
                <div className={`art-now-pic art-${type}`}>{data.current ? <img key={stamp} src={`${currentArt(data.current, type)}?v=${stamp}`} alt={t('Now')} onError={(e) => ((e.target as HTMLImageElement).style.visibility = 'hidden')} /> : <span />}</div>
                <div>
                  <strong>{t('In Jellyfin now')}</strong>
                  <p className="hint">{data.chosen[type] ? t('Chosen by hand: the nightly pass leaves it.') : t('Picked by Jellyfin or by Usherr’s pass.')}</p>
                  {last && (
                    <button type="button" className="btn-sm" disabled={!!busy} onClick={() => void undo(last.id)} title={t('Puts back the picture Jellyfin had before this one')}>
                      {t('Put the old one back')}
                    </button>
                  )}
                </div>
              </div>
            )}
            {list.length === 0 ? (
              <p className="hint">{t('TMDB and fanart.tv have no {what} for this title.', { what: ART_LABEL[type].toLowerCase() })}</p>
            ) : (
              <ul className={`art-grid art-${type}`}>
                {list.map((c) => (
                  <li key={c.url}>
                    <button type="button" disabled={!!busy || !data.inJellyfin} onClick={() => void choose(c.url)} title={t('Use this one')}>
                      <img src={c.preview || c.url} alt="" loading="lazy" onError={(e) => artImgError(e, c.url, () => setGone((g) => new Set(g).add(c.url)))} />
                      {busy === c.url && <span className="art-busy">{t('Sending…')}</span>}
                    </button>
                    <small>{candLine(c)}</small>
                  </li>
                ))}
              </ul>
            )}
          </>
        )}
      </div>
    </div>,
    document.body,
  )
}

// StyleTab: the title's poster and background in the household's style (from
// the art picked below, a picture of their own, or the best without words;
// with their own words if they like), and for a show its season posters and
// title cards.
function StyleTab({ kind, tmdbId, title, data, busy, setBusy, onDone, onUndo }: { kind: ArtKind; tmdbId: number; title: string; data: ArtTitle; busy: string; setBusy: (b: string) => void; onDone: () => void; onUndo: (id: number) => void }) {
  const toast = useToast()
  const confirm = useConfirm()
  const [base, setBase] = useState('') // '' = the best without words
  const [mine, setMine] = useState<{ url: string; preview: string }[]>([]) // pictures sent from here
  const [typed, setTyped] = useState('') // their own words, as typed
  const [words, setWords] = useState('') // ... a moment later
  const [pics, setPics] = useState<Partial<Record<StyleWhat, string>>>({})
  const [errs, setErrs] = useState<Partial<Record<StyleWhat, string>>>({})
  const fileIn = useRef<HTMLInputElement>(null)
  const whats: StyleWhat[] = kind === 'tv' ? ['poster', 'backdrop', 'season', 'card'] : ['poster', 'backdrop']
  useEffect(() => {
    const id = window.setTimeout(() => setWords(typed.trim()), 600)
    return () => window.clearTimeout(id)
  }, [typed])
  useEffect(() => {
    const ctl = new AbortController()
    const made: string[] = []
    for (const what of whats) {
      const own = what === 'poster' || what === 'backdrop'
      stylePreview({ what, kind, tmdbId, url: own ? base || undefined : undefined, words: own ? words || undefined : undefined }, ctl.signal)
        .then((u) => {
          made.push(u)
          setPics((p) => ({ ...p, [what]: u }))
          setErrs((e) => ({ ...e, [what]: '' }))
        })
        .catch((e: Error) => e.name !== 'AbortError' && setErrs((x) => ({ ...x, [what]: e.message })))
    }
    return () => {
      ctl.abort()
      made.forEach((u) => URL.revokeObjectURL(u))
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [kind, tmdbId, base, words])

  const PUT: Record<StyleWhat, 'poster' | 'backdrop' | 'seasons' | 'cards'> = { poster: 'poster', backdrop: 'backdrop', season: 'seasons', card: 'cards' }
  const put = async (what: StyleWhat) => {
    const one = what === 'poster' || what === 'backdrop'
    const body = one
      ? t('It goes on at once, on every copy, and stays: the nightly pass won’t replace it. The one there now is kept, so it can be put back.')
      : t('Every one of them is made and sent to Jellyfin, in the background (a long show takes a while). The pictures they replace are kept, so they can all be put back.')
    const head = what === 'poster' ? t('Use this poster?') : what === 'backdrop' ? t('Use this background?') : what === 'season' ? t('Make the season posters?') : t('Make the title cards?')
    if (!(await confirm({ title: head, body, confirmLabel: t('Go ahead') }))) return
    setBusy(PUT[what])
    try {
      const r = await artApi.titleStyle({ kind, tmdbId, what: PUT[what], url: one ? base || undefined : what === 'season' ? base || undefined : undefined, words: one ? words || undefined : undefined })
      toast.success(r.background ? t('Making them; they’re on Re-Posterr’s page under Changed lately when done.') : t('Jellyfin has it.'))
      onDone()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy('')
    }
  }
  const send = async (f: File) => {
    setBusy('upload')
    try {
      const r = await artApi.upload(f)
      setMine((m) => [r, ...m])
      setBase(r.url)
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy('')
    }
  }
  const bases = [...data.candidates.poster, ...data.candidates.backdrop].filter((c) => c.lang === '')
  const lastOf = (ty: string) => (data.changes ?? []).find((c) => c.type === ty && c.style && c.state === 'done')
  const LABEL: Record<StyleWhat, string> = { poster: t('Poster'), backdrop: t('Background'), season: t('Season posters'), card: t('Title cards') }

  return (
    <>
      <p className="hint">
        {t('Made from art without words, with the title in your style.')} <Link to={`/settings/re-posterr?kind=${kind}&tmdbId=${tmdbId}&title=${encodeURIComponent(title)}#style`} title={t('The style page, trying it on this title')}>{t('Change the style')}</Link>
      </p>
      <div className="psm-actions">
        <label className="psm-field field art-own-words">
          <span>{t('Your own words')}</span>
          <input type="text" maxLength={80} value={typed} onChange={(e) => setTyped(e.target.value)} placeholder={t('The title, as Jellyfin has it')} title={t('Put on the poster and background instead of the title')} />
        </label>
        <button type="button" className="btn-sm" disabled={!!busy} onClick={() => fileIn.current?.click()} title={t('A picture of your own to make them from (JPEG, PNG or WebP); kept in the artwork folder')}>
          {busy === 'upload' ? t('Sending…') : t('Your own picture…')}
        </button>
        <input ref={fileIn} type="file" accept="image/jpeg,image/png,image/webp" hidden onChange={(e) => e.target.files?.[0] && void send(e.target.files[0])} />
      </div>
      <div className="art-made">
        {whats.map((w) => {
          const done = lastOf(PUT[w])
          return (
            <figure key={w} className={w === 'card' || w === 'backdrop' ? 'wide' : 'tall'}>
              {pics[w] ? <img src={pics[w]} alt={LABEL[w]} /> : <span className="hint">{errs[w] || t('Drawing…')}</span>}
              <figcaption>
                <strong>{LABEL[w]}</strong>
                {w === 'season' && <span className="hint"> · {t('the first; each season gets its number')}</span>}
                {w === 'card' && <span className="hint"> · {t('the first episode; each gets its own still')}</span>}
              </figcaption>
              <div className="psm-actions">
                <button type="button" className="btn-sm primary" disabled={!!busy || !data.inJellyfin || !pics[w]} onClick={() => void put(w)}>
                  {busy === PUT[w] ? t('Sending…') : w === 'poster' || w === 'backdrop' ? t('Use it') : t('Make them')}
                </button>
                {done && (
                  <button type="button" className="btn-sm" disabled={!!busy} onClick={() => onUndo(done.id)} title={t('Puts back the pictures Jellyfin had before')}>
                    {t('Put the old ones back')}
                  </button>
                )}
              </div>
            </figure>
          )
        })}
      </div>
      {(bases.length > 1 || mine.length > 0) && (
        <>
          <h3>{t('Made from')}</h3>
          <ul className="art-grid art-poster">
            {[...mine.map((m) => ({ url: m.url, preview: m.preview, c: undefined as ArtCand | undefined })), ...bases.slice(0, 24).map((c) => ({ url: c.url, preview: c.preview || c.url, c: c as ArtCand | undefined }))].map((x) => (
              <li key={x.url}>
                <button type="button" className={base === x.url ? 'picked' : ''} disabled={!!busy} onClick={() => setBase(base === x.url ? '' : x.url)} title={t('Make them from this one')}>
                  <img src={x.preview} alt="" loading="lazy" onError={(e) => artImgError(e, x.c ? x.url : '', () => (e.currentTarget.style.visibility = 'hidden'))} />
                </button>
                <small>{x.c ? candLine(x.c) : t('Your own')}</small>
              </li>
            ))}
          </ul>
        </>
      )}
    </>
  )
}
