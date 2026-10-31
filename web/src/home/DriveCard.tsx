// Usherr: Settings > Extras > Virtual drive (for Plex). Plex can't play the
// Streams library's pointer files, so Usherr also serves streamed titles as
// a read-only drive of ordinary video files (WebDAV), which the household
// mounts with rclone (and WinFsp on Windows) and adds to Plex. The card says
// how far along the drive is, holds the drive's own sign-in, and gives the
// exact commands to mount it. Usherr installs nothing on the computer: the
// person installs rclone and WinFsp from their own sites.
import { useCallback, useEffect, useState } from 'react'
import Icon from '../components/Icon'
import InfoTip from '../components/InfoTip'
import Switch from '../components/Switch'
import { useToast } from '../components/Toast'
import { copyText } from '../components/CopyBox'
import { plural, t } from '../i18n'

interface DriveStatus {
  on: boolean
  onByPlex: boolean
  plex: boolean
  address: string
  user: string
  password?: string
  ready: number
  waiting: number
  dead: number
  budget: number
  usedToday: number
  lastRead?: number
  where: string
}

async function call<T>(method: string, body?: unknown, path = '/api/home/drive'): Promise<T> {
  const r = await fetch(path, { method, credentials: 'include', headers: body ? { 'Content-Type': 'application/json' } : undefined, body: body ? JSON.stringify(body) : undefined })
  const d = await r.json().catch(() => ({}))
  if (!r.ok) throw new Error((d as { error?: string }).error ?? t('Something went wrong (error {status}).', { status: r.status }))
  return d as T
}

const RCLONE_KEY = 'usherr.drive.rclone'

// mountCommands are the lines that set up and mount the drive with rclone.
// rc is where rclone is: just rclone when it's on the PATH, or its full path.
function mountCommands(st: DriveStatus, rc: string, letter = 'U:') {
  const url = st.address || 'http://YOUR-SERVER:8264/dav/'
  const pass = st.password ?? 'PASSWORD'
  const exe = /\s/.test(rc) ? `& "${rc}"` : rc // PowerShell runs a quoted path only after &
  return {
    config: `${exe} config create usherr webdav url=${url} vendor=other user=${st.user} pass=${pass} --obscure`,
    windows: `${exe} mount usherr: ${letter} --read-only --vfs-cache-mode off --dir-cache-time 1m --network-mode`,
    linux: `rclone mount usherr: /mnt/usherr --read-only --vfs-cache-mode off --dir-cache-time 1m --allow-other --daemon`,
  }
}

// startupScript is the file for the Windows Startup folder that mounts the
// drive at sign-in: it reopens itself minimised, waits (up to ten minutes)
// for Usherr to answer, since Docker may still be starting, then mounts the
// drive with rclone's window hidden. It holds no password: rclone keeps it.
function startupScript(st: DriveStatus, rc: string, letter = 'U:') {
  const base = (st.address || 'http://YOUR-SERVER:8264/dav/').replace(/\/dav\/?$/, '')
  return (
    [
      '@echo off',
      `rem Usherr's virtual drive: mounts it as ${letter} when you sign in, once Usherr answers.`,
      'rem Made from Usherr (Settings, Extras, Virtual drive). Delete this file to stop it.',
      'if not "%~1"=="min" start "" /min "%~f0" min & exit /b',
      'set tries=0',
      ':wait',
      `curl -s -o nul -m 5 ${base}/api/version && goto mount`,
      'set /a tries+=1',
      'if %tries% geq 60 goto mount',
      'timeout /t 10 /nobreak >nul',
      'goto wait',
      ':mount',
      `start "" "${rc}" mount usherr: ${letter} --read-only --vfs-cache-mode off --dir-cache-time 1m --network-mode --no-console`,
    ].join('\r\n') + '\r\n'
  )
}

function saveFile(name: string, text: string) {
  const url = URL.createObjectURL(new Blob([text], { type: 'application/octet-stream' }))
  const a = document.createElement('a')
  a.href = url
  a.download = name
  a.click()
  setTimeout(() => URL.revokeObjectURL(url), 1000)
}

function Line({ text, label }: { text: string; label: string }) {
  const toast = useToast()
  return (
    <div className="drive-line">
      <code>{text}</code>
      <button type="button" className="btn-sm" onClick={() => void copyText(text).then((ok) => (ok ? toast.success(t('Copied.')) : toast.error(t("Couldn't copy: select it and copy it yourself."))))} aria-label={t('Copy {what}', { what: label })} title={t('Copy')}>
        <Icon name="copy" size={14} />
      </button>
    </div>
  )
}

