// Usherr: Add a series. One box, one Add button, no site to pick: type the English
// title (any other title works too), and say what Usherr should get. Nothing is
// saved until Add, and a search that finds nothing says why.
import { useEffect, useRef, useState } from 'react'
import { isAdmin } from '../api'
import { useAuth } from '../AuthContext'
import Switch from '../components/Switch'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'
import { useToast } from '../components/Toast'
import { plural, t } from '../i18n'
import MangaCover from './MangaCover'
import { mangaApi, type MangaFound, type MangaLookup } from './mangaApi'
import { FORMAT_LABEL } from './mangaStatus'
import './manga.css'

type Want = 'all' | 'new' | 'none'

export default function AddManga() {
  const toast = useToast()
  const nav = useNavigate()
  const [params] = useSearchParams()
  const [q, setQ] = useState(params.get('q') ?? '')
  const [latest, setAnswer] = useState<MangaLookup | null>(null)
  const [asked, setAsked] = useState('')
  const [busy, setBusy] = useState(false)
  const [adding, setAdding] = useState(false)
  const [plan, setPlan] = useState<number | null>(null)
  const [want, setWant] = useState<Want>('all')
  const [error, setError] = useState('')
  const seq = useRef(0)
  const { user } = useAuth()
  const [dexOn, setDexOn] = useState(false)
  const [chapters, setChapters] = useState(false)
  const offerChapters = isAdmin(user) && dexOn

  useEffect(() => {
    mangaApi
      .list()
      .then((d) => setDexOn(!!d.sources.mangadex))
      .catch(() => undefined)
  }, [])

  useEffect(() => {
    const term = q.trim()
    const mine = ++seq.current
    if (term.length < 2) return
    const timer = window.setTimeout(() => {
      setBusy(true)
      setError('')
      mangaApi
        .lookup(term)
        .then((d) => {
          if (mine !== seq.current) return
          setAnswer(d)
          setAsked(term)
        })
        .catch((e: Error) => {
          if (mine === seq.current) setError(e.message)
        })
        .finally(() => {
          if (mine === seq.current) setBusy(false)
        })
    }, 450)
    return () => window.clearTimeout(timer)
  }, [q])

  const add = (body: { anilistId?: number; title?: string; allowFan?: boolean }) => {
    setAdding(true)
    setError('')
    mangaApi
      .add({ ...body, want, ...(offerChapters && chapters ? { chapters: 'new' as const } : {}) })
      .then((r) => {
        toast.success(r.found ? plural(r.found, '{title} added. {n} volume was already in your folder.', '{title} added. {n} volumes were already in your folder.', { title: r.series.title }) : t('{title} added.', { title: r.series.title }))
        nav(`/manga/${r.series.id}`)
      })
      .catch((e: Error) => setError(e.message))
      .finally(() => setAdding(false))
  }

  const answer = q.trim().length < 2 ? null : latest
  const results = answer?.results ?? []
  return (
    <div className="page-pad mg-page">
      <div className="mg-head">
        <div>
          <h1>{t('Add a series')}</h1>
          <p className="mg-hint">{t('Type the English title. That’s all it takes.')}</p>
        </div>
      </div>
      <div className="mg-search">
        <input type="search" data-walk="manga-search" autoFocus value={q} onChange={(e) => {
            setQ(e.target.value)
            setPlan(null)
          }} aria-label={t('Series title')} placeholder={t('Series title')} autoComplete="off" />
        <Link className="btn-sm" to="/manga">
          {t('Done')}
        </Link>
      </div>
      {error && <p className="error-text">{error}</p>}
      <section className="mg-results" aria-live="polite">
        {busy && results.length === 0 && <p className="mg-hint">{t('Searching…')}</p>}
        {!answer && !busy && q.trim().length < 2 && <p className="mg-hint">{t('Other titles for a series work too, such as the Japanese or Korean one.')}</p>}
        {answer?.problem && (
          <div className="mg-well">
            <b>{t('Couldn’t search just now.')}</b> <span className="mg-hint">{answer.problem}</span>
          </div>
        )}
        {answer && !answer.problem && results.length === 0 && !busy && (
          <div className="mg-well mg-stack">
            <div>
              <b>{t('AniList has nothing called “{q}”.', { q: asked })}</b> <span className="mg-hint">{t('Check the spelling, or try the Japanese or Korean title.')}</span>
            </div>
            <HiddenNote answer={answer} />
            <div>
              <button className="btn-sm" disabled={adding} onClick={() => add({ title: asked })}>
                {t('Add “{q}” by this name anyway', { q: asked })}
              </button>
            </div>
          </div>
        )}
        {results.length > 0 && <HiddenNote answer={answer} />}
        {results.map((r) => (
          <Result key={r.anilistId} r={r} open={plan === r.anilistId} onOpen={() => setPlan(plan === r.anilistId ? null : r.anilistId)} want={want} onWant={setWant} offerChapters={offerChapters} chapters={chapters} onChapters={setChapters} adding={adding} onAdd={(allowFan) => add({ anilistId: r.anilistId, allowFan })} />
        ))}
      </section>
      <p className="mg-hint">{t('Titles come from AniList. Searching saves nothing to your library until you press Add.')}</p>
    </div>
  )
}

