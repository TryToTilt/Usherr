import { useCallback } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { useDocumentTitle } from '../documentTitle'
import { t } from '../i18n'
import MangaReader from '../manga/MangaReader'

// A manga volume read inside Usherr Books, so one kept on this device opens with no
// connection (the Books worker covers /bookshelf/).
export default function MangaVolume() {
  const { id } = useParams()
  const nav = useNavigate()
  useDocumentTitle(t('Manga reader'))
  const close = useCallback(() => nav('/bookshelf'), [nav])
  const volumeId = Number(id)
  if (!volumeId) return null
  return <MangaReader volumeId={volumeId} onClose={close} />
}
