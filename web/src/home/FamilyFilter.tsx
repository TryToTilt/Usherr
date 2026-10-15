// Home fork: Discover's family filter, as one of v3's dropdowns.
import HelpLink from '../components/HelpLink'
import Dropdown from '../components/Dropdown'
import { t } from '../i18n'
import { useFamilyLevel, type FamilyLevel } from './useFamily'

export default function FamilyFilter() {
  const [level, setLevel] = useFamilyLevel()
  return (
    <>
    <Dropdown
      icon="shield"
      label={t('Family filter')}
      title={t('Leaves out what needs an older age, by Common Sense Media, the US rating or an AI estimate')}
      value={level}
      onChange={(v) => setLevel(v as FamilyLevel)}
      options={[
        { value: 'off', label: t('Family: off') },
        { value: '7', label: t('Little kids (up to 7)') },
        { value: '10', label: t('Kids (up to 10)') },
        { value: '13', label: t('Teens (up to 13)') },
      ]}
    />
    <HelpLink topic="home-finding#hide-what-i-have-and-the-family-filter" />
    </>
  )
}
