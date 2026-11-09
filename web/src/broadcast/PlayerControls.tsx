// Usherr Broadcast: the player's own controls, over the picture, in place of
// the browser's (home/Player.tsx keeps <video controls> for Neu and Classic).
// From the mock-up: the title, a seek bar (Left and Right move 10 seconds
// while it has the focus, the FF and RW keys 30, OK or Play/Pause pauses),
// then Subtitles, Audio, Version and Start over. They go away after a few
// seconds of playing; any key brings them back; Back hides them, then leaves.
import { useCallback, useEffect, useRef, useState, type RefObject } from 'react'
import Icon from '../components/Icon'
import { keepAudioLang, keepSubLang, soundLabel, subLabel, type PlayTrack, type Tracks } from '../home/playerTracks'
import { t } from '../i18n'
import { moveFocus, remoteOn, useRemote } from '../remote'
import { CONTROLS_FOR_MS, percentOf, playClock, playerBack, SEEK_JUMP, SEEK_STEP, seekTarget } from './logic'
import './player.css'

export interface PlayerControlsProps {
  video: RefObject<HTMLVideoElement | null>
  title: string
  subtitle?: string
  /** Seconds the picture starts into the file (a copy converted from part-way). */
  shift: number
  /** A live channel: no seeking, no clock to the end. */
  live?: boolean
  tracks: Tracks | null
  audio?: number
  sub: PlayTrack | null
  burn?: number
  onAudio: (tr: PlayTrack) => void
  onSub: (tr: PlayTrack | null) => void
  startOver?: () => void
  /** Another release of the same title (a stream's "Try another release"). */
  another?: { run: () => void; busy: boolean }
  /** The controls showing or not (the player's top bar follows them). */
  onShown?: (shown: boolean) => void
}

type List = 'subs' | 'audio' | null

