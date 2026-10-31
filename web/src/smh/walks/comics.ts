// Usherr SMH: comics. Reading (panels or page turn), the Review tab, story
// arcs and This week, each with what it's for, not only where it is.
import { t } from '../../i18n'
import { comicsApi } from '../../home/comicsApi'
import type { SmhCtx, SmhStep, Walkthrough } from '../types'

const has = (id: string) => !!document.querySelector(`[data-walk="${id}"]`)
const view = () => {
  try {
    return localStorage.getItem('comic-view') || 'flip'
  } catch {
    return 'flip'
  }
}

// Comics need switching on and a ComicVine key first: every comics
// walkthrough checks, and offers Set up comics when they aren't ready.
const ready = (c: SmhCtx) => c.answers.comicsReady === true
// Reading needs comics on; This week, story arcs and adding series need the
// key too. Only administrators can see the settings: for anyone else the
// menu already shows comics walkthroughs only when comics are on.
const check = (key: boolean): SmhStep[] => [
  {
    kind: 'do',
    title: t('Checking comics are set up'),
    run: async (c) => {
      if (!c.admin) return { comicsReady: true }
      const s = await comicsApi.settings().catch(() => null)
      return { comicsReady: !!s && s.enabled && (!key || s.keySet) }
    },
  },
  {
    kind: 'done',
    when: (c) => !ready(c),
    title: t('Comics aren’t set up yet'),
    text: (c) => (c.admin ? t('They need a free ComicVine key first: that’s where the series and issue details come from. Set up comics walks you through it in a couple of minutes.') : t('An administrator needs to set them up first.')),
    then: { id: 'setup-comics', label: t('Set up comics'), when: (c) => c.admin },
  },
]

const home: SmhStep = {
  kind: 'go',
  to: '/comics',
  title: t('Your comics'),
  text: t('Every series you have, on shelves. A series is one run of a comic, like Saga or Amazing Spider-Man, with its issues numbered #1, #2 and on.'),
}

const tab = (id: string, title: string, text: string): SmhStep => ({ kind: 'point', target: id, act: 'click', until: 'next', path: '/comics', title, text })

// ---- reading ----

