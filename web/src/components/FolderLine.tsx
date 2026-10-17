import type { ReactNode } from 'react'
import { copyText } from './CopyBox'
import Icon from './Icon'
import InfoTip from './InfoTip'
import { useToast } from './Toast'
import { folderHint } from '../folderHint'
import { t } from '../i18n'

// "Folder for its files": the full folder of a movie or a show, with a Copy
// button, so you can find it to put files there by hand.
export default function FolderLine({ path, kind, children }: { path: string; kind: 'movie' | 'series'; children?: ReactNode }) {
  const toast = useToast()

  async function copy() {
    if (await copyText(path)) toast.success(t('Copied'))
    else toast.error(t('Could not copy. Select the path and copy it by hand.'))
  }

  return (
    <div className="folder-line">
      <span className="has-tip">
        {t('Folder for its files')}
        <InfoTip text={folderHint(kind)} />
      </span>
      <code>{path}</code>
      <button type="button" className="btn-sm btn-with-icon" onClick={() => void copy()}>
        <Icon name="copy" size={14} /> {t('Copy')}
      </button>
      {children}
    </div>
  )
}
