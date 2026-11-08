// Usherr: Settings > Manga. Where manga comes from, as a few on/off cards set
// once, and your folder. No internals, no site pickers.
import { useEffect, useState, type ReactNode } from 'react'
import { Link } from 'react-router-dom'
import { useConfirm } from '../components/ConfirmProvider'
import Switch from '../components/Switch'
import { useToast } from '../components/Toast'
import ExtraFolder from '../home/ExtraFolder'
import { t } from '../i18n'
import { useModules } from '../ModulesContext'
import { mangaApi, type MangaNyaaState, type MangaScanResult, type MangaSettings as MangaSettingsData, type MangaSourceSettings } from './mangaApi'
import { MangaDexAccountCard } from './MangaDexAccount'
import MangaMove from './MangaMove'
import { chapterStatus, looksIn, nyaaLines, nyaaWarning } from './mangaStatus'
import './manga.css'

function Source({ name, pill, tone, checked, disabled, onChange, children, warn, later }: { name: string; pill: string; tone: string; checked: boolean; disabled?: boolean; onChange?: (v: boolean) => void; children: ReactNode; warn?: string; later?: string }) {
  return (
    <div className="mg-src">
      <div className="top">
        <h3>{name}</h3>
        <span className={`mg-pill ${tone}`}>{pill}</span>
      </div>
      <div className="sw">
        <Switch checked={checked} disabled={disabled} onChange={(v) => onChange?.(v)} label={<span className="sr-only">{name}</span>} />
      </div>
      <p>{children}</p>
      {later && <div className="later">{later}</div>}
      {warn && checked && <div className="warn">{warn}</div>}
    </div>
  )
}

