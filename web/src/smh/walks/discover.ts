// Usherr SMH: finding things. Discover and its filters, getting a show,
// following a person, Upcoming, Complete your library, and the Ask bar.
import { api } from '../../api'
import { t } from '../../i18n'
import type { SmhChoice, SmhCtx, SmhStep, Walkthrough } from '../types'

const pick = (c: SmhCtx, key: string) => (c.answers[key] as { value: string; label: string } | undefined)?.value
const has = (id: string) => !!document.querySelector(`[data-walk="${id}"]`)
const titleAsk = (kind: 'movie' | 'tv', title: string, text: string): SmhStep => ({
  kind: 'ask',
  key: 'film',
  title,
  text,
  search: async (q) =>
    (await api.tmdbSearch(kind, q)).slice(0, 8).map((f): SmhChoice => ({ value: String(f.tmdbId), label: f.year ? `${f.title} (${f.year})` : f.title, image: f.posterUrl })),
})

// ---- Discover ----

const discoverSteps = (advanced: boolean): SmhStep[] => [
  { kind: 'go', to: '/discover', title: t('Discover'), text: t('Rows of films and shows to browse, each picked a different way: top rated, hidden gems, from people you follow, for you.') },
  { kind: 'point', target: 'kind-pick', until: 'next', path: '/discover', title: t('What to show'), text: t('Movies, TV, Anime, and books, music and comics when they’re on. Pick one here.') },
  { kind: 'point', target: 'filters-btn', act: 'click', until: { seen: 'hide-owned' }, path: '/discover', title: t('Filters'), text: t('Genres, years, rating, runtime, and two handy switches.') },
  {
    kind: 'point',
    target: 'hide-owned',
    until: 'next',
    title: t('Hide what I have'),
    text: t('Leaves out what the house already has, so every row is new to you. The menu next to it chooses what counts: downloaded, streamed, or wanted (added but not here yet).'),
  },
  {
    kind: 'point',
    target: 'family-filter',
    until: 'next',
    optional: true,
    title: t('The family filter'),
    text: t('Leaves out anything for an older age: little kids (up to 7), kids (10) or teens (13). Both switches are remembered on each device, so the TV room and a phone can differ.'),
  },
  {
    kind: 'say',
    title: t('On every card'),
    text: t('Add puts it in your library (Add early waits for something not out yet). Open is for something already here. The × means not interested: it’s never suggested again. Browse more opens a whole row.'),
  },
  ...(advanced
    ? ([
        {
          kind: 'point',
          target: 'recipes-bar',
          until: 'next',
          optional: true,
          title: t('Your recipes'),
          text: t('Your own rows: a recipe is a set of filters with a name (genres, years, rating, who’s in it, an age rating), kept up to date by itself. New recipe starts one. Choose my services tells Usherr which streaming services you pay for, for the On my services rows.'),
        },
        {
          kind: 'say',
          title: t('Rows worth knowing'),
          text: t('Because you watched… follows what you finish in Jellyfin. From people you follow shows new work by actors and directors you follow. Copy to mine turns a ready-made row into your own recipe; the eye hides a row just for you.'),
        },
      ] as SmhStep[])
    : []),
  { kind: 'done', title: t('Happy browsing'), text: t('Tap any poster for its page: the story, cast, trailers, where it streams, and similar titles.'), shortcut: t('Teach Usherr your taste, in Show me how, makes the For you row appear.') },
]

export const discover: Walkthrough = {
  id: 'discover',
  group: 'watch',
  title: t('Browse Discover'),
  blurb: t('The rows, the filters, Hide what I have and the family filter.'),
  icon: 'compass',
  words: 'discover browse rows filters hide what i have family filter kids recipes services',
  simple: discoverSteps(false),
  advanced: discoverSteps(true),
}

// ---- a TV show ----

