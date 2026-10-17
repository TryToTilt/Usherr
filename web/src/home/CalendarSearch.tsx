// Usherr: the calendar's search. Search finds titles as you type; Ask
// reads the question ("anime this weekend", "superhero films next month")
// and picks the entries it means. Matches are listed by date above the
// calendar, and a date opens the calendar on that month.
import { useState } from 'react'
import { Link } from 'react-router-dom'
import type { CalendarEntry } from '../api'
import Icon from '../components/Icon'
import { KindBadge, entryLink } from '../components/CalendarGrid'
import { locale, plural, t } from '../i18n'
import { askAI, useAIOn, useAsk } from './aiShared'
import './news.css'
import './calendarSearch.css'

export type { CalendarPick } from './calendarSearchStore'
import { setCalendarSearch, useCalendarSearch, type CalendarPick } from './calendarSearchStore'

export const entryKey = (e: CalendarEntry) => `${e.kind}:${e.id}`

const fold = (s: string) =>
  s
    .normalize('NFD')
    .replace(/\p{M}/gu, '')
    .toLowerCase()

// textMatch: every word typed is in the title or the line under it.
export function textMatch(e: CalendarEntry, q: string): boolean {
  const hay = fold(`${e.title} ${e.subtitle ?? ''}`)
  return fold(q)
    .split(/\s+/)
    .filter(Boolean)
    .every((w) => hay.includes(w))
}

const MODE_KEY = 'omnibar-mode' // the same switch as the bar on the other pages

const EXAMPLES = () => [t('What anime is on this weekend?'), t('Superhero films next month'), t('What’s in theaters now?')]

// The bar at the top of the calendar, drawn like the search and AI bar on the
// other pages, with the same switch: Search narrows the calendar as you type,
// Ask has the AI pick the entries a question means.
// Usherr: in the top bar on the Calendar (header), the box reads and writes the shared store.
export function CalendarHeaderSearch() {
  const { q, pick } = useCalendarSearch()
  return <CalendarSearchBox header q={q} onQ={(v) => setCalendarSearch({ q: v })} pick={pick} onPick={(p) => setCalendarSearch({ pick: p })} />
}

export function CalendarSearchBox({ q, onQ, pick, onPick, header }: { q: string; onQ: (q: string) => void; pick: CalendarPick | null; onPick: (p: CalendarPick | null) => void; header?: boolean }) {
  const aiOn = useAIOn()
  const [mode, setModeState] = useState<'search' | 'ai'>(() => {
    try {
      return localStorage.getItem(MODE_KEY) === 'search' ? 'search' : 'ai'
    } catch {
      return 'ai'
    }
  })
  const ai = aiOn && mode === 'ai'
  const setMode = (m: 'search' | 'ai') => {
    setModeState(m)
    onPick(null)
    try {
      localStorage.setItem(MODE_KEY, m)
    } catch {
      // remembering is optional
    }
  }
  // In Ask mode the words typed only narrow the calendar once the AI has answered.
  const [text, setText] = useState(q)
  const ask = useAsk<{ answer: string; keys: string[] }>()
  const runAI = (question: string) => {
    if (!question.trim()) return
    void ask.run(
      askAI<{ answer: string; keys: string[] }>('/api/home/calendar/ask', { query: question }).then((d) => {
        onPick({ answer: d.answer, keys: new Set(d.keys) })
        return d
      }),
    )
  }
  const clear = () => {
    setText('')
    onQ('')
    onPick(null)
  }
  const label = ai ? t('Ask about the calendar: anime this weekend, films next month…') : t('Search the calendar')
  return (
    <div className={`omni cal-omni${header ? ' omni-header' : ''}`}>
      <form
        className={`omni-bar ${ai ? 'ai' : 'search'}`}
        onSubmit={(e) => {
          e.preventDefault()
          if (ai) runAI(text)
        }}
      >
        {aiOn && (
          <button type="button" className={`omni-switch${ai ? ' on' : ''}`} onClick={() => setMode(ai ? 'search' : 'ai')} title={ai ? t('Switch to plain search') : t('Switch to Ask the AI')} aria-label={ai ? t('Switch to plain search') : t('Switch to Ask the AI')} aria-pressed={ai}>
            <Icon name={ai ? 'sparkles' : 'search'} size={16} />
          </button>
        )}
        <input
          type="text"
          value={text}
          placeholder={label}
          aria-label={label}
          maxLength={500}
          onChange={(e) => {
            setText(e.target.value)
            if (!ai) onQ(e.target.value)
            if (pick) onPick(null)
          }}
          onKeyDown={(e) => {
            if (e.key === 'Escape') clear()
          }}
        />
        {text && !ask.busy && (
          <button type="button" className="icon-btn" onClick={clear} aria-label={t('Clear')}>
            <Icon name="x" size={14} />
          </button>
        )}
        {ai && (
          <button className="primary btn-sm" type="submit" disabled={ask.busy || !text.trim()}>
            {ask.busy ? t('Thinking…') : t('Ask')}
          </button>
        )}
      </form>
      {ai && !header && !pick && !ask.busy && !ask.error && !text && (
        <div className="omni-examples">
          {EXAMPLES().map((ex) => (
            <button
              key={ex}
              type="button"
              className="room-chip"
              onClick={() => {
                setText(ex)
                runAI(ex)
              }}
            >
              {ex}
            </button>
          ))}
        </div>
      )}
      {ask.error && <p className="error-text">{ask.error}</p>}
      {!header && pick && pick.answer && (
        <p className="cal-search-answer">
          <Icon name="sparkles" size={14} /> {pick.answer}
        </p>
      )}
    </div>
  )
}

// The matches, by date. A title opens its page; its date opens the calendar on that month.
export function CalendarMatches({ entries, onMonth }: { entries: CalendarEntry[]; onMonth: (month: string) => void }) {
  if (entries.length === 0) return <p className="cal-search-none">{t('Nothing on the calendar matches.')}</p>
  const sorted = [...entries].sort((a, b) => a.releaseDate.localeCompare(b.releaseDate))
  const today = new Date().toISOString().slice(0, 10)
  return (
    <div className="cal-matches">
      <p className="cal-matches-count">{plural(entries.length, '{n} match', '{n} matches')}</p>
      <ul>
        {sorted.map((e) => {
          const d = new Date(`${e.releaseDate.slice(0, 10)}T12:00:00`)
          const to = entryLink(e)
          const body = (
            <>
              <span className="cal-match-title">{e.title}</span>
              {e.subtitle && <span className="cal-match-sub">{e.subtitle}</span>}
            </>
          )
          return (
            <li key={entryKey(e)} className={e.releaseDate.slice(0, 10) < today ? 'past' : ''}>
              <button type="button" className="cal-match-date" onClick={() => onMonth(e.releaseDate.slice(0, 7))} title={t('Show this month on the calendar')}>
                <span className="cal-match-day">{d.getDate()}</span>
                <span className="cal-match-mon">{d.toLocaleString(locale(), { month: 'short', year: '2-digit' })}</span>
              </button>
              <KindBadge kind={e.kind} subtitle={e.subtitle} anime={e.anime} />
              {to ? (
                <Link to={to} className="cal-match-body">
                  {body}
                </Link>
              ) : (
                <span className="cal-match-body">{body}</span>
              )}
            </li>
          )
        })}
      </ul>
    </div>
  )
}
