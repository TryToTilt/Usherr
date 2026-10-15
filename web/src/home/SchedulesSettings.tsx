// Home fork: Settings > Schedules. Make named schedules (simple: one time
// range every day; detailed: days and times, several ranges), then give each
// heavy task a schedule and a rule. "Quiet for…" holds everything for a while.
import { useCallback, useEffect, useState } from 'react'
import Icon from '../components/Icon'
import { ALL_DAYS, DAY_SHORT, PRESETS, clock, isSimple, rangeText, schedulesApi, when, type SchedRange, type Schedule, type SchedulesView, type TaskRule, type TaskState } from './schedulesApi'
import './schedules.css'
import { t, tx } from '../i18n'

export const SHARED_TASKS: { key: string; label: string; hint: string }[] = [
  { key: 'search.auto', label: t('Automatic searches'), hint: t('Mediarium looking for missing or better movies, episodes, music and books.') },
  { key: 'downloads', label: t('Downloads'), hint: t('The download line. Downloads you start by hand follow this too once they are in the line.') },
  { key: 'postprocess', label: t('Repair and unpack'), hint: t('PAR2 repair and unpacking after a download (heavy on the processor and disk). Finished downloads wait for it.') },
  { key: 'mediaserver.refresh', label: t('Media server refreshes'), hint: t('Asking Jellyfin, Plex or Emby to rescan after an import. Held ones are sent together later.') },
]

export default function SchedulesSettings() {
  const [view, setView] = useState<SchedulesView | null>(null)
  const [error, setError] = useState('')
  const [note, setNote] = useState('')
  const [editing, setEditing] = useState<Schedule | null>(null)

  const load = useCallback(() => {
    schedulesApi.get().then(setView).catch((e: Error) => setError(e.message))
  }, [])
  useEffect(() => {
    load()
    const timer = window.setInterval(load, 30000)
    return () => window.clearInterval(timer)
  }, [load])

  const run = (p: Promise<SchedulesView>, done: string) => {
    setError('')
    setNote('')
    return p
      .then((v) => {
        setView(v)
        setNote(done)
        return true
      })
      .catch((e: Error) => {
        setError(e.message)
        return false
      })
  }

  if (!view) return <div className="page-pad">{error ? <p className="error-text">{error}</p> : <div className="skeleton" style={{ height: 120 }} />}</div>

  const saveSchedule = (s: Schedule) => {
    const list = view.schedules.some((x) => x.id === s.id && s.id) ? view.schedules.map((x) => (x.id === s.id ? s : x)) : [...view.schedules, s]
    run(schedulesApi.save({ schedules: list }), t('Saved "{name}".', { name: s.name })).then((ok) => ok && setEditing(null))
  }
  const removeSchedule = (s: Schedule) => {
    if (!confirm(t('Delete the schedule "{name}"? Tasks using it go back to running at any time.', { name: s.name }))) return
    run(schedulesApi.save({ schedules: view.schedules.filter((x) => x.id !== s.id) }), t('Deleted "{name}".', { name: s.name }))
  }
  const saveRule = (task: string, rule: TaskRule | null) => run(schedulesApi.save({ tasks: { [task]: rule } }), t('Saved.'))

  return (
    <div className="sched-page">
      <QuietCard view={view} onChange={(p, msg) => run(p, msg)} />

      <section className="card">
        <h2>
          <Icon name="calendar" size={16} /> {t('Schedules')}
        </h2>
        <p className="hint">
          {t("Make a schedule once and use it for as many tasks as you like. Times are the server's ({zone}; it is {now} there now).", { zone: view.timeZone, now: when(view.now) })}
        </p>
        {view.schedules.length === 0 && !editing && <p className="hint">{t('No schedules yet.')}</p>}
        <ul className="sched-list">
          {view.schedules.map((s) =>
            editing?.id === s.id ? (
              <li key={s.id}>
                <ScheduleEditor initial={s} onSave={saveSchedule} onCancel={() => setEditing(null)} />
              </li>
            ) : (
              <li key={s.id} className="sched-row">
                <div>
                  <strong>{s.name}</strong>
                  <span className="hint">{s.ranges.map(rangeText).join(' · ')}</span>
                </div>
                <div className="sched-actions">
                  <button className="btn-sm" onClick={() => setEditing(s)}>
                    {t('Edit')}
                  </button>
                  <button className="btn-sm" onClick={() => removeSchedule(s)} aria-label={t('Delete {name}', { name: s.name })}>
                    <Icon name="trash" size={14} />
                  </button>
                </div>
              </li>
            ),
          )}
          {editing && !editing.id && (
            <li>
              <ScheduleEditor initial={editing} onSave={saveSchedule} onCancel={() => setEditing(null)} />
            </li>
          )}
        </ul>
        {!editing && (
          <button className="btn-sm primary" onClick={() => setEditing({ id: '', name: '', ranges: [{ days: [...ALL_DAYS], start: '01:00', end: '09:00' }] })}>
            <Icon name="plus" size={14} /> {t('New schedule')}
          </button>
        )}
      </section>

      <section className="card">
        <h2>
          <Icon name="clock" size={16} /> {t('What follows a schedule')}
        </h2>
        <p className="hint">{t('Anything you start by hand (a search, a grab, Retry) always runs at once.')}</p>
        {SHARED_TASKS.map((task) => (
          <TaskRuleEditor key={task.key} task={task.key} label={task.label} hint={task.hint} state={view.tasks[task.key]} schedules={view.schedules} onSave={saveRule} />
        ))}
      </section>

      {note && <p className="hint">{note}</p>}
      {error && <p className="error-text">{error}</p>}
    </div>
  )
}

