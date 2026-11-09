// Usherr: Finish on your phone, the phone's half (/p/ABCDEFGH). A TV made the
// link for one of the app's own pages; opened here, signed in as the same
// person, it goes straight there. Someone not signed in signs in first (the
// sign-in page shows at this address), then lands here again.
import { useEffect, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { api } from '../api'
import { t } from '../i18n'
import { titleFor } from '../documentTitle'
import { isAppPath, normalizeTVCode } from '../tvSignIn'

// A link works once, so the answer is kept for the page's life: a second
// render (React's development double check, or coming back) reuses it.
const opened = new Map<string, Promise<{ path: string; what: string }>>()

export default function PhoneLink() {
  const { code = '' } = useParams()
  const navigate = useNavigate()
  const [error, setError] = useState('')

  useEffect(() => {
    document.title = titleFor(t('Finish on your phone'))
  }, [])

  useEffect(() => {
    const c = normalizeTVCode(code)
    if (!c) {
      setError(t('This link isn’t complete. Scan the code on the TV again.'))
      return
    }
    let gone = false
    let call = opened.get(c)
    if (!call) {
      call = api.openHandoff(c)
      opened.set(c, call)
      call.catch(() => opened.delete(c)) // a refusal (another account) can be tried again after signing in
    }
    call
      .then((r) => {
        if (gone) return
        // The server only makes links to the app's own pages; checked again here.
        if (!isAppPath(r.path)) {
          setError(t('This link isn’t complete. Scan the code on the TV again.'))
          return
        }
        navigate(r.path, { replace: true })
      })
      .catch((e) => !gone && setError(e instanceof Error ? e.message : String(e)))
    return () => {
      gone = true
    }
  }, [code, navigate])

  return (
    <div style={{ maxWidth: 560 }}>
      <fieldset className="group">
        {error ? (
          <>
            <p className="error-text">{error}</p>
            <Link to="/">{t('Go to the home page')}</Link>
          </>
        ) : (
          <p className="hint">{t('Opening…')}</p>
        )}
      </fieldset>
    </div>
  )
}
