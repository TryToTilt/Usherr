// Usherr: a series' Chapters tab. Each chapter says whose translation it is and what's
// true of it: here, on its way, in a volume, on another site, or taken down.
import { useCallback, useEffect, useState } from 'react'
import { useConfirm } from '../components/ConfirmProvider'
import { useToast } from '../components/Toast'
import { formatBytes } from '../format'
import { plural, t } from '../i18n'
import { useLive } from '../useLive'
import MangaCover from './MangaCover'
import { mangaApi, type MangaChapter, type MangaChapters as MangaChaptersData, type MangaSeries, type MangaVolume } from './mangaApi'
import { chapterStatus, chapterWhy } from './mangaStatus'
import './manga.css'

const SHOW_FIRST = 30

function chapterName(c: MangaChapter): string {
  return c.number === 'Oneshot' ? t('One-shot') : t('Chapter {n}', { n: c.number })
}

function chapterPill(c: MangaChapter): { label: string; tone: string } {
  switch (c.state) {
    case 'downloaded':
      return { label: t('On disk'), tone: 'good' }
    case 'downloading':
      return { label: t('Downloading'), tone: 'accent' }
    case 'in_volume':
      return { label: t('In Volume {n}', { n: c.inVolume ?? '' }), tone: 'manga' }
    case 'external':
      return { label: t('On another site'), tone: '' }
    case 'unavailable':
      return { label: t('Taken down'), tone: 'warn' }
  }
  if (c.problem) return { label: t('Trying again later'), tone: 'warn' }
  return c.wanted ? { label: t('Wanted'), tone: '' } : { label: t('Not wanted'), tone: '' }
}

