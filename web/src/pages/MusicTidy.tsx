import { useCallback, useEffect, useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import { api, type TidyAlbum, type TidyOverview, type TidyPlanDetail } from '../api'
import { useConfirm } from '../components/ConfirmProvider'
import Icon from '../components/Icon'
import KindGlyph from '../components/KindGlyph'
import { useToast } from '../components/Toast'
import { formatBytes } from '../format'
import { useModules } from '../ModulesContext'
import { plural, st, t, tx } from '../i18n'
import '../import.css'

const TAG_LABEL: Record<string, string> = {
  title: t('Title'),
  artist: t('Artist'),
  album: t('Album'),
  albumartist: t('Album artist'),
  track: t('Track'),
  tracktotal: t('Tracks'),
  disc: t('Disc'),
  disctotal: t('Discs'),
  date: t('Date'),
}

function errorText(e: unknown) {
  return e instanceof Error ? e.message : String(e)
}

function FileChanges({ album }: { album: TidyAlbum }) {
  return (
    <ul className="tidy-files">
      {album.items.map((it) => {
        const renamed = it.oldName !== it.newName
        const tags = (it.changes ?? []).filter((c) => TAG_LABEL[c.tag])
        const ids = (it.changes ?? []).length - tags.length
        return (
          <li key={it.oldName}>
            {renamed ? (
              <>
                <code className="tidy-old">{it.oldName}</code>
                <code className="tidy-new">{it.newName}</code>
              </>
            ) : (
              <code>{it.newName}</code>
            )}
            {tags.length > 0 && (
              <dl className="tidy-tags">
                {tags.map((c) => (
                  <div key={c.tag}>
                    <dt>{TAG_LABEL[c.tag]}</dt>
                    <dd>
                      <s>{c.old || t('(none)')}</s> {c.new}
                    </dd>
                  </div>
                ))}
              </dl>
            )}
            {(ids > 0 || it.cover) && (
              <small>
                {ids > 0 ? t('MusicBrainz ids added') : ''}
                {ids > 0 && it.cover ? ' · ' : ''}
                {it.cover ? t('Cover added') : ''}
              </small>
            )}
          </li>
        )
      })}
    </ul>
  )
}

function Copies({ album }: { album: TidyAlbum }) {
  const copies = album.detail.copies ?? []
  if (copies.length === 0) return null
  return (
    <div className="tidy-copies">
      <strong>{t('You have this album more than once')}</strong>
      <ul>
        {copies.map((c) => (
          <li key={c.folder}>
            <code>{c.folder}</code> · {c.quality === 'Unknown' ? t('quality unknown') : c.quality}
            {c.better ? ` · ${t('the better copy')}` : ''}
          </li>
        ))}
      </ul>
      <small>{t('Usherr never removes a copy by itself. Delete the one you don’t want when you’re ready.')}</small>
    </div>
  )
}

function howFound(source?: string) {
  if (source === 'library') return t('Already in your library')
  if (source === 'fingerprint') return t('Recognised by its sound')
  if (source === 'names') return t('Recognised by its names')
  return ''
}

function AlbumRow({ album, selected, onToggle, canPick }: { album: TidyAlbum; selected: boolean; onToggle: (id: number) => void; canPick: boolean }) {
  const [open, setOpen] = useState(false)
  const moved = album.oldFolder !== album.newFolder
  const renames = album.items.filter((i) => i.oldName !== i.newName).length
  const retags = album.items.filter((i) => (i.changes ?? []).length > 0).length
  const willChange = album.status === 'eligible' || album.status === 'applied' || album.status === 'applying'
  const summary = !willChange ? [] : [
    moved ? t('new folder') : '',
    renames > 0 ? plural(renames, '{n} new name', '{n} new names') : '',
    retags > 0 ? plural(retags, '{n} file with new tags', '{n} files with new tags') : '',
    album.detail.cover ? t('cover added') : '',
  ].filter(Boolean)
  return (
    <li className={`mi-row tidy-row st-${album.status}`}>
      {canPick && album.status === 'eligible' ? (
        <input type="checkbox" checked={selected} onChange={() => onToggle(album.id)} aria-label={t('Tidy {album}', { album: album.title })} />
      ) : (
        <span className="mi-ico">
          <Icon name={album.status === 'applied' ? 'check' : album.status === 'eligible' ? 'music' : 'info'} size={15} />
        </span>
      )}
      <div className="mi-main">
        <button type="button" className="tidy-toggle" aria-expanded={open} onClick={() => setOpen((v) => !v)}>
          <strong>
            {album.artist} – {album.title}
          </strong>
          <small>
            {summary.join(' · ')}
            {summary.length > 0 && howFound(album.detail.source) ? ' · ' : ''}
            {howFound(album.detail.source)}
          </small>
        </button>
        {album.reason && <small className="mi-why">{st(album.reason)}</small>}
        {open && (
          <div className="tidy-detail">
            {moved && (
              <p>
                <code className="tidy-old">{album.oldFolder}</code>
                <code className="tidy-new">{album.newFolder}</code>
              </p>
            )}
            <Copies album={album} />
            {album.items.length > 0 ? <FileChanges album={album} /> : !album.detail.copies?.length && <small>{t('Nothing changes in this folder.')}</small>}
          </div>
        )}
      </div>
    </li>
  )
}

// Usherr: "Tidy up your music", the Picard-style rename and re-tag. A check
// changes nothing; only what an administrator picks is applied, the originals
// are kept for 30 days, and it can be undone.
export default function MusicTidy() {
  const { on, loaded } = useModules()
  const toast = useToast()
  const confirm = useConfirm()
  const [over, setOver] = useState<TidyOverview | null>(null)
  const [plan, setPlan] = useState<TidyPlanDetail | null>(null)
  const [picked, setPicked] = useState<Set<number>>(new Set())
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [key, setKey] = useState('')

  const running = over?.job?.phase === 'running'
  const jobPlan = over?.job?.planId ?? 0
  const lastPlan = over?.plans.find((p) => p.status !== 'undone')?.id ?? 0
  const showPlan = jobPlan > 0 && over?.job?.kind === 'check' ? jobPlan : lastPlan

  const refresh = useCallback(async () => {
    try {
      setOver(await api.tidy())
    } catch (e) {
      setError(errorText(e))
    }
  }, [])

  useEffect(() => {
    void refresh()
  }, [refresh])

  useEffect(() => {
    if (!running) return
    const timer = setInterval(() => void refresh(), 1000)
    return () => clearInterval(timer)
  }, [running, refresh])

  // The plan on show, read again whenever a job ends.
  useEffect(() => {
    if (running || showPlan === 0) {
      if (showPlan === 0) setPlan(null)
      return
    }
    let live = true
    api
      .tidyPlan(showPlan)
      .then((p) => {
        if (!live) return
        setPlan(p)
        setPicked(new Set(p.albums.filter((a) => a.status === 'eligible').map((a) => a.id)))
      })
      .catch((e) => live && setError(errorText(e)))
    return () => {
      live = false
    }
  }, [running, showPlan])

  async function run(work: () => Promise<void>) {
    setBusy(true)
    setError('')
    try {
      await work()
    } catch (e) {
      setError(errorText(e))
    } finally {
      setBusy(false)
    }
  }

  const eligible = useMemo(() => plan?.albums.filter((a) => a.status === 'eligible') ?? [], [plan])
  const applied = useMemo(() => plan?.albums.filter((a) => a.status === 'applied' || a.status === 'applying') ?? [], [plan])
  const leftAlone = useMemo(() => plan?.albums.filter((a) => !['eligible', 'applied', 'applying', 'unchanged'].includes(a.status)) ?? [], [plan])
  const tidyAlready = useMemo(() => plan?.albums.filter((a) => a.status === 'unchanged') ?? [], [plan])
  const history = over?.plans.filter((p) => p.status === 'applied' || p.status === 'undone') ?? []
  const job = over?.job
  const pct = job && job.total > 0 ? Math.min(100, Math.round((job.done / job.total) * 100)) : 0

  function toggle(id: number) {
    setPicked((cur) => {
      const next = new Set(cur)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }

  async function apply() {
    if (!plan || picked.size === 0) return
    const ok = await confirm({
      title: plural(picked.size, 'Tidy {n} album?', 'Tidy {n} albums?'),
      body: <p>{t('New names and tags are written to your files. The originals are kept for {days} days, and you can undo this until then.', { days: over?.recycleDays ?? 30 })}</p>,
      confirmLabel: plural(picked.size, 'Tidy {n} album', 'Tidy {n} albums'),
    })
    if (!ok) return
    void run(async () => {
      await api.tidyApply(plan.plan.id, [...picked])
      await refresh()
    })
  }

  async function undo(id: number) {
    const ok = await confirm({
      title: t('Put everything back?'),
      body: <p>{t('The originals go back where they were, and the tidied copies are removed. A file you changed since stays as it is.')}</p>,
      confirmLabel: t('Undo'),
    })
    if (!ok) return
    void run(async () => {
      await api.tidyUndo(id)
      await refresh()
    })
  }

  async function emptyRecycle() {
    const ok = await confirm({
      title: t('Empty the kept originals?'),
      body: <p>{t('This frees {size}, but you can’t undo what was tidied after that.', { size: formatBytes(over?.recycleBytes ?? 0) })}</p>,
      confirmLabel: t('Empty'),
      danger: true,
    })
    if (!ok) return
    void run(async () => {
      const r = await api.tidyEmptyRecycle()
      toast.success(t('Freed {size}.', { size: formatBytes(r.freedBytes) }))
      await refresh()
    })
  }

  const working = job?.kind === 'check' ? t('Checking your music…') : job?.kind === 'apply' ? t('Tidying your albums…') : t('Putting everything back…')

  return (
    <div className="music-import import-page">
      <div className="page-header">
        <h1>{t('Tidy up my music')}</h1>
        <Link className="btn-link" to="/library?kind=music">
          <Icon name="music" size={15} /> {t('Back to Music')}
        </Link>
      </div>

      {loaded && !on('music') && (
        <div className="notice notice-warn" style={{ marginBottom: 18 }}>
          {tx('Music is switched off. {link} first.', { link: <Link to="/settings/modules">{t('Switch it on in Settings > Media types')}</Link> })}
        </div>
      )}

      <section className="card mi-start">
        <span className={`mi-disc${running ? ' spinning' : ''}`} aria-hidden="true">
          <KindGlyph kind="music" size={72} variant="solid" />
        </span>
        <div className="mi-start-text">
          <h2>{t('Neat names and correct tags')}</h2>
          <p>{tx('Usherr works out what each album really is, then shows you the new names and tags before anything changes. {safe}', { safe: <strong>{t('Checking changes nothing.')}</strong> })}</p>
          <p className="mi-folder-line">
            <Icon name="folder" size={15} /> {tx('Music folder: {path}', { path: <code>{over?.root || t('(not set yet)')}</code> })} <Link to="/settings/media">{t('Change')}</Link>
          </p>
          {over && !over.toolsReady && <p className="error-text">{t('This needs ffmpeg and ffprobe, which aren’t installed here.')}</p>}
        </div>
        <div className="mi-start-action">
          <button
            className="primary big btn-with-icon"
            data-walk="tidy-check"
            disabled={busy || running || !over?.toolsReady || !on('music')}
            onClick={() =>
              void run(async () => {
                await api.tidyCheck()
                await refresh()
              })
            }
          >
            <Icon name="search" size={18} /> {running && job?.kind === 'check' ? t('Checking…') : t('Check my music')}
          </button>
          <small>{t('Safe to run as often as you like.')}</small>
        </div>
      </section>

      <details className="card tidy-key" data-walk="tidy-key">
        <summary>{over?.hasKey ? t('AcoustID key (set)') : t('Recognise music by its sound (optional)')}</summary>
        <p>
          {tx('With a free AcoustID key, Usherr can recognise albums by listening to them, even when the names are a mess. Make an application at {link} and paste its key here.', {
            link: (
              <a href="https://acoustid.org/new-application" target="_blank" rel="noreferrer">
                acoustid.org/new-application
              </a>
            ),
          })}
        </p>
        <form
          className="lb-row"
          onSubmit={(e) => {
            e.preventDefault()
            void run(async () => {
              await api.saveAcoustID(key)
              setKey('')
              await refresh()
              toast.success(t('Saved.'))
            })
          }}
        >
          <input type="password" value={key} onChange={(e) => setKey(e.target.value)} placeholder={over?.hasKey ? t('Application key (kept)') : t('Application key')} aria-label={t('Application key')} autoComplete="off" spellCheck={false} disabled={busy} />
          <button type="submit" className="btn-with-icon" disabled={busy || (key.trim() === '' && !over?.hasKey)}>
            <Icon name="check" size={15} /> {key.trim() === '' && over?.hasKey ? t('Remove') : t('Save')}
          </button>
        </form>
      </details>

      {error && <p className="error-text">{st(error)}</p>}

      {running && job && (
        <section className="card mi-progress" aria-live="polite">
          <div className="mi-progress-top">
            <strong>{working}</strong>
            <span>{job.total > 0 ? plural(job.total, '{done} of {n} album', '{done} of {n} albums', { done: job.done }) : ''}</span>
          </div>
          <div className="bar active" role="progressbar" aria-valuenow={pct} aria-valuemin={0} aria-valuemax={100}>
            <span style={{ width: `${Math.max(4, pct)}%` }} />
          </div>
          {job.current && <small className="mi-hint">{tx('Now: {folder}', { folder: <code>{job.current}</code> })}</small>}
          {job.kind === 'check' && <small className="mi-hint">{t('The music database allows one lookup a second, so a big collection takes a while. You can leave this page, the check keeps going.')}</small>}
          <div className="row-actions">
            <button className="btn-with-icon" onClick={() => void run(async () => setOver(await api.tidyStop().then(() => api.tidy())))} disabled={busy}>
              <Icon name="stop" size={15} /> {t('Stop')}
            </button>
          </div>
        </section>
      )}

      {job?.phase === 'failed' && <p className="error-text">{t('It stopped: {error}.', { error: job.error ? st(job.error) : t('something went wrong') })}</p>}
      {job?.phase === 'stopped' && <p className="notice">{t('You stopped it. Albums it had already finished stay that way, and no album is left half done.')}</p>}
      {job?.kind === 'undo' && job.phase === 'done' && (job.undo ?? []).some((u) => (u.problems ?? []).length > 0) && (
        <div className="notice notice-warn">
          <strong>{t('Not everything could be put back:')}</strong>
          <ul>
            {(job.undo ?? []).flatMap((u) => (u.problems ?? []).map((p) => <li key={u.albumRow + p}>{u.title}: {st(p)}</li>))}
          </ul>
        </div>
      )}

      {!running && plan && (
        <>
          <div className="mi-summary">
            <div className="mi-stat good">
              <b>{eligible.length}</b>
              <span>{t('albums to tidy')}</span>
            </div>
            <div className="mi-stat">
              <b>{plan.unchanged}</b>
              <span>{t('already tidy')}</span>
            </div>
            <div className="mi-stat warn">
              <b>{leftAlone.length}</b>
              <span>{t('left alone')}</span>
            </div>
            {applied.length > 0 && (
              <div className="mi-stat good">
                <b>{applied.length}</b>
                <span>{t('tidied')}</span>
              </div>
            )}
          </div>

          {eligible.length > 0 && (
            <fieldset className="group mi-col found" data-walk="tidy-ready">
              <legend>
                <Icon name="sparkles" size={14} /> {t('Ready to tidy ({n})', { n: eligible.length })}
              </legend>
              <div className="row-actions tidy-actions">
                <button className="primary btn-with-icon" disabled={busy || picked.size === 0} onClick={() => void apply()}>
                  <Icon name="check" size={15} /> {plural(picked.size, 'Tidy {n} album', 'Tidy {n} albums')}
                </button>
                <button type="button" className="btn-sm" onClick={() => setPicked(new Set(eligible.map((a) => a.id)))}>
                  {t('Select all')}
                </button>
                <button type="button" className="btn-sm" onClick={() => setPicked(new Set())}>
                  {t('Select none')}
                </button>
              </div>
              <ul className="mi-list">
                {eligible.map((a) => (
                  <AlbumRow key={a.id} album={a} selected={picked.has(a.id)} onToggle={toggle} canPick />
                ))}
              </ul>
            </fieldset>
          )}

          {eligible.length === 0 && applied.length === 0 && leftAlone.length === 0 && <p className="notice">{t('Everything is already tidy. Nothing to do.')}</p>}

          {applied.length > 0 && (
            <fieldset className="group mi-col found">
              <legend>
                <Icon name="check" size={14} /> {t('Tidied ({n})', { n: applied.length })}
              </legend>
              <ul className="mi-list">
                {applied.map((a) => (
                  <AlbumRow key={a.id} album={a} selected={false} onToggle={toggle} canPick={false} />
                ))}
              </ul>
            </fieldset>
          )}

          {leftAlone.length > 0 && (
            <fieldset className="group mi-col lost">
              <legend>
                <Icon name="info" size={14} /> {t('Left alone ({n})', { n: leftAlone.length })}
              </legend>
              <ul className="mi-list">
                {leftAlone.map((a) => (
                  <AlbumRow key={a.id} album={a} selected={false} onToggle={toggle} canPick={false} />
                ))}
              </ul>
            </fieldset>
          )}

          {tidyAlready.length > 0 && (
            <fieldset className="group mi-col">
              <legend>
                <Icon name="info" size={14} /> {t('Tidy, but you have them twice ({n})', { n: tidyAlready.length })}
              </legend>
              <ul className="mi-list">
                {tidyAlready.map((a) => (
                  <AlbumRow key={a.id} album={a} selected={false} onToggle={toggle} canPick={false} />
                ))}
              </ul>
            </fieldset>
          )}
        </>
      )}

      {(history.length > 0 || (over?.recycleBytes ?? 0) > 0) && (
        <fieldset className="group tidy-history" data-walk="tidy-history">
          <legend>
            <Icon name="clock" size={14} /> {t('Earlier tidying')}
          </legend>
          <ul className="mi-list">
            {history.map((p) => (
              <li key={p.id} className="mi-row">
                <div className="mi-main">
                  <strong>{plural(p.applied, '{n} album tidied', '{n} albums tidied')}</strong>
                  <small>
                    {new Date(p.appliedAt || p.createdAt).toLocaleString()}
                    {p.status === 'undone' ? ` · ${t('undone')}` : ''}
                    {p.recycled ? ` · ${t('originals no longer kept')}` : ''}
                  </small>
                </div>
                {p.status === 'applied' && p.applied > 0 && (
                  <button className="btn-sm btn-with-icon" disabled={busy || running} onClick={() => void undo(p.id)}>
                    <Icon name="refresh" size={14} /> {t('Undo')}
                  </button>
                )}
              </li>
            ))}
          </ul>
          {(over?.recycleBytes ?? 0) > 0 && (
            <p className="tidy-recycle">
              {t('Kept originals use {size}. They are removed after {days} days.', { size: formatBytes(over?.recycleBytes ?? 0), days: over?.recycleDays ?? 30 })}{' '}
              <button className="btn-sm btn-with-icon" disabled={busy || running} onClick={() => void emptyRecycle()}>
                <Icon name="trash" size={14} /> {t('Empty now')}
              </button>
            </p>
          )}
        </fieldset>
      )}
    </div>
  )
}
