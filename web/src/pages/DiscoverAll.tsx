import { useEffect, useMemo, useState } from 'react'
import { ActiveChips, CompactBar, FiltersButton, type ActiveFilter } from '../components/CompactBar' // Usherr
import { useHideOwned } from '../useHideOwned' // Usherr
import { useFamilyLevel } from '../home/useFamily' // Usherr
import { Link, useSearchParams } from 'react-router-dom'
import { api } from '../api'
import AddDialog, { type AddTarget } from '../components/AddDialog'
import Icon from '../components/Icon'
import PagedGrid from '../components/PagedGrid'
import Switch from '../components/Switch'
import { KIND_LABEL, useKinds } from '../ModulesContext'
import { sourceFromQuery, sourceTitle, sourceToQuery } from '../discoverSources'
import { useOwned } from '../useOwned'
import { useIncludeOlder } from '../useIncludeOlder'
import HideOwned from '../home/HideOwned' // home fork
import FamilyFilter from '../home/FamilyFilter' // home fork
import { t } from '../i18n'

// One Discover list on its own page, split into pages you can step through
// (or extend with Load more).
export default function DiscoverAll() {
  const [filtersOpen, setFiltersOpen] = useState(false) // Usherr
  const [hideOwned, setHideOwned] = useHideOwned() // Usherr
  const [familyLevel, setFamilyLevel] = useFamilyLevel() // Usherr
  const [params, setParams] = useSearchParams()
  const [includeOlder, setIncludeOlder] = useIncludeOlder()
  const kindsOn = useKinds().filter((k) => k !== 'music')
  const parsed = useMemo(() => sourceFromQuery(params), [params])
  const similar = parsed.list === 'similar'
  // "More like your library" follows the switch for older titles, which is
  // remembered in this browser, not the address.
  const source = useMemo(() => (similar ? { ...parsed, older: includeOlder || undefined } : parsed), [parsed, similar, includeOlder])
  const owned = useOwned()
  const [adding, setAdding] = useState<AddTarget | null>(null)
  const [genreName, setGenreName] = useState<string | undefined>()

  useEffect(() => {
    if (!source.genre) return setGenreName(undefined)
    api
      .discoverGenres(source.kind)
      .then((g) => setGenreName(g.find((x) => String(x.id) === source.genre)?.name))
      .catch(() => undefined)
  }, [source])

  // Usherr: the filters that are on, each a chip with an x.
  const activeFilters: ActiveFilter[] = [
    ...(hideOwned ? [{ key: 'hide', label: t('Hiding what I have'), clear: () => setHideOwned(false) }] : []),
    ...(familyLevel !== 'off' ? [{ key: 'family', label: t('Family: up to {age}', { age: familyLevel }), clear: () => setFamilyLevel('off') }] : []),
    ...(similar && includeOlder ? [{ key: 'older', label: t('Include older titles'), clear: () => setIncludeOlder(false) }] : []),
  ]

  return (
    <div>
      {/* Usherr: one slim row: back, the list's name and Filters; filters that are on show as chips */}
      <CompactBar
        open={filtersOpen}
        row={
          <>
            <Link to="/discover" className="btn btn-with-icon" aria-label={t('Discover')}>
              <Icon name="chevron-left" size={16} /> <span className="cbar-add-label">{t('Discover')}</span>
            </Link>
            <h2 className="cbar-title">{sourceTitle(source, genreName)}</h2>
            <FiltersButton open={filtersOpen} onToggle={() => setFiltersOpen((o) => !o)} count={activeFilters.length} />
          </>
        }
        panel={
          <>
            <HideOwned label={t('Hide what I have')} /> {/* home fork: choose on disk, streamed, wanted */}
            <FamilyFilter /> {/* home fork */}
            {similar && kindsOn.length > 1 && (
              <div className="seg">
                {kindsOn.map((k) => (
                  <button key={k} aria-pressed={source.kind === k} className={source.kind === k ? 'active' : ''} onClick={() => setParams({ list: 'similar', kind: k }, { replace: true })}>
                    {KIND_LABEL[k]}
                  </button>
                ))}
              </div>
            )}
            {similar && <Switch checked={includeOlder} onChange={setIncludeOlder} label={t('Include older titles')} />}
          </>
        }
        chips={<ActiveChips filters={activeFilters} onClearAll={() => (setHideOwned(false), setFamilyLevel('off'))} />}
      />

      <PagedGrid key={sourceToQuery(source)} source={source} kind={source.kind} owned={owned} onAdd={setAdding} rows={4} />

      {adding && (
        <AddDialog
          target={adding}
          onClose={() => setAdding(null)}
          onAdded={() => {
            // Stay here so you can keep browsing: the card switches to "In library" by itself.
            setAdding(null)
            owned.reload()
          }}
        />
      )}
    </div>
  )
}
