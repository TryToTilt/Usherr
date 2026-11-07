// Usherr: a manga series' cover: AniList's, or a made-up one with its title in
// screentone over two colours when there's none.
import { useState } from 'react'
import { coverTones } from './mangaStatus'

export default function MangaCover({ title, src, small }: { title: string; src?: string; small?: boolean }) {
  const [broken, setBroken] = useState(false)
  const tones = coverTones(title)
  const image = src && !broken
  return (
    <span className={`mg-cover${small ? ' small' : ''}${image ? ' has-image' : ''}`} style={{ ['--a' as string]: tones.a, ['--b' as string]: tones.b }} aria-hidden="true">
      {image ? <img src={src} alt="" loading="lazy" onError={() => setBroken(true)} /> : <b>{title}</b>}
    </span>
  )
}
