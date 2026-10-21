import { useState } from 'react'
import { api, can, type MonitorMode } from '../api'
import { useAuth } from '../AuthContext'
import { useToast } from './Toast'
import { useConfirm } from './ConfirmProvider'
import { monitorModeDone, monitorModeSentence } from '../monitorMode'
import { t } from '../i18n'
import InfoTip from './InfoTip'

// On a show's page: the same three choices as when adding the show (all
// episodes, only the ones that haven't aired yet, none), for a show that is
// already in the library. The select stays on its prompt; picking a choice
// applies it, after a confirmation when episodes stop being monitored.
export default function MonitorModePicker({ id, onDone }: { id: number; onDone: () => void }) {
  const toast = useToast()
  const confirm = useConfirm()
  const me = useAuth().user
  const [busy, setBusy] = useState(false)
  async function change(mode: MonitorMode) {
    setBusy(true)
    try {
      const change = await api.setSeriesMonitorMode(id, mode, true)
      if (change.stopped + change.started === 0) {
        toast.success(t('Nothing to change: the episodes are already set that way.'))
        return
      }
      if (change.stopped > 0) {
        const answer = await confirm({
          title: t('Change which episodes are monitored?'),
          body: <p>{monitorModeSentence(mode, change)}</p>,
          confirmLabel: t('Change'),
        })
        if (!answer) return
      }
      await api.setSeriesMonitorMode(id, mode)
      toast.success(monitorModeDone(mode))
      onDone()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }
  return (
    <label className="inline-field">
      <span className="has-tip">
        {t('Which episodes to monitor')}
        <InfoTip text={t('Only monitored episodes are searched for and downloaded. "Only episodes that haven\'t aired yet" stops monitoring everything that has already aired, so older episodes you delete are not downloaded again. Files you have are never deleted.')} />
      </span>
      <select value="" disabled={busy || !can(me, 'manage')} onChange={(e) => e.target.value && void change(e.target.value as MonitorMode)}>
        <option value="">{t('Change…')}</option>
        <option value="all">{t('All episodes')}</option>
        <option value="future">{t("Only episodes that haven't aired yet")}</option>
        <option value="none">{t('None')}</option>
      </select>
    </label>
  )
}