export default function MangaChapters({ series: s, volumes, admin, onChange, onRead }: { series: MangaSeries; volumes: MangaVolume[]; admin: boolean; onChange: () => void; onRead: (id: number, chapter: boolean) => void }) {
  const toast = useToast()
  const confirm = useConfirm()
  const [d, setD] = useState<MangaChaptersData | null>(null)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [whyOpen, setWhyOpen] = useState(false)
  const [showAll, setShowAll] = useState(false)
  const [open, setOpen] = useState<number | null>(null)
  const [address, setAddress] = useState('')

  const load = useCallback(() => {
    mangaApi
      .chapters(s.id)
      .then((x) => {
        setD(x)
        setError('')
      })
      .catch((e: Error) => setError(e.message))
  }, [s.id])
  useEffect(load, [load])
  useLive(load, 30_000, true)

  const act = async (fn: () => Promise<unknown>, done?: string) => {
    setBusy(true)
    try {
      await fn()
      if (done) toast.success(done)
      load()
      onChange()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  if (error && !d) return <p className="error-text">{error}</p>
  if (!d) return <div className="skeleton" style={{ height: 200 }} />

  const c = d.summary
  const why = chapterWhy(c, s.licensedEn === 1)
  const newestFirst = [...d.chapters].reverse()
  const shown = newestFirst.length > SHOW_FIRST && !showAll ? newestFirst.slice(0, SHOW_FIRST) : newestFirst
  const lastVolume = [...volumes].reverse().find((v) => v.state === 'downloaded')
  const canTidy = d.chapters.some((x) => x.state === 'in_volume' && x.status === 'downloaded' && !x.setAside)
  const canUntidy = d.chapters.some((x) => x.setAside)
  const heading = lastVolume ? t('Chapters since Volume {n}', { n: lastVolume.number }) : plural(d.chapters.length, '{n} chapter', '{n} chapters')

  const follow = async (v: '' | 'new' | 'all') => {
    if (v === 'all' && c.follow !== 'all') {
      const ok = await confirm({ title: t('Follow every chapter?'), body: t('A few chapters come straight away. A long series catches up a batch at a time, between 2 and 9 in the morning, so MangaDex isn’t rushed.'), confirmLabel: t('Follow every chapter') })
      if (!ok) return
    }
    await act(() => mangaApi.setMangaDex(s.id, { follow: v }), v ? t('Looking on MangaDex now.') : t('No more chapters will come for this series.'))
  }

  return (
    <section className="mg-stack" data-walk="manga-chapters">
      <div className="mg-head">
        <h2>{heading}</h2>
      </div>
      <div className="mg-well mg-status">
        <p>{chapterStatus(c)}</p>
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
        {c.stats.licensedLink && (
          <a className="mg-hint" href={c.stats.licensedLink} target="_blank" rel="noreferrer">
            {t('The official English edition')}
          </a>
        )}
      </div>

      {admin && c.allowed && (
        <div className="mg-card mg-stack">
          <div className="mg-ask">
            <b>{t('Chapters to get')}</b>
            <div className="seg" role="group" aria-label={t('Chapters to get')}>
              {(
                [
                  ['', t('None')],
                  ['new', t('New ones')],
                  ['all', t('All of them')],
                ] as const
              ).map(([key, label]) => (
                <button key={key} className={c.follow === key ? 'active' : ''} aria-pressed={c.follow === key} disabled={busy} onClick={() => void follow(key)}>
                  {label}
                </button>
              ))}
            </div>
          </div>
          {d.groups.length > 1 && (
            <label className="mg-ask">
              <b>{t('When several groups did a chapter')}</b>
              <select value={d.preferGroup} disabled={busy} onChange={(e) => void act(() => mangaApi.setMangaDex(s.id, { preferGroup: e.target.value }), t('Saved. Chapters already here stay as they are.'))}>
                <option value="">{t('The group with the most chapters')}</option>
                {d.groups.map((g) => (
                  <option key={g.key} value={g.key}>
                    {plural(g.chapters, '{name} ({n} chapter)', '{name} ({n} chapters)', { name: g.name })}
                  </option>
                ))}
              </select>
            </label>
          )}
          <div className="mg-tools">
            <button className="btn-sm" disabled={busy} onClick={() => void act(() => mangaApi.checkChapters(s.id), t('Looking on MangaDex now.'))}>
              {t('Look now')}
            </button>
            {c.wanted > 0 && (
              <button className="btn-sm" disabled={busy} onClick={() => void act(() => mangaApi.getChapters(s.id, { next: 10 }), t('Downloading. See Activity.'))}>
                {t('Get the next 10 now')}
              </button>
            )}
            {canTidy && (
              <button className="btn-sm" disabled={busy} onClick={() => void act(() => mangaApi.tidyChapters(s.id), t('Set aside in the folder’s .replaced. Nothing was deleted.'))}>
                {t('Tidy chapters')}
              </button>
            )}
            {canUntidy && (
              <button className="btn-sm" disabled={busy} onClick={() => void act(() => mangaApi.tidyChapters(s.id, true), t('Chapters put back.'))}>
                {t('Put chapters back')}
              </button>
            )}
          </div>
          {(c.link === 'unsure' || c.link === 'none') && (
            <div className="mg-stack">
              {(d.candidates ?? []).length > 0 && <b>{t('Which of these is it?')}</b>}
              <ul className="mg-rel">
                {(d.candidates ?? []).map((m) => (
                  <li key={m.id}>
                    <div>
                      <a className="title" href={`https://mangadex.org/title/${m.id}`} target="_blank" rel="noreferrer">
                        {m.title}
                      </a>
                      {m.year ? <span className="mg-hint">{m.year}</span> : null}
                    </div>
                    <button className="btn-sm" disabled={busy} onClick={() => void act(() => mangaApi.setMangaDex(s.id, { dexId: m.id }), t('Linked. Looking for its chapters now.'))}>
                      {t('This one')}
                    </button>
                  </li>
                ))}
              </ul>
              <form
                className="mg-tools"
                onSubmit={(e) => {
                  e.preventDefault()
                  void act(() => mangaApi.setMangaDex(s.id, { dexId: address.trim() }), t('Linked. Looking for its chapters now.'))
                }}
              >
                <input value={address} onChange={(e) => setAddress(e.target.value)} aria-label={t('Its MangaDex address')} placeholder={t('Its MangaDex address')} style={{ flex: '1 1 260px', minWidth: 0 }} />
                <button className="btn-sm" type="submit" disabled={busy || !address.trim()}>
                  {t('Use this')}
                </button>
              </form>
            </div>
          )}
        </div>
      )}

      {d.chapters.length > 0 && <p className="mg-hint">{t('Translated by fans and credited to the group. When a volume arrives, its chapters show “In Volume N” and your place carries over.')}</p>}
      <div className="mg-list">
        {shown.map((x) => (
          <ChapterRow key={x.id} c={x} title={s.title} cover={s.coverUrl} admin={admin} allowed={c.allowed} busy={busy} open={open === x.id} onToggle={() => setOpen(open === x.id ? null : x.id)} onAct={act} onRead={onRead} />
        ))}
        {shown.length < newestFirst.length && (
          <div>
            <button className="btn-sm" onClick={() => setShowAll(true)}>
              {t('Show all {n}', { n: newestFirst.length })}
            </button>
          </div>
        )}
      </div>
      <p className="mg-hint">
        {t('Chapters and group names come from')}{' '}
        <a href="https://mangadex.org" target="_blank" rel="noreferrer">
          MangaDex
        </a>
        {t(', which is free and run by its volunteers.')}
      </p>
    </section>
  )
}

function ChapterRow({ c, title, cover, admin, allowed, busy, open, onToggle, onAct, onRead }: { c: MangaChapter; title: string; cover?: string; admin: boolean; allowed: boolean; busy: boolean; open: boolean; onToggle: () => void; onAct: (fn: () => Promise<unknown>, done?: string) => Promise<void>; onRead: (id: number, chapter: boolean) => void }) {
  const pill = chapterPill(c)
  const name = chapterName(c)
  const here = c.status === 'downloaded'
  const inVolume = c.state === 'in_volume' && !!c.inVolumeId
  const sub = [c.title, c.group, c.page ? t('Page {page} of {pages}', { page: c.page, pages: c.pagesFor ?? 0 }) : '', c.copies && c.copies > 1 ? t('{n} groups did this', { n: c.copies }) : ''].filter(Boolean).join(' · ')
  const canGet = admin && allowed && c.state === 'missing'
  const body = (
    <>
      <MangaCover title={title} src={cover} small />
      <span style={{ minWidth: 0 }}>
        <span className="n">{name}</span>
        {sub && <span className="sub">{sub}</span>}
      </span>
      <span className={`mg-pill ${pill.tone}`}>{pill.label}</span>
    </>
  )
  return (
    <div className="mg-vol">
      <div className="mg-vol-line">
        {c.state === 'external' && c.externalUrl ? (
          <a className="row mg-row-link" href={c.externalUrl} target="_blank" rel="noreferrer" aria-label={t('Read {name} on another site', { name })}>
            {body}
          </a>
        ) : (
          <button className="row" disabled={!here && !inVolume && !canGet} onClick={inVolume ? () => onRead(c.inVolumeId ?? 0, false) : here ? () => onRead(c.id, true) : onToggle} aria-label={inVolume ? t('Read Volume {n}', { n: c.inVolume ?? '' }) : here ? t('Read {name}', { name }) : undefined}>
            {body}
          </button>
        )}
        {c.read && (
          <span className="mg-tick" role="img" aria-label={t('Read')} title={t('Read')}>
            ✓
          </span>
        )}
        {here && !inVolume && (
          <button className="btn-sm" onClick={onToggle} aria-expanded={open} aria-label={t('More for {name}', { name })}>
            ⋯
          </button>
        )}
      </div>
      {open && (here || canGet) && (
        <div className="mg-vol-more">
          {c.problem && <p className="mg-hint">{c.problem}</p>}
          <div className="mg-tools">
            {here && (
              <>
                <button className="btn-sm" disabled={busy} onClick={() => void onAct(() => mangaApi.markChapterRead(c.id, !c.read))}>
                  {c.read ? t('Mark unread') : t('Mark read')}
                </button>
                {!c.read && (
                  <button className="btn-sm" disabled={busy} onClick={() => void onAct(() => mangaApi.markChapterRead(c.id, true, true))}>
                    {t('Mark read up to here')}
                  </button>
                )}
                <a className="btn-sm" href={mangaApi.chapterFileUrl(c.id)} download>
                  {t('Save the file')}
                </a>
              </>
            )}
            {canGet && (
              <>
                <button className="btn-sm primary" disabled={busy} onClick={() => void onAct(() => mangaApi.getChapters(c.seriesId, { ids: [c.id] }), t('Downloading. See Activity.'))}>
                  {t('Download')}
                </button>
                <button className="btn-sm" disabled={busy} onClick={() => void onAct(() => mangaApi.setChaptersWanted([c.id], !c.wanted))}>
                  {c.wanted ? t('Not wanted') : t('Want it')}
                </button>
              </>
            )}
          </div>
          {here && c.fileSize ? <p className="mg-hint">{formatBytes(c.fileSize)}</p> : null}
          {here && c.unavailable && <p className="mg-hint">{t('MangaDex no longer has this chapter. Your copy stays.')}</p>}
          {c.filePath && admin && <p className="mg-hint mg-mono" style={{ overflowWrap: 'anywhere' }}>{c.filePath}</p>}
        </div>
      )}
    </div>
  )
}
