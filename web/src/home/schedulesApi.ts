// Home fork: schedules. Named schedules are made on Settings > Schedules and
// picked by each task; the NSFW page sets its own tasks with the same names.
import { locale, t } from '../i18n'

export interface SchedRange {
  days: number[] // 0 = Sunday … 6 = Saturday: days the range starts on
  start: string // "HH:MM"
  end: string // "HH:MM"; earlier than start runs past midnight; equal = the whole day
}

export interface Schedule {
  id: string
  name: string
  ranges: SchedRange[]
}

export type DownloadAction = 'pause' | 'limit' | 'connections'

export interface TaskRule {
  schedule: string
  mode: 'only' | 'never'
  action?: DownloadAction
  when?: 'now' | 'finish'
  limitMB?: number
  connections?: number
}

export interface TaskState {
  rule?: TaskRule
  allowed: boolean
  nextAllowed?: string
}

export interface SchedulesView {
  schedules: Schedule[]
  tasks: Record<string, TaskState>
  quietUntil?: string
  quietWhen?: 'now' | 'finish'
  now: string
  timeZone: string
}

async function call<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch('/api' + path, {
    method,
    credentials: 'include',
    cache: 'no-store',
    headers: body === undefined ? undefined : { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  const text = await res.text()
  const data = text ? JSON.parse(text) : null
  if (!res.ok) throw new Error(data?.error ?? t('Something went wrong (error {status}).', { status: res.status }))
  return data as T
}

export const schedulesApi = {
  get: () => call<SchedulesView>('GET', '/home/schedules'),
  save: (body: { schedules?: Schedule[]; tasks?: Record<string, TaskRule | null> }) => call<SchedulesView>('PUT', '/home/schedules', body),
  quiet: (body: { minutes?: number; untilMorning?: boolean; when?: 'now' | 'finish' }) => call<SchedulesView>('POST', '/home/quiet', body),
  nsfw: () => call<SchedulesView>('GET', '/nsfw/schedule'),
  saveNsfw: (tasks: Record<string, TaskRule | null>) => call<SchedulesView>('PUT', '/nsfw/schedule', { tasks }),
}

// Short day names in the reader's language, Sunday first (7 Jan 2024 was a Sunday).
export const DAY_SHORT = [0, 1, 2, 3, 4, 5, 6].map((d) => new Date(2024, 0, 7 + d).toLocaleDateString(locale(), { weekday: 'short' }))
export const ALL_DAYS = [0, 1, 2, 3, 4, 5, 6]

// Ready-made simple schedules (every day).
export const PRESETS: { name: string; start: string; end: string }[] = [
  { name: t('Overnight'), start: '01:00', end: '09:00' },
  { name: t('Daytime'), start: '09:00', end: '17:00' },
  { name: t('Evenings'), start: '18:00', end: '23:00' },
]

export function clock(hhmm: string) {
  const [h, m] = hhmm.split(':').map(Number)
  const d = new Date(2000, 0, 1, h, m)
  return d.toLocaleTimeString(locale(), { hour: 'numeric', minute: '2-digit' })
}

export function when(iso?: string) {
  if (!iso) return ''
  const d = new Date(iso)
  if (isNaN(d.getTime())) return ''
  const sameDay = d.toDateString() === new Date().toDateString()
  return sameDay ? d.toLocaleTimeString(locale(), { hour: 'numeric', minute: '2-digit' }) : d.toLocaleString(locale(), { weekday: 'short', hour: 'numeric', minute: '2-digit' })
}

function daysText(days: number[]) {
  const set = [...days].sort()
  const key = set.join(',')
  if (key === '0,1,2,3,4,5,6') return t('Every day')
  if (key === '1,2,3,4,5') return t('Weekdays')
  if (key === '0,6') return t('Weekends')
  return set.map((d) => DAY_SHORT[d]).join(', ')
}

export function rangeText(r: SchedRange) {
  const time = r.start === r.end ? t('all day') : `${clock(r.start)}–${clock(r.end)}`
  return `${daysText(r.days)} ${time}`
}

export function isSimple(s: Schedule) {
  return s.ranges.length === 1 && s.ranges[0].days.length === 7
}
