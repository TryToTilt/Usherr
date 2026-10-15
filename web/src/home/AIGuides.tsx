// Home fork: AI guides on title pages.
//   - What's it like? (films and shows): tone, pace, intensity, who it's for.
//   - Skip guide (shows): the episodes that matter and the ones you can skip.
import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { t } from '../i18n'
import { AIButton, AIDialog, askAI, ScrollText, useAIOn, useAsk } from './aiShared'

interface Vibe {
  feel: string
  pace: string
  intensity: string
  forWho: string
  like: { title: string; to: string }[]
}

export function WhatsItLike({ kind, tmdbId, title, autoOpen }: { kind: 'movie' | 'tv'; tmdbId: number; title: string; autoOpen?: boolean }) {
  const on = useAIOn()
  const [open, setOpen] = useState(false)
  const ask = useAsk<Vibe>()
  useEffect(() => {
    if (!autoOpen || !on || !tmdbId) return
    setOpen(true)
    void ask.run(askAI<Vibe>('/api/home/ai/vibe', { kind, tmdbId }))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [autoOpen, on, kind, tmdbId])
  if (!on || !tmdbId) return null
  const start = () => {
    setOpen(true)
    if (!ask.data) void ask.run(askAI<Vibe>('/api/home/ai/vibe', { kind, tmdbId }))
  }
  const v = ask.data
  return (
    <>
      <AIButton label={t('What’s it like?')} title={t('A spoiler-free feel for it: tone, pace and who it’s for')} onClick={start} />
      {open && (
        <AIDialog title={t('What’s {title} like?', { title })} onClose={() => setOpen(false)}>
          {ask.busy && <p className="hint">{t('Thinking…')}</p>}
          {ask.error && <p className="error-text">{ask.error}</p>}
          {v && (
            <div className="vibe">
              <ScrollText className="recap-text">{v.feel}</ScrollText>
              <dl className="vibe-facts">
                <dt>{t('Pace')}</dt>
                <dd>{v.pace}</dd>
                <dt>{t('How intense')}</dt>
                <dd>{v.intensity}</dd>
                <dt>{t('For')}</dt>
                <dd>{v.forWho}</dd>
              </dl>
              {v.like.length > 0 && (
                <p className="vibe-like">
                  <span className="hint">{t('If you liked:')}</span>{' '}
                  {v.like.map((l) => (
                    <Link key={l.title} to={l.to} className="news-chip find">
                      {l.title}
                    </Link>
                  ))}
                </p>
              )}
              <p className="hint">{t('Written by AI, without spoilers.')}</p>
            </div>
          )}
        </AIDialog>
      )}
    </>
  )
}

interface SkipPick {
  episode: number
  name: string
  why: string
}

interface SkipGuideData {
  show: string
  note: string
  seasons: { season: number; must: SkipPick[]; skip: SkipPick[] }[]
}

const code = (s: number, e: number) => `S${String(s).padStart(2, '0')}E${String(e).padStart(2, '0')}`

export function SkipGuide({ tmdbId, title, autoOpen }: { tmdbId: number; title: string; autoOpen?: boolean }) {
  const on = useAIOn()
  const [open, setOpen] = useState(false)
  const ask = useAsk<SkipGuideData>()
  useEffect(() => {
    if (!autoOpen || !on || !tmdbId) return
    setOpen(true)
    void ask.run(askAI<SkipGuideData>('/api/home/ai/skip-guide', { tmdbId }))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [autoOpen, on, tmdbId])
  if (!on || !tmdbId) return null
  const start = () => {
    setOpen(true)
    if (!ask.data) void ask.run(askAI<SkipGuideData>('/api/home/ai/skip-guide', { tmdbId }))
  }
  const g = ask.data
  return (
    <>
      <AIButton label={t('Skip guide')} title={t('Which episodes matter, and which you can skip')} onClick={start} />
      {open && (
        <AIDialog title={t('Skip guide: {title}', { title })} onClose={() => setOpen(false)} wide>
          {ask.busy && <p className="hint">{t('Reading the episode list… (a long show takes a little while)')}</p>}
          {ask.error && <p className="error-text">{ask.error}</p>}
          {g && (
            <>
              {g.note && <p className="recap-text">{g.note}</p>}
              {g.seasons.length === 0 && <p className="hint">{t('Every episode is worth watching: nothing to skip.')}</p>}
              <div className="skip-seasons">
                {g.seasons.map((s) => (
                  <section key={s.season} className="skip-season">
                    <h3>{t('Season {n}', { n: s.season })}</h3>
                    {s.must.length > 0 && (
                      <ul className="skip-list must">
                        {s.must.map((p) => (
                          <li key={`m${p.episode}`}>
                            <span className="badge downloaded">{t('Don’t miss')}</span> <b>{code(s.season, p.episode)}</b> {p.name} <span className="hint">· {p.why}</span>
                          </li>
                        ))}
                      </ul>
                    )}
                    {s.skip.length > 0 && (
                      <ul className="skip-list skip">
                        {s.skip.map((p) => (
                          <li key={`s${p.episode}`}>
                            <span className="badge">{t('Can skip')}</span> <b>{code(s.season, p.episode)}</b> {p.name} <span className="hint">· {p.why}</span>
                          </li>
                        ))}
                      </ul>
                    )}
                  </section>
                ))}
              </div>
              <p className="hint">{t('Written by AI from TMDB’s episode list. Episodes not listed are worth watching but not essential.')}</p>
            </>
          )}
        </AIDialog>
      )}
    </>
  )
}
