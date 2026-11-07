// Usherr: the NSFW page's Tools tab, for administrators: match files by
// fingerprint, bring in StashDB favorites, rename and file with undo. The jobs
// run on the server one at a time and this page polls them (like MusicTidy.tsx).
import { useCallback, useEffect, useState } from 'react'
import { useConfirm } from '../components/ConfirmProvider'
import { plural, st, t } from '../i18n'
import { nsfwApi, type FavoritesPreview, type IdentifyCandidate, type IdentifyItem, type IdentifyOverview, type NsfwJob, type RenameOverview, type RenamePlanDetail } from './nsfwApi'

const POLL_MS = 1500

function errorText(e: unknown) {
  return e instanceof Error ? e.message : String(e)
}

function clockLength(sec: number): string {
  if (sec <= 0) return '?'
  const h = Math.floor(sec / 3600)
  const m = Math.floor((sec % 3600) / 60)
  const s = String(Math.floor(sec % 60)).padStart(2, '0')
  return h > 0 ? `${h}:${String(m).padStart(2, '0')}:${s}` : `${m}:${s}`
}

function JobProgress({ job, onStop }: { job: NsfwJob; onStop: () => void }) {
  if (job.phase !== 'running') return null
  return (
    <div className="nt-progress" role="status">
      <progress max={job.total || 1} value={job.done} aria-label={t('Progress')} />
      <span>
        {job.total > 0 ? t('{done} of {total}', { done: job.done, total: job.total }) : t('Looking…')}
        {job.current ? ` · ${job.current}` : ''}
      </span>
      <button className="btn-sm" onClick={onStop}>
        {t('Stop')}
      </button>
    </div>
  )
}

function JobEnd({ job }: { job?: NsfwJob }) {
  if (!job) return null
  if (job.phase === 'failed') return <p className="error-text">{st(job.error)}</p>
  if (job.phase === 'stopped') return <p className="hint">{t('Stopped. What was done so far stays.')}</p>
  return null
}

// ---- match by fingerprint ----

function lengthLine(item: IdentifyItem, c: IdentifyCandidate): string {
  const lengths = { file: clockLength(item.duration), stash: clockLength(c.stashLength) }
  if (c.length === 'agrees') return t('Length agrees ({file})', lengths)
  if (c.length === 'differs') return t('Length differs: file {file}, StashDB {stash}', lengths)
  return t('Length could not be compared: file {file}, StashDB {stash}', lengths)
}

function MatchRow({ item, checked, pick, onToggle, onPick }: { item: IdentifyItem; checked: boolean; pick: string; onToggle: () => void; onPick: (id: string) => void }) {
  const chosen = item.candidates.find((c) => c.video.stashId === pick) ?? item.candidates[0]
  const open = item.status === 'matched' || item.status === 'blocked'
  return (
    <li className="nt-row">
      {open && chosen ? <input type="checkbox" checked={checked} onChange={onToggle} aria-label={t('Choose {name}', { name: item.path })} /> : <span className="nt-nobox" />}
      <div className="nt-body">
        <code>{item.path}</code>
        {chosen && (
          <div className="nt-video">
            {item.candidates.length > 1 && open ? (
              <select value={chosen.video.stashId} onChange={(e) => onPick(e.target.value)} aria-label={t('Which video is this?')}>
                {item.candidates.map((c) => (
                  <option key={c.video.stashId} value={c.video.stashId}>
                    {[c.video.studioName, c.video.releaseDate, c.video.title].filter(Boolean).join(' - ')}
                  </option>
                ))}
              </select>
            ) : (
              <strong>{[chosen.video.studioName, chosen.video.releaseDate, chosen.video.title].filter(Boolean).join(' - ')}</strong>
            )}
            {chosen.unsure && open && <span className="nt-unsure">{t('Unsure')}</span>}
          </div>
        )}
        {chosen && (
          <small>
            {t('Matched by {how}', { how: chosen.matched })} · {lengthLine(item, chosen)}
            {item.candidates.length > 1 ? ` · ${t('StashDB has {n} videos with this fingerprint', { n: item.candidates.length })}` : ''}
          </small>
        )}
        {item.status === 'registered' && <small className="nt-good">{t('Added to your library')}</small>}
        {item.reason && <small className="nt-bad">{st(item.reason)}</small>}
      </div>
    </li>
  )
}

