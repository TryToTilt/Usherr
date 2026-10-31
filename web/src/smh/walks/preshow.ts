// Usherr SMH: set up the pre-show for movie night.
import { t } from '../../i18n'
import type { SmhStep, Walkthrough } from '../types'

const page: SmhStep = {
  kind: 'go',
  to: '/preshow/manage',
  title: t('The pre-show'),
  text: t('Before a film, Usherr plays a short show like a cinema’s: a welcome, a reminder to silence phones, trailers of what’s coming, your own clips, the countdown, then “Feature presentation”. “Movie night” is ready, so there’s nothing you have to set up.'),
}

const sample: SmhStep = {
  kind: 'point',
  target: 'psm-sample',
  until: 'next',
  title: t('See it'),
  text: t('Watch a sample plays tonight’s pre-show here, without a film. Skip or close it to come back.'),
}

const theater: SmhStep = {
  kind: 'point',
  target: 'psm-theater',
  until: 'next',
  title: t('Your theater’s name'),
  text: t('Shown on the welcome card: “Welcome to …”. Type a name, then click anywhere else to save it.'),
}

const clips: SmhStep = {
  kind: 'point',
  target: 'psm-upload',
  until: 'next',
  title: t('Your own clips (optional)'),
  text: t('An old cinema ad, a sound logo, a holiday bumper: type a category, then Upload clips. A category named after a holiday (Halloween, Christmas) plays only in its season. MP4 plays everywhere.'),
}

const done: SmhStep = {
  kind: 'done',
  title: t('Your pre-show is ready'),
  text: t('It plays before every Showtime. Book a film for a time, and leave the Showtime screen open on the TV.'),
  then: { id: 'book-showtime', label: t('Book a showtime now') },
}

export const setUpPreshow: Walkthrough = {
  id: 'preshow',
  group: 'night',
  admin: true,
  title: t('Set up a pre-show for movie night'),
  blurb: t('Trailers, your own clips and the cinema look before the film. Ready as it is; make it yours.'),
  icon: 'film',
  words: 'preshow pre-show trailers bumpers clips intro cinema theater movie night',
  simple: [page, sample, theater, clips, done],
  advanced: [
    page,
    sample,
    {
      kind: 'point',
      target: 'psm-before',
      until: 'next',
      title: t('Before any film, too'),
      text: t('Normally the pre-show plays before a booked Showtime. Switch this on to have it before any film you press Play on in this browser. Skip is always on screen.'),
    },
    {
      kind: 'point',
      target: 'psm-themes',
      until: 'next',
      title: t('Themes'),
      text: t('A theme is the pre-show piece by piece, in order: cards (welcome, silence your phones, countdown), clips from a category, the holiday’s clip, coming-soon trailers. Edit one to change its pieces; Only when… limits a piece to some films, like a Dolby logo only before Atmos films.'),
    },
    {
      kind: 'point',
      target: 'psm-schedule',
      until: 'next',
      title: t('When each theme plays'),
      text: t('Pick a theme for days of the week, dates or holidays: a kids’ theme on Saturday mornings, a scary one in October. A Showtime can also pick its own.'),
    },
    {
      kind: 'point',
      target: 'psm-clips',
      until: 'next',
      title: t('Clips'),
      text: t('Your clips, by category. Each category is a folder. Untick a clip to leave it out without deleting it. Clips take turns, so the same ones don’t come round every night.'),
    },
    clips,
    theater,
    done,
  ],
}
