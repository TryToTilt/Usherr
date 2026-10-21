// Usherr: Stack on a title's page: puts it on your stack, or takes it off.
import Icon from '../components/Icon'
import { useToast } from '../components/Toast'
import { t } from '../i18n'
import { addToStack, removeFromStack, useStack, type StackItem } from './stack'

export default function StackButton({ item }: { item: Omit<StackItem, 'libraryId' | 'downloaded'> }) {
  const stack = useStack()
  const toast = useToast()
  const on = !!stack?.some((i) => i.kind === item.kind && i.tmdbId === item.tmdbId)
  const toggle = () => void (on ? removeFromStack(item.kind, item.tmdbId) : addToStack(item)).catch((e: unknown) => toast.error(e instanceof Error ? e.message : String(e)))
  return (
    <button className={`btn-with-icon${on ? ' on' : ''}`} onClick={toggle} aria-pressed={on} title={on ? t('On your stack (tap to take it off)') : t('Put it on your stack')}>
      <Icon name="stack" size={16} /> {on ? t('On your stack') : t('Stack')}
    </button>
  )
}
