import { lazy, Suspense, useEffect, type ComponentType, type ReactNode } from 'react'
import { Navigate, Route, BrowserRouter as Router, Routes, useLocation } from 'react-router-dom'
import { isAdmin } from './api'
import GuidePopupHost from './components/GuidePopup'
import { BASE_PATH, withoutBase } from './base'
import { t } from './i18n'
import { AuthProvider, useAuth } from './AuthContext'
import { ModulesProvider } from './ModulesContext'
import AppShell from './components/AppShell'
import ErrorBoundary from './components/ErrorBoundary'
import Dashboard from './pages/Dashboard'
import { lobbyOn } from './neu/look' // Usherr: the new layout
import { broadcastOn } from './onemenu/broadcast' // Usherr Broadcast: the one menu and its pages
import Splash from './components/Splash'
import Login from './pages/Login'
import MusicAlbumRedirect from './pages/MusicAlbumRedirect'
import NotFound from './pages/NotFound'
import SettingsLayout from './pages/settings/SettingsLayout'
import { SETTINGS_REDIRECTS } from './settingsNav'

// Mediarium Books (reader and player), an app of its own, loaded when opened.
import MusicPopout from './components/MusicPopout'
const BookshelfApp = lazy(() => import('./bookshelf/BookshelfApp'))
const ResetPassword = lazy(() => import('./pages/ResetPassword')) // Usherr: a reset email's link

// resetLinkToken is the secret in a reset email's link (/reset/<it>), signed in or not.
function resetLinkToken(): string {
  const m = /^\/reset\/([0-9a-f]{16,128})\/?$/.exec(withoutBase(window.location.pathname))
  return m ? m[1] : ''
}
const Marquee = lazy(() => import('./home/Marquee')) // Usherr: the lobby display
const MarqueeRemote = lazy(() => import('./home/MarqueeRemote')) // Usherr: the Marquee's phone remote
const MarqueeHub = lazy(() => import('./home/MarqueeHub')) // Usherr: the Marquee's own page
const PreshowSettings = lazy(() => import('./home/PreshowSettings')) // Usherr: the pre-show
const ArtworkSettings = lazy(() => import('./home/ArtworkSettings')) // Usherr: Re-Posterr, artwork picking
const PushCollectionsSettings = lazy(() => import('./home/PushCollectionsSettings')) // Usherr: collections pushed to Jellyfin and Plex
const PosterBadgesSettings = lazy(() => import('./home/PosterBadgesSettings')) // Usherr: badges on posters
const CommunitySettings = lazy(() => import('./home/CommunitySettings')) // Usherr: Community (preview)
const LinkedUsherrs = lazy(() => import('./home/LinkedUsherrs')) // Usherr: linked Usherrs
const PartyChatPage = lazy(() => import('./home/PartyChat')) // Usherr: Community, the watch party chat on a phone
const LobbyHome = lazy(() => import('./pages/LobbyHome')) // Usherr: Home in the new layout
const BroadcastHome = lazy(() => import('./broadcast/BroadcastHome')) // Usherr Broadcast: Home for the sofa
const ServerHub = lazy(() => import('./pages/ServerHub')) // Usherr: the new layout's Server area
const ShowtimePage = lazy(() => import('./home/ShowtimePage')) // Usherr: Showtimes at home
const Crews = lazy(() => import('./home/Crews')) // Usherr: Crews, the crew space between parties
const SportsPage = lazy(() => import('./home/Sports')) // Usherr: Sports
const SportsLeaguePage = lazy(() => import('./home/Sports').then((m) => ({ default: m.SportsLeaguePage }))) // Usherr

