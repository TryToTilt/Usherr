// Usherr Broadcast: the walkthroughs for what Broadcast has of its own: the
// menu's three ways to sit, the Options key and a title's More sheet, the
// Live TV guide made for a remote, the Movie night lobby, reading with a
// remote, and finishing on your phone. Signing a TV in with your phone works
// in every look, so that one is listed everywhere.
import { api } from '../../api'
import { t } from '../../i18n'
import { PRESS } from '../look'
import type { SmhChoice, SmhCtx, Walkthrough } from '../types'
import { openLobby } from './party'

const has = (id: string) => !!document.querySelector(`[data-walk="${id}"]`)
const pick = (c: SmhCtx, key: string) => (c.answers[key] as { value: string } | undefined)?.value

// ---- sign in on a TV ----

export const tvSignIn: Walkthrough = {
  id: 'tv-sign-in',
  group: 'people',
  title: t('Sign in on a TV with your phone'),
  blurb: t('No typing with the remote: the TV shows a code and a QR code, and your phone says yes.'),
  icon: 'tv',
  words: 'tv sign in login code qr phone remote shield google fire android box approve device',
  simple: [
    {
      kind: 'say',
      title: t('On the TV'),
      text: t('Open Usherr in the TV’s browser. On the sign-in page, choose Sign in with your phone: the TV shows a short code and a QR code. A code works once, for five minutes.'),
    },
    {
      kind: 'say',
      title: t('On your phone'),
      text: t('Scan the QR code with your phone’s camera while signed in to Usherr there, check the code matches the TV, and press Approve. The TV signs in as you.'),
    },
    { kind: 'go', to: '/tv-code', title: t('No camera handy?'), text: t('Type the code here instead, on this phone or computer.') },
    { kind: 'point', target: 'tv-code', until: 'next', title: t('Type the TV’s code'), text: t('Then Continue, check the code matches, and press Approve. Only approve a TV you can see right now.') },
    { kind: 'go', to: '/settings/profile', title: t('Signed-in TVs'), text: t('Each TV signed in this way is listed under Signed-in devices, where you can sign it out.') },
    {
      kind: 'point',
      target: 'tv-signin-card',
      until: 'next',
      optional: true,
      title: t('Sign in on a TV'),
      text: (c) => (c.admin ? t('An administrator can switch Sign in with your phone off here, for everyone.') : t('Type a TV’s code opens the page you just saw.')),
    },
    { kind: 'done', title: t('The TV is yours'), text: t('Nothing secret ever shows on the TV: the code is only good for this one sign-in.') },
  ],
}

// ---- finish on your phone ----

export const finishOnPhone: Walkthrough = {
  id: 'finish-on-phone',
  group: 'people',
  look: 'broadcast',
  title: t('Finish on your phone'),
  blurb: t('Typing with a remote is slow: open the same page on your phone, type there, and the TV catches up.'),
  icon: 'share',
  words: 'phone type typing keyboard remote tv finish handoff qr settings review chat search',
  simple: [
    {
      kind: 'say',
      title: t('Typing goes to your phone'),
      text: t('On a TV, pages with things to type (settings, a review, the chat) have Finish on your phone. It shows a QR code that opens the same page on your phone, signed in as you. The link works once, for ten minutes.'),
    },
    { kind: 'go', to: '/settings/profile', title: t('For example, your profile'), text: t('Every settings page has it at the top on a TV.') },
    { kind: 'point', target: 'phone-finish', act: 'click', until: { seen: 'phone-handoff' }, optional: true, title: t('Finish on your phone'), text: t('Opens the code.') },
    {
      kind: 'say',
      when: () => !has('phone-handoff'),
      title: t('It shows on a TV'),
      text: t('Finish on your phone appears on a TV-size screen: a TV’s own browser, or Text size TV under You, Your settings.'),
    },
    {
      kind: 'point',
      target: 'phone-handoff',
      until: 'next',
      optional: true,
      title: t('Scan it'),
      text: t('Point your phone’s camera at the code and open the link. Type on the phone, then press Done here: the TV loads what changed.'),
      remote: PRESS.ok,
    },
    { kind: 'done', title: t('No more typing with arrows'), text: t('The link carries only the page, never a password or a key.') },
  ],
}

// ---- the menu ----