const readSteps = (advanced: boolean): SmhStep[] => [
  ...check(false),
  home,
  ...(advanced
    ? [
        {
          kind: 'say',
          path: '/comics',
          title: t('How the shelves work'),
          text: t('Rows group your series by publisher, decade, A–Z, or what’s still wanted (pick above the shelves). “Story arcs for you” suggests big stories that run through series you have.'),
        } as SmhStep,
      ]
    : []),
  {
    kind: 'point',
    target: 'cx-lib-search',
    until: { path: '/comics/*' },
    title: t('Find your series'),
    text: t('Type part of its name, then tap its cover.'),
  },
  {
    kind: 'point',
    target: 'cx-read',
    act: 'click',
    until: { seen: 'cx-reader' },
    title: t('Open an issue'),
    text: (c) =>
      c.watch
        ? t('Opening the first issue you have.')
        : t('Press Read on the issue you want. No Read button? That issue isn’t downloaded yet: it says Wanted while Usherr looks for it.'),
  },
  {
    kind: 'do',
    title: t('Opening the reader'),
    run: async () => ({ viewWas: view() }),
  },
  {
    kind: 'ask',
    key: 'view',
    title: t('How do you want to read?'),
    card: {
      title: t('Panels or page turn'),
      options: [
        { name: t('Panels (guided view)'), good: t('Zooms to each panel in reading order and dims the rest, like a comics app. Best on a phone, where a whole page is too small to read.'), bad: t('Needs a moment to find the panels on each page. Splash pages and covers are shown whole.') },
        { name: t('Page turn'), good: t('Pages curl like paper; two side by side on a wide screen. Best on a computer, a tablet or the TV.'), bad: t('Small text is hard to read on a phone.') },
      ],
      recommend: t('Panels on a phone, page turn on a big screen.'),
      later: t('Switch any time from the bar at the top of the reader. Usherr remembers your choice.'),
    },
    choices: () => [
      { value: 'panels', label: t('Panels'), recommended: window.innerWidth < 760 },
      { value: 'flip', label: t('Page turn'), recommended: window.innerWidth >= 760 },
    ],
  },
  {
    kind: 'point',
    target: 'cx-view-panels',
    act: 'click',
    until: 'next',
    when: (c) => (c.answers.view as { value: string } | undefined)?.value === 'panels' && c.answers.viewWas !== 'panels',
    title: t('Panels'),
    text: t('Press Panels. Then tap the right side of the screen, or swipe, to go panel by panel. Whole page shows the full page; the next tap goes on.'),
  },
  {
    kind: 'say',
    when: (c) => (c.answers.view as { value: string } | undefined)?.value === 'panels' && c.answers.viewWas === 'panels',
    title: t('You’re reading in Panels'),
    text: t('Tap the right side of the screen, or swipe, to go panel by panel. Whole page shows the full page; the next tap goes on.'),
  },
  {
    kind: 'point',
    target: 'cx-view-panels',
    act: 'click',
    until: 'next',
    when: (c) => (c.answers.view as { value: string } | undefined)?.value === 'flip' && c.answers.viewWas === 'panels',
    title: t('Page turn'),
    text: t('Press Panels again to go back to whole pages, turning like paper.'),
  },
  {
    kind: 'say',
    when: (c) => (c.answers.view as { value: string } | undefined)?.value === 'flip' && c.answers.viewWas !== 'panels',
    title: t('Turning the pages'),
    text: t('Drag a corner, tap the right side, or swipe; the arrow keys and the space bar work too. Speed sets how fast the pages turn.'),
  },
  {
    kind: 'point',
    target: 'cx-reader-bar',
    until: 'next',
    title: t('The reader’s bar'),
    text: advanced
      ? t('The slider at the bottom jumps to any page. Start over goes back to page 1. Slide shows one page at a time with fit to width, an in-between of the two. X closes: your page is kept, on every device, and an issue read to the end counts as read.')
      : t('The slider at the bottom jumps to any page. X closes: Usherr keeps your page, on every device.'),
  },
  ...(advanced
    ? [
        {
          kind: 'say',
          title: t('Catch me up'),
          text: t('Back on the series page, Catch me up (when AI is on) writes a recap of the story up to the last issue you read. Handy after a break.'),
        } as SmhStep,
      ]
    : []),
  {
    kind: 'done',
    title: t('Happy reading'),
    text: t('Your place is kept in every issue, so you can stop any time.'),
    shortcut: t('Next time: Library, Comics, your series, Read.'),
  },
]

export const readComic: Walkthrough = {
  id: 'read-comic',
  group: 'read',
  needs: 'comics',
  title: t('Read a comic'),
  blurb: t('Find a series, open an issue, and read it panel by panel or turning the pages.'),
  icon: 'book',
  words: 'comic comics read panels guided view page turn flip manga issue',
  simple: readSteps(false),
  advanced: readSteps(true),
}

// ---- the Review tab ----

