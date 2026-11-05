// Usherr: the profile editor's release tester, and the Simple mode's list of
// what a profile would pick. Both ask the server, which judges the names
// with the same checks a search makes (internal/api/profile_tester.go),
// against the profile as it stands in the form.
import { useEffect, useState } from 'react'
import { api, type ProfileTry, type QualityProfileInput } from '../api'
import { SAMPLE_RELEASES } from '../profileExtras'
import { t } from '../i18n'

function useTry(body: QualityProfileInput, releases: { title: string; sizeGB?: number }[]) {
  const [results, setResults] = useState<ProfileTry[] | null>(null)
  const [error, setError] = useState('')
  const key = JSON.stringify([body, releases])
  useEffect(() => {
    if (releases.every((r) => !r.title.trim())) {
      setResults(null)
      setError('')
      return
    }
    let live = true
    const timer = setTimeout(() => {
      api
        .tryProfile(body, releases.filter((r) => r.title.trim()))
        .then((r) => {
          if (!live) return
          setResults(r.results)
          setError('')
        })
        .catch((e) => {
          if (!live) return
          setResults(null)
          setError(e instanceof Error ? e.message : String(e))
        })
    }, 350)
    return () => {
      live = false
      clearTimeout(timer)
    }
    // key stands for body and releases
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key])
  return { results, error }
}

function Verdict({ r }: { r: ProfileTry }) {
  return (
    <span className={`pt-pill ${r.take ? 'take' : 'skip'}`}>{r.take ? t('Takes') : t('Skips')}</span>
  )
}

function Why({ r }: { r: ProfileTry }) {
  const lines = r.take ? r.counts : r.skip
  if (lines.length === 0) return <span className="pt-why">{r.take ? t('fits every rule') : ''}</span>
  return <span className="pt-why">{lines.join(' · ')}</span>
}

// What the profile would pick from a few example releases (Simple mode).
export function SamplePicks({ body }: { body: QualityProfileInput }) {
  const { results, error } = useTry(body, SAMPLE_RELEASES)
  return (
    <div className="pt-samples" aria-live="polite">
      {error && <p className="error-text">{error}</p>}
      {(results ?? []).map((r, i) => (
        <div key={r.title} className="pt-sample">
          <Verdict r={r} />
          <code>
            {r.title} · {SAMPLE_RELEASES[i]?.sizeGB} GB
          </code>
          <Why r={r} />
        </div>
      ))}
      {!results && !error && <p className="pt-wait">{t('Checking…')}</p>}
    </div>
  )
}

// Paste any release name to see whether the profile takes it, and why.
export function ReleaseTester({ body }: { body: QualityProfileInput }) {
  const [title, setTitle] = useState('')
  const [size, setSize] = useState('')
  const releases = [{ title, sizeGB: Number(size) > 0 ? Number(size) : 0 }]
  const { results, error } = useTry(body, releases)
  const r = results?.[0]
  return (
    <fieldset className="profile-fieldset pt-tester">
      <legend>{t('Try a release name')}</legend>
      <p className="pt-hint">{t('Paste any release name to see whether this profile would take it, and why. Unsaved changes count.')}</p>
      <div className="pt-inputs">
        <label>
          {t('Release name')}
          <input value={title} onChange={(e) => setTitle(e.target.value)} placeholder="Movie.2024.1080p.BluRay.x264-GROUP" spellCheck={false} />
        </label>
        <label className="pt-size">
          {t('Size (GB, optional)')}
          <input inputMode="decimal" value={size} onChange={(e) => setSize(e.target.value)} placeholder="12" />
        </label>
      </div>
      {error && <p className="error-text">{error}</p>}
      {r && (
        <div className="pt-verdict" aria-live="polite">
          <div>
            <Verdict r={r} /> <strong>{r.take ? t('Would take it') : t('Would skip it')}</strong> · {r.quality} · {t('score {n}', { n: r.score })}
          </div>
          <ul>
            {(r.take ? r.counts : r.skip).map((line) => (
              <li key={line}>{line}</li>
            ))}
            {r.take && r.counts.length === 0 && <li>{t('fits every rule')}</li>}
          </ul>
          {!r.take && r.counts.length > 0 && <p className="pt-hint">{t('Counting for it: {list}', { list: r.counts.join(', ') })}</p>}
        </div>
      )}
    </fieldset>
  )
}
