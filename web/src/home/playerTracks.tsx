// Usherr: the player's sound and subtitle menu, and the languages each
// person prefers (kept in this browser): the sound in their language when a
// file has it, and their subtitles on when they read any.
import { useEffect, useRef, useState } from 'react'
import Icon from '../components/Icon'
import { locale, t } from '../i18n'

export interface PlayTrack {
  index: number
  key?: string
  kind: 'audio' | 'text' | 'image'
  codec: string
  lang?: string
  title?: string
  channels?: number
  default?: boolean
  forced?: boolean
}
export interface Tracks {
  audio: PlayTrack[]
  subtitles: PlayTrack[]
}

const AUDIO_KEY = 'usherr.player.audioLang'
const SUB_KEY = 'usherr.player.subLang'
const get = (k: string) => {
  try {
    return localStorage.getItem(k) ?? ''
  } catch {
    return ''
  }
}
const set = (k: string, v: string) => {
  try {
    if (v) localStorage.setItem(k, v)
    else localStorage.removeItem(k)
  } catch {
    // private window: not kept
  }
}
export const prefAudioLang = () => get(AUDIO_KEY)
export const prefSubLang = () => get(SUB_KEY)

const TWO: Record<string, string> = {
  eng: 'en',
  spa: 'es',
  fre: 'fr',
  fra: 'fr',
  ger: 'de',
  deu: 'de',
  ita: 'it',
  por: 'pt',
  jpn: 'ja',
  kor: 'ko',
  chi: 'zh',
  zho: 'zh',
  rus: 'ru',
  dut: 'nl',
  nld: 'nl',
  swe: 'sv',
  dan: 'da',
  nor: 'no',
  nob: 'no',
  fin: 'fi',
  pol: 'pl',
  hin: 'hi',
  ara: 'ar',
  tur: 'tr',
  cze: 'cs',
  ces: 'cs',
  hun: 'hu',
  gre: 'el',
  ell: 'el',
  heb: 'he',
  tha: 'th',
  vie: 'vi',
  ind: 'id',
  ukr: 'uk',
  ron: 'ro',
  rum: 'ro',
}
const base = (l?: string) => {
  const s = (l ?? '').toLowerCase().split(/[-_]/)[0]
  return TWO[s] ?? s
}
export const sameLang = (a?: string, b?: string) => !!a && !!b && base(a) === base(b)

function langName(l?: string) {
  if (!l) return t('Unknown language')
  try {
    return new Intl.DisplayNames([locale() ?? 'en'], { type: 'language' }).of(base(l)) ?? l
  } catch {
    return l
  }
}

function soundLabel(tr: PlayTrack) {
  const ch = tr.channels ? (tr.channels >= 8 ? '7.1' : tr.channels >= 6 ? '5.1' : tr.channels === 2 ? t('stereo') : tr.channels === 1 ? t('mono') : `${tr.channels} ch`) : ''
  return [langName(tr.lang), tr.title && !tr.title.toLowerCase().includes((tr.lang ?? '~').toLowerCase()) ? tr.title : '', [tr.codec.toUpperCase(), ch].filter(Boolean).join(' ')].filter(Boolean).join(' · ')
}

function subLabel(tr: PlayTrack) {
  const bits = [langName(tr.lang)]
  if (tr.forced) bits.push(t('forced'))
  if (tr.title && tr.title.length < 30) bits.push(tr.title)
  if (tr.index < 0) bits.push(t('file'))
  if (tr.kind === 'image') bits.push(t('burned in, slower to start'))
  return bits.join(' · ')
}

// pickSub is the subtitle to show by itself: the person's language, as text
// and not just the forced lines; none when they don't read subtitles.
export function pickSub(tr: Tracks, lang: string): PlayTrack | null {
  if (!lang) return null
  const text = tr.subtitles.filter((s) => s.kind === 'text' && sameLang(s.lang, lang))
  return text.find((s) => !s.forced) ?? text[0] ?? null
}

export function TracksMenu({
  tracks,
  audio,
  burn,
  sub,
  onAudio,
  onSub,
}: {
  tracks: Tracks
  audio?: number
  burn?: number
  sub: PlayTrack | null
  onAudio: (tr: PlayTrack) => void
  onSub: (tr: PlayTrack | null) => void
}) {
  const [open, setOpen] = useState(false)
  const box = useRef<HTMLDivElement>(null)
  useEffect(() => {
    if (!open) return
    const away = (e: MouseEvent) => box.current && !box.current.contains(e.target as Node) && setOpen(false)
    document.addEventListener('mousedown', away)
    return () => document.removeEventListener('mousedown', away)
  }, [open])
  if (tracks.audio.length < 2 && tracks.subtitles.length === 0) return null
  const subOn = burn !== undefined ? tracks.subtitles.find((s) => s.index === burn) : sub
  return (
    <div className="hp-tracks-wrap" ref={box}>
      <button type="button" className={`btn-sm btn-with-icon${subOn ? ' primary' : ''}`} onClick={() => setOpen(!open)} aria-expanded={open} title={t('Sound and subtitles')}>
        <Icon name="captions" size={16} /> <span className="hp-tracks-label">{t('Audio & subtitles')}</span>
      </button>
      {open && (
        <div className="hp-tracks" role="menu">
          {tracks.audio.length > 1 && (
            <>
              <strong>{t('Sound')}</strong>
              {tracks.audio.map((tr) => (
                <button
                  key={tr.index}
                  type="button"
                  role="menuitemradio"
                  aria-checked={audio === tr.index}
                  className={audio === tr.index ? 'on' : ''}
                  onClick={() => {
                    set(AUDIO_KEY, tr.lang ?? '')
                    setOpen(false)
                    if (audio !== tr.index) onAudio(tr)
                  }}
                >
                  {audio === tr.index ? <Icon name="check" size={14} /> : <span className="hp-tracks-dot" />} {soundLabel(tr)}
                </button>
              ))}
            </>
          )}
          <strong>{t('Subtitles')}</strong>
          <button
            type="button"
            role="menuitemradio"
            aria-checked={!subOn}
            className={!subOn ? 'on' : ''}
            onClick={() => {
              set(SUB_KEY, '')
              setOpen(false)
              onSub(null)
            }}
          >
            {!subOn ? <Icon name="check" size={14} /> : <span className="hp-tracks-dot" />} {t('Off')}
          </button>
          {tracks.subtitles.map((tr) => {
            const on = subOn?.key === tr.key
            return (
              <button
                key={tr.key}
                type="button"
                role="menuitemradio"
                aria-checked={on}
                className={on ? 'on' : ''}
                onClick={() => {
                  if (!tr.forced) set(SUB_KEY, tr.lang ?? '')
                  setOpen(false)
                  if (!on) onSub(tr)
                }}
              >
                {on ? <Icon name="check" size={14} /> : <span className="hp-tracks-dot" />} {subLabel(tr)}
              </button>
            )
          })}
          {tracks.subtitles.length === 0 && <small className="hint">{t('This file has no subtitles. Bazarr can fetch some.')}</small>}
        </div>
      )}
    </div>
  )
}
