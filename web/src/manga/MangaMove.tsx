// Usherr: Settings > Manga > Bring manga over from Comics. Look first: every new file
// name is shown, and nothing changes until you press Move. Undo puts it all back.
// Files are never deleted or overwritten.
import { useCallback, useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { useConfirm } from '../components/ConfirmProvider'
import { useToast } from '../components/Toast'
import { t } from '../i18n'
import MangaCover from './MangaCover'
import { mangaApi, type MangaFound, type MangaMoveCandidate, type MangaMoveFile, type MangaMovePlan, type MangaMoveState } from './mangaApi'

function stayWhy(f: MangaMoveFile): string {
  switch (f.why) {
    case 'identical':
      return t('The same file as one that moves. It stays where it is: remove it yourself if you like.')
    case 'copy':
      return t('Another copy of this volume. It stays where it is.')
    case 'exists':
      return t('Manga already has this volume. This one stays where it is.')
    case 'unread':
      return t('The name doesn’t say which volume, so it stays where it is.')
    case 'changed':
      return t('It’s gone or has changed since Usherr looked, so it stays where it is.')
  }
  return t('It stays where it is.')
}

function fileName(p: string) {
  return p.split(/[\\/]/).pop() ?? p
}

export default function MangaMove() {
  const toast = useToast()
  const confirm = useConfirm()
  const [state, setState] = useState<MangaMoveState | null>(null)
  const [choosing, setChoosing] = useState<MangaMoveCandidate | null>(null)
  const [plan, setPlan] = useState<MangaMovePlan | null>(null)
  const [ticked, setTicked] = useState<Set<number>>(new Set())
  const [busy, setBusy] = useState(false)

  const load = useCallback(() => {
    mangaApi
      .moveState()
      .then(setState)
      .catch(() => undefined)
  }, [])
  useEffect(load, [load])
  const running = state?.moves.some((m) => m.status === 'applying' || m.status === 'undoing')
  useEffect(() => {
    if (!running) return
    const tm = window.setInterval(load, 1500)
    return () => window.clearInterval(tm)
  }, [running, load])

  if (!state) return null
  const moves = state.moves.filter((m) => m.status !== 'undone').slice(0, 8)
  if (state.candidates.length === 0 && moves.length === 0) return null
  const empty = state.candidates.filter((c) => c.files === 0)
  const withFiles = state.candidates.filter((c) => c.files > 0)

  const run = (fn: () => Promise<{ message: string }>) => {
    setBusy(true)
    fn()
      .then((r) => toast.success(r.message))
      .catch((e: Error) => toast.error(e.message))
      .finally(() => {
        setBusy(false)
        load()
      })
  }

  const moveTicked = () => {
    run(() => mangaApi.moveEmpty([...ticked]))
    setTicked(new Set())
  }

  const preview = (c: MangaMoveCandidate, body: { anilistId?: number; title?: string }) => {
    setBusy(true)
    mangaApi
      .movePreview({ comicSeriesId: c.id, ...body })
      .then((p) => {
        setPlan(p)
        setChoosing(null)
      })
      .catch((e: Error) => toast.error(e.message))
      .finally(() => setBusy(false))
  }

  const undo = async (id: number, name: string) => {
    const ok = await confirm({ title: t('Put {name} back in Comics?', { name }), body: t('Its files go back to where they were, and reading places go with them.'), confirmLabel: t('Put it back') })
    if (ok) run(() => mangaApi.moveUndo(id))
  }

  return (
    <section className="mg-card mg-stack" data-walk="manga-move">
      <div>
        <h2>{t('Bring manga over from Comics')}</h2>
        <p className="mg-hint">{t('Series Comics knows as manga. You see every new file name first. Files are moved and never deleted, and a move can be undone. Where you’d got to and your bookmarks come too.')}</p>
      </div>
      {withFiles.length > 0 && (
        <ul className="mg-scan">
          {withFiles.map((c) => (
            <li key={c.id}>
              <div>
                <b>{c.name}</b>
                <div className="mg-hint">{[c.publisher, t('{n} files', { n: c.files }), t('{n} issues', { n: c.issues }), c.magazine ? t('Looks like a magazine: best left in Comics') : ''].filter(Boolean).join(' · ')}</div>
              </div>
              <button className={`btn-sm${c.magazine ? '' : ' primary'}`} disabled={busy} onClick={() => setChoosing(c)}>
                {t('Look first')}
              </button>
            </li>
          ))}
        </ul>
      )}
      {empty.length > 0 && (
        <div className="mg-stack">
          <h3>{t('No files yet')}</h3>
          <p className="mg-hint">{t('Nothing to rename, so a tick is enough. They come over with what you wanted, and can be put back.')}</p>
          <ul className="mg-scan">
            {empty.map((c) => (
              <li key={c.id}>
                <label className="mg-tools" style={{ flex: 1 }}>
                  <input
                    type="checkbox"
                    checked={ticked.has(c.id)}
                    onChange={(e) => {
                      const next = new Set(ticked)
                      if (e.target.checked) next.add(c.id)
                      else next.delete(c.id)
                      setTicked(next)
                    }}
                  />
                  <span>
                    <b>{c.name}</b>
                    <span className="mg-hint"> {[t('{n} issues', { n: c.issues }), c.magazine ? t('Looks like a magazine: best left in Comics') : ''].filter(Boolean).join(' · ')}</span>
                  </span>
                </label>
              </li>
            ))}
          </ul>
          <div>
            <button className="btn-sm" disabled={busy || ticked.size === 0} onClick={moveTicked}>
              {ticked.size === 1 ? t('Bring over 1 series') : t('Bring over {n} series', { n: ticked.size })}
            </button>
          </div>
        </div>
      )}
      {moves.length > 0 && (
        <div className="mg-stack">
          <h3>{t('Brought over')}</h3>
          <ul className="mg-scan">
            {moves.map((m) => (
              <li key={m.id}>
                <div>
                  <b>{m.title}</b>
                  <div className="mg-hint">
                    {m.status === 'applying' && t('Moving: {done} of {total}', { done: m.done, total: m.total })}
                    {m.status === 'undoing' && t('Putting it back…')}
                    {m.status === 'applied' && [t('{n} files moved', { n: m.moved }), m.stayed ? t('{n} stayed where they were', { n: m.stayed }) : ''].filter(Boolean).join(' · ')}
                    {m.status === 'stopped' && t('It stopped part way: {why} {n} files moved, and they can be put back.', { why: m.problem ?? '', n: m.moved })}
                  </div>
                  {(m.undoLeft ?? []).map((f) => (
                    <div key={f} className="mg-hint mg-mono" style={{ overflowWrap: 'anywhere' }}>
                      {t('Couldn’t go back: {file}', { file: f })}
                    </div>
                  ))}
                </div>
                <div className="mg-tools">
                  {m.mangaSeriesId > 0 && m.status === 'applied' && (
                    <Link className="btn-sm" to={`/manga/${m.mangaSeriesId}`}>
                      {t('Open')}
                    </Link>
                  )}
                  {(m.status === 'applied' || m.status === 'stopped') && (
                    <button className="btn-sm" disabled={busy} onClick={() => void undo(m.id, m.comicName)}>
                      {t('Undo')}
                    </button>
                  )}
                </div>
              </li>
            ))}
          </ul>
        </div>
      )}
      {choosing && <ChooseMatch c={choosing} busy={busy} onClose={() => setChoosing(null)} onChoose={(body) => preview(choosing, body)} />}
      {plan && (
        <Preview
          plan={plan}
          busy={busy}
          onClose={() => setPlan(null)}
          onApply={() => {
            const id = plan.id
            setPlan(null)
            run(() => mangaApi.moveApply(id))
          }}
        />
      )}
    </section>
  )
}

// Which series is it? Always a choice, never a guess; the comic's name will do too.
function ChooseMatch({ c, busy, onClose, onChoose }: { c: MangaMoveCandidate; busy: boolean; onClose: () => void; onChoose: (body: { anilistId?: number; title?: string }) => void }) {
  const [list, setList] = useState<MangaFound[] | null>(null)
  const [problem, setProblem] = useState('')
  useEffect(() => {
    mangaApi
      .lookup(c.name)
      .then((r) => {
        setList(r.results)
        setProblem(r.problem ?? '')
      })
      .catch((e: Error) => setProblem(e.message))
  }, [c.name])
  return (
    <div className="mg-modal" role="dialog" aria-modal="true" aria-label={t('Which series is {name}?', { name: c.name })} onClick={onClose}>
      <div className="mg-modal-box" onClick={(e) => e.stopPropagation()}>
        <header>
          <h2>{t('Which series is {name}?', { name: c.name })}</h2>
          <button className="btn-sm" onClick={onClose}>
            {t('Close')}
          </button>
        </header>
        {problem && <p className="mg-hint">{problem}</p>}
        {!list && !problem && <p className="mg-hint">{t('Searching…')}</p>}
        <div className="mg-results">
          {list?.map((f) => (
            <div className="mg-res" key={f.anilistId}>
              <MangaCover title={f.title} src={f.coverUrl} small />
              <div>
                <b>{f.title}</b>
                <div className="mg-hint">{[f.author, f.startYear || '', f.volumes ? t('{n} volumes', { n: f.volumes }) : ''].filter(Boolean).join(' · ')}</div>
              </div>
              <button className="btn-sm primary btn-wide" disabled={busy} onClick={() => onChoose({ anilistId: f.anilistId })}>
                {t('This one')}
              </button>
            </div>
          ))}
        </div>
        <div>
          <button className="btn-sm" disabled={busy} onClick={() => onChoose({ title: c.name })}>
            {t('Keep the name {name}', { name: c.name })}
          </button>
        </div>
      </div>
    </div>
  )
}

function Preview({ plan, busy, onClose, onApply }: { plan: MangaMovePlan; busy: boolean; onClose: () => void; onApply: () => void }) {
  const move = plan.files.filter((f) => f.action === 'move')
  const stay = plan.files.filter((f) => f.action !== 'move')
  return (
    <div className="mg-modal" role="dialog" aria-modal="true" aria-label={t('Preview: {name}', { name: plan.title })} onClick={onClose}>
      <div className="mg-modal-box" onClick={(e) => e.stopPropagation()}>
        <header>
          <h2>{t('Preview: {name}', { name: plan.title })}</h2>
          <button className="btn-sm" onClick={onClose}>
            {t('Close')}
          </button>
        </header>
        <p className="mg-hint">{t('Nothing has changed yet. They’ll go in {folder}. Volumes: {n}.', { folder: plan.folder, n: plan.volumes })}</p>
        {move.length > 0 && (
          <div className="mg-stack">
            <h3>{move.length === 1 ? t('1 file moves') : t('{n} files move', { n: move.length })}</h3>
            <ul className="mg-rel">
              {move.map((f) => (
                <li key={f.from}>
                  <div>
                    <span className="title">{fileName(f.to)}</span>
                    <span className="mg-hint mg-mono" style={{ overflowWrap: 'anywhere' }}>
                      {t('was {name}', { name: fileName(f.from) })}
                    </span>
                  </div>
                </li>
              ))}
            </ul>
          </div>
        )}
        {stay.length > 0 && (
          <div className="mg-stack">
            <h3>{stay.length === 1 ? t('1 file stays') : t('{n} files stay', { n: stay.length })}</h3>
            <ul className="mg-rel">
              {stay.map((f) => (
                <li key={f.from}>
                  <div>
                    <span className="title">{fileName(f.from)}</span>
                    <span className="mg-hint">{stayWhy(f)}</span>
                  </div>
                </li>
              ))}
            </ul>
          </div>
        )}
        {move.length === 0 && stay.length === 0 && <p className="mg-hint">{t('It has no files. Its volumes come over, as they were wanted.')}</p>}
        <div className="mg-tools">
          <button className="btn-sm primary" disabled={busy} onClick={onApply}>
            {t('Move')}
          </button>
          <button className="btn-sm" onClick={onClose}>
            {t('Not now')}
          </button>
        </div>
      </div>
    </div>
  )
}