export default function PlayerControls(p: PlayerControlsProps) {
  const { video, shift } = p
  const [shown, setShown] = useState(true)
  const [list, setList] = useState<List>(null)
  const [paused, setPaused] = useState(false)
  const [at, setAt] = useState(0)
  const [length, setLength] = useState(0)
  const bar = useRef<HTMLButtonElement>(null)
  const box = useRef<HTMLDivElement>(null)
  const listBox = useRef<HTMLDivElement>(null)
  const opener = useRef<HTMLButtonElement | null>(null)
  const timer = useRef(0)
  const state = useRef({ shown, list, paused })
  state.current = { shown, list, paused }

  const onShown = p.onShown
  useEffect(() => {
    onShown?.(shown)
  }, [shown, onShown])

  // Shows the controls, and hides them again after a few seconds of playing.
  const wake = useCallback(() => {
    setShown(true)
    window.clearTimeout(timer.current)
    timer.current = window.setTimeout(() => {
      const v = video.current
      if (v && !v.paused && !state.current.list) setShown(false)
    }, CONTROLS_FOR_MS)
  }, [video])
  useEffect(() => () => window.clearTimeout(timer.current), [])

  // The picture's time, length and playing state.
  useEffect(() => {
    const v = video.current
    if (!v) return
    const tick = () => {
      setAt(v.currentTime)
      const d = v.duration
      setLength(Number.isFinite(d) ? d : v.seekable.length ? v.seekable.end(v.seekable.length - 1) : 0)
    }
    const play = () => {
      setPaused(false)
      wake()
    }
    const pause = () => {
      setPaused(true)
      setShown(true)
      window.clearTimeout(timer.current)
    }
    // a tap on the picture shows or hides the controls
    const tap = () => {
      if (state.current.shown && !state.current.list) setShown(false)
      else wake()
    }
    tick()
    setPaused(v.paused)
    v.addEventListener('click', tap)
    v.addEventListener('timeupdate', tick)
    v.addEventListener('durationchange', tick)
    v.addEventListener('loadedmetadata', tick)
    v.addEventListener('play', play)
    v.addEventListener('pause', pause)
    return () => {
      v.removeEventListener('timeupdate', tick)
      v.removeEventListener('durationchange', tick)
      v.removeEventListener('loadedmetadata', tick)
      v.removeEventListener('play', play)
      v.removeEventListener('pause', pause)
      v.removeEventListener('click', tap)
    }
  }, [video, wake])

  // The seek bar has the remote's first focus, so OK pauses.
  useEffect(() => {
    wake()
    if (remoteOn()) bar.current?.focus({ preventScroll: true })
  }, [wake])

  const toggle = useCallback(() => {
    const v = video.current
    if (!v) return
    if (v.paused) void v.play().catch(() => undefined)
    else v.pause()
    wake()
  }, [video, wake])

  const seek = useCallback(
    (by: number) => {
      const v = video.current
      if (!v || p.live) return
      const end = Number.isFinite(v.duration) ? v.duration : v.seekable.length ? v.seekable.end(v.seekable.length - 1) : Infinity
      v.currentTime = seekTarget(v.currentTime, by, end)
      setAt(v.currentTime)
      wake()
    },
    [video, wake, p.live],
  )

  const closeList = useCallback(() => {
    setList(null)
    window.setTimeout(() => opener.current?.focus({ preventScroll: true }), 0)
  }, [])

  // Back: the list, then the controls, then out of the player (the player's own Escape).
  const back = useCallback((): boolean => {
    const what = playerBack({ listOpen: !!state.current.list, controls: state.current.shown })
    if (what === 'close-list') closeList()
    else if (what === 'hide') {
      window.clearTimeout(timer.current)
      setShown(false)
    }
    return what !== 'leave'
  }, [closeList])

  // Any key shows the controls; while they're hidden the first press only shows them.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.ctrlKey || e.altKey || e.metaKey) return
      if (e.key === 'Escape' || e.key === 'Esc') {
        if (back()) {
          e.preventDefault()
          e.stopPropagation()
        }
        return
      }
      if (e.key === 'Backspace' || e.key === 'GoBack' || e.key === 'BrowserBack') return // the remote's Back (useRemote below)
      const hidden = !state.current.shown
      wake()
      // typing (the party chat): its keys stay its own
      const focused = document.activeElement
      if (focused instanceof HTMLElement && (focused.isContentEditable || focused.tagName === 'TEXTAREA' || (focused.tagName === 'INPUT' && !['button', 'checkbox', 'radio', 'range'].includes((focused as HTMLInputElement).type)))) return
      const inside = box.current?.contains(focused)
      if (hidden && (e.key.startsWith('Arrow') || e.key === 'Enter' || e.key === ' ')) {
        e.preventDefault()
        e.stopPropagation()
        if (!inside) bar.current?.focus({ preventScroll: true })
      } else if (!inside && e.key.startsWith('Arrow') && (!focused || focused === document.body || focused.matches('video, .hp-player, .hp-box, .hp-stage'))) {
        // nothing has the focus (it was on a button that went away): the bar has it
        e.preventDefault()
        e.stopPropagation()
        bar.current?.focus({ preventScroll: true })
      }
    }
    window.addEventListener('keydown', onKey, true)
    return () => window.removeEventListener('keydown', onKey, true)
  }, [back, wake])

  useRemote((action) => {
    switch (action) {
      case 'back':
        return back() // false: the player closes (its own Escape)
      case 'playpause':
        toggle()
        return true
      case 'play':
        if (video.current?.paused) toggle()
        return true
      case 'pause':
        if (video.current && !video.current.paused) toggle()
        return true
      case 'fastforward':
        seek(SEEK_JUMP)
        return true
      case 'rewind':
        seek(-SEEK_JUMP)
        return true
      case 'options':
        wake()
        return true
    }
  })

  // The list just opened: focus on what's chosen in it.
  useEffect(() => {
    if (!list) return
    const el = listBox.current?.querySelector<HTMLElement>('[aria-checked="true"]') ?? listBox.current?.querySelector<HTMLElement>('button')
    el?.focus({ preventScroll: true })
  }, [list])

  const openList = (which: List, from: HTMLButtonElement) => {
    opener.current = from
    setList(which)
    wake()
  }

  const tracks = p.tracks
  const subOn = p.burn !== undefined ? tracks?.subtitles.find((s) => s.index === p.burn) ?? null : p.sub
  const audioOn = tracks?.audio.find((a) => a.index === p.audio) ?? tracks?.audio[0]
  const shortLang = (tr: PlayTrack) => subLabel(tr).split(' · ')[0]
  const pct = percentOf(at, length)
  const clock = p.live ? t('Live') : `${playClock(at + shift)} / ${playClock(length + shift)}`

  return (
    <div className={`bp-osd${shown ? '' : ' off'}`} ref={box} onPointerMove={wake}>
      <span className="bp-where">
        {t('Playing')} · {p.title}
      </span>
      <div className="bp-bottom">
        <h2 className="bp-title">
          {p.title}
          {p.subtitle && <span className="bp-sub"> · {p.subtitle}</span>}
        </h2>
        <button
          ref={bar}
          type="button"
          className="bp-bar"
          data-remote-own-arrows=""
          aria-label={paused ? t('Paused at {time}. OK plays; Left and Right move 10 seconds.', { time: playClock(at + shift) }) : t('Playing at {time}. OK pauses; Left and Right move 10 seconds.', { time: playClock(at + shift) })}
          onClick={(e) => {
            // the mouse on the line: go there; OK, a tap elsewhere, or Space: pause or play
            const track = (e.target as HTMLElement).closest('.bp-track')
            if (track && e.detail > 0 && !p.live && length > 0) {
              const r = track.getBoundingClientRect()
              const v = video.current
              if (v) v.currentTime = Math.max(0, Math.min(length, ((e.clientX - r.left) / r.width) * length))
              wake()
              return
            }
            toggle()
          }}
          onKeyDown={(e) => {
            if (e.key === 'ArrowLeft' || e.key === 'ArrowRight') {
              e.preventDefault()
              seek(e.key === 'ArrowLeft' ? -SEEK_STEP : SEEK_STEP)
            } else if (e.key === 'ArrowUp' || e.key === 'ArrowDown') {
              // the bar keeps Left and Right; Up and Down still move the focus
              e.preventDefault()
              moveFocus(e.key === 'ArrowUp' ? 'up' : 'down')
              wake()
            }
          }}
        >
          <Icon name={paused ? 'play' : 'pause'} size={26} />
          <span className="bp-track" aria-hidden="true">
            <span style={{ width: `${pct}%` }} />
          </span>
          <span className="bp-clock">{clock}</span>
        </button>
        <div className="bp-row">
          {tracks && tracks.subtitles.length > 0 && (
            <button type="button" className="bp-btn" aria-haspopup="menu" aria-expanded={list === 'subs'} onClick={(e) => openList('subs', e.currentTarget)}>
              <Icon name="captions" size={20} /> {t('Subtitles: {name}', { name: subOn ? shortLang(subOn) : t('Off') })}
            </button>
          )}
          {tracks && tracks.audio.length > 1 && (
            <button type="button" className="bp-btn" aria-haspopup="menu" aria-expanded={list === 'audio'} onClick={(e) => openList('audio', e.currentTarget)}>
              <Icon name="volume" size={20} /> {t('Audio: {name}', { name: audioOn ? soundLabel(audioOn) : '' })}
            </button>
          )}
          {p.another && (
            <button type="button" className="bp-btn" disabled={p.another.busy} onClick={p.another.run} title={t('Plays the next release found instead of this one')}>
              <Icon name="refresh" size={20} /> {p.another.busy ? t('Finding another…') : t('Version: try another')}
            </button>
          )}
          {p.startOver && (
            <button type="button" className="bp-btn" onClick={p.startOver}>
              <Icon name="skip-prev" size={20} /> {t('Start over')}
            </button>
          )}
        </div>
      </div>
      {list && tracks && (
        <div className="bp-list" role="menu" ref={listBox} aria-label={list === 'subs' ? t('Subtitles') : t('Audio')}>
          <strong>{list === 'subs' ? t('Subtitles') : t('Audio')}</strong>
          {list === 'subs' && (
            <button
              type="button"
              role="menuitemradio"
              aria-checked={!subOn}
              onClick={() => {
                keepSubLang('')
                closeList()
                if (subOn) p.onSub(null)
              }}
            >
              {!subOn ? <Icon name="check" size={18} /> : <span className="bp-dot" />} {t('Off')}
            </button>
          )}
          {(list === 'subs' ? tracks.subtitles : tracks.audio).map((tr) => {
            const on = list === 'subs' ? !!subOn && (subOn.key !== undefined ? subOn.key === tr.key : subOn.index === tr.index) : audioOn?.index === tr.index
            return (
              <button
                key={`${tr.kind}-${tr.index}-${tr.key ?? ''}`}
                type="button"
                role="menuitemradio"
                aria-checked={on}
                onClick={() => {
                  closeList()
                  if (list === 'subs') {
                    if (!tr.forced) keepSubLang(tr.lang)
                    if (!on) p.onSub(tr)
                  } else {
                    keepAudioLang(tr.lang)
                    if (!on) p.onAudio(tr)
                  }
                }}
              >
                {on ? <Icon name="check" size={18} /> : <span className="bp-dot" />} {list === 'subs' ? subLabel(tr) : soundLabel(tr)}
              </button>
            )
          })}
        </div>
      )}
    </div>
  )
}
