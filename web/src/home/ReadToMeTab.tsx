// Usherr: the lower part of the Read-to-Me page (ReadToMePage.tsx). The queue
// (what is being made, how far, how long is left) and every audiobook
// Read-to-Me has made, with Play, Remake and Delete; the engines, the voices
// and the words said your way. fold: each part folds up under its heading.
import { useEffect, useState, type ReactNode } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { listenPath, openBookApp } from '../bookshelf/open'
import { useConfirm } from '../components/ConfirmProvider'
import Icon from '../components/Icon'
import { t } from '../i18n'
import { minutesText } from './ReadToMe'
import { jobActive, readToMeApi, type RtvJob, type RtvMade } from './readToMeApi'
import RtmEarly from './RtmEarly'
import RtmEngines from './RtmEngines'
import RtmLines from './RtmLines'
import RtmPresets from './RtmPresets'
import RtmVoiceLib from './RtmVoiceLib'
import SayList from './SayList'
import './readtome.css'

const engineName = (e: string) => (e === 'chatterbox' ? 'Chatterbox' : 'Kokoro')

// One part: a heading and its content, or with fold a box that opens on a click.
function Part({ fold, id, title, summary, open, children }: { fold?: boolean; id: string; title: string; summary?: string; open?: boolean; children: ReactNode }) {
  if (!fold)
    return (
      <section id={id}>
        <h2>{title}</h2>
        {children}
      </section>
    )
  return (
    <details id={id} className="rp-part" open={open}>
      <summary>
        <span className="rp-part-title">{title}</span>
        {summary && <span className="rp-part-sum">{summary}</span>}
      </summary>
      <div className="rp-part-body">{children}</div>
    </details>
  )
}

