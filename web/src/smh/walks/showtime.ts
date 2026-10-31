// Usherr SMH: book a showtime (a film at a set time, with a pre-show first).
import { api } from '../../api'
import { t } from '../../i18n'
import { openShowtimeBooking } from '../../home/ShowtimePill'
import type { ShowtimeFilm } from '../../home/ShowtimeButton'
import type { SmhChoice, SmhCtx, SmhStep, Walkthrough } from '../types'

const film = (c: SmhCtx) => c.answers.film as { label: string; data: ShowtimeFilm } | undefined

const findFilm = (): SmhStep => ({
  kind: 'ask',
  key: 'film',
  title: t('Which film?'),
  text: t('Any film: one you have, one in Streams, or one Usherr streams for the night.'),
  search: async (q) =>
    (await api.tmdbSearch('movie', q)).slice(0, 8).map(
      (f): SmhChoice => ({
        value: String(f.tmdbId),
        label: f.year ? `${f.title} (${f.year})` : f.title,
        image: f.posterUrl,
        data: { tmdbId: f.tmdbId, title: f.title, year: f.year, posterUrl: f.posterUrl },
      }),
    ),
})

const openBooking = (): SmhStep => ({
  kind: 'do',
  title: t('Opening the booking window'),
  run: async (c) => {
    const f = film(c)
    if (f) openShowtimeBooking(f.data)
  },
})

const when = (): SmhStep => ({
  kind: 'point',
  target: 'showtime-when',
  until: 'next',
  title: t('When does the film start?'),
  text: t('Pick the day and time the film itself starts. The pre-show plays just before it.'),
})

const book = (): SmhStep => ({
  kind: 'point',
  target: 'showtime-book',
  act: 'click',
  until: { gone: 'showtime-book' },
  title: t('Book it'),
  text: (c) => (c.watch ? t('Booking it for you.') : t('Press Book it.')),
})

const pill = (): SmhStep => ({
  kind: 'point',
  target: 'showtime-pill',
  until: 'next',
  title: t('Your countdown'),
  text: t('This button counts down to the showtime, from any page. Whatever it says, it opens the Showtime screen.'),
})

export const bookShowtime: Walkthrough = {
  id: 'book-showtime',
  group: 'night',
  title: t('Book a showtime'),
  blurb: t('A film at a set time, with trailers and a pre-show first, like at the cinema.'),
  icon: 'clock',
  words: 'movie night cinema schedule preshow pre-show trailers',
  simple: [
    findFilm(),
    openBooking(),
    when(),
    book(),
    pill(),
    {
      kind: 'done',
      title: (c) => t('{title} is booked', { title: film(c)?.data.title ?? t('Your film') }),
      text: t('At showtime, open the Showtime screen in the browser on the TV and leave it there. It runs the pre-show and starts the film by itself.'),
      shortcut: t('Next time: the clock on any film card, then Book a showtime.'),
    },
  ],
  advanced: [
    findFilm(),
    {
      kind: 'ask',
      key: 'screen',
      title: t('How will you watch it?'),
      card: {
        title: t('Where the film plays'),
        options: [
          { name: t('On the Showtime screen'), good: t('Open Usherr in the TV’s browser and leave it: it counts down, plays the pre-show and the film.'), bad: t('Needs a browser on the TV or a computer plugged into it.') },
          { name: t('Play on TV'), good: t('The film opens on your TV’s Jellyfin app at showtime.'), bad: t('Only when a TV is set up for Play on TV.') },
          { name: t('Just the countdown'), good: t('The Marquee counts down and runs the pre-show; you start the film yourself.'), bad: t('You press play at the right moment.') },
        ],
        recommend: t('The Showtime screen: nothing to press on the night.'),
        later: t('You can change it on the booking any time before the show.'),
      },
      choices: [
        { value: 'here', label: t('On the Showtime screen'), recommended: true },
        { value: 'tv', label: t('Play on TV') },
        { value: 'none', label: t('Just the countdown') },
      ],
    },
    openBooking(),
    when(),
    {
      kind: 'point',
      target: 'showtime-preshow',
      until: 'next',
      title: t('How long a pre-show?'),
      text: t('Trailers of what’s coming, your bumpers and clips, then the film. Up to 10 minutes is a good start; None skips it.'),
    },
    {
      kind: 'point',
      target: 'showtime-theme',
      until: 'next',
      when: () => !!document.querySelector('[data-walk="showtime-theme"]'),
      title: t('A theme for the pre-show'),
      text: t('Themes pick the clips: Halloween, Christmas, a kids’ night. “Tonight’s” lets the schedule choose.'),
    },
    {
      kind: 'point',
      target: 'showtime-playon',
      until: 'next',
      title: t('Where the film plays'),
      text: (c) => {
        const pick = { here: t('On the Showtime screen'), tv: t('Play on TV'), none: t('Just the countdown') }[String(c.answers.screen)] ?? ''
        return t('Choose the one you picked: {pick}.', { pick })
      },
    },
    book(),
    pill(),
    {
      kind: 'go',
      to: '/showtime',
      title: t('The Showtime screen'),
      text: t('This is what the TV shows: your bookings, the countdown, then the pre-show and the film.'),
    },
    {
      kind: 'done',
      title: (c) => t('{title} is booked', { title: film(c)?.data.title ?? t('Your film') }),
      text: t('Your own clips and themes are under Settings, Pre-show. Show Me How has a walkthrough for that too.'),
      shortcut: t('Next time: the clock on any film card, then Book a showtime.'),
    },
  ],
}
