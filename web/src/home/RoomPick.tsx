// Home fork: Pick for the Room. Choose who's watching, a film or a show and
// a mood; the AI picks three titles from the household's library that
// nobody in the room has seen, each with a line on why.
import { useEffect, useState } from 'react'
import type { DiscoverMovie } from '../api'
import type { AddTarget } from '../components/AddDialog'
import DiscoverRail from '../components/DiscoverRail'
import Icon from '../components/Icon'
import { currentLanguage, t } from '../i18n'
import type { Owned } from '../useOwned'

interface Person {
  id: string
  name: string
}

interface RoomResult {
  note: string
  items: DiscoverMovie[]
  why: Record<string, string>
}

export function moods(): { key: string; label: string }[] {
  return [
    { key: '', label: t('Anything') },
    { key: 'funny', label: t('Funny') },
    { key: 'cozy', label: t('Cozy') },
    { key: 'exciting', label: t('Exciting') },
    { key: 'scary', label: t('Scary') },
    { key: 'mystery', label: t('Mystery') },
    { key: 'thoughtful', label: t('Thoughtful') },
    { key: 'family', label: t('For the family') },
  ]
}

export default function RoomPick({ owned, onAdd }: { owned: Owned; onAdd: (t: AddTarget) => void }) {
  const [people, setPeople] = useState<Person[]>([])
  const [room, setRoom] = useState<string[]>([])
  const [kind, setKind] = useState<'movie' | 'tv'>('movie')
  const [mood, setMood] = useState('')
  const [wish, setWish] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [result, setResult] = useState<RoomResult | null>(null)
  const [shownKind, setShownKind] = useState<'movie' | 'tv'>('movie')

  useEffect(() => {
    fetch('/api/home/discover/people?room=1', { credentials: 'include' })
      .then((r) => (r.ok ? r.json() : null))
      .then((d: { people: Person[]; me: string } | null) => {
        if (!d) return
        setPeople(d.people)
        if (d.me) setRoom([d.me])
      })
      .catch(() => undefined)
  }, [])

  const toggle = (id: string) => setRoom((cur) => (cur.includes(id) ? cur.filter((x) => x !== id) : [...cur, id]))

  const pick = async (again: boolean) => {
    setBusy(true)
    setError('')
    try {
      const r = await fetch('/api/home/ai/room', {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ people: room, kind, mood, wish, lang: currentLanguage(), again }),
      })
      const d = await r.json().catch(() => null)
      if (!r.ok) throw new Error(d?.error ?? t('Something went wrong (error {status}).', { status: r.status }))
      setResult(d as RoomResult)
      setShownKind(kind)
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  const why: Record<number, string> = {}
  for (const [id, line] of Object.entries(result?.why ?? {})) why[Number(id)] = line

  return (
    <div className="room-pick">
      {people.length > 1 && (
        <div className="room-row" role="group" aria-label={t("Who's watching")}>
          <span className="room-label">{t("Who's watching")}</span>
          {people.map((p) => (
            <button key={p.id} type="button" className={`room-chip${room.includes(p.id) ? ' on' : ''}`} aria-pressed={room.includes(p.id)} onClick={() => toggle(p.id)}>
              {p.name}
            </button>
          ))}
        </div>
      )}
      <div className="room-row" role="group" aria-label={t('Film or show')}>
        <button type="button" className={`room-chip${kind === 'movie' ? ' on' : ''}`} aria-pressed={kind === 'movie'} onClick={() => setKind('movie')}>
          <Icon name="film" size={14} /> {t('A film')}
        </button>
        <button type="button" className={`room-chip${kind === 'tv' ? ' on' : ''}`} aria-pressed={kind === 'tv'} onClick={() => setKind('tv')}>
          <Icon name="tv" size={14} /> {t('A show')}
        </button>
        <span className="room-sep" />
        {moods().map((m) => (
          <button key={m.key} type="button" className={`room-chip${mood === m.key ? ' on' : ''}`} aria-pressed={mood === m.key} onClick={() => setMood(m.key)}>
            {m.label}
          </button>
        ))}
      </div>
      <form
        className="room-row"
        onSubmit={(e) => {
          e.preventDefault()
          void pick(false)
        }}
      >
        <input className="room-wish" value={wish} onChange={(e) => setWish(e.target.value)} placeholder={t('Anything else? “under two hours”, “nothing too sad”…')} aria-label={t('Anything else?')} />
        <button className="primary btn-sm btn-with-icon" type="submit" disabled={busy || room.length === 0}>
          <Icon name="sparkles" size={14} /> {busy ? t('Thinking…') : t('Pick three')}
        </button>
      </form>
      {error && <p className="error-text">{error}</p>}
      {result && (
        <div className="ai-search-results">
          {result.note && <p className="hint ai-note">{result.note}</p>}
          {result.items.length > 0 && <DiscoverRail title={t('For the room')} items={result.items} kind={shownKind} owned={owned} onAdd={onAdd} rows={1} why={why} keepOwned scroll />}
          <div className="room-row">
            <button className="btn-sm btn-with-icon" onClick={() => void pick(true)} disabled={busy}>
              <Icon name="shuffle" size={14} /> {t('Pick again')}
            </button>
            <button className="btn-sm" onClick={() => setResult(null)}>
              {t('Clear')}
            </button>
          </div>
        </div>
      )}
    </div>
  )
}
