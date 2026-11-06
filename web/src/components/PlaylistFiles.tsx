import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { api, type PlaylistFileReport, type PlaylistImportReport, type PlaylistImportSettings } from '../api'
import Icon from './Icon'
import Switch from './Switch'
import { useToast } from './Toast'
import { plural, t } from '../i18n'

// Usherr: the .m3u and .m3u8 files in the music folder, brought in as
// playlists. Administrators only (it reads the library's folder).

function statusText(f: PlaylistFileReport): string {
  switch (f.status) {
    case 'new':
      return t('New playlist')
    case 'updated':
      return t('Updated')
    case 'same':
      return t('Up to date')
    case 'removed':
      return t('Deleted here, left out')
    case 'empty':
      return t('Nothing matched')
    default:
      return f.error || t('Couldn’t read it')
  }
}

export default function PlaylistFiles({ onImported }: { onImported: () => void }) {
  const toast = useToast()
  const [open, setOpen] = useState(false)
  const [settings, setSettings] = useState<PlaylistImportSettings | null>(null)
  const [report, setReport] = useState<PlaylistImportReport | null>(null)
  const [busy, setBusy] = useState(false)
  const [onlyMissing, setOnlyMissing] = useState(true)

  useEffect(() => {
    if (!open || settings) return
    api
      .playlistFiles()
      .then((r) => {
        setSettings(r.settings)
        if (r.last.files.length > 0) setReport(r.last)
      })
      .catch((e) => toast.error(e instanceof Error ? e.message : String(e)))
  }, [open, settings, toast])

  async function run(again: boolean) {
    setBusy(true)
    try {
      const r = await api.importPlaylistFiles(again)
      setReport(r)
      if (r.found === 0) toast.success(t('No .m3u files in your music folder.'))
      else toast.success(t('{found} files read: {made} new, {changed} updated.', { found: r.found, made: r.new, changed: r.updated }))
      onImported()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  async function save(change: Partial<PlaylistImportSettings>) {
    try {
      setSettings(await api.savePlaylistFiles(change))
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    }
  }

  const files = (report?.files ?? []).filter((f) => !onlyMissing || f.unmatched.length > 0 || f.status === 'failed' || f.status === 'empty')
  const missing = (report?.files ?? []).reduce((n, f) => n + f.unmatched.length, 0)

  return (
    <details className="mpl-files" open={open} onToggle={(e) => setOpen((e.target as HTMLDetailsElement).open)}>
      <summary>
        <Icon name="download" size={15} /> {t('Bring in playlists')}
        <small> · {t('.m3u files in your music folder')}</small>
      </summary>
      <div className="mpl-files-body">
        <p className="mpl-meta">{t('Each file becomes a playlist. Tracks are found by file, then by name and length. Your files are only read.')}</p>
        <div className="mpl-actions">
          <button type="button" className="primary btn-with-icon" disabled={busy} onClick={() => void run(false)}>
            <Icon name="download" size={15} /> {busy ? t('Reading…') : t('Bring in playlists')}
          </button>
          {report && report.files.some((f) => f.status === 'removed') && (
            <button type="button" className="btn-with-icon" disabled={busy} onClick={() => void run(true)} title={t('Also bring back the playlists you deleted')}>
              <Icon name="refresh" size={15} /> {t('Bring back deleted ones')}
            </button>
          )}
        </div>
        {settings && (
          <>
            <Switch checked={settings.shared} onChange={(v) => void save({ shared: v })} label={t('Share new ones with everyone')} description={t('Everyone on this server can see and play them.')} />
            <Switch checked={settings.auto} onChange={(v) => void save({ auto: v })} label={t('Check the files each day')} description={t('New files become playlists, and changed ones update theirs.')} />
          </>
        )}
        {report && report.files.length > 0 && (
          <div className="mpl-report">
            <p className="mpl-meta">
              {plural(report.files.length, '{n} file', '{n} files')}
              {' · '}
              {missing === 0 ? t('every track found') : plural(missing, '{n} track not found', '{n} tracks not found')}
            </p>
            <label className="mpl-share">
              <input type="checkbox" checked={onlyMissing} onChange={(e) => setOnlyMissing(e.target.checked)} /> {t('Only show files with tracks not found')}
            </label>
            <ul className="mpl-report-list">
              {files.map((f) => (
                <li key={f.file}>
                  <div className="mpl-report-head">
                    {f.playlistId ? <Link to={`/music/playlists/${f.playlistId}`}>{f.name || f.file}</Link> : <strong>{f.name || f.file}</strong>}
                    <small>
                      {statusText(f)} · {t('{matched} of {entries} found', { matched: f.matched, entries: f.entries })}
                    </small>
                  </div>
                  <small className="mpl-report-file" title={f.file}>
                    {f.file}
                  </small>
                  {f.unmatched.length > 0 && (
                    <details>
                      <summary>{plural(f.unmatched.length, '{n} not found', '{n} not found')}</summary>
                      <ul>
                        {f.unmatched.map((u) => (
                          <li key={u.line}>
                            <small>{t('line {n}', { n: u.line })}</small> {u.entry}
                          </li>
                        ))}
                      </ul>
                    </details>
                  )}
                </li>
              ))}
            </ul>
          </div>
        )}
      </div>
    </details>
  )
}
