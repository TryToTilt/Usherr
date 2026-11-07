// Usherr: a manga series: what it is, one sentence on what's happening and why,
// and its volumes, each a tap away from the reader.
import { lazy, Suspense, useCallback, useEffect, useMemo, useState } from 'react'
import { Link, useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { can, isAdmin } from '../api'
import { useAuth } from '../AuthContext'
import { KeepButton, useKept } from '../bookshelf/Keep'
import { keptId } from '../bookshelf/offline'
import { useConfirm } from '../components/ConfirmProvider'
import { useToast } from '../components/Toast'
import { formatBytes } from '../format'
import { t } from '../i18n'
import { useLive } from '../useLive'
import MangaCover from './MangaCover'
import { FoundTile, MangaPortal, useOnePress, type OnePress } from './MangaDiscover'
import { mangaApi, type MangaAbout, type MangaDetail, type MangaFound, type MangaRelease, type MangaReview, type MangaVolume } from './mangaApi'
import MangaChapters from './MangaChapters'
import { chapterStatus, chapterWhy, FORMAT_LABEL, mangaStatus, mangaWhen, mangaWhy, stateLabel } from './mangaStatus'
import './manga.css'

const MangaReader = lazy(() => import('./MangaReader'))

const SHOW_FIRST = 14

export default function MangaSeries() {
  const { id = '' } = useParams()
  const { user } = useAuth()
  const admin = isAdmin(user)
  const toast = useToast()
  const confirm = useConfirm()
  const nav = useNavigate()
  const [d, setD] = useState<MangaDetail | null>(null)
  const [error, setError] = useState('')
  const [whyOpen, setWhyOpen] = useState(false)
  const [showAll, setShowAll] = useState(false)
  const [open, setOpen] = useState<number | null>(null)
  const [pick, setPick] = useState<MangaVolume | null>(null)
  const [sourcesOpen, setSourcesOpen] = useState(false)
  const [busy, setBusy] = useState(false)
  const [addingVolume, setAddingVolume] = useState(false)
  const [newVolume, setNewVolume] = useState('')
  const [reading, setReading] = useState<{ id: number; chapter: boolean } | null>(null)
  const [tab, setTab] = useState<'volumes' | 'chapters'>('volumes')
  const [kept] = useKept()
  const keptIds = useMemo(() => new Set(kept.map((k) => k.id)), [kept])

  const [about, setAbout] = useState<MangaAbout | null>(null)
  const [params, setParams] = useSearchParams()
  const canAdd = admin || can(user, 'manga')
  const press = useOnePress()

  const load = useCallback(() => {
    mangaApi
      .get(id)
      .then((x) => {
        setD(x)
        setError('')
      })
      .catch((e: Error) => setError(e.message))
  }, [id])
  useEffect(load, [load])
  useLive(load, 30_000, true)

  // Genres, the score and what's related come from AniList, kept there and not here.
  useEffect(() => {
    let gone = false
    setAbout(null)
    mangaApi
      .about(id)
      .then((x) => {
        if (!gone) setAbout(x)
      })
      .catch(() => undefined)
    return () => {
      gone = true
    }
  }, [id])

  // Opened from Home's Continue reading: straight into the volume or chapter.
  const readParam = Number(params.get('read')) || 0
  const readChapter = params.get('chapter') === '1'
  useEffect(() => {
    if (!readParam) return
    setReading({ id: readParam, chapter: readChapter })
    setParams({}, { replace: true })
  }, [readParam, readChapter, setParams])

  if (error && !d) {
    return (
      <div className="page-pad mg-page">
        <p className="error-text">{error}</p>
        <div>
          <Link className="btn-sm" to="/manga">
            {t('Back to Manga')}
          </Link>
        </div>
      </div>
    )
  }
  if (!d) return <div className="page-pad"><div className="skeleton" style={{ height: 320 }} /></div>

  const act = async (fn: () => Promise<unknown>, done?: string) => {
    setBusy(true)
    try {
      await fn()
      if (done) toast.success(done)
      load()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  const s = d.series
  const status = mangaStatus(s, d.sources, d.problems)
  const when = mangaWhen(d.search)
  const why = [...(d.search.last && (s.total === 0 || s.have < s.total) ? mangaWhy(d.search.last.summary, d.search.number) : []), ...chapterWhy(d.chapters, s.licensedEn === 1)]
  const showChapters = d.chapters.allowed || d.chapters.total > 0
  const rtl = s.format === 'manga' || s.format === 'oneshot'
  const volumes = d.volumes
  const shown = volumes.length > SHOW_FIRST && !showAll ? volumes.slice(0, SHOW_FIRST) : volumes
  // The main button: your place, or the next volume, or from the beginning.
  const place = d.reading?.[0]
  const firstHere = volumes.find((v) => v.state === 'downloaded')
  const readAll = !place && !!firstHere && volumes.every((v) => v.state !== 'downloaded' || v.read)
  const placeLabel = place?.chapterId ? (place.page > 0 ? t('Continue Chapter {n}, page {page}', { n: place.number, page: place.page }) : t('Start Chapter {n}', { n: place.number })) : place ? (place.page > 0 ? t('Continue Volume {n}, page {page}', { n: place.number, page: place.page }) : t('Start Volume {n}', { n: place.number })) : ''
  const mainLabel = place ? placeLabel : readAll ? t('Read again') : t('Start reading')
  // With nothing but chapters here, they're where reading starts.
  const mainChapter = place ? !!place.chapterId : !firstHere && !!d.chapters.firstHave
  const mainVolume = place ? (place.chapterId ?? place.volumeId) : (firstHere?.id ?? d.chapters.firstHave)
  const packs = d.reviews.filter((r) => !r.volumeId)
  const others = d.aliases.map((a) => a.alias)
  const haveFiles = s.have > 0 || d.chapters.have > 0

  const remove = async () => {
    const ok = await confirm({ title: t('Remove {title}?', { title: s.title }), body: t('It leaves the library. Its files stay where they are.'), confirmLabel: t('Remove'), danger: true })
    if (!ok) return
    await act(() => mangaApi.remove(s.id))
    nav('/manga')
  }

  const addVolume = async () => {
    const n = newVolume.trim()
    if (!n) return
    await act(() => mangaApi.addVolume(s.id, n), t('Volume {n} added, and wanted.', { n }))
    setNewVolume('')
    setAddingVolume(false)
  }

  return (
    <div className="page-pad mg-page">
      <div className="mg-head">
        <Link className="btn-sm" to="/manga">
          {`‹ ${t('Manga')}`}
        </Link>
      </div>
      <section className="mg-hero">
        <span className="mg-slot">
          <MangaCover title={s.title} src={s.coverUrl} />
        </span>
        <div className="info">
          <div className="ttl">
            <h1>{s.title}</h1>
            {(s.author || s.startYear > 0) && <p className="mg-hint">{[s.author, s.startYear || ''].filter(Boolean).join(', ')}</p>}
            <div className="mg-chips">
              <span className="mg-pill manga">{FORMAT_LABEL[s.format]?.() ?? s.format}</span>
              {stateLabel(s.status) && <span className="mg-pill">{stateLabel(s.status)}</span>}
              {about?.score ? <span className="mg-pill">{t('Score {n}%', { n: about.score })}</span> : null}
              <span className="mg-pill">{rtl ? t('Right to left') : t('Top to bottom')}</span>
              {s.licensedEn === 1 ? (
                <span className="mg-pill good">{t('Official: {publisher}', { publisher: s.publisher })}</span>
              ) : d.sources.fan || s.following ? (
                <span className="mg-pill warn">{t('Fan scans, your choice')}</span>
              ) : (
                <span className="mg-pill warn">{t('No official edition known')}</span>
              )}
            </div>
          </div>
          <div className="rest">
            {about && about.genres.length > 0 && (
              <div className="mg-chips">
                {about.genres.map((g) =>
                  canAdd ? (
                    <Link key={g} className="mg-pill mg-pill-link" to={`/manga/discover?genre=${encodeURIComponent(g)}`}>
                      {g}
                    </Link>
                  ) : (
                    <span key={g} className="mg-pill">
                      {g}
                    </span>
                  ),
                )}
              </div>
            )}
            {s.description && <p className="mg-desc">{s.description}</p>}
            {others.length > 0 && <p className="mg-hint">{t('Other titles: {titles}', { titles: others.join(', ') })}</p>}
            <div className="mg-well mg-status" data-walk="manga-status">
              <p>{status.line}</p>
              {d.chapters.follow && tab !== 'chapters' && <span className="mg-hint">{chapterStatus(d.chapters)}</span>}
              {when && <span className="mg-hint">{when}</span>}
              {why.length > 0 && (
                <div>
                  <button className="btn-sm" aria-expanded={whyOpen} onClick={() => setWhyOpen(!whyOpen)}>
                    {whyOpen ? t('Hide') : t('Why nothing yet?')}
                  </button>
                  {whyOpen && (
                    <ul className="mg-why">
                      {why.map((w) => (
                        <li key={w}>{w}</li>
                      ))}
                    </ul>
                  )}
                </div>
              )}
              {!d.search.last && s.wanted > 0 && s.downloading === 0 && <span className="mg-hint">{t('Usherr hasn’t searched yet. The next automatic search will look.')}</span>}
              {status.askFan && admin && (
                <div className="mg-ask" style={{ marginTop: 6 }}>
                  <button className="btn-sm primary" disabled={busy} onClick={() => void act(() => mangaApi.update(s.id, { sourceMode: 'fan' }), t('Fan scans allowed for this series only.'))}>
                    {t('Allow fan scans for this series')}
                  </button>
                  <span className="mg-hint">{t('or keep waiting for an official edition')}</span>
                </div>
              )}
            </div>
            <div className="mg-tools">
              {haveFiles && mainVolume && (
                <button className="btn-sm primary" data-walk="manga-read" onClick={() => setReading({ id: mainVolume, chapter: mainChapter })}>
                  {mainLabel}
                </button>
              )}
              {s.buyUrl && (
                <a className="btn-sm" href={s.buyUrl} target="_blank" rel="noreferrer">
                  {t('Buy it from {publisher}', { publisher: s.publisher })}
                </a>
              )}
              {admin && (
                <>
                  <button className="btn-sm" disabled={busy} onClick={() => void act(() => mangaApi.search(s.id), t('Looking now. This page updates as things are found.'))}>
                    {t('Search now')}
                  </button>
                  <button className="btn-sm" onClick={() => setSourcesOpen(true)}>
                    {t('Sources for this series')}
                  </button>
                  <button className="btn-sm" disabled={busy} onClick={() => void act(() => mangaApi.refresh(s.id), t('Refreshed.'))}>
                    {t('Refresh')}
                  </button>
                  <button className="btn-sm" disabled={busy} onClick={() => void remove()}>
                    {t('Remove')}
                  </button>
                </>
              )}
            </div>
          </div>
        </div>
      </section>

      {admin && packs.length > 0 && (
        <section className="mg-card mg-stack">
          <h2>{t('Packs that might be what you want')}</h2>
          <p className="mg-hint">{t('A pack fills every volume it holds with one download. Usherr doesn’t take them by itself, because they’re big.')}</p>
          <Reviews list={packs} onChange={load} />
        </section>
      )}

      {showChapters && (
        <div className="seg" role="group" aria-label={t('Show')} style={{ alignSelf: 'flex-start' }}>
          <button className={tab === 'volumes' ? 'active' : ''} aria-pressed={tab === 'volumes'} onClick={() => setTab('volumes')}>
            {t('Volumes')}
          </button>
          <button className={tab === 'chapters' ? 'active' : ''} aria-pressed={tab === 'chapters'} onClick={() => setTab('chapters')}>
            {t('Chapters')}
          </button>
        </div>
      )}
      {tab === 'chapters' && showChapters && <MangaChapters series={s} volumes={volumes} admin={admin} onChange={load} onRead={(id, chapter) => setReading({ id, chapter })} />}
      {(tab === 'volumes' || !showChapters) && (
      <section className="mg-stack">
        <div className="mg-head">
          <h2>{volumes.length === 1 ? t('1 volume') : t('{n} volumes', { n: volumes.length })}</h2>
          {admin && (
            <div className="mg-tools">
              {addingVolume ? (
                <form
                  className="mg-tools"
                  onSubmit={(e) => {
                    e.preventDefault()
                    void addVolume()
                  }}
                >
                  <input className="mg-number" autoFocus inputMode="decimal" value={newVolume} onChange={(e) => setNewVolume(e.target.value)} aria-label={t('Volume number')} placeholder={t('Number')} />
                  <button className="btn-sm primary" type="submit" disabled={busy || !newVolume.trim()}>
                    {t('Add')}
                  </button>
                  <button className="btn-sm" type="button" onClick={() => setAddingVolume(false)}>
                    {t('Cancel')}
                  </button>
                </form>
              ) : (
                <button className="btn-sm" onClick={() => setAddingVolume(true)}>
                  {t('New volume')}
                </button>
              )}
            </div>
          )}
        </div>
        {volumes.length === 0 && <p className="mg-hint">{t('No volumes are known yet. Usherr adds them when a release names one, or you can add one yourself.')}</p>}
        <div className="mg-list">
          {shown.map((v) => (
            <VolumeRow key={v.id} v={v} title={s.title} cover={s.coverUrl} admin={admin} open={open === v.id} busy={busy} reviews={d.reviews.filter((r) => r.volumeId === v.id)} onToggle={() => setOpen(open === v.id ? null : v.id)} onPick={() => setPick(v)} onAct={act} onChange={load} onRead={() => setReading({ id: v.id, chapter: false })} seriesTitle={s.title} keptIds={keptIds} />
          ))}
          {shown.length < volumes.length && (
            <div>
              <button className="btn-sm" onClick={() => setShowAll(true)}>
                {t('Show all {n}', { n: volumes.length })}
              </button>
            </div>
          )}
        </div>
      </section>
      )}

      <FoundShelf title={t('Related series')} list={about?.related} press={press} canAdd={canAdd} />
      <FoundShelf title={t('More like this')} list={about?.similar} press={press} canAdd={canAdd} />

      {reading !== null && (
        <Suspense fallback={null}>
          <MangaReader
            volumeId={reading.id}
            chapter={reading.chapter}
            onClose={() => {
              setReading(null)
              // What was saved on closing is on its way: ask after it has landed.
              window.setTimeout(load, 400)
            }}
          />
        </Suspense>
      )}
      {pick && <MangaPortal><ChooseRelease v={pick} onClose={() => setPick(null)} onDone={load} /></MangaPortal>}
      {sourcesOpen && <MangaPortal><SeriesSources mode={s.sourceMode} onClose={() => setSourcesOpen(false)} onSave={(m) => act(() => mangaApi.update(s.id, { sourceMode: m }), t('Saved for this series.')).then(() => setSourcesOpen(false))} /></MangaPortal>}
    </div>
  )
}

// A row of series from AniList. Someone who can't add series sees those already here.
function FoundShelf({ title, list, press, canAdd }: { title: string; list?: MangaFound[]; press: OnePress; canAdd: boolean }) {
  const shown = (list ?? []).filter((f) => canAdd || f.inLibrary)
  if (shown.length === 0) return null
  return (
    <section className="mg-stack" aria-label={title}>
      <h2>{title}</h2>
      <div className="mg-shelf">
        {shown.map((f) => (
          <FoundTile key={f.anilistId} f={f} press={press} canAdd={canAdd} />
        ))}
      </div>
    </section>
  )
}

function volumePill(v: MangaVolume): { label: string; tone: string } {
  switch (v.state) {
    case 'downloaded':
      return { label: t('On disk'), tone: 'good' }
    case 'downloading':
      return { label: t('Downloading'), tone: 'accent' }
    case 'waiting':
      return { label: t('Waiting for you'), tone: 'warn' }
  }
  return v.wanted ? { label: t('Wanted'), tone: '' } : { label: t('Not wanted'), tone: '' }
}

function VolumeRow({ v, title, cover, admin, open, busy, reviews, onToggle, onPick, onAct, onChange, onRead, seriesTitle, keptIds }: { v: MangaVolume; title: string; cover?: string; admin: boolean; open: boolean; busy: boolean; reviews: MangaReview[]; onToggle: () => void; onPick: () => void; onAct: (fn: () => Promise<unknown>, done?: string) => Promise<void>; onChange: () => void; onRead: () => void; seriesTitle: string; keptIds: Set<string> }) {
  const pill = volumePill(v)
  const name = t('Volume {n}', { n: v.number })
  const here = v.state === 'downloaded'
  const sub = reviews.length > 0 ? (reviews.length === 1 ? t('1 release to look at') : t('{n} releases to look at', { n: reviews.length })) : v.page ? t('Page {page} of {pages}', { page: v.page, pages: v.pages ?? 0 }) : v.fileSize ? formatBytes(v.fileSize) : ''
  const hasMore = here || admin
  return (
    <div className="mg-vol">
      <div className="mg-vol-line">
        {/* A volume that's here opens in the reader; the rest open their choices. */}
        <button className="row" onClick={here ? onRead : onToggle} aria-expanded={!here && hasMore ? open : undefined} disabled={!hasMore} aria-label={here ? t('Read Volume {n}', { n: v.number }) : undefined}>
          <MangaCover title={title} src={cover} small />
          <span style={{ minWidth: 0 }}>
            <span className="n">{name}</span>
            {sub && <span className="sub">{sub}</span>}
          </span>
          <span className={`mg-pill ${pill.tone}`}>{pill.label}</span>
        </button>
        {v.read && (
          <span className="mg-tick" role="img" aria-label={t('Read')} title={t('Read')}>
            ✓
          </span>
        )}
        {here && (
          <button className="btn-sm" onClick={onToggle} aria-expanded={open} aria-label={t('More for Volume {n}', { n: v.number })}>
            ⋯
          </button>
        )}
      </div>
      {open && hasMore && (
        <div className="mg-vol-more">
          {reviews.length > 0 && admin && <Reviews list={reviews} onChange={onChange} />}
          <div className="mg-tools">
            {here && (
              <>
                <button className="btn-sm" disabled={busy} onClick={() => void onAct(() => mangaApi.markRead(v.id, !v.read))}>
                  {v.read ? t('Mark unread') : t('Mark read')}
                </button>
                {!v.read && (
                  <button className="btn-sm" disabled={busy} onClick={() => void onAct(() => mangaApi.markRead(v.id, true, true))}>
                    {t('Mark read up to here')}
                  </button>
                )}
                <a className="btn-sm" href={mangaApi.fileUrl(v.id)} download>
                  {t('Save the file')}
                </a>
                {!/\.pdf$/i.test(v.filePath ?? '') && (
                  <KeepButton
                    kind="volume"
                    id={v.id}
                    title={`${seriesTitle}, ${name}`}
                    open={`/bookshelf/manga/${v.id}`}
                    kept={keptIds.has(keptId('volume', v.id))}
                    count={() =>
                      mangaApi.pages(v.id).then((r) => {
                        if (r.pdf || !r.pages) throw new Error(t('This volume is a PDF, which can’t be kept on a device yet.'))
                        return r.pages
                      })
                    }
                    compact
                  />
                )}
              </>
            )}
            {admin && v.state !== 'downloaded' && v.state !== 'downloading' && (
              <>
                <button
                  className="btn-sm"
                  disabled={busy}
                  onClick={() =>
                    void onAct(async () => {
                      const r = await mangaApi.searchVolume(v.id)
                      if (r.grabbed) return
                      const lines = mangaWhy(r.summary, v.number)
                      throw new Error(lines.join(' ') || t('Nothing suitable was found right now.'))
                    }, t('Found it. It’s downloading now.'))
                  }
                >
                  {t('Search now')}
                </button>
                <button className="btn-sm" onClick={onPick}>
                  {t('Choose a release')}
                </button>
                <button className="btn-sm" disabled={busy} onClick={() => void onAct(() => mangaApi.setWanted([v.id], !v.wanted))}>
                  {v.wanted ? t('Not wanted') : t('Want it')}
                </button>
              </>
            )}
          </div>
          {v.filePath && admin && <p className="mg-hint mg-mono" style={{ overflowWrap: 'anywhere' }}>{v.filePath}</p>}
        </div>
      )}
    </div>
  )
}

function Reviews({ list, onChange }: { list: MangaReview[]; onChange: () => void }) {
  const toast = useToast()
  const [busy, setBusy] = useState(0)
  const run = (id: number, fn: () => Promise<{ message: string }>) => {
    setBusy(id)
    fn()
      .then((r) => {
        toast.success(r.message)
        onChange()
      })
      .catch((e: Error) => toast.error(e.message))
      .finally(() => setBusy(0))
  }
  return (
    <ul className="mg-rel">
      {list.map((r) => (
        <li key={r.id}>
          <div>
            <span className="title">{r.title}</span>
            <span className="mg-hint">{[r.indexer, r.sizeBytes ? formatBytes(r.sizeBytes) : ''].filter(Boolean).join(' · ')}</span>
            {(r.notes ?? []).map((n) => (
              <span key={n} className="mg-hint">
                {n}
              </span>
            ))}
          </div>
          <div className="mg-tools" style={{ flex: '0 0 auto' }}>
            <button className="btn-sm primary" disabled={busy !== 0} onClick={() => run(r.id, () => mangaApi.acceptReview(r.id))}>
              {t('Take it')}
            </button>
            <button className="btn-sm" disabled={busy !== 0} onClick={() => run(r.id, () => mangaApi.rejectReview(r.id))}>
              {r.kind === 'copy' ? t('Keep mine') : t('Not this')}
            </button>
          </div>
        </li>
      ))}
    </ul>
  )
}

function ChooseRelease({ v, onClose, onDone }: { v: MangaVolume; onClose: () => void; onDone: () => void }) {
  const toast = useToast()
  const [list, setList] = useState<MangaRelease[] | null>(null)
  const [error, setError] = useState('')
  useEffect(() => {
    mangaApi
      .releases(v.id)
      .then((r) => setList(r.releases))
      .catch((e: Error) => setError(e.message))
  }, [v.id])
  const grab = (r: MangaRelease) => {
    mangaApi
      .grab(v.id, r)
      .then(() => {
        toast.success(t('Downloading it now.'))
        onDone()
        onClose()
      })
      .catch((e: Error) => toast.error(e.message))
  }
  return (
    <div className="mg-modal" role="dialog" aria-modal="true" aria-label={t('Choose a release')} onClick={onClose}>
      <div className="mg-modal-box" onClick={(e) => e.stopPropagation()}>
        <header>
          <h2>{t('Volume {n}: choose a release', { n: v.number })}</h2>
          <button className="btn-sm" onClick={onClose}>
            {t('Close')}
          </button>
        </header>
        {!list && !error && <p className="mg-hint">{t('Searching…')}</p>}
        {error && <p className="error-text">{error}</p>}
        {list && list.length === 0 && <p className="mg-hint">{t('Your indexers have nothing for this volume.')}</p>}
        {list && (
          <ul className="mg-rel">
            {list.map((r) => {
              const no = !!r.verdict.reject || r.blocklisted
              return (
                <li key={r.downloadUrl} className={no ? 'no' : ''}>
                  <div>
                    <span className="title">{r.title}</span>
                    <span className="mg-hint">{[r.indexerName ?? r.indexer, r.sizeBytes ? formatBytes(r.sizeBytes) : '', r.verdict.class === 'official' ? t('Official edition') : r.verdict.class === 'fan' ? t('Fan scan') : ''].filter(Boolean).join(' · ')}</span>
                    {r.verdict.reject && <span className="mg-hint">{r.verdict.reject}</span>}
                    {r.blocklisted && <span className="mg-hint">{t('On your blocklist')}</span>}
                    {(r.verdict.notes ?? []).map((n) => (
                      <span key={n} className="mg-hint">
                        {n}
                      </span>
                    ))}
                  </div>
                  <button className={`btn-sm ${no ? '' : 'primary'}`} onClick={() => grab(r)}>
                    {t('Download')}
                  </button>
                </li>
              )
            })}
          </ul>
        )}
      </div>
    </div>
  )
}

function SeriesSources({ mode, onClose, onSave }: { mode: string; onClose: () => void; onSave: (m: string) => void }) {
  return (
    <div className="mg-modal" role="dialog" aria-modal="true" aria-label={t('Sources for this series')} onClick={onClose}>
      <div className="mg-modal-box" onClick={(e) => e.stopPropagation()}>
        <header>
          <h2>{t('Sources for this series')}</h2>
          <button className="btn-sm" onClick={onClose}>
            {t('Close')}
          </button>
        </header>
        <p className="mg-hint">{t('This changes only this series. Everything else keeps following Settings > Manga.')}</p>
        <div className="seg" role="group" aria-label={t('Sources for this series')} style={{ alignSelf: 'flex-start' }}>
          {(
            [
              ['', t('My settings')],
              ['official', t('Official only')],
              ['fan', t('Also fan scans')],
            ] as const
          ).map(([key, label]) => (
            <button key={key} className={mode === key ? 'active' : ''} aria-pressed={mode === key} onClick={() => onSave(key)}>
              {label}
            </button>
          ))}
        </div>
        <p className="mg-hint">{mode === 'official' ? t('Only official English editions, even if fan scans are allowed elsewhere. It looks again now.') : mode === 'fan' ? t('Fan scans are allowed for this series, even if they’re off elsewhere. Official editions are still taken first. It looks again now.') : t('This series takes whatever Settings > Manga allows.')}</p>
      </div>
    </div>
  )
}