function MatchFiles() {
  const confirm = useConfirm()
  const [over, setOver] = useState<IdentifyOverview | null>(null)
  const [picked, setPicked] = useState<Set<number>>(new Set())
  const [choice, setChoice] = useState<Record<number, string>>({})
  const [error, setError] = useState('')

  const refresh = useCallback(async (reselect = false) => {
    const next = await nsfwApi.identify()
    setOver(next)
    // Sure matches start chosen; an unsure one takes a click.
    if (reselect) setPicked(new Set(next.items.filter((it) => it.status === 'matched' && it.candidates[0] && !it.candidates[0].unsure).map((it) => it.id)))
    return next
  }, [])

  useEffect(() => {
    refresh(true).catch((e) => setError(errorText(e)))
  }, [refresh])

  const running = over?.job?.phase === 'running'
  useEffect(() => {
    if (!running) return
    const timer = setInterval(() => {
      refresh().then((next) => {
        if (next.job?.phase !== 'running') void refresh(true)
      }).catch((e) => setError(errorText(e)))
    }, POLL_MS)
    return () => clearInterval(timer)
  }, [running, refresh])

  const run = (fn: () => Promise<unknown>) => {
    setError('')
    fn().then(() => refresh(true)).catch((e) => setError(errorText(e)))
  }

  const addChosen = async () => {
    const ok = await confirm({
      title: plural(picked.size, 'Add {n} file to your library?', 'Add {n} files to your library?'),
      body: <p>{t('They are added as downloaded, where they are. No file is moved or changed.')}</p>,
      confirmLabel: plural(picked.size, 'Add {n} file', 'Add {n} files'),
    })
    if (!ok || !over) return
    const items = over.items.filter((it) => picked.has(it.id)).map((it) => ({ id: it.id, stashId: choice[it.id] ?? it.candidates[0]?.video.stashId ?? '' }))
    run(() => nsfwApi.identifyConfirm(items))
  }

  const toggle = (id: number) => setPicked((old) => {
    const next = new Set(old)
    if (next.has(id)) next.delete(id)
    else next.add(id)
    return next
  })

  const counts = over?.counts ?? {}
  return (
    <section className="card nt-card">
      <h2>{t('Match files by fingerprint')}</h2>
      <p className="hint">{t('Finds video files in your NSFW folder that Usherr doesn’t know, and asks StashDB about them. Only a small part of each file is read. Nothing is added until you confirm it. This only runs when you press the button.')}</p>
      <div className="sc-actions">
        <button className="btn-sm primary" disabled={running} onClick={() => run(nsfwApi.identifyStart)}>
          {t('Find matches')}
        </button>
      </div>
      {over?.job && <JobProgress job={over.job} onStop={() => run(nsfwApi.toolsStop)} />}
      <JobEnd job={over?.job} />
      {error && <p className="error-text">{error}</p>}
      {over && Object.keys(counts).length > 0 && (
        <p className="hint">
          {t('Last run: {matched} with a match, {none} without', { matched: (counts.matched ?? 0) + (counts.blocked ?? 0) + (counts.registered ?? 0), none: counts.none ?? 0 })}
          {counts.unreadable ? ` · ${t('{n} couldn’t be read', { n: counts.unreadable })}` : ''}
        </p>
      )}
      {over && over.items.length > 0 && (
        <>
          <ul className="nt-list">
            {over.items.map((item) => (
              <MatchRow key={item.id} item={item} checked={picked.has(item.id)} pick={choice[item.id] ?? ''} onToggle={() => toggle(item.id)} onPick={(id) => setChoice((old) => ({ ...old, [item.id]: id }))} />
            ))}
          </ul>
          <div className="sc-actions">
            <button className="btn-sm primary" disabled={running || picked.size === 0} onClick={() => void addChosen()}>
              {plural(picked.size, 'Add {n} file', 'Add {n} files')}
            </button>
          </div>
        </>
      )}
    </section>
  )
}