export const menuModes: Walkthrough = {
  id: 'menu-modes',
  group: 'people',
  look: 'broadcast',
  title: t('Choose how the menu sits'),
  blurb: t('Always open, a row of icons, or hidden until you go left.'),
  icon: 'menu',
  words: 'menu sidebar rail icons hidden always open left edge remote layout navigation slide away',
  simple: [
    { kind: 'go', to: '/settings/general', title: t('You, Your settings'), text: t('Your settings, kept in this browser: the TV and your laptop can each have their own.') },
    {
      kind: 'point',
      target: 'menu-mode',
      until: 'next',
      title: t('The menu'),
      text: t('Always open keeps the whole menu in sight. Icons is a narrow row that opens when you move to it. Hidden keeps it out of the way until you go left.'),
      remote: () => t('Press Left or Right to change it'),
    },
    {
      kind: 'say',
      title: t('Opening it'),
      text: t('With a remote: press Left at the left edge of any page, and Right to leave the menu. With a mouse: the menu button, or point at the left edge. On a phone the places are in the bar along the bottom.'),
    },
    { kind: 'done', title: t('The menu is yours'), text: t('With a mouse, the open menu slides away after a few seconds; that’s the setting just under it.') },
  ],
}

// ---- the Options key and More ----

export const optionsMore: Walkthrough = {
  id: 'options-more',
  group: 'watch',
  look: 'broadcast',
  title: t('Everything else for a title (More)'),
  blurb: t('One big button, then More: the Options key opens the rest, on a title’s page or any poster.'),
  icon: 'more',
  words: 'more options key menu button remote context title page actions showtime watched rate subtitles manage artwork report',
  simple: [
    {
      kind: 'ask',
      key: 'film',
      title: t('Which film?'),
      text: t('Any film.'),
      search: async (q) =>
        (await api.tmdbSearch('movie', q)).slice(0, 8).map((f): SmhChoice => ({ value: String(f.tmdbId), label: f.year ? `${f.title} (${f.year})` : f.title, image: f.posterUrl })),
    },
    { kind: 'go', to: (c) => `/title/${pick(c, 'film')}`, title: (c) => (c.answers.film as { label: string }).label, text: t('One main button (Play, or Add it), then your Stack, the Trailer and More.') },
    { kind: 'point', target: 'title-more', act: 'click', until: { seen: 'more-sheet' }, title: t('More'), text: t('Opens everything else.'), remote: PRESS.more },
    {
      kind: 'point',
      target: 'more-sheet',
      until: 'next',
      title: t('The More sheet'),
      text: t('The everyday things first: book a showtime, mark it watched, rate it, subtitles. Administrators get theirs below: a better copy, a release, the artwork, the files.'),
      remote: PRESS.back,
    },
    {
      kind: 'say',
      title: t('On any poster too'),
      text: t('Move to a poster anywhere and press Options: that title’s More opens without leaving the page. The Options key is ☰ or Menu on most remotes, M on a keyboard, or hold OK.'),
    },
    { kind: 'done', title: t('That’s More'), text: t('Play/Pause on a focused poster plays it straight away.') },
  ],
}

// ---- reading with a remote ----

export const readWithRemote: Walkthrough = {
  id: 'read-with-remote',
  group: 'read',
  look: 'broadcast',
  title: t('Read with a remote'),
  blurb: t('Comics, manga and ebooks on the TV: turn pages with the arrows, and the Reader sheet for how it reads.'),
  icon: 'book',
  words: 'read reader remote tv comic manga ebook pdf page turn arrows sheet direction layout settings options',
  simple: [
    {
      kind: 'say',
      title: t('Turning pages'),
      text: t('Open any comic, manga volume or ebook. Left and Right turn the page the way the book reads (right to left for manga), Down goes on, and Fast forward and Rewind turn too.'),
    },
    {
      kind: 'say',
      title: t('The bar'),
      text: t('OK shows or hides the bar along the top; Up moves to it, Down comes back to the page. Back leaves the bar, then closes the book. Your place is kept on every device.'),
    },
    {
      kind: 'point',
      target: 'reader-sheet',
      until: 'next',
      optional: true,
      title: t('The Reader sheet'),
      text: t('Options opens it: reading direction, how pages show, and the rest. Left and Right change each one; it’s kept for each series.'),
      remote: PRESS.more,
    },
    {
      kind: 'say',
      when: () => !has('reader-sheet'),
      title: t('The Reader sheet'),
      text: t('In a book, press Options: reading direction, how pages show, and the rest. Left and Right change each one; it’s kept for each series. Back closes it.'),
    },
    { kind: 'done', title: t('Happy reading'), text: t('A mouse and touch work as before, right beside the remote.') },
  ],
}