// The main pages are fetched as they are opened, not with the first load, so the
// page that opens first is ready sooner (the rest is a big part of the program).
// Once the app is up and the browser is idle they are all fetched in the
// background, so opening one later is as quick as if it had been there all along.
// (Moving between pages keeps the old page on screen until the new one is ready,
// so nothing flashes.)
const toFetchLater: (() => Promise<unknown>)[] = []
function page<P>(load: () => Promise<{ default: ComponentType<P> }>) {
  toFetchLater.push(load)
  return lazy(load)
}
const Upcoming = page(() => import('./pages/Upcoming'))
const Discover = page(() => import('./pages/Discover'))
const DiscoverAll = page(() => import('./pages/DiscoverAll'))
const Complete = page(() => import('./home/Complete')) // home fork: complete your library
const You = page(() => import('./home/You')) // home fork: your taste, votes and more
const Library = page(() => import('./pages/Library'))
const MovieDetail = page(() => import('./pages/MovieDetail'))
const Queue = page(() => import('./pages/Queue'))
const MusicDiscoverAll = page(() => import('./pages/MusicDiscoverAll'))
const MusicArtist = page(() => import('./pages/MusicArtist'))
const BookDetail = page(() => import('./pages/BookDetail'))
const BookWork = page(() => import('./pages/BookWork'))
const MusicRelease = page(() => import('./pages/MusicRelease'))
const Search = page(() => import('./pages/Search'))
const SeriesDetail = page(() => import('./pages/SeriesDetail'))
const ShowDetail = page(() => import('./pages/ShowDetail'))
const Person = page(() => import('./pages/Person')) // Usherr: with Follow (home/FollowButton)

// Fetches the pages above one after another once the browser has nothing else to do.
function fetchPagesWhenIdle() {
  const later = (fn: () => void) => (typeof window.requestIdleCallback === 'function' ? window.requestIdleCallback(fn, { timeout: 4000 }) : window.setTimeout(fn, 1500))
  let i = 0
  const next = () => {
    const load = toFetchLater[i++]
    if (!load) return
    load().catch(() => undefined).finally(() => later(next))
  }
  later(next)
}

