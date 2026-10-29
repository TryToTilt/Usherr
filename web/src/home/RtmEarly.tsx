// Usherr: listening to an audiobook while it is being made. The chapters
// already spoken can be played (made into sound as they play); the whole
// audiobook appears in the library when it is finished.
import { useState } from 'react'
import Icon from '../components/Icon'
import { t } from '../i18n'
import type { RtvJob } from './readToMeApi'

export default function RtmEarly({ job }: { job: RtvJob }) {
  const ready = job.readyChapters ?? 0
  const [chapter, setChapter] = useState(1)
  const [src, setSrc] = useState('')
  if (ready < 1) return null
  const n = Math.min(chapter, ready)
  return (
    <div className="rtm-say-foot">
      <small className="hint">{t('Listen while it is being made:')}</small>
      <select value={n} onChange={(e) => setChapter(Number(e.target.value))} aria-label={t('Chapter')}>
        {Array.from({ length: ready }, (_, i) => (
          <option key={i + 1} value={i + 1}>
            {t('Chapter {n}', { n: i + 1 })}
          </option>
        ))}
      </select>
      <button type="button" className="btn-sm btn-with-icon" onClick={() => setSrc(`/api/home/readtome/jobs/${job.id}/chapters/${n}?at=${Date.now()}`)}>
        <Icon name="play" size={14} /> {t('Play')}
      </button>
      {src && <audio src={src} controls autoPlay className="rtm-audio" />}
    </div>
  )
}