export const comicReview: Walkthrough = {
  id: 'comic-review',
  group: 'read',
  needs: 'comics',
  admin: true,
  title: t('Check comic downloads Usherr wasn’t sure about'),
  blurb: t('The Review tab: releases that might be the wrong issue wait there for you to say yes or no.'),
  icon: 'eye',
  words: 'comics review tab unsure wrong issue tpb pack annual',
  simple: [
    ...check(false),
    home,
    tab('cx-tab-review', t('The Review tab'), t('When Usherr looks for an issue by itself and finds a release it isn’t sure is the right one, it doesn’t download it. It waits here for you instead. The number on the tab is how many are waiting.')),
    {
      kind: 'point',
      target: 'cx-review-issue',
      until: 'next',
      optional: true,
      title: t('One issue, and what was found'),
      text: t('Each card is one issue. Under it are the releases Usherr found, and why it’s unsure: a year that doesn’t fit, a size that looks wrong, or a pack or collected edition (TPB) that may hold the issue among others.'),
    },
    {
      kind: 'point',
      target: 'cx-review-actions',
      until: 'next',
      optional: true,
      title: t('Yes or no'),
      text: t('Get downloads that release. Not this one sets it aside for good, and Usherr keeps looking for a better match. Not sure? The series link at the top opens the issue list to compare.'),
    },
    {
      kind: 'say',
      when: () => !has('cx-review-issue'),
      title: t('Nothing waiting right now'),
      text: t('When something needs a look, the Review tab shows a number. Come back then.'),
    },
    { kind: 'done', title: t('That’s the Review tab'), text: t('Usherr only asks when it isn’t sure, so most issues come in by themselves.'), shortcut: t('Check it whenever the tab shows a number.') },
  ],
}

// ---- story arcs ----

const how = (c: { answers: Record<string, unknown> }) => (c.answers.how as { value: string } | undefined)?.value

export const comicArcs: Walkthrough = {
  id: 'comic-arcs',
  group: 'read',
  needs: 'comics',
  title: t('Read a big story in the right order'),
  blurb: t('Story arcs: one story told across several series (Civil War, Knightfall), as a reading list in order.'),
  icon: 'list',
  words: 'comics story arc arcs event crossover reading order civil war',
  simple: [
    ...check(true),
    home,
    tab('cx-tab-arcs', t('Story arcs'), t('Big comic stories often run through several series at once, so the issues come out in different places and the order is hard to work out. A story arc here is that story’s reading list, in the right order, with what you have and what’s missing.')),
    {
      kind: 'point',
      target: 'cx-arc-find',
      until: { seen: 'cx-arc-results' },
      when: (c) => c.admin,
      title: t('Find an arc'),
      text: t('Type the story’s name (Civil War, Secret Wars, Knightfall) and press Find.'),
    },
    {
      kind: 'ask',
      key: 'how',
      when: (c) => c.admin,
      title: t('Get the issues too?'),
      card: {
        title: t('The whole arc, or just the list'),
        options: [
          { name: t('Get the whole arc'), good: t('Adds the reading list and looks for every issue in it, so it’s ready to read.'), bad: t('A big event can be dozens of issues to download.') },
          { name: t('Just the list'), good: t('Adds the reading list only: see what’s in it and what you already have.'), bad: t('Missing issues stay missing until you get them.') },
        ],
        recommend: t('Just the list first, if you’re not sure how big it is.'),
        later: t('Get the whole arc is on the arc’s page too, any time.'),
      },
      choices: [
        { value: 'all', label: t('Get the whole arc') },
        { value: 'list', label: t('Just the list'), recommended: true },
      ],
    },
    {
      kind: 'point',
      target: 'cx-arc-results',
      until: { path: '/comics/arc/*' },
      when: (c) => c.admin,
      title: t('Pick it'),
      text: (c) => (how(c) === 'all' ? t('Press Get the whole arc on the one you want.') : t('Press Just the list on the one you want.')),
    },
    {
      kind: 'point',
      target: 'cx-arc-mine',
      until: { path: '/comics/arc/*' },
      when: (c) => !c.admin,
      title: t('Open an arc'),
      text: t('These are the household’s story arcs, with how far along each is. Tap one.'),
    },
    {
      kind: 'point',
      target: 'cx-arc-order',
      until: 'next',
      title: t('The reading order'),
      text: t('Top to bottom is the order to read in. Issues you have say Read (or Read again); the others say Wanted, Downloading or Missing.'),
    },
    {
      kind: 'point',
      target: 'cx-arc-start',
      until: 'next',
      optional: true,
      title: t('Start reading'),
      text: t('Opens the first issue you haven’t finished, whichever series it’s in. Next time it says Continue reading.'),
    },
    {
      kind: 'done',
      title: t('Your arc is ready'),
      text: t('“Story arcs for you” on the Comics shelves suggests arcs that run through series you already have.'),
      shortcut: t('Next time: Comics, Story arcs, your arc, Continue reading.'),
    },
  ],
}

