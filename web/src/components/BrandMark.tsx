// The app's mark. Usherr: its icon (branding/usherr), a gold curtained "U" on
// burgundy, in place of upstream's single-colour Mediarium mark.
import { withBase } from '../base'

export default function BrandMark({ className }: { className?: string }) {
  return <img className={className ?? 'brand-mark'} src={withBase('/usherr-mark.png')} alt="" aria-hidden="true" draggable={false} />
}
