// Usherr: the Read-to-Me page. Making an audiobook of an ebook in six
// colour-coded steps (book, narrator, cast, sound, words, make it), each a
// box of its own that folds to one line once it is done. Simple shows the
// three that matter; Full all six. "Guide me" lights one step at a time and
// moves on with Next (on by itself until the household has made an
// audiobook). Below the steps: what is being made, what was made, the engines
// and the voices (ReadToMeTab). /read-to-me picks a book first;
// /read-to-me/:bookId starts with it, ?remake=1 from how it was made.
import { useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { Link, useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { api, type Book } from '../api'
import { listenPath, openBookApp } from '../bookshelf/open'
import { useConfirm } from '../components/ConfirmProvider'
import Icon from '../components/Icon'
import InfoTip from '../components/InfoTip'
import { t } from '../i18n'
import CastPanel from './CastPanel'
import { minutesText } from './ReadToMe'
import ReadToMeTab from './ReadToMeTab'
import { jobActive, mixId, parseMixId, PAUSES, readToMeApi, readToMeOn, type CbModel, type RtvBook, type RtvCast, type RtvEngines, type RtvOptions, type RtvPreset, type RtvTune, type RtvVoice, type RtvVoices } from './readToMeApi'
import RtmCompare from './RtmCompare'
import RtmEarly from './RtmEarly'
import RtmLines from './RtmLines'
import RtmTextEdit from './RtmTextEdit'
import RtmTune from './RtmTune'
import SayList from './SayList'
import './readtome.css'
import './rtm-page.css'

const KEY_VOICE = 'usherr.readtome.voice'
const KEY_SPEED = 'usherr.readtome.speed'
const KEY_GUIDE = 'usherr.readtome.guide'
const KEY_SIMPLE = 'usherr.readtome.simple'

type Step = 'book' | 'voice' | 'cast' | 'sound' | 'words' | 'make'
const ALL: Step[] = ['book', 'voice', 'cast', 'sound', 'words', 'make']
const SIMPLE: Step[] = ['book', 'voice', 'make']
const NAME: Record<Step, () => string> = {
  book: () => t('Book'),
  voice: () => t('Narrator'),
  cast: () => t('Cast'),
  sound: () => t('Sound'),
  words: () => t('Words'),
  make: () => t('Make it'),
}
const GUIDE: Record<Step, [() => string, () => string]> = {
  book: [() => t('Pick the book'), () => t('Check the chapters you want read, then press Next.')],
  voice: [() => t('Choose a narrator'), () => t('Press the play button on a few voices to hear them, then pick one.')],
  cast: [() => t('Cast the characters'), () => t('Optional. Leave it off and the narrator reads every line.')],
  sound: [() => t('Shape the sound'), () => t('Leave these as they are for a natural read, or change one at a time and hear a sample.')],
  words: [() => t('Fix tricky words'), () => t('Optional. Add names the voice gets wrong.')],
  make: [() => t('Make it'), () => t('Check the estimate, then press Make the audiobook.')],
}

const keyOf = (v: Pick<RtvVoice, 'engine' | 'id'>) => `${v.engine}:${v.id}`
// A voice's own name ("Heart"), from its label ("Heart · American · woman").
const shortName = (v: RtvVoice) => v.label.split(' · ')[0] || v.name

function saved(key: string): string {
  try {
    return localStorage.getItem(key) ?? ''
  } catch {
    return ''
  }
}
function keep(key: string, value: string) {
  try {
    localStorage.setItem(key, value)
  } catch {
    // not kept
  }
}

export default function ReadToMePage() {
  const params = useParams()
  const bookId = Number(params.bookId) || 0
  const [on, setOn] = useState<boolean | null>(null)
  useEffect(() => {
    let live = true
    void readToMeOn().then((v) => live && setOn(v))
    return () => {
      live = false
    }
  }, [])
  // The guide's default: on until the household has made an audiobook.
  const [made, setMade] = useState<number | null>(null)
  useEffect(() => {
    readToMeApi
      .made()
      .then((m) => setMade((m.made ?? []).length))
      .catch(() => setMade(0))
  }, [])

  return (
    <div className="rp">
      <header className="rp-head">
        <div className="rp-title">
          <nav className="rp-crumbs" aria-label={t('Where you are')}>
            <Link to="/library?kind=ebook">{t('Ebooks')}</Link> / <span>{t('Read-to-Me')}</span>
          </nav>
          <h1>{t('Read to me')}</h1>
          <p>{t('Turn an ebook into an audiobook, one step at a time.')}</p>
        </div>
      </header>
      {on === false && (
        <p className="notice">
          {t('Read-to-Me is switched off or has no voice engine yet.')} <Link to="/settings/extras">{t('Settings > Extras > Read-to-Me')}</Link>
        </p>
      )}
      {on && made !== null && (bookId ? <Maker key={bookId} bookId={bookId} guideDefault={made === 0} /> : <BookPicker />)}
      <section className="rp-yours" id="rp-yours" aria-labelledby="rp-yours-h">
        <h2 id="rp-yours-h">{t('Your Read-to-Me')}</h2>
        <ReadToMeTab fold />
      </section>
    </div>
  )
}

// ---- Step 1 without a book: the ebooks that can be read --------------------------

function BookPicker() {
  const navigate = useNavigate()
  const [books, setBooks] = useState<Book[] | null>(null)
  const [q, setQ] = useState('')
  useEffect(() => {
    api
      .listBooks()
      .then(setBooks)
      .catch(() => setBooks([]))
  }, [])
  const readable = useMemo(() => (books ?? []).filter((b) => b.ebook?.status === 'downloaded' && b.ebook?.format === 'epub'), [books])
  const shown = q.trim() ? readable.filter((b) => `${b.title} ${b.author}`.toLowerCase().includes(q.trim().toLowerCase())) : readable
  return (
    <section className="rp-st rp-c-book cur" data-walk="rtm-pick" aria-labelledby="rp-pick-h">
      <div className="rp-st-head">
        <span className="rp-n">1</span>
        <h2 id="rp-pick-h">{t('Pick a book')}</h2>
        <span className="rp-sum">{books ? t('{n} ebooks can be read aloud', { n: readable.length }) : ''}</span>
      </div>
      <div className="rp-st-body">
        <label className="rp-search">
          <Icon name="search" size={16} />
          <input value={q} onChange={(e) => setQ(e.target.value)} placeholder={t('Find a book or an author')} aria-label={t('Find a book or an author')} />
        </label>
        {books === null ? (
          <div className="skeleton" style={{ height: 200 }} />
        ) : shown.length === 0 ? (
          <p className="hint">{readable.length === 0 ? t('No ebook can be read yet: it has to be downloaded, as an EPUB.') : t('No book matches that.')}</p>
        ) : (
          <ul className="rp-books">
            {shown.slice(0, 60).map((b) => (
              <li key={b.id}>
                <button type="button" onClick={() => navigate(`/read-to-me/${b.id}`)}>
                  <span className="rp-cover">{b.coverUrl ? <img src={b.coverUrl} alt="" loading="lazy" /> : <Icon name="book" size={28} />}</span>
                  <b>{b.title}</b>
                  <small>{b.author}</small>
                </button>
              </li>
            ))}
          </ul>
        )}
        {shown.length > 60 && <p className="hint">{t('{n} more: type part of the title to find yours.', { n: shown.length - 60 })}</p>}
      </div>
    </section>
  )
}

// ---- One step's box --------------------------------------------------------------

interface StationProps {
  id: Step
  n: number
  title: string
  hint: string
  optional?: boolean
  summary: string
  open: boolean
  state: '' | 'cur' | 'dim'
  onToggle: () => void
  onNext?: () => void
  nextLabel?: string
  children: ReactNode
}

function Station({ id, n, title, hint, optional, summary, open, state, onToggle, onNext, nextLabel, children }: StationProps) {
  const [why, setWhy] = useState(false)
  return (
    <section id={`rp-${id}`} data-walk={`rtm-${id}`} className={`rp-st rp-c-${id} ${state}`.trim()} aria-labelledby={`rp-${id}-h`}>
      <div className="rp-st-head">
        <span className="rp-n" aria-hidden="true">
          {n}
        </span>
        <h2 id={`rp-${id}-h`}>{title}</h2>
        <button type="button" className="rp-q" aria-expanded={why} onClick={() => setWhy((v) => !v)} aria-label={t('What is this step?')} title={t('What is this step?')}>
          ?
        </button>
        {optional && <span className="rp-opt">{t('Optional')}</span>}
        <span className="rp-sum">{open ? '' : summary}</span>
        <button type="button" className="rp-fold" aria-expanded={open} aria-controls={`rp-${id}-b`} onClick={onToggle}>
          {open ? t('Fold up') : t('Open')} <Icon name={open ? 'chevron-up' : 'chevron-down'} size={14} />
        </button>
      </div>
      {why && <p className="rp-why">{hint}</p>}
      {open && (
        <>
          <div className="rp-st-body" id={`rp-${id}-b`}>
            {children}
          </div>
          {onNext && (
            <div className="rp-st-foot">
              <button type="button" className="rp-next" onClick={onNext}>
                {nextLabel} <Icon name="open" size={15} />
              </button>
            </div>
          )}
        </>
      )}
    </section>
  )
}

function Fold({ label, tip, children }: { label: string; tip?: string; children: ReactNode }) {
  return (
    <details className="rp-more">
      <summary>
        {label}
        {tip && <InfoTip text={tip} />}
      </summary>
      <div className="rp-more-body">{children}</div>
    </details>
  )
}

// ---- The six steps for one book ----------------------------------------------------

function Maker({ bookId, guideDefault }: { bookId: number; guideDefault: boolean }) {
  const [search] = useSearchParams()
  const confirm = useConfirm()
  const audio = useRef<HTMLAudioElement>(null)

  // How the page is shown: the guide, Simple or Full, which step is open.
  const [guide, setGuide] = useState(() => (saved(KEY_GUIDE) ? saved(KEY_GUIDE) === 'on' : guideDefault))
  const [simple, setSimple] = useState(() => saved(KEY_SIMPLE) === '1')
  const steps = simple ? SIMPLE : ALL
  const [current, setCurrent] = useState<Step>('book')
  const [open, setOpen] = useState<Set<Step>>(() => new Set<Step>(['book']))
  const [done, setDone] = useState<Set<Step>>(() => new Set<Step>())
  const setGuideOn = (v: boolean) => {
    setGuide(v)
    keep(KEY_GUIDE, v ? 'on' : 'off')
    if (v) setOpen(new Set([current]))
  }
  const setSimpleOn = (v: boolean) => {
    setSimple(v)
    keep(KEY_SIMPLE, v ? '1' : '')
    if (v && !SIMPLE.includes(current)) go('voice')
  }
  const scrollTo = (s: Step) => window.setTimeout(() => document.getElementById(`rp-${s}`)?.scrollIntoView({ behavior: 'smooth', block: 'start' }), 60)
  const go = (s: Step) => {
    setCurrent(s)
    setOpen((o) => (guide ? new Set([s]) : new Set([...o, s])))
    scrollTo(s)
  }
  const toggle = (s: Step) => {
    if (open.has(s)) {
      setOpen((o) => {
        const n = new Set(o)
        n.delete(s)
        return n
      })
    } else go(s)
  }
  const nextOf = (s: Step): Step | null => {
    const i = steps.indexOf(s)
    return i >= 0 && i < steps.length - 1 ? steps[i + 1] : null
  }
  const finish = (s: Step) => {
    setDone((d) => new Set([...d, s]))
    const n = nextOf(s)
    if (!n) return
    setOpen((o) => {
      const m = new Set(o)
      m.delete(s)
      m.add(n)
      return guide ? new Set([n]) : m
    })
    setCurrent(n)
    scrollTo(n)
  }
  const stateOf = (s: Step): '' | 'cur' | 'dim' => (!guide ? '' : current === s ? 'cur' : 'dim')
  const nextLabel = (s: Step) => {
    const n = nextOf(s)
    return n ? t('Next: {step}', { step: NAME[n]().toLowerCase() }) : ''
  }

  // ---- What the book and the voices are (from the dialog Read-to-Me had) ----
  const [data, setData] = useState<RtvVoices | null>(null)
  const [error, setError] = useState('')
  const [picked, setPicked] = useState(saved(KEY_VOICE))
  const [speed, setSpeed] = useState(() => {
    const n = Number(saved(KEY_SPEED))
    return n >= 0.8 && n <= 1.3 ? n : 1
  })
  const [kind, setKind] = useState<'kokoro' | 'chatterbox'>('kokoro')
  const [hearing, setHearing] = useState('')
  const [previewing, setPreviewing] = useState(false)
  const previewUrl = useRef('')
  const abort = useRef<AbortController | null>(null)
  const [info, setInfo] = useState<RtvBook | null>(null)
  const [cover, setCover] = useState('')
  const [making, setMaking] = useState(false)
  const [remaking, setRemaking] = useState(search.get('remake') === '1')
  const prefilled = useRef(false)
  const [picks, setPicks] = useState<number[] | null>(null)
  const [intro, setIntro] = useState(true)
  const [titles, setTitles] = useState(true)
  const [pauses, setPauses] = useState(PAUSES)
  const [mix, setMix] = useState<{ second: string; weight: number } | null>(null)
  const [presets, setPresets] = useState<RtvPreset[] | null>(null)
  const [presetId, setPresetId] = useState(0)
  const [presetName, setPresetName] = useState('')
  const [presetDefault, setPresetDefault] = useState(false)
  const [machines, setMachines] = useState<RtvEngines | null>(null)
  const [cbModel, setCbModel] = useState<CbModel>('standard')
  const [cast, setCast] = useState<RtvCast | null>(null)
  const [tune, setTune] = useState<RtvTune | null>(null)
  const [waking, setWaking] = useState(false)
  const [heard, setHeard] = useState(false) // the player shows once something has played
  const [editing, setEditing] = useState<number | null>(null)
  const [checking, setChecking] = useState(false)
  const wasActive = useRef(false)
  const loadInfo = () =>
    readToMeApi
      .book(bookId)
      .then((b) => {
        setInfo(b)
        wasActive.current = jobActive(b.job)
      })
      .catch((e: Error) => setError(e.message))
  useEffect(() => {
    void loadInfo()
    api
      .getBook(bookId)
      .then((b) => setCover(b.coverUrl ?? ''))
      .catch(() => undefined)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [bookId])
  useEffect(() => {
    if (!jobActive(info?.job)) return
    const timer = window.setInterval(() => void loadInfo(), 3000)
    return () => window.clearInterval(timer)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [info?.job?.state, info?.job?.id])
  useEffect(() => {
    readToMeApi
      .voices()
      .then((d) => {
        setData(d)
        const all = d.voices ?? []
        setPicked((cur) => {
          const k = all.some((v) => keyOf(v) === cur) ? cur : all[0] ? keyOf(all[0]) : ''
          const v = all.find((x) => keyOf(x) === k)
          if (v) setKind(v.engine)
          return k
        })
      })
      .catch((e: Error) => setError(e.message))
    readToMeApi
      .presets()
      .then((d) => setPresets(d.presets ?? []))
      .catch(() => setPresets([]))
    readToMeApi
      .engines()
      .then((d) => {
        setMachines(d)
        setCbModel(d.default)
      })
      .catch(() => undefined)
    return () => {
      abort.current?.abort()
      if (previewUrl.current) URL.revokeObjectURL(previewUrl.current)
    }
  }, [])

  const apply = (engine: string, voiceId: string, sp: number, o: RtvOptions, chapters: boolean) => {
    const m = engine === 'kokoro' ? parseMixId(voiceId) : null
    const k = keyOf({ engine: engine as RtvVoice['engine'], id: m ? m.main : voiceId })
    if ((data?.voices ?? []).some((v) => keyOf(v) === k)) {
      setPicked(k)
      setKind(engine as RtvVoice['engine'])
    }
    setMix(m ? { second: m.second, weight: m.weight } : null)
    if (sp >= 0.8 && sp <= 1.3) setSpeed(sp)
    if (chapters) {
      setPicks(o.chapters?.length ? o.chapters : null)
      setCast(o.cast ?? null)
    }
    setIntro(!o.noIntro)
    setTitles(!o.noTitles)
    setPauses({ paraMs: o.paraMs || PAUSES.paraMs, sceneMs: o.sceneMs || PAUSES.sceneMs, chapterMs: o.chapterMs || PAUSES.chapterMs })
    if (o.cbModel) setCbModel(o.cbModel)
    setTune(o.tune ?? null)
  }
  const madeOne = info?.made
  useEffect(() => {
    if (!data || !info || presets === null || prefilled.current) return
    prefilled.current = true
    if (remaking && madeOne) {
      apply(madeOne.engine, madeOne.voice, madeOne.speed, madeOne.options ?? {}, true)
      return
    }
    const d = presets.find((p) => p.default)
    if (d) {
      setPresetId(d.id)
      apply(d.engine, d.voice, d.speed, d.options, false)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [remaking, madeOne, data, info, presets])

  const chapterList = info?.chapterList ?? []
  const options: RtvOptions = { chapters: picks ?? undefined, noIntro: !intro, noTitles: !titles, ...pauses, cbModel: machines?.runner ? cbModel : undefined, cast: cast ?? undefined, tune: tune ?? undefined }
  const togglePick = (i: number) => {
    const cur = picks ?? chapterList.map((_, j) => j)
    const next = cur.includes(i) ? cur.filter((j) => j !== i) : [...cur, i].sort((a, b) => a - b)
    setPicks(next.length === chapterList.length ? null : next)
  }
  const pickedWords = picks ? picks.reduce((n, i) => n + (chapterList[i]?.words ?? 0), 0) : (info?.words ?? 0)
  const voices = data?.voices ?? []
  const engines = data?.engines ?? []
  const kokoro = voices.filter((v) => v.engine === 'kokoro')
  const chatterbox = voices.filter((v) => v.engine === 'chatterbox')
  const chatterboxOn = engines.some((e) => e.name === 'chatterbox' && e.on)
  const voice = voices.find((v) => keyOf(v) === picked)
  const mixed = voice?.engine === 'kokoro' && mix && mix.second && mix.second !== voice.id ? mix : null
  const voiceId = voice ? (mixed ? mixId(voice.id, mixed.second, mixed.weight) : voice.id) : ''
  const nameOf = (id: string) => {
    const v = voices.find((x) => x.engine === 'kokoro' && x.id === id)
    return v ? shortName(v) : id
  }
  const choosePreset = (id: number) => {
    setPresetId(id)
    const p = presets?.find((x) => x.id === id)
    if (p) apply(p.engine, p.voice, p.speed, p.options, false)
  }
  const savePreset = () => {
    if (!voice || !presetName.trim()) return
    setError('')
    const name = presetName.trim().replace(/\s+/g, ' ')
    readToMeApi
      .savePreset({ name, engine: voice.engine, voice: voiceId, speed, options: { ...options, chapters: undefined, cast: undefined }, default: presetDefault })
      .then((d) => {
        setPresets(d.presets)
        setPresetId(d.presets.find((p) => p.name.toLowerCase() === name.toLowerCase())?.id ?? 0)
        setPresetName('')
      })
      .catch((e: Error) => setError(e.message))
  }
  const usesChatterbox = voice?.engine === 'chatterbox' || Object.values({ ...cast?.people, d: cast?.dialogue, m: cast?.men, w: cast?.women }).some((r) => r?.engine === 'chatterbox')
  const cold = !!machines?.runner && machines.model !== cbModel
  const startNote = cold ? t('Starting Chatterbox ({model}) first: about a minute the first time.', { model: cbModel === 'turbo' ? 'Turbo' : t('Standard') }) : ''
  const refreshEngines = () =>
    readToMeApi
      .engines()
      .then(setMachines)
      .catch(() => undefined)
  const choose = (v: RtvVoice) => {
    setPicked(keyOf(v))
    keep(KEY_VOICE, keyOf(v))
  }
  const play = (src: string) => {
    const a = audio.current
    if (!a) return
    setHeard(true)
    a.src = src
    void a.play().catch(() => undefined)
  }
  const hear = (v: Pick<RtvVoice, 'engine' | 'id'>) => {
    setError('')
    setHearing(keyOf(v))
    const a = audio.current
    if (!a) return
    if (v.engine === 'chatterbox' && cold) setWaking(true)
    const finished = () => {
      setHearing('')
      if (v.engine === 'chatterbox') {
        setWaking(false)
        void refreshEngines()
      }
      a.removeEventListener('playing', finished)
      a.removeEventListener('error', failed)
    }
    const failed = () => {
      finished()
      setError(t("That sample couldn't be made. The voice engine may be busy."))
    }
    a.addEventListener('playing', finished)
    a.addEventListener('error', failed)
    play(readToMeApi.sampleUrl(v, v.engine === 'chatterbox' && machines?.runner ? cbModel : undefined))
  }
  const preview = () => {
    if (!voice) return
    abort.current?.abort()
    const c = new AbortController()
    abort.current = c
    setError('')
    setPreviewing(true)
    if (usesChatterbox && cold) setWaking(true)
    readToMeApi
      .preview({ bookId, engine: voice.engine, voice: voiceId, speed, ...options }, c.signal)
      .then((url) => {
        if (previewUrl.current) URL.revokeObjectURL(previewUrl.current)
        previewUrl.current = url
        play(url)
        if (usesChatterbox) void refreshEngines()
      })
      .catch((e: Error) => e.name !== 'AbortError' && setError(e.message))
      .finally(() => {
        setPreviewing(false)
        setWaking(false)
      })
  }
  const job = info?.job
  const active = jobActive(job)
  const make = () => {
    if (!voice) return
    setMaking(true)
    setError('')
    readToMeApi
      .make({ bookId, engine: voice.engine, voice: voiceId, speed, remake: !!madeOne && remaking, ...options })
      .then(() => {
        setRemaking(false)
        return loadInfo()
      })
      .catch((e: Error) => setError(e.message))
      .finally(() => setMaking(false))
  }
  const cancel = () => {
    if (!job) return
    readToMeApi
      .cancel(job.id)
      .then(() => loadInfo())
      .catch((e: Error) => setError(e.message))
  }
  const deleteMade = async () => {
    if (!madeOne) return
    const ok = await confirm({
      title: t('Delete the audiobook of {title}?', { title: info?.title ?? '' }),
      body: <p>{t('The audiobook Read-to-Me made is deleted from your Audiobooks library. The ebook stays as it is, and you can make it again any time.')}</p>,
      confirmLabel: t('Delete'),
      danger: true,
    })
    if (!ok) return
    setError('')
    readToMeApi
      .deleteMade(madeOne.id)
      .then(() => loadInfo())
      .catch((e: Error) => setError(e.message))
  }
  const madeFrom = madeOne ? voices.find((v) => v.engine === madeOne.engine && v.id === madeOne.voice) : undefined
  const madeVoice = madeFrom ? shortName(madeFrom) : (madeOne?.voice ?? '')
  const percent = job && job.piecesTotal > 0 ? Math.min(100, Math.round((job.piecesDone / job.piecesTotal) * 100)) : 0
  const audioMinutes = pickedWords / 150
  const makeMinutes = voice?.engine === 'chatterbox' ? audioMinutes * (cbModel === 'turbo' ? 0.5 : 1.2) : audioMinutes / 12
  const canStart = !active && !!info?.canMake && (!info.hasAudiobook || (!!madeOne?.canChange && remaking))
  const speedText = `${speed.toFixed(2).replace(/0$/, '')}×`

  // ---- The step boxes' one-line summaries ----
  const summary: Record<Step, string> = {
    book: info ? `${info.title} · ${picks ? t('{n} of {m} chapters', { n: picks.length, m: chapterList.length }) : t('the whole book')}` : '',
    voice: voice ? (mixed ? `${shortName(voice)} + ${nameOf(mixed.second)}` : voice.label) : t('No voice picked yet'),
    cast: cast ? t('Its own voices for the dialogue') : t('The narrator reads everything'),
    sound: `${speedText}${usesChatterbox && tune ? ' · ' + t('Chatterbox tuned') : ''}`,
    words: t('Names and words said your way'),
    make: active ? t('Being made: {n}%', { n: percent }) : madeOne && !remaking ? t('Already made') : audioMinutes > 0 ? t('About {audio} of audio', { audio: minutesText(audioMinutes) }) : '',
  }
  const station = (s: Step, optional = false) => ({
    id: s,
    n: steps.indexOf(s) + 1,
    title: NAME[s](),
    summary: summary[s],
    optional,
    open: open.has(s),
    state: stateOf(s),
    onToggle: () => toggle(s),
    onNext: nextOf(s) ? () => finish(s) : undefined,
    nextLabel: nextLabel(s),
  })

  const voiceCard = (v: RtvVoice) => {
    const k = keyOf(v)
    return (
      <li key={k} className={picked === k ? 'on' : ''}>
        <button type="button" className="rp-play" disabled={hearing === k} onClick={() => hear(v)} aria-label={t('Hear {name}', { name: v.label })} title={t('Hear a sample')}>
          <Icon name={hearing === k ? 'refresh' : 'play'} size={15} />
        </button>
        <button type="button" role="radio" aria-checked={picked === k} className="rp-pick" onClick={() => choose(v)}>
          <b>{shortName(v)}</b>
          <small>{[...v.label.split(' · ').slice(1), v.grade, v.note].filter(Boolean).join(' · ') || (v.engine === 'chatterbox' ? t('Expressive') : t('Everyday'))}</small>
        </button>
      </li>
    )
  }
  const shownVoices = kind === 'chatterbox' ? chatterbox : kokoro

  return (
    <div className="rp-maker">
      <div className="rp-controls">
        <div className="seg" data-walk="rtm-mode" role="radiogroup" aria-label={t('How much to show')}>
          <button type="button" role="radio" aria-checked={simple} className={simple ? 'active' : ''} onClick={() => setSimpleOn(true)}>
            {t('Simple')}
          </button>
          <button type="button" role="radio" aria-checked={!simple} className={!simple ? 'active' : ''} onClick={() => setSimpleOn(false)}>
            {t('Full')}
          </button>
        </div>
        <InfoTip text={t('Simple shows the three steps that matter: the book, the narrator and making it, with sensible settings for the rest. Full shows all six.')} />
        <button type="button" data-walk="rtm-guide" className={`rp-guide${guide ? ' on' : ''}`} role="switch" aria-checked={guide} onClick={() => setGuideOn(!guide)}>
          <span className="rp-track" aria-hidden="true">
            <span />
          </span>
          {t('Guide me')}
        </button>
      </div>

      <ol className="rp-dots" aria-label={t('Steps')}>
        {steps.map((s, i) => (
          <li key={s} className={`rp-c-${s}`}>
            <button type="button" className={`${current === s ? 'cur' : ''} ${done.has(s) ? 'done' : ''}`.trim()} aria-current={current === s ? 'step' : undefined} onClick={() => go(s)}>
              <span className="rp-n">{done.has(s) ? <Icon name="check" size={14} /> : i + 1}</span>
              {NAME[s]()}
            </button>
          </li>
        ))}
      </ol>

      {guide && (
        <div className={`rp-banner rp-c-${current}`} role="status">
          <span className="rp-banner-step">{t('Step {n} of {m}', { n: steps.indexOf(current) + 1, m: steps.length })}</span>
          <strong>{GUIDE[current][0]()}</strong>
          <span className="rp-banner-text">{GUIDE[current][1]()}</span>
          <button type="button" className="btn-sm" onClick={() => setGuideOn(false)}>
            {t('Turn the guide off')}
          </button>
        </div>
      )}

      {error && <p className="error-text">{error}</p>}
      {info?.problem && <p className="error-text">{info.problem}</p>}

      {/* 1 Book */}
      <Station {...station('book')} hint={t('Which chapters are read. The contents, the copyright page and the acknowledgments are left out by themselves. Text lets you correct a chapter before it is read.')}>
        <div className="rp-book">
          <span className="rp-cover big">{cover ? <img src={cover} alt="" /> : <Icon name="book" size={36} />}</span>
          <div className="rp-book-info">
            <div className="rp-book-title">{info?.title ?? '…'}</div>
            <div className="hint">
              {info?.author}
              {info?.chapters ? ' · ' + t('{n} chapters', { n: info.chapters }) : ''}
              {info?.words ? ' · ' + t('{n} words', { n: info.words.toLocaleString() }) : ''}
            </div>
            <div className="rp-links">
              <Link to={`/book/${bookId}`}>{t("The book's page")}</Link>
              <Link to="/read-to-me">{t('Choose another book')}</Link>
            </div>
            <div className="rp-field rp-checks">
              <label>
                <input type="checkbox" checked={intro} onChange={(e) => setIntro(e.target.checked)} /> {t('Start with the title and the author')}
              </label>
              <label>
                <input type="checkbox" checked={titles} onChange={(e) => setTitles(e.target.checked)} /> {t("Read each chapter's title")}
              </label>
            </div>
          </div>
        </div>
        {chapterList.length > 1 && (
          <div className="rp-field">
            <div className="rp-field-head">
              <strong>{t('Chapters to read')}</strong>
              <InfoTip text={t('Untick the chapters you do not want. With all of them ticked the whole book is read.')} />
              <span className="spacer" />
              <button type="button" className="btn-sm" onClick={() => setPicks(null)} disabled={!picks}>
                {t('All')}
              </button>
            </div>
            <ul className="rp-chapters">
              {chapterList.map((c, i) => (
                <li key={i}>
                  <label>
                    <input type="checkbox" checked={!picks || picks.includes(i)} onChange={() => togglePick(i)} disabled={!!picks && picks.length === 1 && picks[0] === i} />
                    <span>
                      {c.title}
                      {c.edited && <small className="hint"> · {t('your text')}</small>}
                    </span>
                    <small className="hint">{t('{n} words', { n: c.words.toLocaleString() })}</small>
                    <button type="button" className="btn-sm" aria-expanded={editing === i} onClick={(e) => (e.preventDefault(), setEditing(editing === i ? null : i))}>
                      {t('Text')}
                    </button>
                  </label>
                  {editing === i && (
                    <RtmTextEdit
                      bookId={bookId}
                      chapter={i}
                      onDone={(edited) => {
                        setEditing(null)
                        if (edited !== null) void loadInfo()
                      }}
                    />
                  )}
                </li>
              ))}
            </ul>
          </div>
        )}
      </Station>

      {/* 2 Narrator */}
      <Station {...station('voice')} hint={t('The voice that reads the book. Everyday voices (Kokoro) are quick to make. Expressive voices (Chatterbox) sound more human but take longer; copied voices are among them. Press play on any card to hear it.')}>
        {!!presets?.length && (
          <label className="rp-field rp-inline">
            <span className="rp-label">{t('Preset')}</span>
            <select value={presetId} onChange={(e) => choosePreset(Number(e.target.value))}>
              <option value={0}>{t("None, I'll choose")}</option>
              {presets.map((p) => (
                <option key={p.id} value={p.id}>
                  {p.default ? t('{name} (default)', { name: p.name }) : p.name}
                </option>
              ))}
            </select>
            <InfoTip text={t('A preset fills in the narrator, the speed and how it sounds in one go. Save your own in the last step.')} />
          </label>
        )}
        {!data && !error && <p className="hint">{t('Getting the voices…')}</p>}
        {data && voices.length === 0 && (
          <p className="error-text">
            {engines.find((e) => e.on && e.problem)?.problem ?? t('No voice engine is set up.')} <Link to="/settings/extras">{t('Settings > Extras > Read-to-Me')}</Link>
          </p>
        )}
        {voices.length > 0 && (
          <div className="seg" role="tablist" aria-label={t('Kinds of voice')}>
            <button type="button" role="tab" aria-selected={kind === 'kokoro'} className={kind === 'kokoro' ? 'active' : ''} onClick={() => setKind('kokoro')} disabled={kokoro.length === 0}>
              {t('Everyday')} <small>{kokoro.length}</small>
            </button>
            {chatterboxOn && (
              <button type="button" role="tab" aria-selected={kind === 'chatterbox'} className={kind === 'chatterbox' ? 'active' : ''} onClick={() => setKind('chatterbox')}>
                {t('Expressive')} <small>{chatterbox.length}</small>
              </button>
            )}
          </div>
        )}
        <p className="hint">
          {kind === 'chatterbox'
            ? t('Chatterbox: more human, takes longer to make. Copied voices are made from a short recording.')
            : t('Kokoro: quick to make, faster than real time.')}
          {kind === 'chatterbox' && engines.some((e) => e.name === 'chatterbox' && e.onDemand) && ' ' + t('Chatterbox is started by itself when it is needed (about a minute), and stopped again afterwards.')}
        </p>
        {kind === 'chatterbox' && chatterbox.length === 0 && <p className="hint">{engines.find((e) => e.name === 'chatterbox')?.problem}</p>}
        <ul className="rp-voices" role="radiogroup" aria-label={t('Voices')}>
          {shownVoices.map(voiceCard)}
        </ul>
        {kind === 'chatterbox' && machines?.runner && (
          <Fold label={t('Chatterbox model')} tip={t('Standard has the best sound. Turbo is about twice as fast to make and a little flatter.')}>
            <div className="rp-checks">
              <label>
                <input type="radio" name="rp-cbmodel" checked={cbModel === 'standard'} onChange={() => setCbModel('standard')} /> {t('Standard (the best sound)')}
              </label>
              <label>
                <input type="radio" name="rp-cbmodel" checked={cbModel === 'turbo'} onChange={() => setCbModel('turbo')} /> {t('Turbo (about twice as fast, a little flatter)')}
              </label>
            </div>
          </Fold>
        )}
        {voice?.engine === 'kokoro' && kokoro.length > 1 && (
          <Fold label={t('Mix in a second voice')} tip={t('Blends two everyday voices into a new one. Move the slider to choose how much of each.')}>
            <label className="rp-checks">
              <span>
                <input type="checkbox" checked={!!mixed} onChange={(e) => setMix(e.target.checked ? { second: kokoro.find((v) => v.id !== voice.id)?.id ?? '', weight: 7 } : null)} /> {t('Blend {name} with another voice', { name: shortName(voice) })}
              </span>
            </label>
            {mixed && (
              <div className="rp-mix">
                <select value={mixed.second} onChange={(e) => setMix({ ...mixed, second: e.target.value })} aria-label={t('The second voice')}>
                  {kokoro
                    .filter((v) => v.id !== voice.id)
                    .map((v) => (
                      <option key={v.id} value={v.id}>
                        {v.label}
                      </option>
                    ))}
                </select>
                <label className="rp-slider">
                  <span>{t('{a} {x}% + {b} {y}%', { a: shortName(voice), x: mixed.weight * 10, b: nameOf(mixed.second), y: 100 - mixed.weight * 10 })}</span>
                  <input type="range" min={1} max={9} step={1} value={mixed.weight} onChange={(e) => setMix({ ...mixed, weight: Number(e.target.value) })} />
                </label>
                <button type="button" className="btn-sm btn-with-icon" disabled={hearing === `kokoro:${voiceId}`} onClick={() => hear({ engine: 'kokoro', id: voiceId })}>
                  <Icon name="play" size={14} /> {t('Hear the mix')}
                </button>
              </div>
            )}
          </Fold>
        )}
        <p className="hint">
          {t('More voices: copy one from a short recording in')} <a href="#rp-yours">{t('Your Read-to-Me > Voices')}</a>.
        </p>
      </Station>

      {/* 3 Cast */}
      {!simple && info?.canMake && voices.length > 0 && (
        <Station {...station('cast', true)} hint={t('A full cast gives the characters’ spoken lines their own voices, while the narrator reads the rest. Usherr finds who says what; you can change any of it.')}>
          <CastPanel bookId={bookId} voices={voices} cast={cast} onChange={setCast} />
        </Station>
      )}

      {/* 4 Sound */}
      {!simple && (
        <Station {...station('sound')} hint={t('How the reading sounds. Small changes go a long way: change one thing at a time and press Hear a sample at the bottom.')}>
          <div className="rp-grid">
            <label className="rp-field rp-slider">
              <span className="rp-field-head">
                <strong>{t('Speed')}</strong>
                <InfoTip text={t('How fast the narrator reads. 1× is a normal reading pace.')} />
                <span className="spacer" />
                <b className="rp-val">{speedText}</b>
              </span>
              <input
                type="range"
                min={0.8}
                max={1.3}
                step={0.05}
                value={speed}
                onChange={(e) => {
                  const n = Number(e.target.value)
                  setSpeed(n)
                  keep(KEY_SPEED, String(n))
                }}
              />
            </label>
          </div>
          {usesChatterbox && (
            <div className="rp-field">
              <RtmTune tune={tune} onChange={setTune} model={machines?.runner ? cbModel : undefined} />
            </div>
          )}
          <Fold label={t('Pauses')} tip={t('How long the narrator stops after a paragraph, at a scene break and between chapters.')}>
            {(
              [
                ['paraMs', t('After a paragraph'), 200, 2000],
                ['sceneMs', t('At a scene break'), 500, 4000],
                ['chapterMs', t('Between chapters'), 1000, 6000],
              ] as const
            ).map(([key, label, lo, hi]) => (
              <label key={key} className="rp-field rp-slider">
                <span className="rp-field-head">
                  <strong>{label}</strong>
                  <span className="spacer" />
                  <b className="rp-val">{t('{n} s', { n: (pauses[key] / 1000).toFixed(1) })}</b>
                </span>
                <input type="range" min={lo} max={hi} step={100} value={pauses[key]} onChange={(e) => setPauses({ ...pauses, [key]: Number(e.target.value) })} />
              </label>
            ))}
            <button type="button" className="btn-sm" onClick={() => setPauses(PAUSES)} disabled={JSON.stringify(pauses) === JSON.stringify(PAUSES)}>
              {t('The usual pauses')}
            </button>
          </Fold>
          {info?.canMake && voice && (
            <Fold label={t('Compare two ways of reading')} tip={t('Hear the same passage read two ways, one after the other, and keep the one you like.')}>
              <RtmCompare
                a={{ bookId, engine: voice.engine, voice: voiceId, speed, ...options }}
                presets={presets ?? []}
                takes={usesChatterbox}
                startNote={startNote}
                onSpoke={() => usesChatterbox && void refreshEngines()}
                onUseSeed={(seed) => setTune({ ...tune, seed })}
                onUsePreset={choosePreset}
              />
            </Fold>
          )}
        </Station>
      )}

      {/* 5 Words */}
      {!simple && info?.canMake && (
        <Station {...station('words', true)} hint={t('Names and made-up words a voice gets wrong: write them the way they sound. Numbers, titles like Dr. and dashes are already read the way a narrator would.')}>
          <div className="rp-field">
            <div className="rp-field-head">
              <strong>{t('In this book')}</strong>
            </div>
            <SayList bookId={bookId} />
          </div>
          <Fold label={t('In every book')} tip={t('Words said your way in every book Read-to-Me makes.')}>
            <SayList />
          </Fold>
        </Station>
      )}

      {/* 6 Make it */}
      <Station {...station('make')} hint={t('Making an audiobook keeps the computer busy for a while, and it waits while something is playing. You get a notice when it is done, and it goes in your Audiobooks library.')}>
        {active && job ? (
          <div className="rp-field rp-job" role="status">
            <strong>
              {job.state === 'queued'
                ? t('Waiting its turn…')
                : job.options?.redo
                  ? job.options.redo.line < 0
                    ? t('Reading chapter {n} again', { n: job.options.redo.chapter + 1 })
                    : t('Reading a line again')
                  : t('Making the audiobook: chapter {n} of {m}', { n: job.chapter, m: job.chapters })}
            </strong>
            <div className="rtm-bar" aria-hidden="true">
              <span style={{ width: `${percent}%` }} />
            </div>
            <small className="hint">
              {percent}%{job.etaSec ? ' · ' + t('about {time} left', { time: minutesText(job.etaSec / 60) }) : ''}
              {job.note ? ' · ' + job.note : ''}
            </small>
            {!job.options?.redo && <RtmEarly job={job} />}
            <div>
              <button className="btn-sm" onClick={cancel}>
                {t('Stop making it')}
              </button>{' '}
              <small className="hint">{t('You can leave this page: it carries on, and the audiobook appears in your Audiobooks library when it is done.')}</small>
            </div>
          </div>
        ) : (
          <>
            {madeOne && (
              <div className="rp-field rp-made">
                <span>
                  <strong>{t('Already made: {voice}, {date}', { voice: madeVoice, date: madeOne.doneMs ? new Date(madeOne.doneMs).toLocaleDateString() : '' })}</strong>{' '}
                  <small className="hint">{t('It is in your Audiobooks library, marked as read by a computer voice.')}</small>
                </span>
                <span className="rtm-made-actions">
                  <button className="btn-sm btn-with-icon" onClick={() => openBookApp(listenPath(bookId))}>
                    <Icon name="play" size={14} /> {t('Play')}
                  </button>
                  {madeOne.kept && (
                    <button className="btn-sm" aria-expanded={checking} onClick={() => setChecking((v) => !v)}>
                      {madeOne.flagged ? t('Lines to check ({n})', { n: madeOne.flagged }) : t('Lines')}
                    </button>
                  )}
                  {madeOne.canChange && (
                    <>
                      <button className="btn-sm btn-with-icon" aria-pressed={remaking} onClick={() => setRemaking((v) => !v)}>
                        <Icon name="refresh" size={14} /> {t('Remake')}
                      </button>
                      <button className="btn-sm btn-with-icon danger" onClick={() => void deleteMade()}>
                        <Icon name="trash" size={14} /> {t('Delete')}
                      </button>
                    </>
                  )}
                </span>
                {remaking && <small className="hint">{t('Pick the voice and the rest above, then Remake the audiobook. The one you have stays playable until the new one is finished, and then takes its place.')}</small>}
                {checking && <RtmLines made={madeOne} busy={false} onQueued={() => void loadInfo()} />}
              </div>
            )}
            {job?.state === 'failed' && <p className="error-text">{t("The last try didn't finish: {why}", { why: job.error ?? '' })}</p>}
            {info?.hasAudiobook && !madeOne && <p className="hint">{t('This book already has an audiobook.')}</p>}
            {audioMinutes > 0 && (
              <div className="rp-facts">
                <span>
                  <small>{t('Audio')}</small>
                  {t('about {time}', { time: minutesText(audioMinutes) })}
                </span>
                <span>
                  <small>{t('Time to make')}</small>
                  {t('about {time}', { time: minutesText(makeMinutes) })}
                  <InfoTip text={t('A guess from the number of words and the engine. Chatterbox Standard takes about as long as the audio; Turbo half that; Kokoro a few minutes for a whole book.')} />
                </span>
                <span>
                  <small>{t('Engine')}</small>
                  {voice?.engine === 'chatterbox' ? `Chatterbox${machines?.runner ? ' ' + (cbModel === 'turbo' ? 'Turbo' : t('Standard')) : ''}` : 'Kokoro'}
                </span>
              </div>
            )}
          </>
        )}
        {voice && (
          <Fold label={t('Save these settings as a preset')} tip={t('The voice (or the mix), the speed, the pauses, how Chatterbox reads and whether the titles are read. The chapters and the cast stay with the book.')}>
            <div className="rp-inline">
              <input value={presetName} onChange={(e) => setPresetName(e.target.value)} placeholder={t('Name, like Warm narrator')} aria-label={t('Preset name')} maxLength={40} />
              <label className="rp-checks">
                <span>
                  <input type="checkbox" checked={presetDefault} onChange={(e) => setPresetDefault(e.target.checked)} /> {t('Use it by default')}
                </span>
              </label>
              <button type="button" className="btn-sm primary" onClick={savePreset} disabled={!presetName.trim()}>
                {t('Save preset')}
              </button>
            </div>
          </Fold>
        )}
      </Station>

      <audio ref={audio} controls className="rp-audio" hidden={!heard} />
      {waking && startNote && (
        <p className="hint" role="status">
          {startNote}
        </p>
      )}

      <div className="rp-bar">
        <div className="rp-bar-in">
          <span className="rp-chip rp-c-book">{info?.title ?? t('Book')}</span>
          <span className="rp-chip rp-c-voice">{voice ? (mixed ? `${shortName(voice)} + ${nameOf(mixed.second)}` : shortName(voice)) : t('No voice')}</span>
          {!simple && <span className="rp-chip rp-c-sound">{speedText}</span>}
          {active && <span className="rp-chip rp-c-make">{t('Being made: {n}%', { n: percent })}</span>}
          <span className="spacer" />
          <button type="button" className="btn-with-icon" data-walk="rtm-hear" onClick={preview} disabled={!voice || previewing}>
            <Icon name="play" size={16} /> {previewing ? (waking ? t('Starting Chatterbox…') : t('Making a sample…')) : t('Hear a sample')}
          </button>
          {canStart && (
            <button type="button" className="primary btn-with-icon" data-walk="rtm-make" onClick={make} disabled={!voice || making}>
              <Icon name={madeOne ? 'refresh' : 'headphones'} size={16} /> {making ? t('Starting…') : madeOne ? t('Remake the audiobook') : t('Make the audiobook')}
            </button>
          )}
          {!canStart && madeOne && !active && (
            <button type="button" className="btn-with-icon" onClick={() => openBookApp(listenPath(bookId))}>
              <Icon name="headphones" size={16} /> {t('Listen')}
            </button>
          )}
        </div>
      </div>
    </div>
  )
}