// ---- favorites ----

function FavoriteChecks({ title, side, picked, onToggle }: { title: string; side: FavoritesPreview['performers']; picked: Set<string>; onToggle: (id: string) => void }) {
  if (side.toFollow.length === 0 && side.leftAlone.length === 0) return null
  return (
    <div className="nt-favs">
      <h3>{title}</h3>
      {side.toFollow.map((f) => (
        <label key={f.stashId} className="sc-check">
          <input type="checkbox" checked={picked.has(f.stashId)} onChange={() => onToggle(f.stashId)} /> {f.name}
        </label>
      ))}
      {side.leftAlone.length > 0 && (
        <p className="hint">
          {t('You stopped following these here, so they stay as they are: {names}', { names: side.leftAlone.map((f) => f.name).join(', ') })}
        </p>
      )}
    </div>
  )
}

function Favorites() {
  const [auto, setAuto] = useState<boolean | null>(null)
  const [preview, setPreview] = useState<FavoritesPreview | null>(null)
  const [picked, setPicked] = useState<Set<string>>(new Set())
  const [busy, setBusy] = useState(false)
  const [msg, setMsg] = useState('')
  const [error, setError] = useState('')

  useEffect(() => {
    nsfwApi.favorites().then((f) => setAuto(f.auto)).catch((e) => setError(errorText(e)))
  }, [])

  const look = () => {
    setBusy(true)
    setError('')
    setMsg('')
    nsfwApi.favoritesPreview()
      .then((p) => {
        setPreview(p)
        setPicked(new Set([...p.performers.toFollow, ...p.studios.toFollow].map((f) => f.stashId)))
      })
      .catch((e) => setError(errorText(e)))
      .finally(() => setBusy(false))
  }

  const follow = () => {
    if (!preview) return
    setBusy(true)
    setError('')
    nsfwApi.favoritesApply(preview.performers.toFollow.filter((f) => picked.has(f.stashId)).map((f) => f.stashId), preview.studios.toFollow.filter((f) => picked.has(f.stashId)).map((f) => f.stashId))
      .then((res) => {
        setPreview(null)
        setMsg(t('Now following {performers} and {studios}.', { performers: plural(res.performers, '{n} performer', '{n} performers'), studios: plural(res.studios, '{n} studio', '{n} studios') }))
      })
      .catch((e) => setError(errorText(e)))
      .finally(() => setBusy(false))
  }

  const switchAuto = (on: boolean) => {
    setError('')
    nsfwApi.favoritesAuto(on).then((f) => setAuto(f.auto)).catch((e) => setError(errorText(e)))
  }

  const toggle = (id: string) => setPicked((old) => {
    const next = new Set(old)
    if (next.has(id)) next.delete(id)
    else next.add(id)
    return next
  })

  const nothing = preview && preview.performers.toFollow.length + preview.studios.toFollow.length === 0
  return (
    <section className="card nt-card">
      <h2>{t('StashDB favorites')}</h2>
      <p className="hint">{t('Follows the performers and studios you marked as favorite on the StashDB account of your saved key. It only adds follows here: it never unfollows anyone and never changes anything on StashDB.')}</p>
      <div className="sc-actions">
        <button className="btn-sm primary" disabled={busy} onClick={look}>
          {t('Bring in my StashDB favorites')}
        </button>
      </div>
      {preview && (
        <>
          {nothing && <p className="hint">{t('Nothing new to follow.')}</p>}
          <FavoriteChecks title={t('Performers')} side={preview.performers} picked={picked} onToggle={toggle} />
          <FavoriteChecks title={t('Studios')} side={preview.studios} picked={picked} onToggle={toggle} />
          {!nothing && (
            <>
              <p className="hint">{t('They are followed from today. Nothing older is added and no search starts now. When one of them has a new video, the usual follow check adds it as wanted, and your automatic video search looks for it if that’s on. Use the Find tab to add older videos.')}</p>
              <div className="sc-actions">
                <button className="btn-sm primary" disabled={busy || picked.size === 0} onClick={follow}>
                  {plural(picked.size, 'Follow {n}', 'Follow {n}')}
                </button>
                <button className="btn-sm" disabled={busy} onClick={() => setPreview(null)}>
                  {t('Cancel')}
                </button>
              </div>
            </>
          )}
        </>
      )}
      {msg && <p className="hint">{msg}</p>}
      {error && <p className="error-text">{error}</p>}
      {auto !== null && (
        <label className="sc-check nt-auto">
          <input type="checkbox" checked={auto} onChange={(e) => switchAuto(e.target.checked)} />
          <span>{t('Do this every night, between 2 and 9 in the morning, without asking')}</span>
        </label>
      )}
    </section>
  )
}