export default function DriveCard() {
  const toast = useToast()
  const [st, setSt] = useState<DriveStatus | null>(null)
  const [busy, setBusy] = useState(false)
  const [note, setNote] = useState('')
  const [auto, setAuto] = useState(false)
  const [rc, setRc] = useState(() => {
    try {
      return localStorage.getItem(RCLONE_KEY) || 'rclone'
    } catch {
      return 'rclone'
    }
  })
  const keepRc = (v: string) => {
    setRc(v)
    try {
      localStorage.setItem(RCLONE_KEY, v)
    } catch {
      // only this visit then
    }
  }
  const load = useCallback(() => {
    call<DriveStatus>('GET')
      .then(setSt)
      .catch(() => setSt(null))
  }, [])
  useEffect(load, [load])
  if (!st) return null
  const put = (body: Record<string, unknown>, done?: string) =>
    call<DriveStatus>('PUT', body)
      .then((d) => {
        setSt(d)
        if (done) toast.success(done)
      })
      .catch((e: Error) => toast.error(e.message))
  const prepare = () => {
    setBusy(true)
    setNote('')
    call<{ title?: string; error?: string }>('POST', undefined, '/api/home/drive/prepare')
      .then((r) => setNote(r.error ? t('{title}: {error}', { title: r.title || t('Nothing'), error: r.error }) : t('{title} is on the drive.', { title: r.title ?? '' })))
      .catch((e: Error) => setNote(e.message))
      .finally(() => {
        setBusy(false)
        load()
      })
  }
  const exe = rc.trim() || 'rclone'
  // The drive letter the commands mount it as follows where Plex sees it.
  const letter = /^[A-Za-z]:$/.test(st.where) ? st.where.toUpperCase() : 'U:'
  const cmd = mountCommands(st, exe, letter)
  const state = st.on ? (st.ready > 0 ? 'ready' : 'setup') : 'off'
  return (
    <section id="virtual-drive" className={`card service-card ${state === 'ready' ? 'is-included' : 'is-optional'}`} data-walk="drive-card">
      <h2>
        {t('Virtual drive (for Plex)')} <span className="req-badge optional">{t('Optional')}</span>{' '}
        <span className={`badge ${state === 'ready' ? 'downloaded' : 'missing'}`}>{state === 'ready' ? t('ready') : state === 'setup' ? t('getting ready') : t('off')}</span>
      </h2>
      <p style={{ color: 'var(--text-dim)' }}>
        {t('Plex can’t play the Streams library’s pointer files. This puts your streamed films and shows on a drive of ordinary video files, played straight from Usenet when watched. Plex (or Jellyfin) reads it like any folder once it’s mounted on this computer with rclone.')}
      </p>
      <Switch
        checked={st.on}
        onChange={(v) => void put({ on: v }, v ? t('The virtual drive is on.') : t('The virtual drive is off.'))}
        label={t('Serve the virtual drive')}
        description={st.onByPlex ? t('On because Plex is your media server. Jellyfin plays the Streams library without it.') : undefined}
      />
      {st.on && (
        <>
          <p className="drive-stat" data-walk="drive-status">
            <strong>{plural(st.ready, '{n} film or episode ready', '{n} films and episodes ready', { n: st.ready })}</strong>
            {' · '}
            {plural(st.waiting, '{n} waiting for a release', '{n} waiting for a release', { n: st.waiting })}
            {st.dead > 0 && ` · ${plural(st.dead, '{n} to replace', '{n} to replace', { n: st.dead })}`}{' '}
            <InfoTip text={t('A file shows on the drive once Usherr knows its release, because a drive needs every file’s size up front. Anything someone plays is kept for free. A stream you add goes on right away (the film, or the show’s newest season), and a stream’s page has Put it on the virtual drive now. The rest are looked for one at a time, within the searches a day below, new episodes first. A release that disappears from Usenet is replaced the same way.')} />
          </p>
          <div className="drive-row">
            <label>
              {t('Searches a day')}{' '}
              <InfoTip text={t('How many titles the drive may look for each day, besides the ones that are played. Each look is one search on your indexers, which have daily limits of their own, so they are spread out: one every ten minutes at most.')} />
              <input type="number" min={0} max={500} defaultValue={st.budget} style={{ width: 90, marginLeft: 8 }} onBlur={(e) => Number(e.target.value) !== st.budget && void put({ budget: Number(e.target.value) }, t('Saved.'))} />
            </label>
            <span className="hint">{t('{n} used today', { n: st.usedToday })}</span>
            <button type="button" className="btn-sm" disabled={busy} onClick={prepare} title={t('Looks for one more title now (it counts as one of today’s searches)')}>
              {busy ? t('Looking…') : t('Get one ready now')}
            </button>
          </div>
          {note && <p className="hint">{note}</p>}

          <h3 className="drive-h">
            {t('Mount it on the computer that runs Plex')} <InfoTip text={t('Usherr runs in Docker and can’t make a drive letter on Windows itself. rclone does it, with WinFsp, the free Windows add-on rclone needs for drives. Both come from their own websites; Usherr doesn’t install anything.')} />
          </h3>
          {!st.address && <p className="notice notice-warn">{t('First set Usherr’s address on your network (the card at the top of this page): the commands below need it.')}</p>}
          <ol className="drive-steps" data-walk="drive-steps">
            <li>
              {t('Install WinFsp')} (<a href="https://winfsp.dev/rel/" target="_blank" rel="noreferrer">winfsp.dev</a>) {t('and rclone')} (<a href="https://rclone.org/downloads/" target="_blank" rel="noreferrer">rclone.org</a>). {t('On Linux, rclone alone.')}
              <label className="drive-rc">
                {t('Where rclone is')}{' '}
                <InfoTip text={t('Leave it as rclone if typing rclone in a terminal works. If it says rclone isn’t found, put the full path to rclone.exe here, for example C:\\rclone\\rclone.exe: the commands below use it.')} />
                <input value={rc} onChange={(e) => keepRc(e.target.value)} spellCheck={false} aria-label={t('Where rclone is')} />
              </label>
            </li>
            <li>
              {t('Open a terminal and tell rclone about the drive (once):')}
              <Line text={cmd.config} label={t('the rclone setup command')} />
            </li>
            <li>
              {t('Mount it. On Windows, as drive {letter} (any free letter):', { letter })}
              <Line text={cmd.windows} label={t('the Windows mount command')} />
              {t('On Linux:')}
              <Line text={cmd.linux} label={t('the Linux mount command')} />
              <label className="drive-rc">
                {t('Where Plex sees the drive')}{' '}
                <InfoTip text={t('The drive letter (or, on Linux, the folder) the drive is mounted as on the computer that runs Plex. When a title comes onto the drive, Usherr asks Plex to look at just that folder, so it shows up within a couple of minutes instead of at Plex’s next scan. A Windows letter also goes into the commands above.')} />
                <input
                  key={st.where}
                  defaultValue={st.where}
                  spellCheck={false}
                  aria-label={t('Where Plex sees the drive')}
                  onBlur={(e) => e.target.value.trim() !== st.where && void put({ where: e.target.value.trim() }, t('Saved.'))}
                />
              </label>
            </li>
            <li data-walk="drive-startup">
              <label className="drive-check">
                <input type="checkbox" checked={auto} onChange={(e) => setAuto(e.target.checked)} /> {t('Optional, on Windows: mount it by itself whenever you sign in')}
              </label>
              {auto && (
                <>
                  <p className="notice notice-warn">
                    {t('This installs a startup item: a small file in your Windows Startup folder that runs every time you sign in. It waits until Usherr answers, then mounts the drive with its window hidden. It holds no password (rclone keeps that). To stop it, delete usherr-drive.cmd from that folder.')}
                  </p>
                  <ol className="drive-sub">
                    <li>
                      <button type="button" className="btn-sm" onClick={() => saveFile('usherr-drive.cmd', startupScript(st, exe, letter))}>
                        <Icon name="download" size={14} /> {t('Download usherr-drive.cmd')}
                      </button>
                    </li>
                    <li>{t('Press Windows+R, type shell:startup and press Enter: your Startup folder opens.')}</li>
                    <li>{t('Move usherr-drive.cmd into that folder. It mounts the drive from your next sign-in.')}</li>
                  </ol>
                </>
              )}
            </li>
            <li>{t('In Plex, add {letter}\\Movies to a Movies library and {letter}\\TV (and {letter}\\Anime) to a TV library.', { letter })}</li>
          </ol>
          <p className="hint" data-walk="drive-plex-tip">
            <Icon name="info" size={13} /> {t('In Plex, Settings, Library: set video preview thumbnails, chapter thumbnails and voice activity data to never, scan periodically (Windows doesn’t tell Plex about new files on the drive), and untick Empty trash automatically, so a moment without the drive doesn’t empty its titles. In Scheduled Tasks, untick extensive media analysis. Each of these would read whole films from Usenet. Show me how, Set up Plex with the virtual drive, walks you through it.')}
          </p>
          <div className="drive-row" data-walk="drive-signin">
            <span>
              {t('Its own sign-in:')} <code>{st.user}</code> / <code>{st.password}</code>{' '}
              <InfoTip text={t('Only for the drive, and only to read: never anyone’s account. A new password means running the rclone setup command again.')} />
            </span>
            <button type="button" className="btn-sm" onClick={() => confirm(t('Make a new password for the drive? rclone will need the setup command again.')) && void put({ newPassword: true }, t('New password made.'))}>
              {t('New password')}
            </button>
          </div>
          <p className="hint">
            {st.lastRead ? t('Last read {when}.', { when: new Date(st.lastRead).toLocaleString() }) : t('Nothing has read the drive yet since Usherr started.')} {t('rclone and WinFsp are free, open-source programs by their own makers.')}
          </p>
        </>
      )}
    </section>
  )
}