// ---- Quiet for… ----

function QuietCard({ view, onChange }: { view: SchedulesView; onChange: (p: Promise<SchedulesView>, msg: string) => void }) {
  const [whenRunning, setWhenRunning] = useState<'now' | 'finish'>('finish')
  const quiet = (body: { minutes?: number; untilMorning?: boolean }, done: string) => onChange(schedulesApi.quiet({ ...body, when: whenRunning }), done)
  return (
    <section className={`card sched-quiet${view.quietUntil ? ' on' : ''}`}>
      <h2>
        <Icon name="moon" size={16} /> {t('Quiet for a while')}
      </h2>
      {view.quietUntil ? (
        <div className="sched-quiet-on">
          <p>
            {view.quietWhen === 'now'
              ? tx('Quiet until {time}: searches, downloads, repair and refreshes are held (running downloads were paused).', { time: <strong>{when(view.quietUntil)}</strong> })
              : tx('Quiet until {time}: searches, downloads, repair and refreshes are held (running downloads were left to finish).', { time: <strong>{when(view.quietUntil)}</strong> })}
          </p>
          <button className="btn-sm primary" onClick={() => onChange(schedulesApi.quiet({ minutes: 0 }), t('Quiet ended.'))}>
            {t('End quiet now')}
          </button>
        </div>
      ) : (
        <>
          <p className="hint">{t('Hold every heavy task right now, for example when TV time starts early. Everything picks up again by itself.')}</p>
          <div className="sched-quiet-buttons">
            <button className="btn-sm" onClick={() => quiet({ minutes: 60 }, t('Quiet for 1 hour.'))}>{t('1 hour')}</button>
            <button className="btn-sm" onClick={() => quiet({ minutes: 120 }, t('Quiet for 2 hours.'))}>{t('2 hours')}</button>
            <button className="btn-sm" onClick={() => quiet({ minutes: 240 }, t('Quiet for 4 hours.'))}>{t('4 hours')}</button>
            <button className="btn-sm" onClick={() => quiet({ untilMorning: true }, t('Quiet until 7:00.'))}>{t('Until morning')}</button>
          </div>
          <WhenRunning value={whenRunning} onChange={setWhenRunning} name="quiet-when" />
        </>
      )}
    </section>
  )
}

function WhenRunning({ value, onChange, name }: { value: 'now' | 'finish'; onChange: (v: 'now' | 'finish') => void; name: string }) {
  return (
    <div className="sched-radios" role="radiogroup" aria-label={t('Downloads already running')}>
      <label>
        <input type="radio" name={name} checked={value === 'finish'} onChange={() => onChange('finish')} /> {t('Let running downloads finish')}
      </label>
      <label>
        <input type="radio" name={name} checked={value === 'now'} onChange={() => onChange('now')} /> {t('Pause running downloads at once')}
      </label>
    </div>
  )
}

// ---- schedule editor ----