// ---- this week ----

export const comicWeek: Walkthrough = {
  id: 'comic-week',
  group: 'read',
  needs: 'comics',
  title: t('See this week’s new comics'),
  blurb: t('This week: every comic out this week, and how to get the ones you want.'),
  icon: 'calendar',
  words: 'comics new this week release wednesday pull list',
  simple: [
    ...check(true),
    home,
    tab('cx-tab-week', t('This week'), t('Every comic out this week, from ComicVine. New comics come out on Wednesdays in the US, so early in the week the list is still thin.')),
    {
      kind: 'point',
      target: 'cx-week-nav',
      until: 'next',
      title: t('Moving around'),
      text: t('‹ and › go a week back or forward. Find a series narrows it down, and Only my series shows just the ones you follow.'),
    },
    {
      kind: 'point',
      target: 'cx-week-pub',
      until: 'next',
      optional: true,
      title: t('Publishers'),
      text: t('Pick one publisher (Marvel, DC, Image…) to see only theirs. Usherr remembers it.'),
    },
    {
      kind: 'point',
      target: 'cx-week-pull',
      until: 'next',
      optional: true,
      title: t('Pull'),
      text: t('Pull gets that issue, and follows its series if you don’t already, so the next issues come in by themselves. That’s a comic shop’s pull list, done for you.'),
    },
    { kind: 'done', title: t('That’s This week'), text: t('Your series always come first, then every publisher.'), shortcut: t('Next time: Comics, This week.') },
  ],
}

// ---- setting comics up ----

export const setupComics: Walkthrough = {
  id: 'setup-comics',
  group: 'setup',
  admin: true,
  title: t('Set up comics'),
  blurb: t('Switch comics on with a free ComicVine key, so Usherr can find series, new issues and story arcs.'),
  icon: 'book',
  words: 'comics comicvine key setup switch on api',
  simple: [
    {
      kind: 'say',
      title: t('What you need'),
      text: t('Usherr gets every series, issue, cover and story arc from ComicVine, a free comics database. It needs a key: a code that says the requests come from you. It takes a couple of minutes and costs nothing.'),
    },
    {
      kind: 'say',
      title: t('Get your ComicVine key'),
      text: t('Open comicvine.gamespot.com/api in a new tab, sign in (or make a free account), and your key is shown on that page. Copy it, then come back here.'),
    },
    {
      kind: 'ask',
      key: 'cvkey',
      title: t('Paste your key'),
      text: t('Usherr checks it with ComicVine before saving it.'),
      input: {
        placeholder: t('Your ComicVine key'),
        secret: true,
        test: async (v) => {
          try {
            await comicsApi.saveSettings({ key: v })
            return { ok: true, message: t('ComicVine accepted it. Saved.') }
          } catch (e) {
            return { ok: false, message: e instanceof Error ? e.message : String(e) }
          }
        },
      },
    },
    {
      kind: 'do',
      title: t('Switching comics on'),
      run: async () => {
        const s = await comicsApi.settings()
        if (!s.enabled) await comicsApi.saveSettings({ enabled: true })
      },
    },
    {
      kind: 'go',
      to: '/comics',
      title: t('Comics are on'),
      text: t('Your comics live here. The Add tab finds a series to follow; new issues then come in by themselves. Files go in the comics folder shown in the settings tab (the sliders).'),
    },
    {
      kind: 'done',
      title: t('Ready to read'),
      text: t('Add a series, or use This week to see what’s new.'),
      then: { id: 'read-comic', label: t('Read a comic') },
    },
  ],
}
