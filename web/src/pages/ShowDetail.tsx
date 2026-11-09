import { Suspense, lazy, useEffect, useState, type ReactNode } from 'react'
import { useStreamed } from '../home/useHave' // Usherr
import { describeState } from '../components/state' // Usherr
import VoteButtons from '../home/VoteButtons' // home fork
import StackButton from '../home/StackButton' // Usherr: your stack
import { SkipGuide, WhatsItLike } from '../home/AIGuides' // home fork
import { StreamingOn } from '../home/Recipes' // home fork
import CommunityReviews from '../home/CommunityReviews' // Usherr: Community, reviews
import { ScoresLine } from '../home/scores' // home fork
import { useNavigate, useParams } from 'react-router-dom'
import AddDialog from '../components/AddDialog'
import Icon from '../components/Icon'
import TitleHero, { TitleFacts } from '../components/TitleHero'
import { useFilmPage } from '../home/filmLayout' // Usherr: the title page's layouts
import TitleLayout from '../home/TitleLayout' // Usherr
import { lobbyOn } from '../neu/look' // Usherr: the new layout
import { broadcastOn } from '../onemenu/broadcast' // Usherr Broadcast: one main button, the rest in More
import { BroadcastTitleActions, markWatched, MoreSheet, PrimaryButton, useTitleMore, type MoreRow } from '../broadcast/MoreSheet' // Usherr Broadcast
import { openSmhMenu } from '../smh/engine' // Usherr Broadcast: Show me how, from More
import { openReport } from '../report' // Usherr Broadcast
import { useToast } from '../components/Toast' // Usherr Broadcast: Mark watched
import SimilarShows from '../home/SimilarShows' // Usherr
import { CastCard, PeopleLinks, SeasonsCard, titleLinks } from '../components/TitlePreview'
import { crewWith } from '../people'
import { api, can, isAdmin, type DiscoverMovie, type TVDetail } from '../api'
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
  const me = useAuth().user
  const admin = isAdmin(me) // Usherr: Re-Posterr
  const [show, setShow] = useState<TVDetail | null>(null)
  // Usherr: the layout this browser chose (Settings, General), and Manage.
  const { resolved: layout, show: shows } = useFilmPage()
  const [manageOpen, setManageOpen] = useState(false)
  // Usherr Broadcast: the More sheet (the Options key opens it too).
  const broadcast = broadcastOn()
  const moreSheet = useTitleMore(broadcast)
  const toast = useToast()
  const [similar, setSimilar] = useState<DiscoverMovie[]>([])
  useEffect(() => {
    api.tmdbSimilarShows(id).then(setSimilar).catch(() => undefined)
  }, [id])
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

  const info = { ...show, kind: 'tv' as const, certification: show.contentRating ?? show.certification, facts, links: titleLinks('tv', show.tmdbId, show.imdbId, show.homepage) }
  const status = streamedHere && (
    <span className="state-tag st-streams" title={describeState('streams').hint}>
      <Icon name="play" size={13} /> {describeState('streams').label}
    </span>
  )
  // The everyday actions; artwork (administrators) is under Manage.
  const everyday = (
    <>
      {streamedHere && (
        <button className="primary btn-with-icon big" onClick={() => void watchStreamed()}>
          <Icon name="play" size={18} /> {t('Watch (streams)')}
        </button>
      )}
      <button className={`${streamedHere ? '' : 'primary '}btn-with-icon big`} data-walk="title-add" onClick={() => setAdding(true)}>
        <Icon name="plus" size={18} /> {!lobbyOn() && !broadcast ? t('Add to library') : can(me, 'addDirect') ? t('Add it') : t('Ask for it')}
      </button>
      <StackButton item={{ kind: 'tv', tmdbId: show.tmdbId, title: show.title, year: show.year, posterUrl: show.posterUrl }} /> {/* Usherr */}
      <VoteButtons kind="tv" refId={show.tmdbId} meta={{ title: show.title, year: show.year, image: show.posterUrl }} /> {/* home fork */}
      <CatchUp tmdbId={show.tmdbId} /> {/* home fork */}
      <WhatsItLike kind="tv" tmdbId={show.tmdbId} title={show.title} /> {/* home fork */}
      <SkipGuide tmdbId={show.tmdbId} title={show.title} /> {/* home fork */}
      {admin && (
        <button className={`btn-with-icon${manageOpen ? ' primary' : ''}`} data-walk="title-manage" aria-expanded={manageOpen} onClick={() => setManageOpen(!manageOpen)} title={t('The artwork Jellyfin shows')}>
          <Icon name="sliders" size={16} /> {t('Manage')}
        </button>
      )}
    </>
  )
  // Usherr Broadcast: one main button (Watch when it streams, else Ask for
  // it), then Stack, Trailer and More; the rest is in the More sheet.
  moreSheet.playRef.current = streamedHere ? () => void watchStreamed() : undefined // the remote's Play key
  const ask = can(me, 'addDirect') ? t('Add it') : t('Ask for it')
  const broadcastActions = broadcast && (
    <BroadcastTitleActions
      primary={streamedHere ? <PrimaryButton icon="play" label={t('Watch (streams)')} onClick={() => void watchStreamed()} /> : <PrimaryButton icon="plus" walk="title-add" label={ask} onClick={() => setAdding(true)} />}
      stack={<StackButton item={{ kind: 'tv', tmdbId: show.tmdbId, title: show.title, year: show.year, posterUrl: show.posterUrl }} />}
      onMore={() => moreSheet.setOpen(true)}
    />
  )
  const moreRows: MoreRow[] = !broadcast
    ? []
    : [
        ...(streamedHere ? [{ key: 'ask', icon: 'plus' as const, label: ask, run: () => setAdding(true) }] : []),
        { key: 'watched', icon: 'check', label: t('Mark watched'), hint: t('In your diary'), run: () => markWatched('tv', show.tmdbId).then(() => toast.success(t('Marked watched.')), (e: Error) => toast.error(e.message)) },
        { key: 'rate', icon: 'star', label: t('Rate it'), node: <VoteButtons kind="tv" refId={show.tmdbId} meta={{ title: show.title, year: show.year, image: show.posterUrl }} /> },
        { key: 'catch-up', node: <CatchUp tmdbId={show.tmdbId} /> },
        { key: 'like', node: <WhatsItLike kind="tv" tmdbId={show.tmdbId} title={show.title} /> },
        { key: 'skip', node: <SkipGuide tmdbId={show.tmdbId} title={show.title} /> },
        { key: 'smh', icon: 'sparkles', label: t('Show me how'), hint: t('From here'), run: openSmhMenu },
        ...(!admin ? [{ key: 'report', icon: 'warning' as const, label: t('Something wrong with it?'), hint: t('Tell an admin'), run: () => openReport('problem') }] : []),
        ...(admin ? [{ key: 'art', admin: true, icon: 'image' as const, label: t('Change the artwork'), node: <ArtworkButton kind="tv" tmdbId={show.tmdbId} title={show.title} /> }] : []),
      ]
  const manage = admin && (
    <div className="fp-manage" data-walk="title-manage-panel">
      <div className="title-actions">
        <ArtworkButton kind="tv" tmdbId={show.tmdbId} title={show.title} /> {/* Usherr: a streamed show is in Jellyfin too */}
      </div>
    </div>
  )
  const seasons = (show.seasonList ?? []).length > 0 && <SeasonsCard seasons={show.seasonList ?? []} />
  return (
    <TitleLayout
      layout={layout}
      show={shows}
      hero={<TitleHero info={info} status={status} actions={broadcastActions || everyday} layout={layout} fullPlot={shows.fullPlot} />} /* Usherr Broadcast: its own buttons */
      facts={<TitleFacts info={info} />}
      scores={shows.scores && <ScoresLine kind="tv" tmdbId={show.tmdbId} />}
      where={shows.streaming && <StreamingOn kind="tv" tmdbId={show.tmdbId} />}
      reviews={shows.reviews && <CommunityReviews kind="tv" tmdbId={show.tmdbId} />}
      cast={shows.cast && (show.cast ?? []).length > 0 && <CastCard cast={show.cast ?? []} />}
      more={shows.similar && similar.length > 0 && <SimilarShows shows={similar} />}
      episodes={seasons}
      manage={manage}
      manageSummary={t('Not in your library')}
      manageOpen={manageOpen}
      setManageOpen={setManageOpen}
      label={t('About this show')}
      dialogs={
        <>
          {moreSheet.open && <MoreSheet eyebrow={show.title} rows={moreRows} onClose={() => moreSheet.setOpen(false)} />} {/* Usherr Broadcast */}
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
        </>
      }
    />
  )
}

