// Home fork: "Stream now" for a movie or an episode. The button opens a
// small sheet: pick the quality (remembered on this device), then a stream
// (StreamNZB's, best first), and it plays here; "Keep it" in the player
// downloads it for later.
import { useEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import Icon from '../components/Icon'
import { playStream, type StreamExtras } from './Player'
import type { SearchResult } from '../api'
import { browserTrouble, rememberQuality, rememberWhere, rememberedQuality, rememberedWhere, streamApi, streamingOn, type StreamOption, type StreamStart } from './streamApi'
import './stream.css'
import { t } from '../i18n'

export interface StreamTarget {
  kind: 'movie' | 'series'
  tmdbId: number
  season?: number
  episode?: number
  title: string // what the player shows: "Yellowstone S01E04"
}

export default function StreamButton({ target, onKeep, small = false }: { target: StreamTarget; onKeep?: () => void; small?: boolean }) {
  const [open, setOpen] = useState(false)
  const [on, setOn] = useState(true)
  useEffect(() => {
    let live = true
    void streamingOn().then((v) => live && setOn(v))
    return () => {
      live = false
    }
  }, [])
  if (!on) return null // streaming isn't set up (Settings > Extras)
  return (
    <>
      <button
        className={small ? 'btn-sm btn-with-icon' : 'btn-with-icon'}
        onClick={(e) => {
          e.stopPropagation()
          setOpen(true)
        }}
        title={t('Watch now without downloading')}
      >
        <Icon name="play" size={small ? 14 : 16} /> {t('Stream')}
      </button>
      {open && createPortal(<StreamSheet target={target} onKeep={onKeep} onClose={() => setOpen(false)} />, document.body)}
    </>
  )
}

function StreamSheet({ target, onKeep, onClose }: { target: StreamTarget; onKeep?: () => void; onClose: () => void }) {
  const [qualities, setQualities] = useState<string[] | null>(null)
  const [quality, setQuality] = useState('')
  const [options, setOptions] = useState<StreamOption[] | null>(null)
  const [error, setError] = useState('')
  const [starting, setStarting] = useState('')
  const [tv, setTv] = useState<{ name: string } | null>(null)
  const [where, setWhere] = useState<'here' | 'tv'>(rememberedWhere())
  const [tvMessage, setTvMessage] = useState('')
  const abort = useRef<AbortController | null>(null)

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && onClose()
    document.addEventListener('keydown', onKey)
    streamApi
      .status()
      .then((st) => {
        if (!st.ready) {
          setError(st.problem || t('Streaming isn’t set up yet.'))
          return
        }
        const qs = st.qualities ?? []
        setQualities(qs)
        setQuality(rememberedQuality(qs))
        setTv(st.tv ?? null)
      })
      .catch((e: Error) => setError(e.message))
    return () => {
      document.removeEventListener('keydown', onKey)
      abort.current?.abort()
    }
  }, [onClose])

  const onTV = where === 'tv' && !!tv

  // Already in the streamed library? Its saved release starts right away,
  // without a search.
  const [saved, setSaved] = useState<{ fileId: number; release: string } | null>(null)
  useEffect(() => {
    const q = new URLSearchParams({ kind: target.kind, tmdbId: String(target.tmdbId), season: String(target.season ?? 0), episode: String(target.episode ?? 0) })
    fetch(`/api/streamed/lookup?${q}`, { credentials: 'include' })
      .then((r) => (r.ok ? r.json() : null))
      .then((d) => d?.found && setSaved({ fileId: d.fileId, release: d.release }))
      .catch(() => undefined)
  }, [target.kind, target.tmdbId, target.season, target.episode])
  const playSaved = () => {
    if (!saved) return
    setStarting('saved')
    setError('')
    fetch(`/api/streamed/files/${saved.fileId}/watch`, { method: 'POST', credentials: 'include' })
      .then(async (r) => {
        const d = await r.json().catch(() => null)
        if (!r.ok) throw new Error(d?.error ?? t("Couldn't start it (error {status}).", { status: r.status }))
        onClose()
        playStream(d, target.title, onKeep)
      })
      .catch((e: Error) => setError(e.message))
      .finally(() => setStarting(''))
  }

  // Look for streams whenever the quality changes (here only: on the TV,
  // Stremio picks).
  useEffect(() => {
    if (!quality || onTV) return
    abort.current?.abort()
    const c = new AbortController()
    abort.current = c
    setOptions(null)
    setError('')
    streamApi
      .options({ kind: target.kind, tmdbId: target.tmdbId, season: target.season, episode: target.episode, quality }, c.signal)
      .then((r) => !c.signal.aborted && setOptions(r.options))
      .catch((e: Error) => !c.signal.aborted && setError(e.message))
    return () => c.abort()
  }, [quality, onTV, target.kind, target.tmdbId, target.season, target.episode])

  const openOnTV = () => {
    setStarting('tv')
    setError('')
    streamApi
      .tvTitle({ kind: target.kind, tmdbId: target.tmdbId, season: target.season, episode: target.episode })
      .then((r) => setTvMessage(r.message))
      .catch((e: Error) => setError(e.message))
      .finally(() => setStarting(''))
  }

  const start = (o: StreamOption) => {
    setStarting(o.id)
    setError('')
    streamApi
      .start({ option: o.id, title: target.title })
      .then((st) => {
        onClose()
        playStream(st, target.title, onKeep, nextExtras(st, target.title, onKeep))
      })
      .catch((e: Error) => setError(e.message))
      .finally(() => setStarting(''))
  }

  return (
    <div className="hs-backdrop" role="dialog" aria-modal="true" aria-label={t('Stream {title}', { title: target.title })} onClick={onClose}>
      <div className="hs-sheet" onClick={(e) => e.stopPropagation()}>
        <div className="hs-head">
          <strong>{t('Stream {title}', { title: target.title })}</strong>
          <button className="btn-sm" onClick={onClose} aria-label={t('Close')}>
            <Icon name="x" size={14} />
          </button>
        </div>
        {tv && (
          <div className="seg hs-quality" role="tablist" aria-label={t('Where to play')}>
            {(['here', 'tv'] as const).map((w) => (
              <button
                key={w}
                role="tab"
                aria-selected={where === w}
                className={where === w ? 'active' : ''}
                onClick={() => {
                  setWhere(w)
                  rememberWhere(w)
                  setTvMessage('')
                }}
              >
                {w === 'here' ? t('This device') : tv.name}
              </button>
            ))}
          </div>
        )}
        {onTV && (
          <div className="hs-tv">
            <p className="hint">{t('Opens it in Stremio on the {tv}, with its own 4K stream ready. Press OK on the remote to start.', { tv: tv?.name ?? '' })}</p>
            <button className="primary btn-with-icon" disabled={starting === 'tv'} onClick={openOnTV}>
              <Icon name="play" size={16} /> {starting === 'tv' ? t('Opening…') : t('Open on the {tv}', { tv: tv?.name ?? '' })}
            </button>
            {tvMessage && <p className="hint">{tvMessage}</p>}
          </div>
        )}
        {!onTV && qualities && qualities.length > 1 && (
          <div className="seg hs-quality" role="tablist" aria-label={t('Quality')}>
            {qualities.map((q) => (
              <button
                key={q}
                role="tab"
                aria-selected={q === quality}
                className={q === quality ? 'active' : ''}
                onClick={() => {
                  setQuality(q)
                  rememberQuality(q)
                }}
              >
                {q}
              </button>
            ))}
          </div>
        )}
        {!onTV && saved && (
          <button className="hs-option hs-saved" disabled={!!starting} onClick={playSaved}>
            <strong>
              <Icon name="play" size={14} /> {starting === 'saved' ? t('Starting…') : t('Your saved release, starts right away')}
            </strong>
            <small>{saved.release}</small>
          </button>
        )}
        {error && <p className="error-text">{error}</p>}
        {!onTV && !error && quality && options === null && <p className="hint">{t('Finding streams… this takes a few seconds.')}</p>}
        {!onTV && options && options.length === 0 && <p className="hint">{t('No stream found at {quality}. Try another quality, or download it instead.', { quality })}</p>}
        {!onTV && options && options.length > 0 && (
          <ul className="hs-list">
            {options.map((o, i) => {
              const lines = o.title.split('\n')
              const trouble = browserTrouble({ codec: o.codec, release: lines[0] })
              return (
                <li key={o.id}>
                  <button className="hs-option" disabled={!!starting} onClick={() => start(o)}>
                    <span className="hs-main">
                      <span className="hs-name">{optionLabel(o, i)}</span>
                      {lines[0] && lines[0] !== 'StreamNZB' && <span className="hs-rel">{lines[0]}</span>}
                      {lines.slice(1).join(' · ') && <span className="hint">{lines.slice(1).join(' · ')}</span>}
                      {trouble && <span className="hs-warn">{trouble} {t('Plays fine on the TV.')}</span>}
                    </span>
                    <span className="hs-go">{starting === o.id ? t('Starting…') : <Icon name="play" size={18} />}</span>
                  </button>
                </li>
              )
            })}
          </ul>
        )}
      </div>
    </div>
  )
}

