// Home fork: "Explain" on a failed or stopped download in Activity. The AI
// reads the error and the title's recent log and says, in plain words, what
// went wrong and what to try. It only explains; it never acts.
import { useEffect, useState } from 'react'
import Icon from '../components/Icon'
import { currentLanguage, t } from '../i18n'

let aiOn: Promise<boolean> | null = null
function useAIOn() {
  const [on, setOn] = useState(false)
  useEffect(() => {
    aiOn ??= fetch('/api/home/ai/on', { credentials: 'include' })
      .then((r) => (r.ok ? r.json() : { on: false }))
      .then((d: { on: boolean }) => d.on)
      .catch(() => false)
    void aiOn.then(setOn)
  }, [])
  return on
}

export default function ExplainFailure({ id }: { id: number }) {
  const on = useAIOn()
  const [busy, setBusy] = useState(false)
  const [text, setText] = useState('')
  const [error, setError] = useState('')
  if (!on) return null

  const ask = async () => {
    setBusy(true)
    setError('')
    try {
      const r = await fetch('/api/home/ai/explain', {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ id, lang: currentLanguage() }),
      })
      const d = await r.json().catch(() => null)
      if (!r.ok) throw new Error(d?.error ?? t('Something went wrong (error {status}).', { status: r.status }))
      setText(d.text)
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  if (text) {
    return (
      <div className="explain-box">
        <Icon name="sparkles" size={14} />
        <p>{text}</p>
      </div>
    )
  }
  return (
    <div className="explain-row">
      <button className="btn-sm btn-with-icon" onClick={() => void ask()} disabled={busy} title={t('The AI reads the error and the log and explains it in plain words')}>
        <Icon name="sparkles" size={13} /> {busy ? t('Thinking…') : t('Explain this')}
      </button>
      {error && <span className="error-text">{error}</span>}
    </div>
  )
}
