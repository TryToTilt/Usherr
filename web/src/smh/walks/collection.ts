// Usherr SMH: complete a film series (a TMDB collection), or get the whole
// series at once. Works for any series: the person names a film from it.
import { api } from '../../api'
import { t } from '../../i18n'
import type { SmhChoice, SmhCtx, SmhStep, Walkthrough } from '../types'

const film = (c: SmhCtx) => (c.answers.film as { value: string; data: { title: string } } | undefined)
const coll = (c: SmhCtx) => c.answers.coll as { id: number; name: string } | null | undefined
const has = (id: string) => !!document.querySelector(`[data-walk="${id}"]`)

const find: SmhStep[] = [
  {
    kind: 'ask',
    key: 'film',
    title: t('Name any film from the series'),
    text: t('A trilogy or a franchise: type one of its films and pick it.'),
    search: async (q) =>
      (await api.tmdbSearch('movie', q)).slice(0, 8).map(
        (f): SmhChoice => ({ value: String(f.tmdbId), label: f.year ? `${f.title} (${f.year})` : f.title, image: f.posterUrl, data: { title: f.title } }),
      ),
  },
  {
    kind: 'do',
    title: t('Looking up its series'),
    run: async (c) => {
      const m = await api.tmdbMovieDetail(Number(film(c)?.value))
      return { coll: m.collection ? { id: m.collection.id, name: m.collection.name } : null }
    },
  },
  {
    kind: 'say',
    when: (c) => !coll(c),
    title: (c) => t('{title} isn’t part of a series', { title: film(c)?.data.title ?? '' }),
    text: t('TMDB doesn’t list it in a collection. Press Back and try another film from the series.'),
  },
  {
    kind: 'go',
    when: (c) => !!coll(c),
    to: (c) => `/title/${film(c)?.value}`,
    title: (c) => coll(c)?.name ?? '',
    text: t('This is the film’s page. Its series is further down.'),
  },
  {
    kind: 'point',
    target: 'collection-card',
    until: 'next',
    when: (c) => !!coll(c),
    title: t('The whole series'),
    text: t('Every film in it, in order. Bright posters are in your library; dimmed ones are missing; “Not out yet” are on the way.'),
  },
  {
    kind: 'say',
    when: (c) => !!coll(c) && !has('collection-add'),
    title: t('You have every film'),
    text: t('Nothing is missing from this series. Usherr can still add the next one when it joins: that’s the switch under the posters, Also add new films in this collection.'),
  },
  {
    kind: 'point',
    target: 'collection-add',
    act: 'click',
    until: { seen: 'collection-form' },
    when: (c) => !!coll(c) && has('collection-add'),
    title: t('Add the whole collection'),
    text: t('Adds only the films you’re missing. Films you already have are left alone.'),
  },
]

const go: SmhStep = {
  kind: 'point',
  target: 'collection-go',
  act: 'click',
  until: { gone: 'collection-go' },
  when: (c) => !!coll(c) && has('collection-go'),
  title: t('Add them'),
  text: (c) => (c.watch ? t('Adding them for you.') : t('Press Add.')),
}

const done: SmhStep = {
  kind: 'done',
  title: (c) => (coll(c) ? t('{name} is on its way', { name: coll(c)!.name }) : t('Try another film')),
  text: t('Usherr looks for each missing film and downloads it into your Library; Activity shows how it’s going. Films that aren’t out yet wait until they are.'),
  shortcut: t('Next time: any film’s page, its collection, Add the whole collection.'),
}

export const completeCollection: Walkthrough = {
  id: 'complete-collection',
  group: 'library',
  admin: true,
  needs: 'movies',
  title: t('Complete a film series'),
  blurb: t('Any trilogy or franchise: see what you have, what’s missing and what’s coming, and get the rest in one go.'),
  icon: 'grid',
  words: 'collection series franchise trilogy saga complete missing whole all at once get every film',
  simple: [
    ...find,
    {
      kind: 'point',
      target: 'collection-form',
      until: 'next',
      when: (c) => !!coll(c) && has('collection-form'),
      title: t('The usual choices'),
      text: t('Already picked: your usual quality, and searching right away. Nothing to change unless you want to.'),
    },
    go,
    done,
  ],
  advanced: [
    {
      kind: 'say',
      title: t('Library or Streams?'),
      text: t('A whole series goes into your Library: downloaded, to keep. To watch one film without downloading it, use Stream on that film’s page instead.'),
    },
    ...find,
    {
      kind: 'point',
      target: 'collection-profile',
      until: 'next',
      when: (c) => !!coll(c),
      title: t('Quality profile'),
      text: t('How good a copy to get: the size and quality Usherr looks for (1080p, 4K…). Default is your usual one.'),
    },
    {
      kind: 'point',
      target: 'collection-monitor',
      until: 'next',
      when: (c) => !!coll(c),
      title: t('Monitoring'),
      text: t('Monitored: Usherr searches for the films and downloads them. Not monitored: they’re only listed in your library, for later.'),
    },
    {
      kind: 'ask',
      key: 'follow',
      when: (c) => !!coll(c),
      title: t('What about films added to the series later?'),
      card: {
        title: t('Also add new films in this collection'),
        options: [
          { name: t('Yes, add them'), good: t('When a new film joins the series, Usherr adds it with these choices, by itself.'), bad: t('Every new film comes in, even a spin-off you may not want.') },
          { name: t('No, just these'), good: t('Only the films the series has today.'), bad: t('You add new ones yourself.') },
        ],
        later: t('It’s a switch on the series card, any time.'),
      },
      choices: [
        { value: 'yes', label: t('Yes, add them') },
        { value: 'no', label: t('No, just these'), recommended: true },
      ],
    },
    {
      kind: 'point',
      target: 'collection-follow',
      until: 'next',
      when: (c) => !!coll(c),
      title: t('Also add new films'),
      text: (c) => ((c.answers.follow as { value: string } | undefined)?.value === 'yes' ? t('Switch this on.') : t('Leave this off.')),
    },
    go,
    done,
  ],
}
