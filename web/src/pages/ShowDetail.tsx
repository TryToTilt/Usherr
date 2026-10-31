import { Suspense, lazy, useEffect, useState, type ReactNode } from 'react'
import { useStreamed } from '../home/useHave' // Usherr
import { describeState } from '../components/state' // Usherr
import VoteButtons from '../home/VoteButtons' // home fork
import StackButton from '../home/StackButton' // Usherr: your stack
import { SkipGuide, WhatsItLike } from '../home/AIGuides' // home fork
import { StreamingOn } from '../home/Recipes' // home fork
import { ScoresLine } from '../home/scores' // home fork
import { useNavigate, useParams } from 'react-router-dom'
import AddDialog from '../components/AddDialog'
import Icon from '../components/Icon'
import TitleHero from '../components/TitleHero'
import { CastCard, PeopleLinks, SeasonsCard, titleLinks } from '../components/TitlePreview'
import { crewWith } from '../people'
import { api, isAdmin, type TVDetail } from '../api'
import { useAuth } from '../AuthContext'
import ArtworkButton from '../home/ArtworkButton' // Usherr: Re-Posterr
import { useDocumentTitle } from '../documentTitle'
import { t } from '../i18n'
import CatchUp from '../home/CatchUp' // Catch me up (home fork)
import { showStatusLabel } from '../home/showStatus' // home fork

// Usherr: the streamed episode list loads only when it's opened.
const StreamedSheet = lazy(() => import('../home/Streamed').then((m) => ({ default: m.StreamedSheet })))

// A TV show that is not in the library yet: full info plus one action.
// Shows you already own go straight to their library page instead.
export default function ShowDetail() {
  const { tmdbId } = useParams<{ tmdbId: string }>()
  const navigate = useNavigate()
  const id = Number(tmdbId)
  const admin = isAdmin(useAuth().user) // Usherr: Re-Posterr
  const [show, setShow] = useState<TVDetail | null>(null)
  useDocumentTitle(show?.title)
  const [error, setError] = useState('')
  const [adding, setAdding] = useState(false)
  const streamedHere = useStreamed('tv')(id) // Usherr: a streamed show says so
  const [sheet, setSheet] = useState(0) // Usherr: the streamed show's episode list, open
  const watchStreamed = () =>
    fetch(`/api/streamed/lookup?kind=series&tmdbId=${id}`, { credentials: 'include' })
      .then((r) => (r.ok ? r.json() : null))
      .then((d: { found: boolean; titleId?: number } | null) => d?.found && d.titleId && setSheet(d.titleId))
      .catch(() => undefined)

  useEffect(() => {
    api
      .tmdbTVDetail(id)
      .then((d) => {
        if (d.libraryId) navigate(`/series/${d.libraryId}`, { replace: true })
        else setShow(d)
      })
      .catch((e) => setError(e instanceof Error ? e.message : String(e)))
  }, [id, navigate])

  if (error) return <p className="error-text">{error}</p>
  if (!show) return <div className="skeleton" style={{ height: 380, borderRadius: 22 }} />

  const creators = crewWith(show.crew, 'Creator')
  const facts = [
    creators.length ? { label: creators.length > 1 ? t('Creators') : t('Creator'), value: <PeopleLinks people={creators} /> } : null,
    show.firstAirDate ? { label: t('First aired'), value: show.firstAirDate } : null,
    show.seasons ? { label: t('Seasons'), value: String(show.seasons) } : null,
    show.episodes ? { label: t('Episodes'), value: String(show.episodes) } : null,
    show.networks?.length ? { label: t('Network'), value: show.networks.join(', ') } : null,
    show.releaseStatus ? { label: t('Status'), value: showStatusLabel(undefined, show.releaseStatus) ?? show.releaseStatus } : null, // home fork: in words
  ].filter(Boolean) as { label: string; value: ReactNode }[]

  return (
    <div>
      <TitleHero
        info={{ ...show, kind: 'tv', certification: show.contentRating ?? show.certification, facts, links: titleLinks('tv', show.tmdbId, show.imdbId, show.homepage) }}
        actions={
          <>
            {streamedHere && (
              <span className="state-tag st-streams" title={describeState('streams').hint}>
                <Icon name="play" size={13} /> {describeState('streams').label}
              </span>
            )}
            {streamedHere && (
              <button className="primary btn-with-icon big" onClick={() => void watchStreamed()}>
                <Icon name="play" size={18} /> {t('Watch (streams)')}
              </button>
            )}
            <button className={`${streamedHere ? '' : 'primary '}btn-with-icon big`} data-walk="title-add" onClick={() => setAdding(true)}>
              <Icon name="plus" size={18} /> {t('Add to library')}
            </button>
            <CatchUp tmdbId={show.tmdbId} /> {/* home fork */}
            <WhatsItLike kind="tv" tmdbId={show.tmdbId} title={show.title} /> {/* home fork */}
            <VoteButtons kind="tv" refId={show.tmdbId} meta={{ title: show.title, year: show.year, image: show.posterUrl }} /> {/* home fork */}
            <StackButton item={{ kind: 'tv', tmdbId: show.tmdbId, title: show.title, year: show.year, posterUrl: show.posterUrl }} /> {/* Usherr */}
            {admin && <ArtworkButton kind="tv" tmdbId={show.tmdbId} title={show.title} />} {/* Usherr: a streamed show is in Jellyfin too */}
            <SkipGuide tmdbId={show.tmdbId} title={show.title} /> {/* home fork */}
          </>
        }
      >
        <ScoresLine kind="tv" tmdbId={show.tmdbId} /> {/* home fork */}
        <StreamingOn kind="tv" tmdbId={show.tmdbId} /> {/* home fork */}
      </TitleHero>
      <SeasonsCard seasons={show.seasonList ?? []} />
      <CastCard cast={show.cast ?? []} />
      {sheet > 0 && (
        <Suspense fallback={null}>
          <StreamedSheet id={sheet} tmdbId={show.tmdbId} title={show.title} onClose={() => setSheet(0)} />
        </Suspense>
      )}
      {adding && (
        <AddDialog
          target={{ kind: 'tv', tmdbId: show.tmdbId, title: show.title, year: show.year, posterUrl: show.posterUrl, overview: show.overview }}
          onClose={() => setAdding(false)}
          onAdded={(libraryId) => navigate(`/series/${libraryId}`)}
        />
      )}
    </div>
  )
}