export default function MangaSettings() {
  const toast = useToast()
  const { on } = useModules()
  const confirm = useConfirm()
  const [src, setSrc] = useState<MangaSourceSettings | null>(null)
  const [folderProblem, setFolderProblem] = useState('')
  const [nyaa, setNyaa] = useState<MangaNyaaState | null>(null)
  const [chapters, setChapters] = useState<MangaSettingsData['chapters'] | null>(null)
  const [error, setError] = useState('')
  const [scan, setScan] = useState<MangaScanResult | null>(null)
  const [scanning, setScanning] = useState(false)
  const enabled = on('manga')

  useEffect(() => {
    mangaApi
      .settings()
      .then((d) => {
        setSrc(d.sources)
        setNyaa(d.nyaa)
        setChapters(d.chapters)
        setFolderProblem(d.folderProblem ?? '')
      })
      .catch((e: Error) => setError(e.message))
    if (enabled) {
      mangaApi
        .lastScan()
        .then((r) => {
          if (r.at) setScan(r)
        })
        .catch(() => undefined)
    }
  }, [enabled])

  const change = (key: keyof MangaSourceSettings, v: boolean) => {
    if (!src) return
    const before = src
    setSrc({ ...src, [key]: v })
    mangaApi
      .saveSources({ [key]: v })
      .then((d) => {
        setSrc(d.sources)
        setNyaa(d.nyaa)
        setChapters(d.chapters)
        setFolderProblem(d.folderProblem ?? '')
        if (key === 'nyaa' && v) toast.success(d.nyaa.own ? t('Nyaa is added, for Manga only.') : t('Manga will use your Nyaa.'))
      })
      .catch((e: Error) => {
        setSrc(before)
        toast.error(e.message)
      })
  }

  const saveChapters = (body: { chapterLanguage?: string; chapterSaver?: boolean; tidyChapters?: boolean }) => {
    mangaApi
      .saveSources(body)
      .then((d) => setChapters(d.chapters))
      .catch((e: Error) => toast.error(e.message))
  }

  // Turning MangaDex on says plainly what it means first.
  const changeDex = async (v: boolean) => {
    if (v) {
      const ok = await confirm({
        title: t('Turn on MangaDex?'),
        body: t('MangaDex hosts chapters translated by fans. Most of it isn’t licensed, and publishers have licensed series taken down. Usherr asks it gently, credits it and each group in every file, and only for series you choose to follow by chapter. Official volumes stay the default.'),
        confirmLabel: t('Turn it on'),
      })
      if (!ok) return
    }
    change('mangadex', v)
  }

  // Turning Nyaa on says plainly what it means first.
  const changeNyaa = async (v: boolean) => {
    if (v) {
      const ok = await confirm({ title: t('Turn on Nyaa?'), body: nyaaWarning(), confirmLabel: t('Turn it on') })
      if (!ok) return
    }
    change('nyaa', v)
  }

  const runScan = () => {
    setScanning(true)
    mangaApi
      .scan()
      .then((r) => {
        setScan(r)
        toast.success(r.claimed ? t('Found {n} new volumes in your folder.', { n: r.claimed }) : t('Looked through your folder.'))
      })
      .catch((e: Error) => toast.error(e.message))
      .finally(() => setScanning(false))
  }

  if (error) return <p className="error-text">{error}</p>
  if (!src) return <div className="skeleton" style={{ height: 300 }} />
  return (
    <div className="mg-page">
      {!enabled && (
        <div className="mg-well">
          {t('Manga is switched off, so nothing here does anything yet.')} <Link to="/settings/modules">{t('Switch it on in Media types')}</Link>
        </div>
      )}
      {folderProblem && (
        <div className="mg-well" role="alert">
          {folderProblem}
        </div>
      )}
      <section className="mg-stack" data-walk="manga-sources">
        <div>
          <h2>{t('Where manga comes from')}</h2>
          <p className="mg-well" style={{ marginTop: 10, fontWeight: 700 }}>
            {looksIn(src)}
          </p>
        </div>
        <Source name={t('Your own files')} pill={t('Always on')} tone="good" checked disabled>
          {t('Files you already have or bought without copy protection. Usherr understands names like Series v01.cbz.')}
        </Source>
        <Source name={t('Official English editions')} pill={t('Default')} tone="good" checked={src.official} onChange={(v) => change('official', v)}>
          {t('The published English translation, recognised by its release name on your indexers. This describes the edition. A copy on an indexer still isn’t a purchase, and a “Buy it” link is on every series with a known publisher.')}
        </Source>
        <Source name={t('Fan scans from your indexers')} pill={t('Your choice')} tone="warn" checked={src.fan} onChange={(v) => change('fan', v)}>
          {t('Volumes translated by fans. Only used when no official edition turns up, or for series with none.')}
        </Source>
        <Source name={t('Fan scans from MangaDex')} pill={t('Your choice')} tone="warn" checked={src.mangadex} onChange={(v) => void changeDex(v)} later={chapters?.away ? chapterStatus({ allowed: true, follow: 'new', link: 'linked', total: 0, have: 0, wanted: 0, stats: { listed: 0, readable: 0, external: 0, unavailable: 0 }, away: chapters.away }) : undefined}>
          {t('Adds chapters, for series you follow by chapter. Groups are credited in each file. Publishers often have licensed series taken down there, and Usherr tells you when that seems to be why a series is empty. No MangaDex account is needed.')}
        </Source>
        {src.mangadex && chapters && (
          <div className="mg-src">
            <div className="top">
              <h3>{t('MangaDex chapters')}</h3>
            </div>
            <div className="mg-stack" style={{ gridColumn: '1 / -1' }}>
              <label className="mg-tools">
                <span>{t('Language')}</span>
                <select value={chapters.language} onChange={(e) => saveChapters({ chapterLanguage: e.target.value })}>
                  {['en', 'es', 'es-la', 'fr', 'de', 'it', 'pt-br', 'ru'].map((l) => (
                    <option key={l} value={l}>
                      {l === 'en' ? t('English') : l}
                    </option>
                  ))}
                </select>
              </label>
              <Switch checked={chapters.saver} onChange={(v) => saveChapters({ chapterSaver: v })} label={t('Smaller files, lower quality: MangaDex’s data saver')} />
              <p className="mg-hint">{t('Usherr asks a few series every 15 minutes, each every few hours. Big catch-ups run between 2 and 9 in the morning, one page at a time, and never use your Usenet connections.')}</p>
            </div>
          </div>
        )}
        <Source name={t('Nyaa')} pill={t('Your choice')} tone="warn" checked={src.nyaa} onChange={(v) => void changeNyaa(v)} warn={nyaa ? nyaaLines(nyaa).join(' ') : ''} later={nyaa && !nyaa.torrentsOn ? t('Nyaa is torrents only, and torrents are switched off. Switch them on in Settings > Downloading > Torrents first.') : undefined}>
          {t('A public torrent index that carries a lot of manga. Turning this on adds it to your indexers, for Manga only: films and TV don’t ask it. Its results are sorted into official and fan scan like any other.')}
        </Source>
        <div className="mg-src off">
          <div className="top">
            <h3>{t('Scraped sites')}</h3>
            <span className="mg-pill">{t('Not built')}</span>
          </div>
          <p>{t('Aggregator sites such as the ones Tranga scrapes. They break often and need tools that get around bot checks, so Usherr doesn’t have them.')}</p>
        </div>
      </section>

      <section className="mg-card mg-stack">
        <h2>{t('Files')}</h2>
        <ExtraFolder kind="manga" />
        <div className="mg-tools">
          <button className="btn-sm" disabled={scanning || !enabled} onClick={runScan}>
            {scanning ? t('Looking…') : t('Look through the folder now')}
          </button>
          <span className="mg-hint">{t('Finds names like Series/Series v01.cbz and marks those volumes as on disk. Nothing is moved or renamed. It also runs once a night, between 2 and 9.')}</span>
        </div>
        {scan && <ScanResult r={scan} />}
        {chapters && <Switch checked={chapters.tidy} onChange={(v) => saveChapters({ tidyChapters: v })} label={t('Tidy chapters once their volume arrives. They’re set aside in .replaced, not deleted, and your place carries over.')} />}
      </section>

      {enabled && <MangaMove />}
      {enabled && <MangaDexAccountCard />}
    </div>
  )
}

