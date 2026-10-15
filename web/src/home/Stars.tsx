// Home fork: stars out of five, in halves (stored 1 to 10). Tapping a star
// gives that many; tapping it again takes half off; a third time clears.
import { useId } from 'react'
import { t } from '../i18n'

const STAR = 'M12 2l3.09 6.26L22 9.27l-5 4.87 1.18 6.88L12 17.77l-6.18 3.25L7 14.14 2 9.27l6.91-1.01z'

export function starsText(v: number): string {
  return v % 2 ? t('{n} and a half stars', { n: (v - 1) / 2 }) : t('{n} stars', { n: v / 2 })
}

export default function Stars({ value, onChange, size = 18 }: { value: number; onChange?: (v: number) => void; size?: number }) {
  const id = useId()
  const next = (i: number) => (value === i * 2 ? i * 2 - 1 : value === i * 2 - 1 ? 0 : i * 2)
  return (
    <span className={`stars${onChange ? ' editable' : ''}`} role={onChange ? 'group' : 'img'} aria-label={value ? starsText(value) : t('No stars')}>
      <svg width="0" height="0" aria-hidden="true" style={{ position: 'absolute' }}>
        <defs>
          <linearGradient id={`${id}-half`}>
            <stop offset="50%" stopColor="currentColor" />
            <stop offset="50%" stopColor="transparent" />
          </linearGradient>
        </defs>
      </svg>
      {[1, 2, 3, 4, 5].map((i) => {
        const fill = value >= i * 2 ? 'currentColor' : value === i * 2 - 1 ? `url(#${id}-half)` : 'none'
        const icon = (
          <svg width={size} height={size} viewBox="0 0 24 24" aria-hidden="true">
            <path d={STAR} fill={fill} stroke="currentColor" strokeWidth="1.6" strokeLinejoin="round" />
          </svg>
        )
        return onChange ? (
          <button key={i} type="button" className="star" onClick={() => onChange(next(i))} aria-label={starsText(next(i) || 0)} title={next(i) ? starsText(next(i)) : t('Clear the stars')}>
            {icon}
          </button>
        ) : (
          <span key={i} className="star">
            {icon}
          </span>
        )
      })}
    </span>
  )
}