function ScheduleEditor({ initial, onSave, onCancel }: { initial: Schedule; onSave: (s: Schedule) => void; onCancel: () => void }) {
  const [name, setName] = useState(initial.name)
  const [detailed, setDetailed] = useState(!isSimple(initial))
  const [ranges, setRanges] = useState<SchedRange[]>(initial.ranges.map((r) => ({ ...r, days: [...r.days] })))
  const simple = ranges[0] ?? { days: [...ALL_DAYS], start: '01:00', end: '09:00' }

  const setRange = (i: number, r: Partial<SchedRange>) => setRanges((l) => l.map((x, j) => (j === i ? { ...x, ...r } : x)))
  const toggleDay = (i: number, d: number) => setRanges((l) => l.map((x, j) => (j === i ? { ...x, days: x.days.includes(d) ? x.days.filter((y) => y !== d) : [...x.days, d].sort() } : x)))
  const save = () => onSave({ id: initial.id, name: name.trim(), ranges: detailed ? ranges : [{ days: [...ALL_DAYS], start: simple.start, end: simple.end }] })

  return (
    <div className="sched-editor">
      <label className="sched-field">
        <span>{t('Name')}</span>
        <input value={name} onChange={(e) => setName(e.target.value)} placeholder={t("e.g. Weeknight TV time")} autoFocus />
      </label>
      <div className="seg" role="tablist" aria-label={t('Kind of schedule')}>
        <button role="tab" aria-selected={!detailed} className={!detailed ? 'active' : ''} onClick={() => setDetailed(false)}>
          {t('Simple')}
        </button>
        <button role="tab" aria-selected={detailed} className={detailed ? 'active' : ''} onClick={() => setDetailed(true)}>
          {t('Detailed')}
        </button>
      </div>

      {!detailed ? (
        <>
          <p className="hint">{t('The same hours every day.')}</p>
          <div className="sched-presets">
            {PRESETS.map((p) => (
              <button
                key={p.name}
                className={`btn-sm${simple.start === p.start && simple.end === p.end ? ' primary' : ''}`}
                onClick={() => {
                  setRanges([{ days: [...ALL_DAYS], start: p.start, end: p.end }])
                  if (!name.trim()) setName(p.name)
                }}
              >
                {p.name} <span className="hint">{clock(p.start)}–{clock(p.end)}</span>
              </button>
            ))}
          </div>
          <div className="sched-times">
            <label className="sched-field">
              <span>{t('From')}</span>
              <input type="time" value={simple.start} onChange={(e) => setRanges([{ ...simple, days: [...ALL_DAYS], start: e.target.value }])} />
            </label>
            <label className="sched-field">
              <span>{t('Until')}</span>
              <input type="time" value={simple.end} onChange={(e) => setRanges([{ ...simple, days: [...ALL_DAYS], end: e.target.value }])} />
            </label>
          </div>
        </>
      ) : (
        <>
          <p className="hint">{t('Pick days and times; add more ranges for different days. A range that ends earlier than it starts runs past midnight; the same start and end means the whole day.')}</p>
          {ranges.map((r, i) => (
            <div key={i} className="sched-range">
              <div className="sched-days">
                {DAY_SHORT.map((d, n) => (
                  <button key={d} className={`sched-day${r.days.includes(n) ? ' on' : ''}`} aria-pressed={r.days.includes(n)} onClick={() => toggleDay(i, n)}>
                    {d}
                  </button>
                ))}
                <button className="linkish" onClick={() => setRange(i, { days: [1, 2, 3, 4, 5] })}>{t('Weekdays')}</button>
                <button className="linkish" onClick={() => setRange(i, { days: [0, 6] })}>{t('Weekends')}</button>
              </div>
              <div className="sched-times">
                <label className="sched-field">
                  <span>{t('From')}</span>
                  <input type="time" value={r.start} onChange={(e) => setRange(i, { start: e.target.value })} />
                </label>
                <label className="sched-field">
                  <span>{t('Until')}</span>
                  <input type="time" value={r.end} onChange={(e) => setRange(i, { end: e.target.value })} />
                </label>
                {ranges.length > 1 && (
                  <button className="btn-sm" onClick={() => setRanges((l) => l.filter((_, j) => j !== i))} aria-label={t('Remove this range')}>
                    <Icon name="x" size={14} />
                  </button>
                )}
              </div>
            </div>
          ))}
          <button className="btn-sm" onClick={() => setRanges((l) => [...l, { days: [1, 2, 3, 4, 5], start: '18:00', end: '22:00' }])}>
            <Icon name="plus" size={14} /> {t('Add a range')}
          </button>
        </>
      )}
      <div className="sched-actions">
        <button className="btn-sm primary" disabled={!name.trim()} onClick={save}>
          {t('Save schedule')}
        </button>
        <button className="btn-sm" onClick={onCancel}>
          {t('Cancel')}
        </button>
      </div>
    </div>
  )
}