// ---- rename ----

function RenameRow({ item, checked, onToggle }: { item: RenamePlanDetail['items'][number]; checked: boolean; onToggle: () => void }) {
  const open = item.status === 'eligible' || item.status === 'failed'
  return (
    <li className="nt-row">
      {open ? <input type="checkbox" checked={checked} onChange={onToggle} aria-label={t('Choose {name}', { name: item.name })} /> : <span className="nt-nobox" />}
      <div className="nt-body">
        <code className={item.newName ? 'nt-old' : ''}>{item.oldName}</code>
        {item.newName && item.status !== 'blocked' && <code className="nt-new">{item.newName}</code>}
        {item.sidecars.length > 0 && <small>{plural(item.sidecars.length, '{n} subtitle or .nfo file moves along', '{n} subtitle or .nfo files move along')}</small>}
        {item.outside && open && <small className="nt-warn">{t('This file is outside the NSFW folder. It moves on the same drive; an app that still manages it, like Whisparr, will lose track of it.')}</small>}
        {item.status === 'applied' && <small className="nt-good">{t('Renamed')}</small>}
        {item.status === 'undone' && <small>{t('Put back')}</small>}
        {item.reason && <small className="nt-bad">{st(item.reason)}</small>}
      </div>
    </li>
  )
}

