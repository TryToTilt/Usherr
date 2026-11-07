import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Link, useParams, useSearchParams } from 'react-router-dom'
import { api, type Book, type BookTrack } from '../api'
import { saveProgress } from './progress'
import Icon from '../components/Icon'
import { PosterFallback } from '../components/PosterCard'
import { useDocumentTitle } from '../documentTitle'
import { t, tc } from '../i18n'
import Marks from './Marks'
import { marksApi, type ReadingMark } from './marksApi'
import { absoluteTime, bookPercent, buildChapters, chapterAt, formatAudioPosition, formatClock, locateTime, nextSpeed, parseAudioPosition, resumePoint, smartRewind, timelineOf, totalTime, type RewindMode } from './shelfMath'

const SPEED_KEY = 'mediarium.player.speed'
const SKIP_BACK_KEY = 'mediarium.player.skipBack'
const SKIP_FORWARD_KEY = 'mediarium.player.skipForward'
const REWIND_KEY = 'mediarium.player.rewind'
const SLEEP_CHOICES = [0, 15, 30, 45, 60, -1] // minutes; -1 = end of this chapter
const SKIP_CHOICES = [5, 10, 15, 30, 45, 60] // seconds
const LENGTHS_POLL_MS = 3000
const LENGTHS_POLLS = 40 // about two minutes; a book that takes longer is known next time it opens

type Panel = '' | 'chapters' | 'marks' | 'settings'

function savedSpeed(): number {
  try {
    const v = Number(localStorage.getItem(SPEED_KEY))
    return v >= 0.5 && v <= 3 ? v : 1
  } catch {
    return 1
  }
}

function savedSkip(key: string): number {
  try {
    const v = Number(localStorage.getItem(key))
    return SKIP_CHOICES.includes(v) ? v : 30
  } catch {
    return 30
  }
}

function savedRewind(): RewindMode {
  try {
    const v = localStorage.getItem(REWIND_KEY)
    return v === 'off' || v === 'more' ? v : 'little'
  } catch {
    return 'little'
  }
}

function remember(key: string, value: string) {
  try {
    localStorage.setItem(key, value)
  } catch {
    // not remembered, that's all
  }
}