// streamRelease plays one exact release from a release list; onKeep grabs it.
export async function streamRelease(r: SearchResult, title: string, onKeep?: () => void) {
  const st = await streamApi.start({ downloadUrl: r.downloadUrl, releaseTitle: r.title, title })
  playStream(st, title, onKeep, nextExtras(st, title, onKeep))
}

// Usherr's own engine keeps the other releases in line: "Try another release"
// (and a release that breaks part-way) plays the next one from where it was.
function nextExtras(st: StreamStart, title: string, onKeep?: () => void): StreamExtras | undefined {
  if (!st.canNext) return undefined
  return {
    onNext: async (at) => {
      const n = await streamApi.next(st.id)
      playStream(n, title, onKeep, { ...nextExtras(n, title, onKeep), startAt: at && at > 1 ? at : undefined })
    },
  }
}

// optionLabel names a stream: StreamNZB calls them "StreamNZB" plus the
// profile on a second line, and "<profile> (next release)".
function optionLabel(o: StreamOption, i: number): string {
  if (i === 0) return t('Best match')
  if (/next release/i.test(o.name)) return t('Next release (if the first one stalls)')
  if (o.name === 'Usherr') return t('Option {n}', { n: i + 1 })
  const parts = o.name
    .split(/\r?\n/)
    .map((x) => x.trim())
    .filter((x) => x && x !== 'StreamNZB')
  return parts[parts.length - 1] || t('Option {n}', { n: i + 1 })
}