// Pages most people open now and then are fetched when first opened, so the first load is smaller.
const Onboarding = lazy(() => import('./pages/Onboarding'))
const Profile = lazy(() => import('./pages/Profile'))
const ImportLibrary = lazy(() => import('./pages/ImportLibrary'))
const MusicImport = lazy(() => import('./pages/MusicImport'))
const MusicTidy = lazy(() => import('./pages/MusicTidy'))
const MusicPlaylists = lazy(() => import('./pages/MusicPlaylists').then((m) => ({ default: m.MusicPlaylists })))
const MusicPlaylistPage = lazy(() => import('./pages/MusicPlaylists').then((m) => ({ default: m.MusicPlaylistPage })))
const Statistics = lazy(() => import('./pages/Statistics'))
const WatchActivity = lazy(() => import('./pages/WatchActivity')) // Usherr: watch history and stats
const ManualImport = lazy(() => import('./pages/ManualImport'))
const ReleaseSearch = lazy(() => import('./pages/ReleaseSearch'))
const About = lazy(() => import('./pages/About'))
const DownloadRulesSettings = lazy(() => import('./pages/settings/DownloadSettings'))
const UsenetSettings = lazy(() => import('./pages/settings/DownloadSettings').then((m) => ({ default: m.UsenetSettings })))
const TorrentSettings = lazy(() => import('./pages/settings/DownloadSettings').then((m) => ({ default: m.TorrentSettings })))
const NamingSettings = lazy(() => import('./pages/settings/MediaSettings').then((m) => ({ default: m.NamingSettings })))
const GeneralSettings = lazy(() => import('./pages/settings/GeneralSettings'))
const SecuritySettings = lazy(() => import('./pages/Profile').then((m) => ({ default: m.SecuritySettings })))
const ServicesSettings = lazy(() => import('./pages/Profile').then((m) => ({ default: m.ServicesSettings })))
const RequestsSettings = lazy(() => import('./pages/Profile').then((m) => ({ default: m.RequestsSettings })))
const SignInSettings = lazy(() => import('./pages/Profile').then((m) => ({ default: m.SignInSettings })))
const Guide = lazy(() => import('./pages/Guide'))
const SystemSettings = lazy(() => import('./pages/settings/SystemSettings'))
const BackupSettings = lazy(() => import('./pages/settings/BackupSettings'))
const AllAccounts = lazy(() => import('./pages/Profile').then((m) => ({ default: m.AllAccounts })))
const AutomationSettings = lazy(() => import('./pages/settings/QualitySettings').then((m) => ({ default: m.AutomationSettings })))
const LogsSettings = lazy(() => import('./pages/settings/LogsSettings'))
const IndexerSettings = lazy(() => import('./pages/settings/IndexerSettings'))
const MediaSettings = lazy(() => import('./pages/settings/MediaSettings'))
const ModulesSettings = lazy(() => import('./pages/settings/ModulesSettings'))
const MetadataSettings = lazy(() => import('./pages/settings/MetadataSettings'))
const ListsSettings = lazy(() => import('./pages/settings/ListsSettings'))
const MediaServerSettings = lazy(() => import('./pages/settings/MediaServerSettings'))
const OtherAppsSettings = lazy(() => import('./pages/settings/OtherAppsSettings'))
const AppLinksSettings = lazy(() => import('./pages/settings/AppLinksSettings'))
const AppFrame = lazy(() => import('./pages/AppFrame'))
const MigrateSettings = lazy(() => import('./pages/settings/MigrateSettings'))
const NotificationSettings = lazy(() => import('./pages/settings/NotificationSettings'))
const QualitySettings = lazy(() => import('./pages/settings/QualitySettings'))
const SubtitleSettings = lazy(() => import('./pages/settings/SubtitleSettings'))
const VPNSettings = lazy(() => import('./pages/settings/VPNSettings'))
const ServiceChecksSettings = lazy(() => import('./pages/settings/ServiceChecksSettings')) // Usherr: Service checks
// home fork: its own pages, loaded when opened
const SchedulesSettings = lazy(() => import('./home/SchedulesSettings'))
const ExtrasSettings = lazy(() => import('./home/ExtrasSettings'))
const SetupChecklist = lazy(() => import('./home/SetupChecklist')) // home fork
const Nsfw = lazy(() => import('./home/Nsfw'))
const VideoBrowse = lazy(() => import('./home/Nsfw').then((m) => ({ default: m.VideoBrowse })))
const VideoDetail = lazy(() => import('./home/Nsfw').then((m) => ({ default: m.VideoDetail })))
const Comics = lazy(() => import('./home/Comics'))
const ComicSeriesPage = lazy(() => import('./home/Comics').then((m) => ({ default: m.ComicSeriesPage })))
const ComicArcPage = lazy(() => import('./home/ComicArcs').then((m) => ({ default: m.ComicArcPage })))
const Manga = lazy(() => import('./manga/Manga')) // Usherr
const AddManga = lazy(() => import('./manga/AddManga'))
const MangaDiscover = lazy(() => import('./manga/MangaDiscover'))
const MangaSeries = lazy(() => import('./manga/MangaSeries'))
const MangaSettings = lazy(() => import('./manga/MangaSettings'))
const MangaDexAccountPage = lazy(() => import('./manga/MangaDexAccount')) // Usherr: each person's own MangaDex account
const TV = lazy(() => import('./home/TV'))
const Streamed = lazy(() => import('./home/Streamed'))
const ReadToMePage = lazy(() => import('./home/ReadToMePage')) // Usherr: Read-to-Me
const ConnectApp = lazy(() => import('./pages/ConnectApp'))
// Usherr Broadcast: its own settings pages and the cards other pages gain there (onemenu/BroadcastSettings.tsx)
const broadcastSettings = () => import('./onemenu/BroadcastSettings')
const BStreamingPage = lazy(() => broadcastSettings().then((m) => ({ default: m.StreamingPage })))
const BLiveTVPage = lazy(() => broadcastSettings().then((m) => ({ default: m.LiveTVPage })))
const BPlayOnTVPage = lazy(() => broadcastSettings().then((m) => ({ default: m.PlayOnTVPage })))
const BAppsPage = lazy(() => broadcastSettings().then((m) => ({ default: m.AppsPage })))
const BComicsPage = lazy(() => broadcastSettings().then((m) => ({ default: m.ComicsPage })))
const BMusicPage = lazy(() => broadcastSettings().then((m) => ({ default: m.MusicPage })))
const BBooksPage = lazy(() => broadcastSettings().then((m) => ({ default: m.BooksPage })))
const BSportsPage = lazy(() => broadcastSettings().then((m) => ({ default: m.SportsPage })))
const BNsfwPage = lazy(() => broadcastSettings().then((m) => ({ default: m.NsfwPage })))
const BAiPage = lazy(() => broadcastSettings().then((m) => ({ default: m.AiPage })))
const BNewsPage = lazy(() => broadcastSettings().then((m) => ({ default: m.NewsPage })))
const BAddressesPage = lazy(() => broadcastSettings().then((m) => ({ default: m.AddressesPage })))
const BBroadcastCards = lazy(() => broadcastSettings().then((m) => ({ default: m.BroadcastCards })))
const BBroadcastQuietTimes = lazy(() => broadcastSettings().then((m) => ({ default: m.BroadcastQuietTimes })))
const BBroadcastServerGeneral = lazy(() => broadcastSettings().then((m) => ({ default: m.BroadcastServerGeneral })))
const BExtrasOrMoved = lazy(() => broadcastSettings().then((m) => ({ default: m.ExtrasOrMoved })))
const BBroadcastOnly = lazy(() => broadcastSettings().then((m) => ({ default: m.BroadcastOnly })))
const Find = lazy(() => import('./onemenu/Find')) // Usherr Broadcast: search, ask, discover and things you can do
const TVCode = lazy(() => import('./pages/TVCode')) // Usherr: approve a TV's sign-in code
const PhoneLink = lazy(() => import('./pages/PhoneLink')) // Usherr: Finish on your phone