export const getShow: Walkthrough = {
  id: 'get-show',
  group: 'watch',
  needs: 'tv',
  title: t('Get a TV show'),
  blurb: t('The whole show, only new episodes, or just the ones you pick.'),
  icon: 'tv',
  words: 'tv show series season episodes add download monitor new episodes',
  simple: [
    titleAsk('tv', t('Which show?'), t('Type its name and pick it.')),
    { kind: 'go', to: (c) => `/show/${pick(c, 'film')}`, title: (c) => (c.answers.film as { label: string }).label, text: t('Its page.') },
    {
      kind: 'point',
      target: 'title-add',
      act: 'click',
      until: { seen: 'add-monitor' },
      optional: true,
      title: t('Add to library'),
      text: t('Opens the choices.'),
    },
    {
      kind: 'say',
      when: () => !has('add-monitor'),
      title: t('It’s already here'),
      text: t('This show is in your library. Its page there lets you pick which seasons and episodes to get.'),
    },
    {
      kind: 'ask',
      key: 'eps',
      when: () => has('add-monitor'),
      title: t('Which episodes?'),
      card: {
        title: t('Which episodes to get'),
        options: [
          { name: t('All episodes'), good: t('Everything that’s out, and new ones as they air.'), bad: t('A long show can be a lot to download.') },
          { name: t('Only new episodes'), good: t('Just the ones still to air: for a show you’re catching up on elsewhere.'), bad: t('Nothing older comes in.') },
          { name: t('None, I’ll pick'), good: t('Adds the show and downloads nothing; you then choose seasons or episodes.'), bad: t('You have to pick.') },
        ],
        recommend: t('All episodes for a show you’re starting.'),
        later: t('Change it on the show’s page any time: season by season, episode by episode.'),
      },
      choices: [
        { value: 'all', label: t('All episodes'), recommended: true },
        { value: 'future', label: t('Only new episodes') },
        { value: 'none', label: t('None, I’ll pick') },
      ],
    },
    {
      kind: 'point',
      target: 'add-monitor',
      until: 'next',
      when: () => has('add-monitor'),
      title: t('Which episodes to monitor'),
      text: (c) =>
        ({ all: t('Choose All episodes.'), future: t('Choose Only episodes that haven’t aired yet.'), none: t('Choose None (add it, download nothing).') })[pick(c, 'eps') ?? 'all'] ?? '',
    },
    {
      kind: 'point',
      target: 'add-go',
      act: 'click',
      until: { gone: 'add-go' },
      when: () => has('add-go'),
      title: t('Add it'),
      text: (c) => (c.watch ? t('Adding it for you.') : t('The usual quality is already picked. Press Add.')),
    },
    {
      kind: 'say',
      when: (c) => pick(c, 'eps') === 'none',
      title: t('Now pick the episodes'),
      text: t('Open the show in your Library: the bookmark next to a season or an episode monitors it, and monitored ones are downloaded.'),
    },
    { kind: 'done', title: t('Your show is on its way'), text: t('New episodes come in by themselves after they air. Upcoming shows when.'), shortcut: t('Next time: a show’s page, Add to library.') },
  ],
}

// ---- following a person ----

export const followPerson: Walkthrough = {
  id: 'follow-person',
  group: 'watch',
  title: t('Follow an actor or director'),
  blurb: t('See their new and coming films and shows as soon as they’re announced.'),
  icon: 'user',
  words: 'follow actor actress director person people cast new films coming',
  simple: [
    titleAsk('movie', t('A film they’re in'), t('Pick any film with them in it.')),
    { kind: 'go', to: (c) => `/title/${pick(c, 'film')}`, title: (c) => (c.answers.film as { label: string }).label, text: t('The cast is further down.') },
    { kind: 'point', target: 'title-cast', until: { path: '/person/*' }, title: t('Tap their face'), text: t('Their page opens: who they are, and their films and shows, best known first.') },
    { kind: 'point', target: 'person-follow', act: 'click', until: 'next', title: t('Follow'), text: (c) => (c.watch ? t('Following them for you.') : t('Press Follow.')) },
    { kind: 'go', to: '/following', title: t('Everyone you follow'), text: t('What’s coming soon and what came out this past year, a row for each person. Search here to follow someone by name; the × unfollows. Their new work also shows in Discover’s From people you follow row.') },
    { kind: 'done', title: t('You’re following them'), text: t('Their next film shows up here as soon as it’s announced.'), shortcut: t('Next time: a face in any cast, then Follow.') },
  ],
}