// ---- one task's rule (also used by the Scenes page) ----

export function TaskRuleEditor({
  task,
  label,
  hint,
  state,
  schedules,
  onSave,
}: {
  task: string
  label: string
  hint: string
  state?: TaskState
  schedules: Schedule[]
  onSave: (task: string, rule: TaskRule | null) => Promise<boolean>
}) {
  const saved = state?.rule
  const [rule, setRule] = useState<TaskRule>(saved ?? { schedule: '', mode: 'only' })
  useEffect(() => setRule(saved ?? { schedule: '', mode: 'only' }), [saved])
  const isDownloads = task === 'downloads'
  const action = rule.action ?? 'pause'
  const dirty = JSON.stringify(rule) !== JSON.stringify(saved ?? { schedule: '', mode: 'only' })

  const status = !state
    ? ''
    : state.allowed
      ? t('Running normally now')
      : isDownloads && saved && action !== 'pause'
        ? action === 'limit'
          ? t('Slowed until {time}', { time: when(state.nextAllowed) })
          : t('Fewer connections until {time}', { time: when(state.nextAllowed) })
        : state.nextAllowed
          ? t('Held until {time}', { time: when(state.nextAllowed) })
          : t('Held until the schedule allows it')

  return (
    <div className="sched-task">
      <div className="sched-task-head">
        <strong>{label}</strong>
        {status && <span className={`badge ${state?.allowed ? 'downloaded' : 'missing'}`}>{status}</span>}
      </div>
      <p className="hint">{hint}</p>
      <div className="sched-task-rule">
        <select value={rule.schedule} onChange={(e) => setRule({ ...rule, schedule: e.target.value })} aria-label={t('{task}: schedule', { task: label })}>
          <option value="">{t('Any time')}</option>
          {schedules.map((s) => (
            <option key={s.id} value={s.id}>
              {s.name}
            </option>
          ))}
        </select>
        {rule.schedule && (
          <select value={rule.mode} onChange={(e) => setRule({ ...rule, mode: e.target.value as TaskRule['mode'] })} aria-label={t('{task}: rule', { task: label })}>
            <option value="only">{t('only during it')}</option>
            <option value="never">{t('never during it')}</option>
          </select>
        )}
      </div>
      {isDownloads && rule.schedule && (
        <div className="sched-dl">
          <span className="hint">{rule.mode === 'only' ? t('Outside the schedule:') : t('During the schedule:')}</span>
          <label>
            <input type="radio" name="dl-action" checked={action === 'pause'} onChange={() => setRule({ ...rule, action: 'pause', when: rule.when ?? 'finish' })} /> {t('Pause downloads')}
          </label>
          {action === 'pause' && (
            <div className="sched-sub">
              <WhenRunning value={rule.when ?? 'finish'} onChange={(w) => setRule({ ...rule, action: 'pause', when: w })} name="dl-when" />
            </div>
          )}
          <label>
            <input type="radio" name="dl-action" checked={action === 'limit'} onChange={() => setRule({ ...rule, action: 'limit', limitMB: rule.limitMB || 5 })} />{' '}
            {tx('Slow down to {n} MB/s', {
              n: <input type="number" min={1} className="sched-num" value={rule.limitMB ?? 5} disabled={action !== 'limit'} onChange={(e) => setRule({ ...rule, limitMB: Number(e.target.value) })} />,
            })}
          </label>
          <label>
            <input type="radio" name="dl-action" checked={action === 'connections'} onChange={() => setRule({ ...rule, action: 'connections', connections: rule.connections || 5 })} />{' '}
            {tx('Use at most {n} connections per Usenet server', {
              n: <input type="number" min={1} className="sched-num" value={rule.connections ?? 5} disabled={action !== 'connections'} onChange={(e) => setRule({ ...rule, connections: Number(e.target.value) })} />,
            })}
          </label>
        </div>
      )}
      {dirty && (
        <div className="sched-actions">
          <button className="btn-sm primary" onClick={() => void onSave(task, rule.schedule ? rule : null)}>
            {t('Save')}
          </button>
          <button className="btn-sm" onClick={() => setRule(saved ?? { schedule: '', mode: 'only' })}>
            {t('Cancel')}
          </button>
        </div>
      )}
    </div>
  )
}