// ---- the Live TV guide ----

export const liveTvGuide: Walkthrough = {
  id: 'live-tv-guide',
  group: 'night',
  look: 'broadcast',
  title: t('Use the Live TV guide with a remote'),
  blurb: t('The grid, what’s on now, reminders, and changing channel like a cable box.'),
  icon: 'tv',
  words: 'live tv guide grid remote channel up down remind reminder record start over favourite star epg',
  simple: [
    { kind: 'go', to: '/tv', title: t('Live TV'), text: t('The guide opens first.') },
    {
      kind: 'point',
      target: 'tv-guide',
      until: 'next',
      optional: true,
      title: t('The guide'),
      text: t('Channels down the side, two hours across, a glowing line at now. Up and Down go channel to channel at the same time; Left and Right go along a channel, and the hours follow.'),
      remote: () => t('Use the arrows, then OK'),
    },
    {
      kind: 'say',
      when: () => !has('tv-guide'),
      title: t('No channels yet'),
      text: t('An administrator makes channels on this page; Show me how has Make a Live TV channel.'),
    },
    { kind: 'point', target: 'tv-guide-card', until: 'next', optional: true, title: t('What’s focused'), text: t('The card under the grid says what it is. OK watches what’s on now, or opens a later programme.') },
    {
      kind: 'point',
      target: 'tv-guide-options',
      until: 'next',
      optional: true,
      title: t('Options'),
      text: t('Remind me (a message five minutes before), Record, Start over, and the channel’s star for your favourites.'),
      remote: PRESS.more,
    },
    {
      kind: 'say',
      title: t('While watching'),
      text: t('Up and Down change channel, the digits tune straight to one, and Back goes to the guide.'),
    },
    { kind: 'done', title: t('Happy channel surfing'), text: t('Guide style, under the guide, changes how it looks.'), then: { id: 'watch-live-tv', label: t('More about Live TV') } },
  ],
}

// ---- the Movie night lobby ----

export const movieNightLobby: Walkthrough = {
  id: 'movie-night-lobby',
  group: 'night',
  look: 'broadcast',
  title: t('The Movie night lobby'),
  blurb: t('Before the film: pick your seat, say hello with a quick reply, react, and use your phone as the remote.'),
  icon: 'clock',
  words: 'lobby movie night showtime seat seats quick replies reactions chat phone remote start film screening party',
  simple: [
    { kind: 'go', to: '/showtime', title: t('Movie night'), text: t('Your next showtime is at the top.') },
    openLobby(),
    {
      kind: 'say',
      when: () => !has('lobby'),
      title: t('Book a showtime first'),
      text: t('The lobby opens for your next showtime. Book one from More on a film’s page, or Book another here.'),
    },
    { kind: 'point', target: 'lobby-seats', until: 'next', optional: true, title: t('Pick your seat'), text: t('Press a free seat to sit, and your own to stand up again. Friends at linked Usherrs show here too.') },
    { kind: 'point', target: 'lobby-replies', until: 'next', optional: true, title: t('Quick replies'), text: t('One press says it in the chat, at every house. Write on my phone for anything longer.') },
    { kind: 'point', target: 'lobby-react', until: 'next', optional: true, title: t('React'), text: t('HA, EEK, WOW and the rest float up over the screen for everyone.') },
    { kind: 'point', target: 'lobby-phone', until: 'next', optional: true, title: t('Your phone as the remote'), text: t('Shows a code: your phone opens the screening’s page, with the chat and, for the host, pause and play for everyone.') },
    {
      kind: 'done',
      title: t('Enjoy the show'),
      text: t('Leave the lobby open on the TV: the pre-show and the film start on time. Back leaves the lobby.'),
      then: { id: 'party-phone', label: t('Use your phone at a screening') },
    },
  ],
}