function ScanResult({ r }: { r: MangaScanResult }) {
  if (r.problem === 'no-folder') return <p className="mg-hint">{t('No manga folder is set yet.')}</p>
  if (r.problem === 'no-access') return <p className="mg-hint">{t('Usherr can’t read {path}. Check the folder is there and mounted.', { path: r.root })}</p>
  if (r.files === 0) return <p className="mg-hint">{t('No manga files in {path}.', { path: r.root })}</p>
  const notes = (g: MangaScanResult['groups'][number]) =>
    [
      g.claimed ? t('{n} new', { n: g.claimed }) : '',
      g.chapters ? t('{n} chapter files, which aren’t kept as volumes', { n: g.chapters }) : '',
      g.packs ? t('{n} holding several volumes', { n: g.packs }) : '',
      g.doubles ? t('{n} more for a volume that has a file', { n: g.doubles }) : '',
      g.unread ? t('{n} with a name that doesn’t say which volume', { n: g.unread }) : '',
    ]
      .filter(Boolean)
      .join(' · ')
  return (
    <div className="mg-stack">
      <p className="mg-hint">{t('{files} files looked at. {claimed} new volumes, {gone} files gone.', { files: r.files, claimed: r.claimed, gone: r.gone })}</p>
      <ul className="mg-scan">
        {r.groups.map((g) => (
          <li key={g.name}>
            <div>
              <b>{g.title ?? g.name}</b>
              <div className="mg-hint">{[t('{n} files', { n: g.files }), notes(g)].filter(Boolean).join(' · ')}</div>
            </div>
            {g.seriesId ? (
              <Link className="btn-sm" to={`/manga/${g.seriesId}`}>
                {t('Open')}
              </Link>
            ) : (
              <Link className="btn-sm primary" to={`/manga/add?q=${encodeURIComponent(g.name)}`}>
                {t('Add this series')}
              </Link>
            )}
          </li>
        ))}
      </ul>
    </div>
  )
}
