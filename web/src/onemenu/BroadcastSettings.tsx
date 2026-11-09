// Usherr Broadcast: the settings pages Broadcast adds (broadcastNav.ts). Each
// gathers the cards of one feature that used to sit together on Settings >
// Extras: the cards are Extras' own (home/ExtrasSettings.tsx), shown with
// `only`, so nothing is written twice and the setup wizard keeps using them
// as before. Classic and Neu still have Settings > Extras with all of them.
import type { ReactNode } from 'react'
import { Link, Navigate, useLocation } from 'react-router-dom'
import Icon from '../components/Icon'
import PublicAddress from '../components/PublicAddress'
import QuietHours from '../components/QuietHours'
import SpeedSchedule from '../components/SpeedSchedule'
import SubsonicCard from '../components/SubsonicCard'
import TipLabel from '../components/TipLabel'
import ExtrasSettings from '../home/ExtrasSettings'
import { t } from '../i18n'
import GeneralSettings from '../pages/settings/GeneralSettings'
import { broadcastOn } from './broadcast'
import { extrasMovedTo } from './broadcastNav'

const Cards = ({ only }: { only: string[] }) => <ExtrasSettings only={only} onPage />

// A short card that leads to the feature's own page.
function GoTo({ title, text, to, icon, label }: { title: string; text: string; to: string; icon: Parameters<typeof Icon>[0]['name']; label: string }) {
  return (
    <section className="card">
      <h2>{title}</h2>
      <p style={{ color: 'var(--text-dim)', marginTop: 0 }}>{text}</p>
      <Link className="btn-sm btn-with-icon" to={to}>
        <Icon name={icon} size={14} /> {label}
      </Link>
    </section>
  )
}

function Stack({ children }: { children: ReactNode }) {
  return <div className="settings-stack">{children}</div>
}

export function StreamingPage() {
  return (
    <>
      <Cards only={['streaming', 'streamed', 'drive']} />
      <Stack>
        <GoTo title={t('The streamed library')} text={t('Every film and show you streamed and kept, in one list.')} to="/streamed" icon="play" label={t('Open the streamed library')} />
      </Stack>
    </>
  )
}

export function LiveTVPage() {
  return (
    <>
      <Cards only={['livetv', 'tvpicture']} />
      <Stack>
        <GoTo title={t('Channels and the guide')} text={t('Build channels, plan what plays and watch them on the Live TV page.')} to="/tv" icon="tv" label={t('Open Live TV')} />
      </Stack>
    </>
  )
}

export function PlayOnTVPage() {
  return <Cards only={['playontv']} />
}

export function AppsPage() {
  return (
    <>
      <Cards only={['addon']} />
      <Stack>
        <SubsonicCard />
        <p className="hint">{t('Each person sets up their own music app under You, Your settings, Linked services.')}</p>
      </Stack>
    </>
  )
}

export function ComicsPage() {
  return (
    <>
      <Cards only={['comics', 'direct']} />
      <Stack>
        <GoTo title={t('The Comics page')} text={t('Your comic series, this week’s issues, story arcs and the comic settings for ComicVine.')} to="/comics" icon="book" label={t('Open Comics')} />
      </Stack>
    </>
  )
}

export function MusicPage() {
  return (
    <Stack>
      <GoTo title={t('Tidy up your music')} text={t('Fix tags, names and covers of the music you already have, a few albums at a time.')} to="/music/tidy" icon="sparkles" label={t('Open Tidy up')} />
      <GoTo title={t('Import music')} text={t('Bring music in from a folder: Usherr matches it and files it with the rest.')} to="/music/import" icon="download" label={t('Open Import music')} />
      <GoTo title={t('Music quality and what to show')} text={t('Music profiles and which releases are shown are with the other quality settings.')} to="/settings/quality" icon="star" label={t('Open Quality')} />
    </Stack>
  )
}

export function BooksPage() {
  return (
    <>
      <Cards only={['readtome']} />
      <Stack>
        <GoTo title={t('Read-to-Me')} text={t('Pick a book and turn it into an audiobook.')} to="/read-to-me" icon="headphones" label={t('Open Read-to-Me')} />
      </Stack>
    </>
  )
}

export function SportsPage() {
  return <Cards only={['sports']} />
}

export function NsfwPage() {
  return <Cards only={['nsfw']} />
}

export function AiPage() {
  return <Cards only={['ai']} />
}

export function NewsPage() {
  return <Cards only={['news', 'cinema']} />
}

// Both addresses side by side, each saying what it's for.
export function AddressesPage() {
  return (
    <>
      <Stack>
        <p className="hint span-all" style={{ margin: 0 }}>
          <strong>{t('From outside')}</strong>: {t('the address people use to reach Usherr from elsewhere (a link in an email, a phone away from home).')}
        </p>
        <PublicAddress />
        <p className="hint span-all" style={{ margin: 0 }}>
          <strong>{t('On your network')}</strong>: {t('the address your TV and your media server use to reach Usherr at home.')}
        </p>
      </Stack>
      <Cards only={['address']} />
    </>
  )
}

// Cards another page gains in Broadcast (Scores on Movie and show info;
// Jellyfin or Emby and Converting on Media servers). Nothing in other looks.
export function BroadcastCards({ only }: { only: string[] }) {
  return broadcastOn() ? <Cards only={only} /> : null
}

// Schedules and quiet times: the speed schedule and the notifications' quiet
// hours join the schedules (the cards are the same, their logic unchanged).
export function BroadcastQuietTimes() {
  if (!broadcastOn()) return null
  return (
    <div className="settings-stack" style={{ marginTop: 16 }}>
      <fieldset className="group folders">
        <legend>
          <Icon name="moon" size={14} /> <TipLabel help="downloads#speed-schedule">{t('Speed schedule')}</TipLabel>
        </legend>
        <SpeedSchedule />
      </fieldset>
      <QuietHours />
    </div>
  )
}

// Server and updates: General's server-wide part (updates through the API,
// usage numbers, the legal notice) sits with the server in Broadcast.
export function BroadcastServerGeneral() {
  return broadcastOn() ? <GeneralSettings part="server" /> : null
}

// /settings/extras: the page in other looks; in Broadcast its cards are on
// their features' pages, so an old link goes to the card's new page.
export function ExtrasOrMoved({ page }: { page: ReactNode }) {
  const { hash } = useLocation()
  return broadcastOn() ? <Navigate to={extrasMovedTo(hash)} replace /> : <>{page}</>
}

// A page only Broadcast lists: in other looks its address opens Settings > Extras.
export function BroadcastOnly({ children }: { children: ReactNode }) {
  return broadcastOn() ? <>{children}</> : <Navigate to="/settings/extras" replace />
}
