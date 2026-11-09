// Usherr: Sign in on a TV, the phone's half (/tv-code?code=ABCDEFGH). The TV's
// QR code opens this page; someone can also type the code here. Approving signs
// the TV in as the person approving, for the usual seven days.
import { useEffect, useState, type FormEvent } from 'react'
import { useSearchParams } from 'react-router-dom'
import { api, type TVCodeInfo } from '../api'
import { useAuth } from '../AuthContext'
import { t } from '../i18n'
import { titleFor } from '../documentTitle'
import { formatTVCode, normalizeTVCode } from '../tvSignIn'
import Icon from '../components/Icon'

type Done = 'approved' | 'denied' | ''

export default function TVCode() {
  const { user } = useAuth()
  const [params, setParams] = useSearchParams()
  const fromLink = normalizeTVCode(params.get('code') ?? '')
  const [typed, setTyped] = useState('')
  const [code, setCode] = useState(fromLink)
  const [info, setInfo] = useState<TVCodeInfo | null>(null)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [done, setDone] = useState<Done>('')
  const [tries, setTries] = useState(0) // the same code typed again is looked up again

  useEffect(() => {
    document.title = titleFor(t('Sign in on a TV'))
  }, [])

  useEffect(() => {
    if (!code) return
    let gone = false
    setError('')
    setInfo(null)
    api
      .tvCode(code)
      .then((i) => !gone && setInfo(i))
      .catch((e) => !gone && setError(e instanceof Error ? e.message : String(e)))
    return () => {
      gone = true
    }
  }, [code, tries])

  function submitTyped(e: FormEvent) {
    e.preventDefault()
    const c = normalizeTVCode(typed)
    if (!c) {
      setError(t('A code is eight letters and numbers, like ABCD-EFGH.'))
      return
    }
    setDone('')
    setCode(c)
    setTries((n) => n + 1)
  }

  async function answer(approve: boolean) {
    if (!info) return
    setBusy(true)
    setError('')
    try {
      await api.tvAnswer(info.code, approve)
      setDone(approve ? 'approved' : 'denied')
      setInfo(null)
      // The code is used up: take it out of the address so a reload doesn't ask again.
      if (params.get('code')) setParams({}, { replace: true })
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  return (
    <div style={{ maxWidth: 560 }}>
      <div className="page-header">
        <h1>
          <Icon name="tv" size={20} /> {t('Sign in on a TV')}
        </h1>
      </div>
      <fieldset className="group" data-walk="tv-code">{/* Usherr Broadcast: data-walk for Show me how */}
        {done === 'approved' && (
          <p className="auth-done" role="status">
            {t('Done. The TV is signing in now. You can sign it out any time under Signed-in devices.')}
          </p>
        )}
        {done === 'denied' && (
          <p role="status">{t('Okay. The TV was not signed in.')}</p>
        )}
        {error && <p className="error-text">{error}</p>}
        {info && (
          <>
            <p style={{ marginTop: 0 }}>
              <strong>{t('Sign in {device} as {name}?', { device: info.device, name: user?.name || user?.username || '' })}</strong>
            </p>
            <p>{t('Check the TV shows this code: {code}', { code: formatTVCode(info.code) })}</p>
            {!info.home && (
              <p className="error-text">
                <Icon name="warning" size={14} /> {t('This TV is not on your home network. Only approve it if it is in front of you.')}
              </p>
            )}
            <p className="hint">{t('Only approve a TV you can see right now. Never approve a code someone sent you.')}</p>
            <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
              <button className="primary" disabled={busy} onClick={() => void answer(true)} autoFocus>
                {t('Approve')}
              </button>
              <button disabled={busy} onClick={() => void answer(false)}>
                {t('Deny')}
              </button>
            </div>
          </>
        )}
        {!info && (
          <form onSubmit={submitTyped} style={{ display: 'grid', gap: 8 }}>
            <label>
              {t('Type the code the TV shows')}
              <input
                value={typed}
                onChange={(e) => setTyped(e.target.value.toUpperCase().slice(0, 9))}
                autoCapitalize="characters"
                autoComplete="off"
                placeholder="ABCD-EFGH"
                className="code-input"
                autoFocus={!fromLink}
              />
            </label>
            <div>
              <button type="submit" className="primary">
                {t('Continue')}
              </button>
            </div>
            <p className="hint">{t('On the TV, choose Sign in with your phone on the sign-in page to get a code.')}</p>
          </form>
        )}
      </fieldset>
    </div>
  )
}