export default function ReadToMeTab({ onChanged, fold }: { onChanged?: () => void; fold?: boolean }) {
  const confirm = useConfirm()
  const navigate = useNavigate()
  const [jobs, setJobs] = useState<RtvJob[] | null>(null)
  const [made, setMade] = useState<RtvMade[] | null>(null)
  const [voiceNames, setVoiceNames] = useState<Record<string, string>>({})
  const [chatterboxOn, setChatterboxOn] = useState(false)
  const [error, setError] = useState('')
  const [checking, setChecking] = useState(0) // the made audiobook whose lines are open

  const load = () =>
    Promise.all([readToMeApi.jobs(), readToMeApi.made()])
      .then(([j, m]) => {
        setJobs(j.jobs ?? [])
        setMade(m.made ?? [])
      })
      .catch((e: Error) => setError(e.message))
  useEffect(() => {
    void load()
    readToMeApi
      .voices()
      .then((v) => {
        setVoiceNames(Object.fromEntries((v.voices ?? []).map((x) => [`${x.engine}:${x.id}`, x.name])))
        setChatterboxOn((v.engines ?? []).some((e) => e.name === 'chatterbox' && e.on))
      })
      .catch(() => undefined)
  }, [])
  const queue = (jobs ?? []).filter(jobActive)
  const failed = (jobs ?? []).filter((j) => j.state === 'failed').slice(0, 5)
  // While something is being made, the list keeps up with it.
  useEffect(() => {
    if (queue.length === 0) return
    const timer = window.setInterval(() => void load(), 5000)
    return () => window.clearInterval(timer)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [queue.length])

  const voiceOf = (j: RtvJob) => voiceNames[`${j.engine}:${j.voice}`] ?? j.voice
  const stop = (j: RtvJob) =>
    readToMeApi
      .cancel(j.id)
      .then(() => load())
      .catch((e: Error) => setError(e.message))
  const remove = async (m: RtvMade) => {
    const ok = await confirm({
      title: t('Delete the audiobook of {title}?', { title: m.title ?? '' }),
      body: <p>{t('The audiobook Read-to-Me made is deleted from your Audiobooks library. The ebook stays as it is, and you can make it again any time.')}</p>,
      confirmLabel: t('Delete'),
      danger: true,
    })
    if (!ok) return
    readToMeApi
      .deleteMade(m.id)
      .then(() => {
        onChanged?.()
        return load()
      })
      .catch((e: Error) => setError(e.message))
  }

  return (
    <div className="rtm-tab">
      {error && <p className="error-text">{error}</p>}

      <Part fold={fold} id="rp-being-made" title={t('Being made')} summary={jobs === null ? '' : queue.length ? t('{n} being made', { n: queue.length }) : t('Nothing right now')} open={queue.length > 0}>
        {jobs === null ? (
          <div className="skeleton" style={{ height: 64, borderRadius: 10 }} />
        ) : queue.length === 0 ? (
          <p className="hint">{t('Nothing is being made. Pick a book above to make one.')}</p>
        ) : (
          <ul className="rtm-list">
            {queue.map((j) => {
              const percent = j.piecesTotal > 0 ? Math.min(100, Math.round((j.piecesDone / j.piecesTotal) * 100)) : 0
              return (
                <li key={j.id} className="rtm-job">
                  <span className="rtm-row">
                    <Link to={`/book/${j.bookId}`}>
                      <strong>{j.title}</strong>
                    </Link>
                    <small className="hint">
                      {voiceOf(j)} · {engineName(j.engine)}
                      {j.options?.redo ? ' · ' + (j.options.redo.line < 0 ? t('chapter {n} read again', { n: j.options.redo.chapter + 1 }) : t('a line read again')) : j.remake ? ' · ' + t('remake') : ''}
                    </small>
                    <span className="spacer" />
                    <button className="btn-sm" onClick={() => void stop(j)}>
                      {t('Stop')}
                    </button>
                  </span>
                  <div className="rtm-bar" aria-hidden="true">
                    <span style={{ width: `${percent}%` }} />
                  </div>
                  <small className="hint">
                    {j.state === 'queued' ? t('Waiting its turn') : t('Chapter {n} of {m}', { n: j.chapter, m: j.chapters }) + ` · ${percent}%`}
                    {j.etaSec ? ' · ' + t('about {time} left', { time: minutesText(j.etaSec / 60) }) : ''}
                    {j.note ? ' · ' + j.note : ''}
                  </small>
                  {!j.options?.redo && <RtmEarly job={j} />}
                </li>
              )
            })}
          </ul>
        )}
        {failed.length > 0 && (
          <ul className="rtm-list">
            {failed.map((j) => (
              <li key={j.id} className="rtm-job">
                <span className="rtm-row">
                  <Link to={`/book/${j.bookId}`}>
                    <strong>{j.title}</strong>
                  </Link>
                  <small className="error-text">{t("Didn't finish: {why}", { why: j.error ?? '' })}</small>
                  <span className="spacer" />
                  <button className="btn-sm" onClick={() => void stop(j)}>
                    {t('Clear')}
                  </button>
                </span>
              </li>
            ))}
          </ul>
        )}
      </Part>

      <Part fold={fold} id="rp-made" title={t('Made by Read-to-Me')} summary={made === null ? '' : t('{n} audiobooks', { n: made.length })}>
        {made === null ? (
          <div className="skeleton" style={{ height: 64, borderRadius: 10 }} />
        ) : made.length === 0 ? (
          <p className="hint">{t('No audiobooks made yet.')}</p>
        ) : (
          <ul className="rtm-list">
            {made.map((m) => (
              <li key={m.id} className="rtm-job">
                <span className="rtm-row">
                  <Link to={`/book/${m.bookId}`}>
                    <strong>{m.title}</strong>
                  </Link>
                  {m.author && <small className="hint">{m.author}</small>}
                  <span className="spacer" />
                  <span className="rtm-made-actions">
                    <button className="btn-sm btn-with-icon" onClick={() => openBookApp(listenPath(m.bookId))}>
                      <Icon name="play" size={14} /> {t('Play')}
                    </button>
                    {m.kept && (
                      <button className="btn-sm" aria-expanded={checking === m.id} onClick={() => setChecking(checking === m.id ? 0 : m.id)}>
                        {m.flagged ? t('Lines to check ({n})', { n: m.flagged }) : t('Lines')}
                      </button>
                    )}
                    {m.canChange && (
                      <>
                        <button className="btn-sm btn-with-icon" onClick={() => navigate(`/read-to-me/${m.bookId}?remake=1`)} disabled={queue.some((j) => j.bookId === m.bookId)}>
                          <Icon name="refresh" size={14} /> {t('Remake')}
                        </button>
                        <button className="btn-sm btn-with-icon danger" onClick={() => void remove(m)} disabled={queue.some((j) => j.bookId === m.bookId)}>
                          <Icon name="trash" size={14} /> {t('Delete')}
                        </button>
                      </>
                    )}
                  </span>
                </span>
                <small className="hint">
                  {voiceOf(m)} · {engineName(m.engine)} · {t('speed {n}×', { n: m.speed })}
                  {m.doneMs ? ' · ' + new Date(m.doneMs).toLocaleDateString() : ''}
                  {m.sizeBytes ? ' · ' + t('{n} MB', { n: Math.max(1, Math.round(m.sizeBytes / 1048576)) }) : ''}
                </small>
                {checking === m.id && <RtmLines made={m} busy={queue.some((j) => j.bookId === m.bookId)} onQueued={() => void load()} />}
              </li>
            ))}
          </ul>
        )}
      </Part>

      <Part fold={fold} id="rp-engines" title={t('Engines')} summary={t('Kokoro and Chatterbox, and when they run')}>
        <RtmEngines />
      </Part>

      <Part fold={fold} id="rp-voices-lib" title={t('Voices')} summary={t('Presets and copied voices')}>
        <RtmPresets voiceNames={voiceNames} />
        <h3>{t('Copied voices')}</h3>
        <RtmVoiceLib chatterboxOn={chatterboxOn} />
      </Part>

      <Part fold={fold} id="rp-words-all" title={t('How words are said')} summary={t('In every book')}>
        <p className="hint">{t('For every book: names and made-up words the voice gets wrong, written the way they sound. A book can have its own list too, in its Words step.')}</p>
        <SayList />
      </Part>

    </div>
  )
}
