// Usherr: Read-to-Me's full cast, in the Read to me dialog. The dialogue can
// be read in other voices than the narration: one voice for all of it, or a
// voice for each character (with voices for other men and other women). Usherr
// works out who says each line from the book's own words, and the AI can be
// asked about the rest; "Check the lines" shows them chapter by chapter, and a
// line given to the wrong person can be set right.
import { useEffect, useState } from 'react'
import { t } from '../i18n'
import { readToMeApi, type RtvCast, type RtvCastInfo, type RtvCastLine, type RtvVoice, type RtvVoiceRef } from './readToMeApi'

type Mode = 'one' | 'dialogue' | 'full'
const SHOWN = 30 // characters offered a voice of their own

const refKey = (r?: RtvVoiceRef) => (r ? `${r.engine}:${r.voice}` : '')
const toRef = (k: string): RtvVoiceRef | undefined => {
  const i = k.indexOf(':')
  return i > 0 ? { engine: k.slice(0, i), voice: k.slice(i + 1) } : undefined
}

function VoicePick({ voices, value, onChange, empty, label }: { voices: RtvVoice[]; value?: RtvVoiceRef; onChange: (r?: RtvVoiceRef) => void; empty: string; label: string }) {
  return (
    <select value={refKey(value)} onChange={(e) => onChange(toRef(e.target.value))} aria-label={label}>
      <option value="">{empty}</option>
      {voices.map((v) => (
        <option key={`${v.engine}:${v.id}`} value={`${v.engine}:${v.id}`}>
          {v.label}
          {v.engine === 'chatterbox' ? ' · Chatterbox' : ''}
        </option>
      ))}
    </select>
  )
}