function HiddenNote({ answer }: { answer: MangaLookup | null }) {
  const parts: string[] = []
  if (answer?.hiddenNovels) parts.push(plural(answer.hiddenNovels, '{n} result left out: a novel, which belongs in Ebooks.', '{n} results left out: novels, which belong in Ebooks.'))
  if (answer?.hiddenAdult) parts.push(plural(answer.hiddenAdult, '{n} result left out: it’s 18+, and this account isn’t allowed that.', '{n} results left out: they’re 18+, and this account isn’t allowed that.'))
  if (!parts.length) return null
  return <p className="mg-hint">{parts.join(' ')}</p>
}

function Result({ r, open, onOpen, want, onWant, offerChapters, chapters, onChapters, adding, onAdd }: { r: MangaFound; open: boolean; onOpen: () => void; want: Want; onWant: (w: Want) => void; offerChapters: boolean; chapters: boolean; onChapters: (v: boolean) => void; adding: boolean; onAdd: (allowFan?: boolean) => void }) {
  const other = r.aliases?.find((a) => a.kind === 'romaji')?.alias ?? r.aliases?.[0]?.alias
  const facts = [r.author, r.startYear ? String(r.startYear) : ''].filter(Boolean).join(', ')
  const count = r.volumes ? t('{n} volumes known', { n: r.volumes }) : t('Volume count not known')
  const known = r.licensedEn === 1
  return (
    <article className="mg-res">
      <MangaCover title={r.title} src={r.coverUrl} small />
      <div style={{ minWidth: 0 }}>
        <h3>{r.title}</h3>
        <p className="mg-hint">{[facts, count, other].filter(Boolean).join(' · ')}</p>
        <div className="mg-chips meta">
          <span className="mg-pill manga">{FORMAT_LABEL[r.format]?.() ?? r.format}</span>
          {known ? <span className="mg-pill good">{t('Official English edition: {publisher}', { publisher: r.publisher })}</span> : <span className="mg-pill warn">{t('No official English edition known')}</span>}
          {r.isAdult && <span className="mg-pill warn">{t('18+')}</span>}
        </div>
      </div>
      {r.inLibrary ? (
        <Link className="btn-sm btn-wide" to={`/manga/${r.inLibrary}`}>
          {t('In your library')}
        </Link>
      ) : (
        <button className={`btn-sm btn-wide ${open ? '' : 'primary'}`} data-walk="manga-result-add" onClick={onOpen} aria-expanded={open}>
          {open ? t('Close') : t('Add')}
        </button>
      )}
      {open && (
        <div className="plan">
          <div className="mg-ask">
            <b>{t('What should Usherr get?')}</b>
            <div className="seg" role="group" aria-label={t('What to get')}>
              {(
                [
                  ['all', t('All volumes')],
                  ['new', t('New volumes only')],
                  ['none', t('Nothing yet')],
                ] as const
              ).map(([key, label]) => (
                <button key={key} data-walk={`manga-want-${key}`} className={want === key ? 'active' : ''} aria-pressed={want === key} onClick={() => onWant(key)}>
                  {label}
                </button>
              ))}
            </div>
          </div>
          {offerChapters && <Switch checked={chapters} onChange={onChapters} label={t('Also follow new chapters from MangaDex')} />}
          {known ? (
            <>
              <p className="mg-hint">{r.format === 'manga' || r.format === 'oneshot' ? t('Usherr will look for official English editions on your indexers. Reads right to left.') : t('Usherr will look for official English editions on your indexers. Reads top to bottom.')}</p>
              <div>
                <button className="btn-sm primary" data-walk="manga-add-go" disabled={adding} onClick={() => onAdd()}>
                  {t('Add {title}', { title: r.title })}
                </button>
              </div>
            </>
          ) : (
            <div className="mg-well">
              <b>{t('No official English edition is known.')}</b> <span className="mg-hint">{t('With your current sources, nothing is likely to come for this series.')}</span>
              <div className="mg-ask" style={{ marginTop: 10 }}>
                <button className="btn-sm primary" data-walk="manga-add-fan" disabled={adding} onClick={() => onAdd(true)}>
                  {t('Add and allow fan scans')}
                </button>
                <button className="btn-sm" data-walk="manga-add-go" disabled={adding} onClick={() => onAdd()}>
                  {t('Add, and wait for an official one')}
                </button>
              </div>
            </div>
          )}
        </div>
      )}
    </article>
  )
}
