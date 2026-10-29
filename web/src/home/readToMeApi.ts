// Usherr: Read-to-Me, what the pages ask the server (home_readtome.go).
import { t } from '../i18n'

export interface RtvVoice {
  engine: 'kokoro' | 'chatterbox'
  id: string
  name: string
  label: string
  note?: string
  grade?: string
}

export interface RtvEngineState {
  name: 'kokoro' | 'chatterbox'
  on: boolean
  ready: boolean
  problem?: string
  onDemand?: boolean // stopped, but started by Usherr when needed (a runner is set up)
}

export type CbModel = 'standard' | 'turbo'

// The voice engines' containers, through the runner.
export interface RtvEngines {
  runner: boolean
  states?: Record<string, 'running' | 'stopped' | 'missing' | 'unknown'>
  model: '' | CbModel // the Chatterbox model answering now
  default: CbModel
  started?: string[]
  problem?: string
}

export interface RtvVoices {
  engines: RtvEngineState[]
  voices: RtvVoice[]
}

export interface RtvJob {
  id: number
  bookId: number
  engine: string
  voice: string
  speed: number
  state: 'queued' | 'running' | 'waiting' | 'done' | 'failed' | 'cancelled'
  chapter: number
  chapters: number
  piecesDone: number
  piecesTotal: number
  note?: string
  error?: string
  outputPath?: string
  title?: string
  author?: string
  etaSec?: number
  readyChapters?: number // chapters finished so far, to listen to while the rest is made
  remake?: boolean
  options?: RtvOptions
  doneMs?: number
  createdMs?: number
}

// What is read of a book: the chapters (from 0; none: all), the title at the start, the chapters' titles.
export interface RtvOptions {
  chapters?: number[]
  noIntro?: boolean
  noTitles?: boolean
  paraMs?: number
  sceneMs?: number
  chapterMs?: number
  cbModel?: CbModel
  cast?: RtvCast
  tune?: RtvTune
  redo?: RtvRedo // a job that reads again a line or a chapter of a made audiobook
}

// Reading again a line (or, with line -1, a chapter) of a made audiobook.
export interface RtvRedo {
  chapter: number
  line: number
  text?: string
  seed?: number
}

// A line of a made audiobook, where it is, and why it may be read badly.
export interface RtvLine {
  index: number
  text: string
  engine: string
  voice: string
  startMs: number
  endMs: number
  flag?: string
}
export interface RtvMadeLines {
  kept: boolean
  chapters: { title: string; lines: number; flagged: number }[]
  flagged: (RtvLine & { chapter: number })[]
  chapter: number
  lines: RtvLine[]
}

// How Chatterbox reads (home_readtome_voices.go); a field left out is its usual way.
export interface RtvTune {
  exaggeration?: number // emotion, 0.25 to 2 (standard only)
  cfg?: number // pacing, 0 to 1 (standard only; lower is slower)
  temperature?: number // variety, 0.05 to 2
  seed?: number // the same take every time
}
export const TUNE_USUAL = { exaggeration: 0.5, cfg: 0.5, temperature: 0.8 }
export const newSeed = () => 1 + Math.floor(Math.random() * 2_000_000_000)

// A voice copied from a recording, kept by Usherr.
export interface RtvCopiedVoice {
  name: string
  seconds: number
  createdMs: number
  canChange: boolean
}

// Full cast: other voices for the dialogue (home_readtome_cast.go).
export interface RtvVoiceRef {
  engine: string
  voice: string
}
export interface RtvCast {
  dialogue?: RtvVoiceRef // lines nobody known says, and characters without a voice
  men?: RtvVoiceRef
  women?: RtvVoiceRef
  people?: Record<string, RtvVoiceRef>
  ai?: boolean
}
export interface RtvCastInfo {
  people: { name: string; lines: number; gender?: 'm' | 'f' }[]
  quotes: number
  known: number
  chapters: { title: string; lines: number; unknown: number; ai: boolean }[]
  aiOn: boolean
  aiRunning: string
  aiError: string
}
export interface RtvCastLine {
  para: number
  index: number
  text: string
  speaker: string
  how: '' | 'tag' | 'beat' | 'ai' | 'turn' | 'you'
}

// The usual pauses, in milliseconds (home_readtome_job.go).
export const PAUSES = { paraMs: 600, sceneMs: 1500, chapterMs: 2500 }

