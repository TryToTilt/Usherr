import { useCallback } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { ComicReader } from '../home/Comics'
import type { ComicIssue as Issue } from '../home/comicsApi'
import { useDocumentTitle } from '../documentTitle'
import { t } from '../i18n'

// An issue of the Comics library read inside Usherr Books, so one kept on
// this device opens with no connection (the Books worker covers /bookshelf/).
export default function ComicIssue() {
  const { id } = useParams()
  const nav = useNavigate()
  useDocumentTitle(t('Comic reader'))
  const close = useCallback(() => nav('/bookshelf'), [nav])
  const issueId = Number(id)
  if (!issueId) return null
  // The reader needs only the issue's id; the rest comes from its pages.
  const issue = { id: issueId } as Issue
  return <ComicReader issue={issue} onClose={close} />
}
