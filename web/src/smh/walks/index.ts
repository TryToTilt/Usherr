// Usherr SMH: every walkthrough, and the menu's groups.
import { t } from '../../i18n'
import type { SmhGroup, Walkthrough } from '../types'
import { completeCollection } from './collection'
import { comicArcs, comicReview, comicWeek, readComic, setupComics } from './comics'
import { askAi, completeLibrary, discover, followPerson, getShow, upcoming } from './discover'
import { diary, familyMember, phone, roulette, stack, streamOrKeep, tonight, vote } from './everyday'
import { betterCopy, importLibrary, moveTitle, restylePosters, subtitles, tags, whyFailed } from './library'
import { getBook, importMusic, listenAudiobook, makePlaylist, playMusic, readEbook } from './listen'
import { followTeam, makeChannel, marquee, marqueeRemote, playOnTv, watchLiveTv } from './night'
import { kidsRequests, notices, stremioAddon } from './people'
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
  bookShowtime,
  setUpPreshow,
  marquee,
  marqueeRemote,
  watchLiveTv,
  makeChannel,
  followTeam,
  playMusic,
  makePlaylist,
  listenAudiobook,
  getBook,
  readEbook,
  readComic,
  comicArcs,
  comicWeek,
  comicReview,
  setupComics,
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
  importLibrary,
  familyMember,
  kidsRequests,
  phone,
  stremioAddon,
  notices,
  playOnTv,
  mediaTypes,
  usenetProvider,
  indexer,
  quality,
  tvTime,
  vpn,
  backups,
  migrate,
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