// The audiobook player: one book, its chapters in order (the audio files, or
// the chapter marks inside a single M4B file), with skips, speed, a sleep
// timer, bookmarks and the phone's lock-screen controls. Once each file's real
// length is known the files make one timeline: the whole-book slider, the time
// left, the skips across files and the lock screen use it. Your place is kept
// on the server, so the book carries on where you stopped on any device.
export default function Player() {
  const { id } = useParams()
  const bookId = Number(id)
  const [search] = useSearchParams()
  const jumpTo = Number(search.get('at')) // a bookmark to open at, from the book's page
  const markItem = `audio:${bookId}`
  const audio = useRef<HTMLAudioElement>(null)
  const [book, setBook] = useState<Book | null>(null)
  const [tracks, setTracks] = useState<BookTrack[] | null>(null)
  const [lengthsPending, setLengthsPending] = useState(false)
  const [error, setError] = useState('')
  const [track, setTrack] = useState(0)
  const [time, setTime] = useState(0)
  const [duration, setDuration] = useState(0)
  const [playing, setPlaying] = useState(false)
  const [speed, setSpeed] = useState(savedSpeed)
  const [skipBack, setSkipBack] = useState(() => savedSkip(SKIP_BACK_KEY))
  const [skipForward, setSkipForward] = useState(() => savedSkip(SKIP_FORWARD_KEY))
  const [rewind, setRewind] = useState(savedRewind)
  const [sleep, setSleep] = useState(0) // minutes left on the timer; -1 = end of chapter
  const [sleepEnds, setSleepEnds] = useState(0)
  const [panel, setPanel] = useState<Panel>('')
  const [marks, setMarks] = useState<ReadingMark[]>([])
  const [notice, setNotice] = useState('')
  const [bookDrag, setBookDrag] = useState<number | null>(null) // the whole-book slider while it is being moved
  const resumeAt = useRef(0) // seconds to jump to once the track has loaded
  const lastSaved = useRef(0)
  const pausedAt = useRef(0) // when it stopped, for the rewind on playing again; 0 = no rewind due
  useDocumentTitle(book ? t('{title} · Listening', { title: book.title }) : t('Listening'))

  useEffect(() => {
    let stale = false
    Promise.all([api.getBook(bookId), api.bookTracks(bookId), api.bookProgress(bookId, 'audiobook').catch(() => null), marksApi.list(markItem).catch(() => [] as ReadingMark[])])
      .then(([b, list, p, m]) => {
        if (stale) return
        setBook(b)
        setTracks(list.tracks)
        setLengthsPending(!!list.lengthsPending)
        setMarks(m)
        const mark = jumpTo ? m.find((x) => x.id === jumpTo) : undefined
        const at = mark ? parseAudioPosition(mark.position) : p?.finished ? { track: 0, seconds: 0 } : parseAudioPosition(p?.position) // Usherr: a finished book starts again
        if (at.track < list.tracks.length) {
          setTrack(at.track)
          resumeAt.current = at.seconds
          setTime(at.seconds)
          // Coming back to a place left earlier counts as a pause since then.
          const left = Date.parse(p?.updatedAt ?? '')
          if (!mark && !p?.finished && at.seconds > 0 && left > 0) pausedAt.current = left
        }
      })
      .catch((e) => !stale && setError(e instanceof Error ? e.message : String(e)))
    return () => {
      stale = true
    }
  }, [bookId, markItem, jumpTo])

  // A book opened for the first time has its lengths read right away: they
  // arrive in a moment.
  useEffect(() => {
    if (!lengthsPending) return
    let polls = 0
    const timer = window.setInterval(() => {
      polls++
      api
        .bookTracks(bookId)
        .then((r) => {
          setTracks(r.tracks)
          if (!r.lengthsPending || polls >= LENGTHS_POLLS) setLengthsPending(false)
        })
        .catch(() => setLengthsPending(false))
    }, LENGTHS_POLL_MS)
    return () => window.clearInterval(timer)
  }, [bookId, lengthsPending])

  const timeline = useMemo(() => timelineOf(tracks ?? [], track, duration), [tracks, track, duration])
  const total = timeline ? totalTime(timeline) : 0
  const bookTime = timeline ? absoluteTime(timeline, track, time) : 0
  const percent = bookPercent(tracks ?? [], timeline, track, time, duration)
  const chapters = useMemo(() => buildChapters(tracks ?? []), [tracks])
  const cur = chapterAt(chapters, track, time)
  const chapter = chapters[cur]
  const chStart = chapter?.start ?? 0
  const chEnd = chapter?.end ?? duration

  // The player element, kept after React lets go of it, so leaving the player
  // can still read where the listener was.
  const lastAudio = useRef<HTMLAudioElement | null>(null)
  useEffect(() => {
    if (audio.current) lastAudio.current = audio.current
  })

  const save = useCallback(
    (keepalive = false) => {
      const a = audio.current ?? lastAudio.current
      if (!tracks || !a) return
      const secs = a.currentTime || resumeAt.current
      const pct = bookPercent(tracks, timeline, track, secs, a.duration)
      const finished = track === tracks.length - 1 && a.duration > 0 && secs >= a.duration - 5
      saveProgress(bookId, { format: 'audiobook', position: formatAudioPosition({ track, seconds: secs }), percent: pct, finished }, keepalive)
      lastSaved.current = Date.now()
    },
    [bookId, track, tracks, timeline],
  )

  // Keep the place: every 15 seconds while playing, and when the page is hidden or closed.
  useEffect(() => {
    const onHide = () => {
      if (document.visibilityState === 'hidden') save(true)
    }
    document.addEventListener('visibilitychange', onHide)
    window.addEventListener('pagehide', onHide)
    return () => {
      document.removeEventListener('visibilitychange', onHide)
      window.removeEventListener('pagehide', onHide)
    }
  }, [save])

  // And when the player closes inside the app (the Back link), which removes
  // the audio without a pause event. save changes with the track, so the
  // newest one is kept in a ref and used only when the player really closes.
  const saveRef = useRef(save)
  saveRef.current = save
  useEffect(() => () => saveRef.current(true), [])

  useEffect(() => {
    if (audio.current) audio.current.playbackRate = speed
    remember(SPEED_KEY, String(speed))
  }, [speed])

  const play = useCallback(() => {
    void audio.current?.play().catch(() => setPlaying(false))
  }, [])
  const pause = useCallback(() => audio.current?.pause(), [])
  const goTrack = useCallback(
    (n: number, autoplay = true, at = 0) => {
      if (!tracks || n < 0 || n >= tracks.length) return
      save()
      pausedAt.current = 0
      resumeAt.current = at
      setTime(at)
      setTrack(n)
      if (autoplay) window.setTimeout(play, 0)
    },
    [tracks, play, save],
  )
  // moveTo goes to a place in the book: within the file that is playing, or by
  // opening the file it is in. It keeps playing if it was, and a place chosen
  // by hand isn't rewound from.
  const moveTo = useCallback(
    (n: number, seconds: number) => {
      const a = audio.current
      if (!a) return
      if (n !== track) {
        goTrack(n, !a.paused, seconds)
        return
      }
      pausedAt.current = 0
      if (a.readyState === 0) resumeAt.current = seconds
      else a.currentTime = seconds
      setTime(seconds)
    },
    [track, goTrack],
  )
  // A skip goes across files once the timeline is known; until then it stays
  // inside the file.
  const seekBy = useCallback(
    (s: number) => {
      const a = audio.current
      if (!a) return
      const here = a.readyState === 0 ? resumeAt.current : a.currentTime
      if (timeline) {
        const to = locateTime(timeline, Math.min(absoluteTime(timeline, track, here) + s, totalTime(timeline) - 0.5))
        moveTo(to.track, to.seconds)
        return
      }
      if (a.readyState > 0) moveTo(track, Math.max(0, Math.min((a.duration || 0) - 0.5, here + s)))
    },
    [timeline, track, moveTo],
  )
  const seekToBook = useCallback(
    (at: number) => {
      if (!timeline) return
      const to = locateTime(timeline, Math.min(at, totalTime(timeline) - 0.5))
      moveTo(to.track, to.seconds)
    },
    [timeline, moveTo],
  )
  // goChapter jumps to the start of chapter n and plays.
  const goChapter = useCallback(
    (n: number) => {
      const c = chapters[n]
      if (!c) return
      const a = audio.current
      pausedAt.current = 0
      if (c.track === track && a) {
        a.currentTime = c.start
        setTime(c.start)
        play()
        return
      }
      goTrack(c.track, true, c.start)
    },
    [chapters, track, goTrack, play],
  )

  // The lock screen and headphone buttons.
  useEffect(() => {
    if (!('mediaSession' in navigator) || !book || !tracks) return
    navigator.mediaSession.metadata = new MediaMetadata({
      title: chapter?.title ?? book.title,
      artist: book.author,
      album: book.title,
      artwork: book.coverUrl ? [{ src: book.coverUrl.replace('-M.jpg', '-L.jpg'), sizes: '500x750', type: 'image/jpeg' }] : [],
    })
    const ms = navigator.mediaSession
    // A browser that doesn't know an action throws instead of ignoring it.
    const handle = (action: MediaSessionAction, fn: MediaSessionActionHandler | null) => {
      try {
        ms.setActionHandler(action, fn)
      } catch {
        // this browser's lock screen doesn't have it
      }
    }
    handle('play', play)
    handle('pause', pause)
    handle('seekbackward', (d) => seekBy(-(d.seekOffset ?? skipBack)))
    handle('seekforward', (d) => seekBy(d.seekOffset ?? skipForward))
    handle('seekto', (d) => {
      if (typeof d.seekTime !== 'number') return
      if (timeline) seekToBook(d.seekTime)
      else moveTo(track, d.seekTime)
    })
    handle('previoustrack', () => goChapter(cur - 1))
    handle('nexttrack', () => goChapter(cur + 1))
    return () => {
      for (const a of ['play', 'pause', 'seekbackward', 'seekforward', 'seekto', 'previoustrack', 'nexttrack'] as MediaSessionAction[]) handle(a, null)
    }
  }, [book, tracks, chapter, cur, play, pause, seekBy, seekToBook, moveTo, goChapter, skipBack, skipForward, timeline, track])

  // The lock screen's progress bar: the whole book once its length is known.
  // Browsers carry it on by themselves between these.
  const syncLockScreen = useCallback(() => {
    const a = audio.current
    if (!a || !('mediaSession' in navigator) || !navigator.mediaSession.setPositionState) return
    const length = timeline ? totalTime(timeline) : a.duration
    const place = timeline ? absoluteTime(timeline, track, a.currentTime) : a.currentTime
    if (!Number.isFinite(length) || length <= 0) return
    try {
      navigator.mediaSession.setPositionState({ duration: length, position: Math.min(place, length), playbackRate: a.playbackRate || 1 })
    } catch {
      // not this browser
    }
  }, [timeline, track])
  useEffect(() => {
    if ('mediaSession' in navigator) navigator.mediaSession.playbackState = playing ? 'playing' : 'paused'
    syncLockScreen()
  }, [playing, speed, duration, syncLockScreen])

  // The sleep timer.
  useEffect(() => {
    if (sleep <= 0 || !sleepEnds) return
    const t = window.setInterval(() => {
      if (Date.now() >= sleepEnds) {
        pause()
        setSleep(0)
        setSleepEnds(0)
      }
    }, 1000)
    return () => window.clearInterval(t)
  }, [sleep, sleepEnds, pause])

  // Space plays and pauses; the arrows skip.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const tag = (e.target as HTMLElement)?.tagName
      if (tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT') return
      if (e.key === ' ') {
        e.preventDefault()
        if (audio.current?.paused) play()
        else pause()
      } else if (e.key === 'ArrowLeft') seekBy(-skipBack)
      else if (e.key === 'ArrowRight') seekBy(skipForward)
    }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [play, pause, seekBy, skipBack, skipForward])

  useEffect(() => {
    if (!notice) return
    const timer = window.setTimeout(() => setNotice(''), 3000)
    return () => window.clearTimeout(timer)
  }, [notice])

  function chooseSleep() {
    const i = SLEEP_CHOICES.indexOf(sleep)
    const next = SLEEP_CHOICES[(i + 1) % SLEEP_CHOICES.length]
    setSleep(next)
    setSleepEnds(next > 0 ? Date.now() + next * 60_000 : 0)
  }

  // Bookmarks this spot with one press, so it works with the book in your ears.
  async function addBookmark() {
    const secs = Math.floor(audio.current?.currentTime || time)
    try {
      const m = await marksApi.add(markItem, {
        kind: 'bookmark',
        position: formatAudioPosition({ track, seconds: secs }),
        label: `${chapter?.title ?? ''} · ${formatClock(Math.max(0, secs - chStart))}`,
        percent,
      })
      setMarks((list) => [...list.filter((x) => x.id !== m.id), m].sort((a, b) => a.percent - b.percent || a.id - b.id))
      setNotice(t('Bookmarked'))
    } catch (e) {
      setNotice(e instanceof Error ? e.message : String(e))
    }
  }

  // Playing again after a pause: back a little, more after a longer pause, so
  // the sentence makes sense again. It stays inside the file.
  function rewindAfterPause() {
    const since = pausedAt.current
    pausedAt.current = 0
    const a = audio.current
    if (!since || !a) return
    const back = smartRewind(Date.now() - since, rewind)
    if (back === 0) return
    if (resumeAt.current > 0) resumeAt.current = Math.max(0, resumeAt.current - back)
    else a.currentTime = Math.max(0, a.currentTime - back)
  }

  const togglePanel = (p: Panel) => setPanel((cur) => (cur === p ? '' : p))

  if (error) {
    return (
      <div className="bks-message">
        <Icon name="headphones" size={44} />
        <h1>{t("Couldn't open this audiobook")}</h1>
        <p>{error}</p>
        <Link className="btn-with-icon" to="/bookshelf">
          <Icon name="chevron-left" size={16} /> {t('Back to your books')}
        </Link>
      </div>
    )
  }
  if (!book || !tracks) return <div className="bks-loading">{t('Opening…')}</div>
  if (tracks.length === 0) {
    return (
      <div className="bks-message">
        <Icon name="headphones" size={44} />
        <h1>{book.title}</h1>
        <p>{t('No audio files were found for this audiobook.')}</p>
        <Link className="btn-with-icon" to="/bookshelf">
          <Icon name="chevron-left" size={16} /> {t('Back to your books')}
        </Link>
      </div>
    )
  }

  const left = sleep > 0 ? Math.max(0, Math.ceil((sleepEnds - Date.now()) / 60_000)) : 0
  const commitBookDrag = () => {
    if (bookDrag === null) return
    seekToBook(bookDrag)
    setBookDrag(null)
  }
  return (
    <div className="ply">
      <header className="rdr-top ply-top">
        <Link className="icon-btn" to="/bookshelf" title={t('Back to your books')} aria-label={t('Back to your books')}>
          <Icon name="chevron-left" size={20} />
        </Link>
        <span className="rdr-title">
          <strong>{book.title}</strong>
          <small>{[book.author, book.narrators && t('read by {names}', { names: book.narrators.split(',').slice(0, 2).join(',').trim() })].filter(Boolean).join(' · ')}</small>
        </span>
        <button className="icon-btn" onClick={() => goChapter(0)} title={t('Start over')} aria-label={t('Start over')}>
          <Icon name="skip-prev" size={18} />
        </button>
        <button className={`icon-btn${panel === 'marks' ? ' active' : ''}`} onClick={() => togglePanel('marks')} title={t('Bookmarks')} aria-label={t('Bookmarks')}>
          <Icon name="bookmark" size={18} />
        </button>
        <button className={`icon-btn${panel === 'chapters' ? ' active' : ''}`} onClick={() => togglePanel('chapters')} title={t('Chapters')} aria-label={t('Chapters')}>
          <Icon name="list" size={18} />
        </button>
        <button className={`icon-btn${panel === 'settings' ? ' active' : ''}`} onClick={() => togglePanel('settings')} title={t('Player settings')} aria-label={t('Player settings')}>
          <Icon name="sliders" size={18} />
        </button>
      </header>

      <div className={`ply-body${panel ? ' with-list' : ''}`}>
        <div className="ply-main">
          <div className="ply-cover">{book.coverUrl ? <img src={book.coverUrl.replace('-M.jpg', '-L.jpg')} alt="" /> : <PosterFallback />}</div>
          <div className="ply-chapter">
            <strong>{chapter?.title}</strong>
            <small>
              {t('Chapter {n} of {total} · {percent}% of the book', { n: cur + 1, total: chapters.length, percent: Math.round(percent) })}
            </small>
          </div>
          <div className="ply-scrub">
            <input
              type="range"
              min={chStart}
              max={chEnd || chStart + 1}
              step={1}
              value={duration ? Math.min(Math.max(time, chStart), chEnd) : chStart}
              disabled={!duration}
              onChange={(e) => {
                const v = Number(e.target.value)
                pausedAt.current = 0
                setTime(v)
                if (audio.current) audio.current.currentTime = v
              }}
              aria-label={t('Place in this chapter')}
            />
            <div className="ply-times">
              <span>{formatClock(Math.max(0, time - chStart))}</span>
              <span>{duration ? `-${formatClock(Math.max(0, (chEnd - time) / speed))}` : '–'}</span>
            </div>
          </div>
          <div className="ply-controls">
            <button className="icon-btn" onClick={() => goChapter(cur - 1)} disabled={cur === 0} title={t('Previous chapter')} aria-label={t('Previous chapter')}>
              <Icon name="chevron-left" size={22} />
            </button>
            <button className="ply-skip" onClick={() => seekBy(-skipBack)} title={t('Back {n} seconds', { n: skipBack })} aria-label={t('Back {n} seconds', { n: skipBack })}>
              −{skipBack}
            </button>
            <button className="ply-play" onClick={() => (playing ? pause() : play())} aria-label={playing ? tc('player', 'Pause') : tc('player', 'Play')}>
              <Icon name={playing ? 'pause' : 'play'} size={30} />
            </button>
            <button className="ply-skip" onClick={() => seekBy(skipForward)} title={t('Forward {n} seconds', { n: skipForward })} aria-label={t('Forward {n} seconds', { n: skipForward })}>
              +{skipForward}
            </button>
            <button className="icon-btn" onClick={() => goChapter(cur + 1)} disabled={cur >= chapters.length - 1} title={t('Next chapter')} aria-label={t('Next chapter')}>
              <Icon name="chevron-right" size={22} />
            </button>
          </div>
          {timeline && chapters.length > 1 && (
            <div className="ply-scrub ply-book">
              <input
                type="range"
                min={0}
                max={total}
                step={1}
                value={bookDrag ?? bookTime}
                onChange={(e) => setBookDrag(Number(e.target.value))}
                onPointerUp={commitBookDrag}
                onPointerCancel={() => setBookDrag(null)}
                onKeyUp={commitBookDrag}
                aria-label={t('Place in the whole book')}
              />
              <div className="ply-times">
                <span>{formatClock(bookDrag ?? bookTime)}</span>
                <span>{t('{time} left in the book', { time: formatClock(Math.max(0, total - (bookDrag ?? bookTime)) / speed) })}</span>
              </div>
            </div>
          )}
          <div className="ply-extras">
            <button className="btn-sm" onClick={() => setSpeed((s) => nextSpeed(s))} title={t('Playback speed')}>
              {speed}×
            </button>
            <button className={`btn-sm btn-with-icon${sleep !== 0 ? ' primary' : ''}`} onClick={chooseSleep} title={t('Sleep timer')}>
              <Icon name="moon" size={14} /> {sleep === 0 ? t('Sleep timer') : sleep === -1 ? t('End of chapter') : `${left} min`}
            </button>
            <button className="btn-sm btn-with-icon" onClick={() => void addBookmark()} title={t('Bookmark this spot')}>
              <Icon name="bookmark" size={14} /> {t('Bookmark')}
            </button>
          </div>
          {notice && (
            <p className="hint" role="status">
              {notice}
            </p>
          )}
        </div>

        {panel === 'chapters' && (
          <ol className="ply-list">
            {chapters.map((c, i) => (
              <li key={`${c.track}-${c.start}`}>
                <button className={i === cur ? 'active' : ''} onClick={() => goChapter(i)}>
                  <span className="ply-n">{i + 1}</span> <span className="ply-name">{c.title}</span>
                  {timeline && <span className="ply-at">{formatClock(absoluteTime(timeline, c.track, c.start))}</span>}
                </button>
              </li>
            ))}
          </ol>
        )}
        {panel === 'marks' && (
          <div className="ply-panel">
            <h2>{t('Bookmarks')}</h2>
            <Marks
              item={markItem}
              list={marks}
              onChange={setMarks}
              onJump={(m) => {
                const at = parseAudioPosition(m.position)
                if (at.track < tracks.length) moveTo(at.track, at.seconds)
              }}
            />
          </div>
        )}
        {panel === 'settings' && (
          <div className="ply-panel ply-settings">
            <h2>{t('Player settings')}</h2>
            <label>
              {t('Skip back')}
              <select
                value={skipBack}
                onChange={(e) => {
                  setSkipBack(Number(e.target.value))
                  remember(SKIP_BACK_KEY, e.target.value)
                }}
              >
                {SKIP_CHOICES.map((n) => (
                  <option key={n} value={n}>
                    {t('{n} seconds', { n })}
                  </option>
                ))}
              </select>
            </label>
            <label>
              {t('Skip forward')}
              <select
                value={skipForward}
                onChange={(e) => {
                  setSkipForward(Number(e.target.value))
                  remember(SKIP_FORWARD_KEY, e.target.value)
                }}
              >
                {SKIP_CHOICES.map((n) => (
                  <option key={n} value={n}>
                    {t('{n} seconds', { n })}
                  </option>
                ))}
              </select>
            </label>
            <label>
              {t('Go back after a pause')}
              <select
                value={rewind}
                onChange={(e) => {
                  setRewind(e.target.value as RewindMode)
                  remember(REWIND_KEY, e.target.value)
                }}
              >
                <option value="off">{t('Never')}</option>
                <option value="little">{t('A little')}</option>
                <option value="more">{t('More')}</option>
              </select>
            </label>
            <p className="hint">{t('When you press play again the book goes back a few seconds, and more the longer it was paused. These settings are kept in this browser.')}</p>
          </div>
        )}
      </div>

      <audio
        ref={audio}
        src={api.bookTrackUrl(book.id, track)}
        preload="metadata"
        onLoadedMetadata={(e) => {
          const a = e.currentTarget
          setDuration(a.duration)
          a.playbackRate = speed
          if (resumeAt.current > 0) a.currentTime = resumePoint(resumeAt.current, a.duration)
          resumeAt.current = 0
        }}
        onTimeUpdate={(e) => {
          const a = e.currentTarget
          setTime(a.currentTime)
          // "End of chapter" inside one file: stop where the next chapter starts.
          if (sleep === -1 && chapter?.end !== undefined && a.currentTime >= chapter.end - 0.3 && !a.paused) {
            a.pause()
            a.currentTime = chapter.end
            setSleep(0)
          }
          if (!a.paused && Date.now() - lastSaved.current > 15_000) save()
        }}
        onSeeked={syncLockScreen}
        onPlay={() => {
          setPlaying(true)
          rewindAfterPause()
        }}
        onPause={(e) => {
          setPlaying(false)
          if (!e.currentTarget.ended) pausedAt.current = Date.now()
          save()
        }}
        onEnded={() => {
          if (sleep === -1) {
            setSleep(0)
            setPlaying(false)
            save()
            return
          }
          if (track < tracks.length - 1) goTrack(track + 1)
          else {
            setPlaying(false)
            save()
          }
        }}
      />
    </div>
  )
}
