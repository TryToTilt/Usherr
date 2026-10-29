// Usherr: Read-to-Me. The button on an ebook's page opens the Read-to-Me page
// (ReadToMePage.tsx) with that book; minutesText is shared with it.
import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import Icon from '../components/Icon'
import { t } from '../i18n'
import { readToMeOn } from './readToMeApi'

export function ReadToMeButton({ bookId }: { bookId: number; title?: string; onChanged?: () => void }) {
  const navigate = useNavigate()
  const [on, setOn] = useState(false)
  useEffect(() => {
    let live = true
    void readToMeOn().then((v) => live && setOn(v))
    return () => {
      live = false
    }
  }, [])
  if (!on) return null
  return (
    <button type="button" className="btn-with-icon" onClick={() => navigate(`/read-to-me/${bookId}`)} title={t('Make an audiobook of this book with a computer voice')}>
      <Icon name="headphones" size={16} /> {t('Read to me')}
    </button>
  )
}

// minutesText says a length of time in words: "7 h 20 min", "12 min", "under a minute".
export function minutesText(min: number): string {
  if (min < 1) return t('under a minute')
  if (min < 60) return t('{n} min', { n: Math.round(min) })
  return t('{h} h {m} min', { h: Math.floor(min / 60), m: Math.round(min % 60) })
}
