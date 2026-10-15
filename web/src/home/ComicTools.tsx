// Home fork: comics library tools (after Pullbox's utilities): check every
// file opens, convert CBR to CBZ, and rename files to the library's naming.
import { useCallback, useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import Icon from '../components/Icon'
import { locale, plural, t, tx } from '../i18n'
import { comicsApi, type ComicTools as Tools } from './comicsApi'

const TOOL_NAME: Record<string, string> = { check: t('Check every file'), convert: t('Convert CBR to CBZ'), rename: t('Rename to the library naming') }

export default function ComicTools() {
  const [tools, setTools] = useState<Tools | null>(null)
  const [error, setError] = useState('')
  const load = useCallback(() => {
    comicsApi
      .tools()
      .then(setTools)
      .catch((e: Error) => setError(e.message))
  }, [])
  useEffect(load, [load])
  useEffect(() => {
    if (!tools?.last.running) return
    const id = window.setInterval(load, 2000)
    return () => window.clearInterval(id)
  }, [tools?.last.running, load])

  const run = (tool: 'check' | 'convert' | 'rename') => {
    setError('')
    comicsApi
      .runTool(tool)
      .then(load)
      .catch((e: Error) => setError(e.message))
  }
  if (!tools) return error ? <p className="error-text">{error}</p> : null
  const last = tools.last
  const busy = last.running
  const problems = plural(last.problems.length, '{n} problem', '{n} problems')
  const when = last.finished ? new Date(last.finished).toLocaleString(locale()) : ''
  return (
    <section className="card cx-tools">
      <h2>{t('Library tools')}</h2>
      <p className="hint">{plural(tools.files, '{n} comic file in the library.', '{n} comic files in the library.')}</p>
      <div className="cx-tool">
        <div>
          <strong>{t('Check every file')}</strong>
          <small className="hint">{t('Opens each comic and reads every page, to find files that are missing, damaged or empty. Changes nothing.')}</small>
        </div>
        <button className="btn-sm" disabled={busy || tools.files === 0} onClick={() => run('check')}>
          <Icon name="check" size={14} /> {t('Check')}
        </button>
      </div>
      <div className="cx-tool">
        <div>
          <strong>{t('Convert CBR to CBZ')}</strong>
          <small className="hint">
            {plural(tools.cbr, '{n} CBR file.', '{n} CBR files.')}{' '}
            {t("Each CBZ is checked page by page before it's used; the CBR goes to the comics folder's .replaced folder, not deleted.")}
          </small>
        </div>
        <button className="btn-sm" disabled={busy || tools.cbr === 0} onClick={() => run('convert')}>
          <Icon name="refresh" size={14} /> {t('Convert')}
        </button>
      </div>
      <div className="cx-tool">
        <div>
          <strong>{t('Rename to the library naming')}</strong>
          <small className="hint">
            {plural(
              tools.misnamed,
              '{n} file is not named the way new downloads are (series folder, "Series (Year) Volume 01 Issue 001"). Nothing is overwritten.',
              '{n} files are not named the way new downloads are (series folder, "Series (Year) Volume 01 Issue 001"). Nothing is overwritten.',
            )}
          </small>
        </div>
        <button className="btn-sm" disabled={busy || tools.misnamed === 0} onClick={() => run('rename')}>
          <Icon name="folder" size={14} /> {t('Rename')}
        </button>
      </div>
      {error && <p className="error-text">{error}</p>}
      {last.tool && (
        <div className="cx-tool-run">
          <p>
            <strong>{TOOL_NAME[last.tool]}</strong>:{' '}
            {busy
              ? t('{done} of {total}…', { done: last.done, total: last.total })
              : last.tool === 'check'
                ? t('done {when}. {fine} fine, {problems}.', { when, fine: last.total - last.problems.length, problems })
                : t('done {when}. {changed} changed, {problems}.', { when, changed: last.changed, problems })}
          </p>
          {busy && (
            <div className="cx-arc-progress">
              <span style={{ width: `${last.total ? (last.done / last.total) * 100 : 0}%` }} />
            </div>
          )}
          {last.problems.length > 0 && (
            <ul className="cx-tool-problems">
              {last.problems.map((p) => (
                <li key={p.issueId}>
                  <span>
                    {p.series} #{p.number}
                  </span>{' '}
                  <span className="hint">
                    {p.file}: {p.problem}
                  </span>
                </li>
              ))}
            </ul>
          )}
          {!busy && last.tool === 'check' && last.problems.length > 0 && (
            <p className="hint">
              {tx('To replace a damaged issue, open its series, set it to wanted and search again ({library}).', { library: <Link to="/comics">{t('Library')}</Link> })}
            </p>
          )}
        </div>
      )}
    </section>
  )
}