function RenameFiles() {
  const confirm = useConfirm()
  const [over, setOver] = useState<RenameOverview | null>(null)
  const [plan, setPlan] = useState<RenamePlanDetail | null>(null)
  const [picked, setPicked] = useState<Set<number>>(new Set())
  const [error, setError] = useState('')

  const showPlan = useCallback(async (id: number) => {
    const detail = await nsfwApi.renamePlan(id)
    setPlan(detail)
    // Files outside the NSFW folder take a click: another app may manage them.
    setPicked(new Set(detail.items.filter((it) => it.status === 'eligible' && !it.outside).map((it) => it.id)))
  }, [])

  const refresh = useCallback(async () => {
    const next = await nsfwApi.renameOverview()
    setOver(next)
    return next
  }, [])

  const settle = useCallback(async () => {
    const next = await refresh()
    const id = next.job?.planId || next.plans[0]?.id
    if (id) await showPlan(id)
    else setPlan(null)
  }, [refresh, showPlan])

  useEffect(() => {
    settle().catch((e) => setError(errorText(e)))
  }, [settle])

  const running = over?.job?.phase === 'running'
  useEffect(() => {
    if (!running) return
    const timer = setInterval(() => {
      refresh().then((next) => {
        if (next.job?.phase !== 'running') return settle()
      }).catch((e) => setError(errorText(e)))
    }, POLL_MS)
    return () => clearInterval(timer)
  }, [running, refresh, settle])

  const run = (fn: () => Promise<unknown>) => {
    setError('')
    // settle, not refresh: a small job can be over before this first look, and
    // then the poll below never starts to bring in the finished plan.
    fn().then(settle).catch((e) => setError(errorText(e)))
  }

  const applyChosen = async () => {
    if (!plan) return
    const ok = await confirm({
      title: plural(picked.size, 'Rename {n} video?', 'Rename {n} videos?'),
      body: <p>{t('Each file gets its new name and folder, with its subtitles and .nfo. Nothing is copied, overwritten or deleted, and you can undo this.')}</p>,
      confirmLabel: plural(picked.size, 'Rename {n} video', 'Rename {n} videos'),
    })
    if (ok) run(() => nsfwApi.renameApply(plan.plan.id, [...picked]))
  }

  const undo = async (id: number) => {
    const ok = await confirm({
      title: t('Put everything back?'),
      body: <p>{t('Every file goes back to its old name and folder. A file that changed since, or whose old name is taken, stays where it is.')}</p>,
      confirmLabel: t('Undo'),
    })
    if (ok) run(() => nsfwApi.renameUndo(id))
  }

  const toggle = (id: number) => setPicked((old) => {
    const next = new Set(old)
    if (next.has(id)) next.delete(id)
    else next.add(id)
    return next
  })

  const undone = over?.job?.kind === 'undo' ? over.job.undo : undefined
  const canUndo = plan?.plan.status === 'applied'
  return (
    <section className="card nt-card">
      <h2>{t('Rename and file')}</h2>
      <p className="hint">{t('Works out a tidy name for each downloaded video and shows it before anything changes. You pick which to rename. This only runs when you press the button.')}</p>
      <p className="hint">
        <code>{t('Studio/Studio - YYYY-MM-DD - Title [quality].ext')}</code>
      </p>
      <div className="sc-actions">
        <button className="btn-sm primary" disabled={running} onClick={() => run(nsfwApi.renameCheck)}>
          {t('Check names')}
        </button>
      </div>
      {over?.job && <JobProgress job={over.job} onStop={() => run(nsfwApi.toolsStop)} />}
      <JobEnd job={over?.job} />
      {error && <p className="error-text">{error}</p>}
      {undone && undone.some((u) => !u.done) && (
        <ul className="nt-undo">
          {undone.filter((u) => !u.done).map((u) => (
            <li key={u.name}>
              <strong>{u.name}</strong>: {st(u.note)}
            </li>
          ))}
        </ul>
      )}
      {plan && !running && (
        <>
          <p className="hint">
            {t('{n} to look at', { n: plan.items.length })} · {t('{n} already named right', { n: plan.plan.unchanged })}
          </p>
          {plan.items.length > 0 && (
            <ul className="nt-list">
              {plan.items.map((item) => (
                <RenameRow key={item.id} item={item} checked={picked.has(item.id)} onToggle={() => toggle(item.id)} />
              ))}
            </ul>
          )}
          <div className="sc-actions">
            {plan.items.some((it) => it.status === 'eligible' || it.status === 'failed') && plan.plan.status !== 'undone' && (
              <button className="btn-sm primary" disabled={picked.size === 0} onClick={() => void applyChosen()}>
                {plural(picked.size, 'Rename {n} video', 'Rename {n} videos')}
              </button>
            )}
            {canUndo && (
              <button className="btn-sm" onClick={() => void undo(plan.plan.id)}>
                {t('Undo')}
              </button>
            )}
          </div>
        </>
      )}
      {over && over.plans.filter((p) => p.status !== 'preview' && p.id !== plan?.plan.id).length > 0 && (
        <details className="nt-history">
          <summary>{t('Earlier renames')}</summary>
          <ul>
            {over.plans.filter((p) => p.status !== 'preview' && p.id !== plan?.plan.id).map((p) => (
              <li key={p.id}>
                <span>
                  {new Date(p.appliedAt || p.createdAt).toLocaleString()} · {plural(p.applied, '{n} video', '{n} videos')}
                  {p.status === 'undone' ? ` · ${t('Put back')}` : ''}
                </span>
                {p.status === 'applied' && (
                  <button className="btn-sm" disabled={running} onClick={() => void undo(p.id)}>
                    {t('Undo')}
                  </button>
                )}
              </li>
            ))}
          </ul>
        </details>
      )}
    </section>
  )
}

export default function NsfwTools() {
  return (
    <div className="nt-tools">
      <MatchFiles />
      <Favorites />
      <RenameFiles />
    </div>
  )
}
