import { useState } from 'react'
import { Link } from 'react-router-dom'
import { api, type Book, type EReaderAccess, type EReaderDevice } from '../api'
import { st, t } from '../i18n'
import Icon from './Icon'
import { useToast } from './Toast'

const errorText = (e: unknown) => (e instanceof Error ? e.message : String(e))

// Email a book to a Kindle or another e-reader, at an address saved on the
// profile page. The server sends it and says how that went.
export default function SendToDevice({ book }: { book: Book }) {
  const toast = useToast()
  const [access, setAccess] = useState<EReaderAccess | null>(null)
  const [open, setOpen] = useState(false)
  const [sending, setSending] = useState(0)

  async function toggle() {
    if (open) {
      setOpen(false)
      return
    }
    try {
      setAccess(await api.eReader())
      setOpen(true)
    } catch (e) {
      toast.error(errorText(e))
    }
  }

  async function send(d: EReaderDevice) {
    setSending(d.id)
    try {
      const r = await api.sendBook(book.id, d.id)
      toast.success(t('{title} is on its way to {device}.', { title: book.title, device: r.device }))
      if (r.kindle) toast.info(t('If it never arrives, add {address} to the approved senders on your Amazon account.', { address: r.from }))
      setOpen(false)
    } catch (e) {
      toast.error(errorText(e))
    } finally {
      setSending(0)
    }
  }

  const devices = access?.send.devices ?? []
  return (
    <>
      <button className="btn-with-icon" aria-expanded={open} onClick={() => void toggle()}>
        <Icon name="external" size={16} /> {t('Send to e-reader')}
      </button>
      {open && (
        <div style={{ flexBasis: '100%' }}>
          {access?.send.problem ? (
            <p className="hint">{st(access.send.problem)}</p>
          ) : devices.length === 0 ? (
            <p className="hint">
              {t('Save the email address of your Kindle or e-reader first.')} <Link to="/profile">{t('Open your profile')}</Link>
            </p>
          ) : (
            <div className="row-actions">
              {devices.map((d) => (
                <button key={d.id} disabled={sending !== 0} title={d.email} onClick={() => void send(d)}>
                  {sending === d.id ? t('Sending…') : d.name}
                </button>
              ))}
            </div>
          )}
        </div>
      )}
    </>
  )
}
