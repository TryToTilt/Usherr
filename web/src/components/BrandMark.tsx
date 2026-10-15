// The app's mark. Usherr: its icon (branding/usherr), a gold curtained "U" on
// burgundy, in place of upstream's single-colour Mediarium mark.
import { markSrc } from '../palette'

export default function BrandMark({ className }: { className?: string }) {
  return <img className={className ?? 'brand-mark'} src={markSrc()} alt="" aria-hidden="true" draggable={false} />
}
