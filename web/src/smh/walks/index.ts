// Usherr SMH: every walkthrough, and the menu's groups.
import { t } from '../../i18n'
import type { SmhGroup, Walkthrough } from '../types'
import { completeCollection } from './collection'
import { communityBot, joinWatchParty, linkDiscord, partyChat, reviewInDiscord, ticketStubs } from './community'
import { comicArcs, comicReview, comicWeek, readComic, setupComics } from './comics'
import { askAi, completeLibrary, discover, followPerson, getShow, upcoming } from './discover'
import { diary, familyMember, filmLayout, phone, roulette, stack, streamOrKeep, tonight, vote } from './everyday'
import { betterCopy, importLibrary, moveTitle, restylePosters, subtitles, tags, whyFailed } from './library'
import { getBook, importMusic, listenAudiobook, makePlaylist, musicApps, playMusic, readEbook, tidyMusic } from './listen'
import { browseManga, getManga, mangaDexAccount, mangaSources, readManga } from './manga'
import { addArtist, connectJellyfin, downloadClients, importLists, naming } from './more'
import { followTeam, getSports, makeChannel, marquee, marqueeRemote, playOnTv, watchLiveTv } from './night'
import { badges, crewVote, hostScreening, linkUsherr, partyPhone, startCrew } from './party'
import { plexDrive } from './plex'
import { kidsRequests, notices, resetByEmail, stremioAddon } from './people'
import { setUpPreshow } from './preshow'
import { ebookToAudiobook } from './readtome'
import { backups, indexer, mediaTypes, migrate, quality, tvTime, usenetProvider, vpn } from './setup'
import { bookShowtime } from './showtime'

export const WALKS: Walkthrough[] = [
  tonight,
  roulette,
  askAi,
  discover,
  streamOrKeep,
  getShow,
  followPerson,
  upcoming,
  vote,
  stack,
  diary,
  filmLayout,
  bookShowtime,
  setUpPreshow,
  communityBot,
  linkDiscord,
  joinWatchParty,
  partyChat,
  reviewInDiscord,
  ticketStubs,
  badges,
  linkUsherr,
  hostScreening,
  partyPhone,
  startCrew,
  crewVote,
  marquee,
  marqueeRemote,
  watchLiveTv,
  makeChannel,
  followTeam,
  getSports,
  playMusic,
  addArtist,
  makePlaylist,
  musicApps,
  listenAudiobook,
  getBook,
  readEbook,
  getManga,
  readManga,
  browseManga,
  mangaDexAccount,
  readComic,
  comicArcs,
  comicWeek,
  comicReview,
  setupComics,
  mangaSources,
  ebookToAudiobook,
  completeCollection,
  completeLibrary,
  betterCopy,
  subtitles,
  tags,
  moveTitle,
  whyFailed,
  restylePosters,
  importMusic,
  tidyMusic,
  importLibrary,
  familyMember,
  kidsRequests,
  phone,
  stremioAddon,
  notices,
  resetByEmail,
  playOnTv,
  mediaTypes,
  usenetProvider,
  indexer,
  quality,
  tvTime,
  vpn,
  backups,
  migrate,
  connectJellyfin,
  plexDrive,
  downloadClients,
  naming,
  importLists,
]

export function walkById(id: string): Walkthrough | undefined {
  return WALKS.find((w) => w.id === id)
}

export const GROUPS: { id: SmhGroup; label: () => string }[] = [
  { id: 'watch', label: () => t('Watch') },
  { id: 'night', label: () => t('Movie night') },
  { id: 'listen', label: () => t('Listen') },
  { id: 'read', label: () => t('Read') },
  { id: 'people', label: () => t('People and devices') },
  { id: 'library', label: () => t('My library') },
  { id: 'setup', label: () => t('Setup') },
]