// ---- Upcoming ----

export const upcoming: Walkthrough = {
  id: 'upcoming',
  group: 'watch',
  title: t('See what’s coming'),
  blurb: t('New episodes and films on the way, what Usherr is still looking for, and the people you follow.'),
  icon: 'calendar',
  words: 'upcoming calendar coming soon new episodes wanted release dates',
  simple: [
    { kind: 'go', to: '/calendar', title: t('The calendar'), text: t('New episodes and films coming out, of what’s in your library, day by day.') },
    { kind: 'point', target: 'upcoming-tabs', until: 'next', title: t('Three tabs'), text: t('Calendar is what’s coming. Wanted is what’s been added but isn’t here yet: Usherr is still looking for it. Following is the people you follow.') },
    { kind: 'go', to: '/wanted', title: t('Wanted'), text: t('Something stuck here a long time may not have a good release yet; it’s searched again by itself.') },
    { kind: 'done', title: t('That’s Upcoming'), text: t('A show you add puts its next episodes on the calendar by itself.'), shortcut: t('Next time: Upcoming, in the menu.') },
  ],
}

// ---- Complete your library ----

export const completeLibrary: Walkthrough = {
  id: 'complete-library',
  group: 'library',
  needs: 'movies',
  title: t('Fill the gaps in your library'),
  blurb: t('Complete your library: what’s missing from series, directors and actors you collect, and the classics.'),
  icon: 'check',
  words: 'complete library missing gaps series director actor classics collect',
  simple: [
    { kind: 'go', to: '/discover/complete', title: t('Complete your library'), text: t('Usherr looks at what you have and shows what’s missing.') },
    {
      kind: 'point',
      target: 'complete-page',
      until: 'next',
      title: t('How complete each is'),
      text: t('Film series you have part of, directors and actors you collect, and the classics, each with how many you have. Tap one to see what’s missing and add it. Ignore hides one you don’t care about.'),
    },
    {
      kind: 'done',
      title: t('That’s Complete your library'),
      text: t('For one film series at a time, Complete a film series does it in a few taps.'),
      then: { id: 'complete-collection', label: t('Complete a film series'), when: (c) => c.admin },
    },
  ],
}

// ---- the Ask bar ----

export const askAi: Walkthrough = {
  id: 'ask-ai',
  group: 'watch',
  title: t('Ask for something in your own words'),
  blurb: t('The Ask bar: “a 90s heist movie, not too violent”, and Usherr finds real titles.'),
  icon: 'sparkles',
  words: 'ask ai bar search own words mood describe suggest chat',
  simple: [
    {
      kind: 'point',
      target: 'ask-bar',
      act: 'focus',
      until: 'next',
      optional: true,
      title: t('The Ask bar'),
      text: t('Type what you’re in the mood for: “a 90s heist movie, not too violent”, “something like Arrival”, “every film by Denis Villeneuve”. You can add a photo too, like a poster you saw. Every title it names is checked, and nothing is added without you.'),
    },
    {
      kind: 'say',
      when: () => !has('ask-bar'),
      title: t('The AI isn’t on here'),
      text: t('An administrator can switch it on in Settings. Until then the bar at the top searches by name.'),
    },
    {
      kind: 'say',
      title: t('On a title’s page'),
      text: t('What’s it like? gives a short, spoiler-free description: the feel, the pace, who it’s for. On a show, Catch me up recaps it up to where you are, and Skip guide says which episodes matter.'),
    },
    { kind: 'done', title: t('Ask away'), text: t('The more you vote, the better its picks fit you.') },
  ],
}
