// Usherr SMH: a decision card. Every choice says the same things in the
// same places: the options with their good and bad sides and costs, what we
// recommend, and whether it can be changed later.
import Icon from '../components/Icon'
import { t } from '../i18n'
import type { SmhDecision } from './types'

export default function DecisionCard({ d }: { d: SmhDecision }) {
  return (
    <div className="smh-card">
      <div className="smh-card-title">{d.title}</div>
      <dl>
        {d.options.map((o) => (
          <div key={o.name} className="smh-opt">
            <dt>{o.name}</dt>
            <dd>
              <span className="good">{o.good}</span>
              {o.bad && <span className="bad">{o.bad}</span>}
              {o.cost && <span className="cost">{t('Costs: {cost}', { cost: o.cost })}</span>}
            </dd>
          </div>
        ))}
      </dl>
      {d.recommend && (
        <p className="smh-card-rec">
          <Icon name="star" size={13} /> {d.recommend}
        </p>
      )}
      <p className="smh-card-later">
        <Icon name="refresh" size={13} /> {d.later}
      </p>
    </div>
  )
}