export default function CastPanel({ bookId, voices, cast, onChange }: { bookId: number; voices: RtvVoice[]; cast: RtvCast | null; onChange: (c: RtvCast | null) => void }) {
  const mode: Mode = !cast ? 'one' : cast.people || cast.men || cast.women || cast.ai ? 'full' : 'dialogue'
  const [info, setInfo] = useState<RtvCastInfo | null>(null)
  const [error, setError] = useState('')
  const [checking, setChecking] = useState(false)
  const load = () =>
    readToMeApi
      .cast(bookId)
      .then(setInfo)
      .catch((e: Error) => setError(e.message))
  useEffect(() => {
    if (mode === 'full' && !info) void load()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [mode])
  useEffect(() => {
    if (!info?.aiRunning) return
    const timer = window.setInterval(() => void load(), 3000)
    return () => window.clearInterval(timer)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [info?.aiRunning])

  const firstOther = voices.find((v) => v.engine === 'kokoro' && v.id !== 'af_heart') ?? voices[0]
  const setMode = (m: Mode) => {
    if (m === 'one') return onChange(null)
    const dialogue = cast?.dialogue ?? (firstOther ? { engine: firstOther.engine, voice: firstOther.id } : undefined)
    if (m === 'dialogue') return onChange({ dialogue })
    onChange({ ...cast, dialogue, people: cast?.people ?? {} })
  }
  const setPerson = (name: string, r?: RtvVoiceRef) => {
    const people = { ...(cast?.people ?? {}) }
    if (r) people[name] = r
    else delete people[name]
    onChange({ ...cast, people })
  }
  const askAI = () => {
    setError('')
    readToMeApi
      .castAI(bookId)
      .then(() => load())
      .catch((e: Error) => setError(e.message))
  }

  return (
    <details className="rtm-more" open={mode !== 'one' || undefined}>
      <summary>
        {t('Voices for the dialogue')}{' '}
        <small className="hint">{mode === 'one' ? t('one voice reads it all') : mode === 'dialogue' ? t('a voice for the dialogue') : t('a full cast')}</small>
      </summary>
      <div className="rtm-say-foot">
        {(
          [
            ['one', t('One voice reads it all')],
            ['dialogue', t('Narrator and a dialogue voice')],
            ['full', t('A voice for each character')],
          ] as const
        ).map(([m, label]) => (
          <label key={m} className="rtm-check">
            <input type="radio" name="rtm-cast" checked={mode === m} onChange={() => setMode(m)} /> {label}
          </label>
        ))}
      </div>
      {error && <p className="error-text">{error}</p>}
      {mode !== 'one' && (
        <label className="rtm-cast-row">
          <span>{mode === 'dialogue' ? t('Dialogue') : t('Everyone else')}</span>
          <VoicePick voices={voices} value={cast?.dialogue} onChange={(r) => onChange({ ...cast, dialogue: r })} empty={t("The narrator's voice")} label={t('The dialogue voice')} />
        </label>
      )}
      {mode === 'dialogue' && <p className="hint">{t('Everything in quotation marks is read in this voice; "said Tom" stays with the narrator.')}</p>}
      {mode === 'full' && (
        <>
          <label className="rtm-cast-row">
            <span>{t('Other men')}</span>
            <VoicePick voices={voices} value={cast?.men} onChange={(r) => onChange({ ...cast, men: r })} empty={t('As everyone else')} label={t('Other men')} />
          </label>
          <label className="rtm-cast-row">
            <span>{t('Other women')}</span>
            <VoicePick voices={voices} value={cast?.women} onChange={(r) => onChange({ ...cast, women: r })} empty={t('As everyone else')} label={t('Other women')} />
          </label>
          {!info ? (
            <p className="hint">{t('Finding the characters…')}</p>
          ) : (
            <>
              <p className="hint">
                {t('Who says it is known for {known} of {quotes} lines.', { known: info.known, quotes: info.quotes })}{' '}
                {info.people.length === 0 && t('No characters were found by name; the dialogue voices above read it.')}
              </p>
              {info.people.slice(0, SHOWN).map((p) => (
                <label key={p.name} className="rtm-cast-row">
                  <span>
                    {p.name === 'I' ? t('The narrator ("I")') : p.name} <small className="hint">{t('{n} lines', { n: p.lines })}{p.gender ? ` · ${p.gender === 'f' ? t('woman') : t('man')}` : ''}</small>
                  </span>
                  <VoicePick voices={voices} value={cast?.people?.[p.name]} onChange={(r) => setPerson(p.name, r)} empty={p.name === 'I' ? t("The narrator's voice") : p.gender === 'm' && cast?.men ? t('As other men') : p.gender === 'f' && cast?.women ? t('As other women') : t('As everyone else')} label={t('Voice for {name}', { name: p.name })} />
                </label>
              ))}
              <div className="rtm-say-foot">
                <label className="rtm-check">
                  <input type="checkbox" checked={!!cast?.ai} disabled={!info.aiOn} onChange={(e) => onChange({ ...cast, ai: e.target.checked })} /> {t("Ask the AI about lines the book doesn't tag")}
                </label>
                {info.aiOn && (
                  <button type="button" className="btn-sm" onClick={askAI} disabled={!!info.aiRunning}>
                    {info.aiRunning ? info.aiRunning : t('Ask it now')}
                  </button>
                )}
                <button type="button" className="btn-sm" onClick={() => setChecking((v) => !v)} aria-expanded={checking}>
                  {checking ? t('Close the lines') : t('Check the lines')}
                </button>
              </div>
              {!info.aiOn && <small className="hint">{t('The AI (Settings > Extras > AI) can work out who says the lines the book doesn’t tag, one request a chapter.')}</small>}
              {info.aiError && <p className="error-text">{info.aiError}</p>}
              {checking && <CastLines bookId={bookId} info={info} onChanged={() => void load()} />}
            </>
          )}
        </>
      )}
    </details>
  )
}

const HOW: Record<string, string> = { tag: t('the book says'), beat: t('from what they do'), ai: t('the AI'), turn: t('their turn'), you: t('set by you'), '': t('not known') }

// CastLines: a chapter's lines and who says each; a name typed in sets it.
function CastLines({ bookId, info, onChanged }: { bookId: number; info: RtvCastInfo; onChanged: () => void }) {
  const withLines = info.chapters.map((c, i) => ({ ...c, i })).filter((c) => c.lines > 0)
  const [chapter, setChapter] = useState(withLines[0]?.i ?? 0)
  const [lines, setLines] = useState<RtvCastLine[] | null>(null)
  const [error, setError] = useState('')
  const load = () =>
    readToMeApi
      .castLines(bookId, chapter)
      .then((d) => setLines(d.lines))
      .catch((e: Error) => setError(e.message))
  useEffect(() => {
    setLines(null)
    void load()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [chapter])
  const set = (l: RtvCastLine, speaker: string) => {
    const who = speaker.trim()
    if (who === l.speaker || (who === '' && l.how !== 'you')) return
    readToMeApi
      .setCastLine(bookId, chapter, l.index, who)
      .then(() => {
        onChanged()
        return load()
      })
      .catch((e: Error) => setError(e.message))
  }
  const names = info.people.map((p) => p.name)
  return (
    <div className="rtm-lines">
      <select value={chapter} onChange={(e) => setChapter(Number(e.target.value))} aria-label={t('Chapter')}>
        {withLines.map((c) => (
          <option key={c.i} value={c.i}>
            {c.title} · {t('{n} lines', { n: c.lines })}
            {c.unknown ? ` · ${t('{n} not known', { n: c.unknown })}` : ''}
          </option>
        ))}
      </select>
      {error && <p className="error-text">{error}</p>}
      <datalist id="rtm-cast-names">
        {names.map((n) => (
          <option key={n} value={n} />
        ))}
      </datalist>
      {lines === null ? (
        <p className="hint">{t('Reading the chapter…')}</p>
      ) : (
        <ul className="rtm-list">
          {lines.map((l) => (
            <li key={`${l.index}:${l.speaker}`} className={l.speaker.startsWith('?') ? 'rtm-line unknown' : 'rtm-line'}>
              <q>{l.text.length > 160 ? l.text.slice(0, 160) + '…' : l.text}</q>
              <span className="rtm-row">
                <input
                  list="rtm-cast-names"
                  defaultValue={l.speaker.startsWith('?') ? '' : l.speaker}
                  placeholder={l.speaker === '?m' ? t('a man') : l.speaker === '?f' ? t('a woman') : t('someone')}
                  onBlur={(e) => set(l, e.target.value)}
                  onKeyDown={(e) => e.key === 'Enter' && (e.target as HTMLInputElement).blur()}
                  aria-label={t('Who says it')}
                />
                <small className="hint">{HOW[l.how] ?? l.how}</small>
                {l.how === 'you' && (
                  <button type="button" className="btn-sm" onClick={() => set(l, '')}>
                    {t('Undo')}
                  </button>
                )}
              </span>
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}