// A named way of reading, shared by the household.
export interface RtvPreset {
  id: number
  name: string
  engine: 'kokoro' | 'chatterbox'
  voice: string
  speed: number
  options: RtvOptions
  default: boolean
}

// A Kokoro mix: "af_heart(7)+bm_george(3)" is af_heart at 70% with bm_george.
export const mixId = (main: string, second: string, weight: number) => `${main}(${weight})+${second}(${10 - weight})`
export function parseMixId(voice: string): { main: string; second: string; weight: number } | null {
  const m = /^([a-z]{2}_[a-z0-9]+)(?:\(([1-9])\))?\+([a-z]{2}_[a-z0-9]+)(?:\(([1-9])\))?$/.exec(voice)
  if (!m) return null
  const a = Number(m[2] || 1)
  const b = Number(m[4] || 1)
  return { main: m[1], second: m[3], weight: Math.min(9, Math.max(1, Math.round((a / (a + b)) * 10))) }
}

// A word, and how the voice should say it.
export interface RtvSay {
  word: string
  say: string
}

// An audiobook Read-to-Me made, still on disk.
export interface RtvMade extends RtvJob {
  sizeBytes: number
  canChange: boolean // may remake or delete it
  kept: boolean // what it was made of is kept: its lines can be read again
  keptBytes?: number
  flagged?: number // lines that may be read badly
}

export interface RtvBook {
  title: string
  author: string
  canMake: boolean
  hasAudiobook: boolean
  chapters?: number
  words?: number
  audioMinutes?: number
  problem?: string
  chapterList?: { title: string; words: number; edited?: boolean }[]
  job?: RtvJob
  made?: RtvMade
}

export const jobActive = (j?: RtvJob | null) => !!j && (j.state === 'queued' || j.state === 'running' || j.state === 'waiting')

export interface RtvSettings {
  kokoroUrl: string
  chatterboxUrl: string
  nightOnly: boolean
  nightFrom: string
  nightTo: string
  engines: RtvEngineState[]
  runnerUrl: string
  runnerTokenSet: boolean
  chatterboxModel: CbModel
}

