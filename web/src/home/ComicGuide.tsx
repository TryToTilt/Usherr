// Home fork: the comic reading guide on the Comics page. Ask about a reading
// order or where to start; each step links to the run you have, or to Add
// with the search filled in.
import { useState } from 'react'
import { Link } from 'react-router-dom'
import Icon from '../components/Icon'
import { t } from '../i18n'
import { askAI, useAIOn, useAsk } from './aiShared'

export interface Guide {
  note: string
  steps: { series: string; volume?: number; issues?: string; why?: string; have: boolean; to: string }[]
}

export default function ComicGuide() {
  const on = useAIOn()
  const [q, setQ] = useState('')
  const ask = useAsk<Guide>()
  if (!on) return null
  return (
    <section className="ai-search">
      <form
        className="ai-search-bar"
        onSubmit={(e) => {
          e.preventDefault()
          if (q.trim()) void ask.run(askAI<Guide>('/api/home/ai/comic-guide', { question: q }))
        }}
      >
        <Icon name="sparkles" size={18} />
        <input value={q} onChange={(e) => setQ(e.target.value)} placeholder={t('Ask the AI: “what do I read before Secret Wars?”, “where do I start with Daredevil?”')} aria-label={t('Ask about a reading order')} />
        <button className="primary btn-sm" type="submit" disabled={ask.busy || !q.trim()}>
          {ask.busy ? t('Thinking…') : t('Ask')}
        </button>
      </form>
      {ask.error && <p className="error-text">{ask.error}</p>}
      {ask.data && (
        <div className="ai-search-results">
          {ask.data.note && <p className="hint ai-note">{ask.data.note}</p>}
          <ComicSteps guide={ask.data} />
          <button className="btn-sm" onClick={() => ask.setData(null)}>
            {t('Clear')}
          </button>
        </div>
      )}
    </section>
  )
}

// ComicSteps lists a reading guide's steps, each linked to the run you have
// or to Add.
export function ComicSteps({ guide }: { guide: Guide }) {
  return (
    <ol className="ai-list comic-steps">
      {guide.steps.map((s, i) => (
        <li key={i}>
          <Link to={s.to} className={`news-chip ${s.have ? 'have' : 'find'}`} title={s.have ? t('In your library') : t('Find it to add')}>
            <Icon name={s.have ? 'open' : 'search'} size={12} /> {s.series}
            {s.volume ? ` (${s.volume})` : ''}
          </Link>{' '}
          {s.issues && <b>{s.issues}</b>}
          {s.why && <div className="ai-why">{s.why}</div>}
        </li>
      ))}
    </ol>
  )
}
