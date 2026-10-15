// Home fork: Discover's rails from what the household watches.
//   - "Because you watched": from a person's watching in Jellyfin (or Emby),
//     so it follows what they actually finish, not just what's on disk. An
//     administrator can switch to anyone in the household (people without a
//     Mediarium account included).
//   - "Complete the collection": films missing from film series you have
//     some of (movies only).
//   - The season's rail: Halloween in October, the holidays in December…
//   - "From people you follow": what they have new or coming, each with
//     who it's from (Follow is on person pages).
import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import type { AddTarget } from '../components/AddDialog'
import DiscoverRail from '../components/DiscoverRail'
import type { Kind, Source } from '../discoverSources'
import { sourceTitle, sourceToQuery } from '../discoverSources'
import { t } from '../i18n'
import type { Owned } from '../useOwned'
import { followWork, whoFor, type FollowWork } from './follows'
import Icon from '../components/Icon'
import RateButton from './RateButton'
import { useForYouWhy, useTaste } from './useTaste'

interface Person {
  id: string
  name: string
}

const WHO_KEY = 'discover-who'

async function getJSON<T>(path: string): Promise<T | null> {
  try {
    const r = await fetch(path, { credentials: 'include' })
    return r.ok ? ((await r.json()) as T) : null
  } catch {
    return null
  }
}

// The household and whose watching to use: kept in this browser.
function usePeople() {
  const [people, setPeople] = useState<Person[]>([])
  const [who, setWhoState] = useState('')
  useEffect(() => {
    void getJSON<{ people: Person[]; me: string }>('/api/home/discover/people').then((d) => {
      if (!d) return
      setPeople(d.people)
      let saved = ''
      try {
        saved = localStorage.getItem(WHO_KEY) ?? ''
      } catch {
        // no storage: use the default
      }
      setWhoState(d.people.some((p) => p.id === saved) ? saved : d.me)
    })
  }, [])
  const setWho = (id: string) => {
    setWhoState(id)
    try {
      localStorage.setItem(WHO_KEY, id)
    } catch {
      // remembering is optional
    }
  }
  return { people, who, setWho }
}

function useSeason() {
  const [season, setSeason] = useState('')
  useEffect(() => {
    void getJSON<{ key: string }>('/api/home/discover/season').then((d) => d && setSeason(d.key))
  }, [])
  return season
}

export default function DiscoverExtras({ kind, owned, onAdd }: { kind: Kind; owned: Owned; onAdd: (t: AddTarget) => void }) {
  const { people, who, setWho } = usePeople()
  const season = useSeason()
  const [seeds, setSeeds] = useState<string[]>([])
  useEffect(() => {
    if (!who) return
    void getJSON<{ titles: string[] }>(`/api/home/discover/watched-seeds?kind=${kind}&who=${encodeURIComponent(who)}`).then((d) => setSeeds(d?.titles ?? []))
  }, [kind, who])
  // Why each one is suggested: the watched titles that led to it.
  const [why, setWhy] = useState<Record<number, string>>({})
  useEffect(() => {
    if (!who) return
    void getJSON<{ why: Record<string, string[]> }>(`/api/home/discover/why?kind=${kind}&who=${encodeURIComponent(who)}`).then((d) => {
      const out: Record<number, string> = {}
      for (const [id, names] of Object.entries(d?.why ?? {})) {
        if (names.length === 1) out[Number(id)] = t('Because you watched {title}', { title: names[0] })
        else if (names.length > 1) out[Number(id)] = t('Because you watched {first} and {second}', { first: names[0], second: names[1] })
      }
      setWhy(out)
    })
  }, [kind, who])

  const [follows, setFollows] = useState<FollowWork | null>(null)
  useEffect(() => {
    followWork()
      .then(setFollows)
      .catch(() => setFollows(null))
  }, [])
  const followItems = (kind === 'movie' ? follows?.movies : follows?.shows) ?? []

  const watched: Source = { kind, list: 'watched', who: who || undefined }
  const collections: Source = { kind, list: 'collections' }
  const seasonal: Source = { kind, list: 'season', season }
  const name = people.find((p) => p.id === who)?.name ?? ''
  const hint = seeds.length > 0 ? t('because {name} watched {titles}', { name, titles: seeds.join(', ') }) : undefined

  return (
    <>
      {who && seeds.length > 0 && (
        <DiscoverRail
          key={sourceToQuery(watched)}
          title={sourceTitle(watched)}
          hint={hint}
          source={watched}
          kind={kind}
          owned={owned}
          onAdd={onAdd}
          rows={1}
          moreLabel={t('Browse more')}
          why={why}
          headExtra={
            people.length > 1 ? (
              <select className="disc-who" value={who} onChange={(e) => setWho(e.target.value)} aria-label={t('Whose watching')}>
                {people.map((p) => (
                  <option key={p.id} value={p.id}>
                    {p.name}
                  </option>
                ))}
              </select>
            ) : undefined
          }
        />
      )}
      {followItems.length > 0 && (
        <DiscoverRail
          title={t('From people you follow')}
          hint={t('new and coming soon')}
          items={followItems}
          kind={kind}
          owned={owned}
          onAdd={onAdd}
          rows={1}
          why={whoFor(follows, kind)}
          keepOwned
          scroll
          headExtra={
            <Link to="/following" className="btn-link">
              {t('Following')}
            </Link>
          }
        />
      )}
      {season && <DiscoverRail key={sourceToQuery(seasonal)} title={sourceTitle(seasonal)} source={seasonal} kind={kind} owned={owned} onAdd={onAdd} rows={1} moreLabel={t('Browse more')} />}
      {kind === 'movie' && (
        <DiscoverRail
          key={sourceToQuery(collections)}
          title={sourceTitle(collections)}
          hint={t('films missing from series you have some of')}
          source={collections}
          kind={kind}
          owned={owned}
          onAdd={onAdd}
          rows={1}
          moreLabel={t('Browse more')}
        />
      )}
    </>
  )
}

// ForYouRail: the taste engine's picks, from what you liked and watched, each
// with its match and why (home/useTaste.ts); first on Discover. Until there's
// a taste to go on, an invitation to Rate 20.
export function ForYouRail({ kind, owned, onAdd, invite = true }: { kind: Kind; owned: Owned; onAdd: (t: AddTarget) => void; invite?: boolean }) {
  const taste = useTaste(kind)
  const forYouWhy = useForYouWhy(kind, !!taste?.ready)
  const forYou: Source = { kind, list: 'foryou' }
  const leaning = (taste?.likes ?? [])
    .filter((l) => l.type !== 'decade')
    .slice(0, 3)
    .map((l) => l.name)

  return (
    <>
      {taste?.ready && (
        <DiscoverRail
          key={sourceToQuery(forYou)}
          title={sourceTitle(forYou)}
          hint={leaning.length > 0 ? t('you lean towards {list}', { list: leaning.join(', ') }) : undefined}
          source={forYou}
          kind={kind}
          owned={owned}
          onAdd={onAdd}
          rows={1}
          moreLabel={t('Browse more')}
          why={forYouWhy}
          headExtra={<RateButton />}
        />
      )}
      {invite && taste && !taste.ready && (
        <div className="taste-start">
          <Icon name="sparkles" size={20} />
          <div>
            <strong>{t('Get suggestions made for you')}</strong>
            <p className="hint">{t('Rate 20 titles, or watch a few in Jellyfin, and a For you row appears here, each title with how well it fits your taste.')}</p>
          </div>
          <RateButton className="btn-sm primary btn-with-icon" />
        </div>
      )}
    </>
  )
}
