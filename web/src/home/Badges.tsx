// Usherr: rewards. The badge cabinet (You → Ticket stubs): badges earned
// from your ticket stubs, here and from linked Usherrs' parties, worked out
// by this Usherr (home_rewards.go). Pick a badge to see how it's earned and
// when you got it. The reaction, vote and star badges are worked out from
// the reactions you sent, your crews' votes and your Diary's stars.
import { useEffect, useState } from 'react'
import { locale, plural, t } from '../i18n'
import './badges.css'

interface Earned {
  id: string
  earned?: number // unix ms
  have: number
  need: number
}

type Tone = 'accent' | 'warning' | 'accent-2' | 'danger' | 'info'

interface Def {
  id: string
  mark: string
  tone: Tone
  name: () => string
  how: () => string
  soon?: boolean
}

const CATALOGUE: Def[] = [
  { id: 'first', mark: '1st', tone: 'accent', name: () => t('First party'), how: () => t('Go to your first watch party.') },
  { id: 'five', mark: '5', tone: 'accent', name: () => t('Five nights'), how: () => t('Go to five watch parties.') },
  { id: 'regular', mark: '10', tone: 'warning', name: () => t('Regular'), how: () => t('Go to ten watch parties.') },
  { id: 'season', mark: '25', tone: 'warning', name: () => t('Season ticket'), how: () => t('Go to twenty-five watch parties.') },
  { id: 'perfect', mark: 'ALL', tone: 'accent-2', name: () => t('Perfect attendance'), how: () => t('Go to every party you’re invited to in a month (three or more).') },
  { id: 'nightowl', mark: '2AM', tone: 'info', name: () => t('Night owl'), how: () => t('Be at a party that ends after 2 AM.') },
  { id: 'host', mark: 'HOST', tone: 'accent', name: () => t('House lights'), how: () => t('Host five watch parties.') },
  { id: 'double', mark: '2X', tone: 'danger', name: () => t('Double feature'), how: () => t('Go to two parties with different films on the same night.') },
  { id: 'scream', mark: 'EEK', tone: 'danger', name: () => t('Loudest scream'), how: () => t('Send the most EEKs of anyone at one party (three or more).') },
  { id: 'clown', mark: 'HA', tone: 'warning', name: () => t('Class clown'), how: () => t('Send the most HAs of anyone in a month (ten or more).') },
  { id: 'taste', mark: 'PICK', tone: 'accent-2', name: () => t('Good taste'), how: () => t('A film you picked or voted for wins a crew vote three times.') },
  { id: 'generous', mark: '5★', tone: 'warning', name: () => t('Generous'), how: () => t('Give ten films or shows five stars, in your Diary or a review.') },
]

export default function Badges() {
  const [got, setGot] = useState<Record<string, Earned> | null>(null)
  const [pick, setPick] = useState('first')
  useEffect(() => {
    fetch('/api/home/party-badges', { credentials: 'include', cache: 'no-store' })
      .then((r) => (r.ok ? r.json() : null))
      .then((x: { badges?: Earned[] } | null) => {
        const m: Record<string, Earned> = {}
        for (const b of x?.badges ?? []) m[b.id] = b
        setGot(m)
      })
      .catch(() => setGot({}))
  }, [])
  if (!got) return null
  const earned = CATALOGUE.filter((d) => got[d.id]?.earned).length
  const sel = CATALOGUE.find((d) => d.id === pick) ?? CATALOGUE[0]
  const b = got[sel.id]
  let state: string
  if (sel.soon) state = t('Coming soon')
  else if (b?.earned) state = t('Earned {date}', { date: new Date(b.earned).toLocaleDateString(locale(), { day: 'numeric', month: 'long', year: 'numeric' }) })
  else if (b && b.need > 1 && b.have > 0) state = t('Not yet: {have} of {need}', { have: b.have, need: b.need })
  else state = t('Not yet')
  return (
    <section className="card badge-cabinet" aria-labelledby="badge-cabinet-title">
      <div className="badge-head">
        <h2 id="badge-cabinet-title">{t('Badge cabinet')}</h2>
        <span className="hint">{plural(earned, 'One of {total} earned', '{n} of {total} earned', { total: CATALOGUE.length })}</span>
      </div>
      <div className="badge-grid" role="group" aria-label={t('Badges')}>
        {CATALOGUE.map((d) => {
          const on = !!got[d.id]?.earned
          return (
            <button
              key={d.id}
              type="button"
              className={`badge-tile${on ? ' on' : ''}${d.soon ? ' soon' : ''}${pick === d.id ? ' picked' : ''}`}
              aria-pressed={pick === d.id}
              aria-label={`${d.name()}: ${on ? t('earned') : d.soon ? t('coming soon') : t('not yet')}`}
              onClick={() => setPick(d.id)}
            >
              <span className={`badge-mark tone-${d.tone}`} aria-hidden="true">
                {d.mark}
              </span>
              <span className="badge-name">{d.name()}</span>
            </button>
          )
        })}
      </div>
      <div className="badge-detail" aria-live="polite">
        <strong>{sel.name()}</strong>
        <p>{sel.how()}</p>
        <p className={`badge-state${b?.earned ? ' on' : ''}`}>{state}</p>
      </div>
    </section>
  )
}
