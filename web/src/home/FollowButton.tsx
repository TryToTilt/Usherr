// Usherr: Follow on a person's page. Their new films and shows then show up
// on Discover and Upcoming → Following.
import { useEffect, useState } from 'react'
import Icon from '../components/Icon'
import { t } from '../i18n'
import { follow, listFollows, unfollow } from './follows'

export default function FollowButton({ id, name, image, known }: { id: number; name: string; image?: string; known?: string }) {
  const [on, setOn] = useState<boolean | null>(null)
  const [busy, setBusy] = useState(false)
  useEffect(() => {
    let gone = false
    listFollows()
      .then((people) => !gone && setOn((people ?? []).some((p) => p.id === id)))
      .catch(() => !gone && setOn(false))
    return () => {
      gone = true
    }
  }, [id])
  if (on === null) return null
  const toggle = () => {
    setBusy(true)
    ;(on ? unfollow(id) : follow({ id, name, image: image ?? '', known: known ?? '' }))
      .then(() => setOn(!on))
      .catch(() => undefined)
      .finally(() => setBusy(false))
  }
  return (
    <button type="button" className={`link-pill${on ? ' active' : ''}`} onClick={toggle} disabled={busy} aria-pressed={on} title={t('Their new films and shows show up on Discover and Upcoming')}>
      <Icon name={on ? 'check' : 'plus'} size={13} /> {on ? t('Following') : t('Follow')}
    </button>
  )
}