async function call<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch('/api/home/readtome' + path, {
    method,
    credentials: 'include',
    cache: 'no-store',
    headers: body === undefined ? undefined : { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  const text = await res.text()
  const data = text && res.headers.get('content-type')?.includes('json') ? JSON.parse(text) : null
  if (!res.ok) throw new Error(data?.error ?? t('Something went wrong (error {status}).', { status: res.status }))
  return data as T
}

export const readToMeApi = {
  voices: () => call<RtvVoices>('GET', '/voices'),
  book: (bookId: number) => call<RtvBook>('GET', `/book?bookId=${bookId}`),
  make: (b: { bookId: number; engine: string; voice: string; speed: number; remake?: boolean } & RtvOptions) => call<RtvJob>('POST', '/jobs', b),
  jobs: () => call<{ jobs: RtvJob[] }>('GET', '/jobs'),
  cancel: (id: number) => call<unknown>('DELETE', `/jobs/${id}`),
  made: () => call<{ made: RtvMade[] }>('GET', '/made'),
  deleteMade: (id: number) => call<unknown>('DELETE', `/made/${id}`),
  say: (bookId = 0) => call<{ all: RtvSay[]; book?: RtvSay[] }>('GET', bookId ? `/say?bookId=${bookId}` : '/say'),
  saveSay: (bookId: number, words: RtvSay[]) => call<{ words: RtvSay[] }>('PUT', '/say', { bookId, words }),
  presets: () => call<{ presets: RtvPreset[] }>('GET', '/presets'),
  cast: (bookId: number) => call<RtvCastInfo>('GET', `/cast?bookId=${bookId}`),
  castAI: (bookId: number) => call<unknown>('POST', '/cast/ai', { bookId }),
  castLines: (bookId: number, chapter: number) => call<{ lines: RtvCastLine[] }>('GET', `/cast/lines?bookId=${bookId}&chapter=${chapter}`),
  setCastLine: (bookId: number, chapter: number, index: number, speaker: string) => call<unknown>('PUT', '/cast/lines', { bookId, chapter, index, speaker }),
  savePreset: (p: Omit<RtvPreset, 'id' | 'default'> & { default?: boolean }) => call<{ presets: RtvPreset[] }>('POST', '/presets', p),
  setDefaultPreset: (id: number, on: boolean) => call<{ presets: RtvPreset[] }>('PUT', `/presets/${id}`, { default: on }),
  deletePreset: (id: number) => call<{ presets: RtvPreset[] }>('DELETE', `/presets/${id}`),
  settings: () => call<RtvSettings>('GET', '/settings'),
  saveSettings: (b: Partial<{ kokoroUrl: string; chatterboxUrl: string; nightOnly: boolean; nightFrom: string; nightTo: string; runnerUrl: string; runnerToken: string; chatterboxModel: CbModel }>) => call<RtvSettings>('PUT', '/settings', b),
  engines: () => call<RtvEngines>('GET', '/engines'),
  engineAction: (name: string, action: 'start' | 'stop') => call<RtvEngines>('POST', `/engines/${name}/${action}`),
  test: (engine: string) => call<{ message: string }>('POST', '/test', { engine }),
  madeLines: (id: number, chapter?: number) => call<RtvMadeLines>('GET', `/made/${id}/lines${chapter === undefined ? '' : `?chapter=${chapter}`}`),
  lineUrl: (id: number, chapter: number, line: number) => `/api/home/readtome/made/${id}/line?chapter=${chapter}&line=${line}`,
  redo: (id: number, r: RtvRedo) => call<RtvJob>('POST', `/made/${id}/redo`, r),
  freeMakings: (id: number) => call<unknown>('DELETE', `/made/${id}/makings`),
  text: (bookId: number, chapter: number) => call<{ title: string; text: string; edited: boolean }>('GET', `/text?bookId=${bookId}&chapter=${chapter}`),
  saveText: (bookId: number, chapter: number, text: string) => call<{ edited: boolean }>('PUT', '/text', { bookId, chapter, text }),
  resetText: (bookId: number, chapter: number) => call<{ edited: boolean }>('DELETE', `/text?bookId=${bookId}&chapter=${chapter}`),
  copiedVoices: () => call<{ voices: RtvCopiedVoice[] }>('GET', '/chatterbox/voices'),
  deleteCopiedVoice: (name: string) => call<unknown>('DELETE', `/chatterbox/voices/${encodeURIComponent(name)}`),
  recordingUrl: (name: string) => `/api/home/readtome/chatterbox/voices/${encodeURIComponent(name)}/recording`,
  async copyVoice(name: string, file: File, rights: boolean): Promise<{ name: string; seconds: number }> {
    const form = new FormData()
    form.append('name', name)
    form.append('rights', rights ? 'yes' : '')
    form.append('file', file)
    const res = await fetch('/api/home/readtome/chatterbox/voices', { method: 'POST', credentials: 'include', body: form })
    const data = await res.json().catch(() => null)
    if (!res.ok) throw new Error(data?.error ?? t('Something went wrong (error {status}).', { status: res.status }))
    return data
  },
  sampleUrl: (v: Pick<RtvVoice, 'engine' | 'id'>, model?: CbModel) => `/api/home/readtome/sample?engine=${encodeURIComponent(v.engine)}&voice=${encodeURIComponent(v.id)}${model ? `&model=${model}` : ''}`,
  // The preview is an MP3 made on the spot: a blob address the page can play.
  async preview(b: { bookId: number; engine: string; voice: string; speed: number; words?: number } & RtvOptions, signal?: AbortSignal): Promise<string> {
    const res = await fetch('/api/home/readtome/preview', { method: 'POST', credentials: 'include', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(b), signal })
    if (!res.ok) {
      const data = await res.json().catch(() => null)
      throw new Error(data?.error ?? t('Something went wrong (error {status}).', { status: res.status }))
    }
    return URL.createObjectURL(await res.blob())
  },
}

// Whether Read-to-Me is set up at all (an engine has an address), asked once
// every couple of minutes, so a book page doesn't ask on every visit.
let onAsk: { at: number; on: Promise<boolean> } | null = null
export function readToMeOn(): Promise<boolean> {
  if (!onAsk || Date.now() - onAsk.at > 120_000) {
    onAsk = { at: Date.now(), on: readToMeApi.voices().then((v) => (v.engines ?? []).some((e) => e.on && e.ready), () => false) }
  }
  return onAsk.on
}