// Pages only an administrator can use. A member who opens one (for example
// by typing its address) is sent somewhere they can use instead.
function AdminOnly({ children, fallback = '/settings/profile' }: { children: ReactNode; fallback?: string }) {
  const { user } = useAuth()
  return isAdmin(user) ? <>{children}</> : <Navigate to={fallback} replace />
}

// home fork: the NSFW 18+ pages, for administrators and accounts given the switch.
function NSFWOnly({ children }: { children: ReactNode }) {
  const { user } = useAuth()
  return isAdmin(user) || (user?.permissions?.nsfw ?? false) ? <>{children}</> : <Navigate to="/" replace />
}

function Gate() {
  const { loading, offline, slow, firstRunNeeded, user, needsWizard, refresh } = useAuth()
  const signedIn = !!user
  useEffect(() => {
    if (signedIn) fetchPagesWhenIdle()
  }, [signedIn])

  if (loading) {
    if (slow) {
      return (
        <Splash
          label={t('Mediarium is slow to answer')}
          hint={t('It may be busy with a download or an import. It keeps trying by itself.')}
          onRetry={() => void refresh()}
        />
      )
    }
    return <Splash label={offline ? t('Reconnecting to Mediarium') : t('Loading')} />
  }
  const resetToken = resetLinkToken()
  if (resetToken) {
    return (
      <Suspense fallback={<Splash label={t('Loading')} />}>
        <ResetPassword token={resetToken} />
      </Suspense>
    )
  }
  if (firstRunNeeded) {
    return (
      <Suspense fallback={<Splash label={t('Loading')} />}>
        <Onboarding />
      </Suspense>
    )
  }
  if (!user) {
    return <Login />
  }
  if (needsWizard) {
    return (
      <Suspense fallback={<Splash label={t('Loading')} />}>
        <Onboarding startStep={1} />
      </Suspense>
    )
  }

  return (
    <>
    <Routes>
      <Route
        path="/bookshelf/*"
        element={
          <Suspense fallback={<Splash label={t('Loading')} />}>
            <BookshelfApp />
          </Suspense>
        }
      />
      <Route path="/music-player" element={<MusicPopout />} />
      <Route
        path="/marquee"
        element={
          <Suspense fallback={<Splash label={t('Loading')} />}>
            <Marquee />
          </Suspense>
        }
      />
      <Route
        path="/connect-app"
        element={
          <Suspense fallback={<Splash label={t('Loading')} />}>
            <ConnectApp />
          </Suspense>
        }
      />
      <Route element={<AppShell />}>
        <Route index element={broadcastOn() ? <BroadcastHome /> : lobbyOn() ? <LobbyHome /> : <Dashboard />} /> {/* Usherr: the new layout (and Broadcast) has its own Home */}
        <Route path="/server" element={<AdminOnly fallback="/"><ServerHub /></AdminOnly>} /> {/* Usherr */}
        <Route path="/server/overview" element={<AdminOnly fallback="/"><Dashboard /></AdminOnly>} /> {/* Usherr: the old dashboard, under Server */}
        <Route path="/find" element={<Find />} /> {/* Usherr Broadcast */}
        <Route path="/search" element={<Search />} />
        <Route path="/search/releases" element={<ReleaseSearch />} />
        <Route path="/stats" element={<Navigate to="/settings/statistics" replace />} />
        <Route path="/import/manual" element={<AdminOnly><ManualImport /></AdminOnly>} />
        <Route path="/discover" element={<Discover />} />
        <Route path="/discover/all" element={<DiscoverAll />} />
        <Route path="/discover/complete" element={<Complete />} />
        <Route path="/person/:id" element={<Person />} />
        <Route path="/discover/music" element={<MusicDiscoverAll />} />
        <Route path="/library" element={<Library />} />
        <Route path="/import" element={<AdminOnly fallback="/library"><ImportLibrary /></AdminOnly>} />
        <Route path="/music/artist/:id" element={<MusicArtist />} />
        <Route path="/music/playlists" element={<MusicPlaylists />} />
        <Route path="/music/playlists/:id" element={<MusicPlaylistPage />} />
        <Route path="/music/album/:id" element={<MusicAlbumRedirect />} />
        <Route path="/music/release/:mbid" element={<MusicRelease />} />
        <Route path="/book/:id" element={<BookDetail />} />
        <Route path="/books/work/:key" element={<BookWork />} />
        <Route path="/music/import" element={<AdminOnly fallback="/library"><MusicImport /></AdminOnly>} />
        <Route path="/music/tidy" element={<AdminOnly fallback="/library"><MusicTidy /></AdminOnly>} />
        <Route path="/title/:tmdbId" element={<MovieDetail />} />
        <Route path="/series/:id" element={<SeriesDetail />} />
        <Route path="/show/:tmdbId" element={<ShowDetail />} />
        <Route path="/wanted" element={<Upcoming tab="wanted" />} />
        <Route path="/calendar" element={<Upcoming tab="calendar" />} />
        <Route path="/following" element={<Upcoming tab="following" />} /> {/* home fork */}
        <Route path="/leaving-soon" element={<Upcoming tab="leaving" />} /> {/* Usherr: clean-up rules */}
        <Route path="/you" element={<You />} /> {/* home fork: your taste, votes and more */}
        <Route path="/tv-code" element={<TVCode />} /> {/* Usherr: Sign in on a TV, the phone's half */}
        <Route path="/p/:code" element={<PhoneLink />} /> {/* Usherr: Finish on your phone */}
        <Route path="/marquee/remote" element={<AdminOnly><MarqueeRemote /></AdminOnly>} /> {/* Usherr */}
        <Route path="/marquee/manage" element={<AdminOnly><MarqueeHub /></AdminOnly>} /> {/* Usherr */}
        <Route path="/preshow/manage" element={<AdminOnly><PreshowSettings /></AdminOnly>} /> {/* Usherr */}
        <Route path="/showtime" element={<ShowtimePage />} /> {/* Usherr: Showtimes at home */}
        <Route path="/crews" element={<Crews />} /> {/* Usherr: Crews */}
        <Route path="/crews/:id" element={<Crews />} /> {/* Usherr: Crews */}
        <Route path="/showtime/chat/:id" element={<PartyChatPage />} /> {/* Usherr: Community, the watch party chat */}
        <Route path="/sports" element={<SportsPage />} /> {/* Usherr: Sports */}
        <Route path="/sports/league/:id" element={<SportsLeaguePage />} /> {/* Usherr */}
        <Route path="/you/diary" element={<You tab="diary" />} /> {/* home fork */}
        <Route path="/you/wrapped" element={<You tab="wrapped" />} /> {/* home fork */}
        <Route path="/you/stubs" element={<You tab="stubs" />} /> {/* Usherr: ticket stubs */}
        <Route path="/queue" element={<Queue />} />
        {/* home fork: NSFW 18+ (administrators only), comics, Live TV, the streamed library */}
        <Route path="/nsfw" element={<NSFWOnly><Nsfw /></NSFWOnly>} />
        <Route path="/scenes/*" element={<OldNsfwAddress />} /> {/* Usherr: the old address */}
        <Route path="/nsfw/:id" element={<NSFWOnly><VideoDetail /></NSFWOnly>} />
        <Route path="/nsfw/browse/:kind/:stashId" element={<NSFWOnly><VideoBrowse /></NSFWOnly>} />
        <Route path="/comics" element={<Comics />} />
        <Route path="/comics/:id" element={<ComicSeriesPage />} />
        <Route path="/comics/arc/:id" element={<ComicArcPage />} />
        <Route path="/manga" element={<Manga />} /> {/* Usherr */}
        <Route path="/manga/add" element={<AddManga />} />
        <Route path="/manga/discover" element={<MangaDiscover />} />
        <Route path="/manga/:id" element={<MangaSeries />} />
        <Route path="/tv" element={<TV />} />
        <Route path="/streamed" element={<Streamed />} />
        <Route path="/read-to-me" element={<ReadToMePage />} />
        <Route path="/read-to-me/:bookId" element={<ReadToMePage />} />
        <Route path="/apps/:id" element={<AppFrame />} />
        <Route path="/help" element={<Guide />} />
        <Route path="/help/:topic" element={<Guide />} />
        <Route path="/settings" element={<SettingsLayout />}>
          <Route index element={<Navigate to="general" replace />} />
          <Route path="modules" element={<AdminOnly><ModulesSettings /></AdminOnly>} />
          <Route path="manga" element={<AdminOnly><MangaSettings /></AdminOnly>} /> {/* Usherr */}
          <Route path="manga-account" element={<MangaDexAccountPage />} /> {/* Usherr: for every account */}
          <Route path="media" element={<AdminOnly><MediaSettings /></AdminOnly>} />
          <Route path="naming" element={<AdminOnly><NamingSettings /></AdminOnly>} />
          <Route path="quality" element={<AdminOnly><QualitySettings /></AdminOnly>} />
          <Route path="automation" element={<AdminOnly><AutomationSettings /></AdminOnly>} />
          <Route path="indexers" element={<AdminOnly><IndexerSettings /></AdminOnly>} />
          <Route path="usenet" element={<AdminOnly><UsenetSettings /></AdminOnly>} />
          <Route path="torrents" element={<AdminOnly><TorrentSettings /></AdminOnly>} />
          <Route path="download-rules" element={<AdminOnly><DownloadRulesSettings /></AdminOnly>} />
          <Route path="vpn" element={<AdminOnly><VPNSettings /></AdminOnly>} />
          <Route path="schedules" element={<AdminOnly><SchedulesSettings /><BBroadcastQuietTimes /></AdminOnly>} /> {/* home fork; Usherr Broadcast: and the quiet times */}
          <Route path="extras" element={<AdminOnly><BExtrasOrMoved page={<ExtrasSettings />} /></AdminOnly>} /> {/* home fork; Usherr Broadcast: its cards moved to their pages */}
          <Route path="re-posterr" element={<AdminOnly><ArtworkSettings /></AdminOnly>} /> {/* Usherr: Re-Posterr */}
          <Route path="collections" element={<AdminOnly><PushCollectionsSettings /></AdminOnly>} /> {/* Usherr: collections in Jellyfin and Plex */}
          <Route path="poster-badges" element={<AdminOnly><PosterBadgesSettings /></AdminOnly>} /> {/* Usherr: poster badges */}
          <Route path="community" element={<AdminOnly><CommunitySettings /></AdminOnly>} /> {/* Usherr: Community (preview) */}
          <Route path="linked" element={<AdminOnly><LinkedUsherrs /></AdminOnly>} /> {/* Usherr: linked Usherrs */}
          <Route path="setup" element={<AdminOnly><SetupChecklist /></AdminOnly>} /> {/* home fork */}
          <Route path="subtitles" element={<AdminOnly><SubtitleSettings /></AdminOnly>} />
          <Route path="metadata" element={<AdminOnly><MetadataSettings /><BBroadcastCards only={['scores']} /></AdminOnly>} /> {/* Usherr Broadcast: + Scores */}
          <Route path="lists" element={<AdminOnly><ListsSettings /></AdminOnly>} />
          <Route path="media-servers" element={<AdminOnly><MediaServerSettings /><BBroadcastCards only={['server', 'convert']} /></AdminOnly>} /> {/* Usherr Broadcast: + Jellyfin or Emby, Converting */}
          <Route path="other-apps" element={<AdminOnly><OtherAppsSettings /></AdminOnly>} />
          <Route path="addons" element={<AdminOnly><AppLinksSettings /></AdminOnly>} />
          <Route path="migrate" element={<AdminOnly><MigrateSettings /></AdminOnly>} />
          <Route path="notifications" element={<AdminOnly><NotificationSettings /></AdminOnly>} />
          <Route path="profile" element={<Profile />} />
          <Route path="system" element={<AdminOnly><SystemSettings /><BBroadcastServerGeneral /></AdminOnly>} /> {/* Usherr Broadcast: + General's server part */}
          <Route path="service-checks" element={<AdminOnly><ServiceChecksSettings /></AdminOnly>} /> {/* Usherr: Service checks */}
          <Route path="backup" element={<AdminOnly><BackupSettings /></AdminOnly>} />
          <Route path="accounts" element={<AdminOnly><AllAccounts /></AdminOnly>} />
          <Route path="requests" element={<AdminOnly><RequestsSettings /></AdminOnly>} />
          <Route path="sign-in" element={<AdminOnly><SignInSettings /></AdminOnly>} />
          <Route path="general" element={<GeneralSettings />} />
          <Route path="security" element={<SecuritySettings />} />
          <Route path="services" element={<ServicesSettings />} />
          <Route path="logs" element={<AdminOnly><LogsSettings /></AdminOnly>} />
          <Route path="about" element={<About />} />
          <Route path="statistics" element={<Statistics />} />
          <Route path="watching" element={<WatchActivity />} /> {/* Usherr: watch history and stats */}
          {/* Usherr Broadcast: the pages only Broadcast lists (other looks go to Settings > Extras) */}
          <Route path="streaming" element={<AdminOnly><BBroadcastOnly><BStreamingPage /></BBroadcastOnly></AdminOnly>} />
          <Route path="live-tv" element={<AdminOnly><BBroadcastOnly><BLiveTVPage /></BBroadcastOnly></AdminOnly>} />
          <Route path="play-on-tv" element={<AdminOnly><BBroadcastOnly><BPlayOnTVPage /></BBroadcastOnly></AdminOnly>} />
          <Route path="apps" element={<AdminOnly><BBroadcastOnly><BAppsPage /></BBroadcastOnly></AdminOnly>} />
          <Route path="comics" element={<AdminOnly><BBroadcastOnly><BComicsPage /></BBroadcastOnly></AdminOnly>} />
          <Route path="music" element={<AdminOnly><BBroadcastOnly><BMusicPage /></BBroadcastOnly></AdminOnly>} />
          <Route path="books" element={<AdminOnly><BBroadcastOnly><BBooksPage /></BBroadcastOnly></AdminOnly>} />
          <Route path="sports" element={<AdminOnly><BBroadcastOnly><BSportsPage /></BBroadcastOnly></AdminOnly>} />
          <Route path="nsfw" element={<AdminOnly><BBroadcastOnly><BNsfwPage /></BBroadcastOnly></AdminOnly>} />
          <Route path="ai" element={<AdminOnly><BBroadcastOnly><BAiPage /></BBroadcastOnly></AdminOnly>} />
          <Route path="news" element={<AdminOnly><BBroadcastOnly><BNewsPage /></BBroadcastOnly></AdminOnly>} />
          <Route path="addresses" element={<AdminOnly><BBroadcastOnly><BAddressesPage /></BBroadcastOnly></AdminOnly>} />
          {SETTINGS_REDIRECTS.map(([from, to]) => (
            <Route key={from} path={from.replace('/settings/', '')} element={<Navigate to={to} replace />} />
          ))}
        </Route>
        <Route path="/profile" element={<Navigate to="/settings/profile" replace />} />
        <Route path="/about" element={<Navigate to="/settings/about" replace />} />
        <Route path="*" element={<NotFound />} />
      </Route>
    </Routes>
    <GuidePopupHost />
    </>
  )
}

// A page that crashes while it draws shows a short message instead of a blank
// screen, and going to another page clears it.
function Guarded({ children }: { children: ReactNode }) {
  return <ErrorBoundary resetKey={useLocation().pathname}>{children}</ErrorBoundary>
}

// Usherr: NSFW 18+ was at /scenes; an old link or bookmark lands on the same page.
function OldNsfwAddress() {
  const l = useLocation()
  return <Navigate to={l.pathname.replace(/^\/scenes/, '/nsfw') + l.search} replace />
}

export default function App() {
  return (
    <Router basename={BASE_PATH || undefined}>
      <Guarded>
        <AuthProvider>
          <ModulesProvider>
            <Gate />
          </ModulesProvider>
        </AuthProvider>
      </Guarded>
    </Router>
  )
}
