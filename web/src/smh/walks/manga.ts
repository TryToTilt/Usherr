// Usherr SMH: manga. Getting a series (official English editions by
// default), where manga comes from, reading a volume and the reader's
// options, Discover and reading online, and a MangaDex account of your own.
import { t } from '../../i18n'
import { mangaApi } from '../../manga/mangaApi'
import type { SmhChoice, SmhCtx, Walkthrough } from '../types'

const pick = (c: SmhCtx, key: string) => (c.answers[key] as { value: string } | undefined)?.value
const has = (id: string) => !!document.querySelector(`[data-walk="${id}"]`)

// ---- getting a series ----

export const getManga: Walkthrough = {
  id: 'get-manga',
  group: 'read',
  needs: 'manga',
  title: t('Get a manga series'),
  blurb: t('Type its English title and press Add: official English editions come in by themselves.'),
  icon: 'book',
  words: 'manga manhwa series volumes add get follow official english edition anilist one piece',
  simple: [
    {
      kind: 'ask',
      key: 'series',
      title: t('Which series?'),
      text: t('Type its English title (the Japanese or Korean one works too) and pick it.'),
      search: async (q) =>
        ((await mangaApi.lookup(q)).results ?? []).slice(0, 8).map((r): SmhChoice => ({ value: r.title, label: r.startYear ? `${r.title} (${r.startYear})` : r.title, image: r.coverUrl, hint: r.inLibrary ? t('In your library') : undefined })),
    },
    { kind: 'go', to: (c) => `/manga/add?q=${encodeURIComponent(pick(c, 'series') ?? '')}`, title: t('Add a series'), text: t('Manga, + Add a series. Titles come from AniList; nothing is saved until you press Add.') },
    {
      kind: 'point',
      target: 'manga-result-add',
      act: 'click',
      until: { seen: 'manga-want-all' },
      optional: true,
      title: t('Add'),
      text: t('Opens the choices for this series. Each result says whether an official English edition is known, and who publishes it.'),
    },
    {
      kind: 'say',
      when: () => !has('manga-want-all'),
      title: t('It’s already here'),
      text: t('This series is in your Manga already: In your library opens its page.'),
    },
    {
      kind: 'ask',
      key: 'want',
      when: () => has('manga-want-all'),
      title: t('Which volumes?'),
      card: {
        title: t('What Usherr should get'),
        options: [
          { name: t('All volumes'), good: t('Every volume that’s out, and new ones as they come.'), bad: t('A long series is a lot to download.') },
          { name: t('New volumes only'), good: t('Just the ones still to come: for a series you have on your shelf.'), bad: t('Nothing older comes in.') },
          { name: t('Nothing yet'), good: t('Adds the series and gets nothing: choose volumes on its page.'), bad: t('You have to pick.') },
        ],
        recommend: t('All volumes for a series you’re starting.'),
        later: t('Change it on the series’ page any time, volume by volume.'),
      },
      choices: [
        { value: 'all', label: t('All volumes'), recommended: true },
        { value: 'new', label: t('New volumes only') },
        { value: 'none', label: t('Nothing yet') },
      ],
    },
    {
      kind: 'point',
      target: (c) => `manga-want-${pick(c, 'want') ?? 'all'}`,
      act: 'click',
      until: 'next',
      when: () => has('manga-want-all'),
      title: t('Pick it'),
      text: (c) => ({ all: t('Choose All volumes.'), new: t('Choose New volumes only.'), none: t('Choose Nothing yet.') })[pick(c, 'want') ?? 'all'] ?? '',
    },
    {
      kind: 'point',
      target: 'manga-add-go',
      act: 'click',
      until: { gone: 'manga-add-go' },
      when: () => has('manga-add-go'),
      title: t('Add it'),
      text: (c) =>
        c.watch
          ? t('Adding it for you, official English editions only.')
          : t('Press Add. When no official English edition is known, Add, and wait for an official one keeps to official editions; Add and allow fan scans lets fan translations in, for this series only.'),
    },
    {
      kind: 'point',
      target: 'manga-status',
      until: 'next',
      optional: true,
      title: t('What’s happening'),
      text: t('One sentence says what Usherr is doing and when it looks next. Why nothing yet? explains an empty series: what was searched, and what was found and turned down. Packs and anything doubtful wait for you here.'),
    },
    {
      kind: 'done',
      title: t('Your manga is on its way'),
      text: t('Volumes arrive by themselves through your usual indexers and Activity. A new volume goes on the calendar on the day it’s noticed. Every series with a known publisher has a Buy it link too.'),
      shortcut: t('Next time: Manga, + Add a series.'),
      then: { id: 'read-manga', label: t('Read manga') },
    },
  ],
}

// ---- where manga comes from ----

export const mangaSources: Walkthrough = {
  id: 'setup-manga',
  group: 'read',
  needs: 'manga',
  admin: true,
  title: t('Choose where manga comes from'),
  blurb: t('Official English editions by default; fan scans, MangaDex chapters and Nyaa only if you choose them.'),
  icon: 'sliders',
  words: 'manga sources official fan scans scanlation mangadex chapters nyaa torrent seeding folder settings import comics',
  simple: [
    {
      kind: 'say',
      title: t('Switch Manga on, and give it a folder'),
      text: t('Settings, Media types: switch on Manga (it’s kept apart from Comics). Then Settings, Library, Folders: the Manga folder, where volumes go as Series/Series v01.cbz. In Docker, the folder has to be connected to the container, such as /your/manga:/data/manga.'),
    },
    { kind: 'go', to: '/settings/manga', title: t('Manga settings'), text: t('Under Settings, Library. A note at the top means the manga folder can’t be saved to: nothing is searched or downloaded until it can.') },
    {
      kind: 'point',
      target: 'manga-sources',
      until: 'next',
      title: t('Where manga comes from'),
      text: t('Your own files are always read. Official English editions are the default. Fan scans from your indexers, Fan scans from MangaDex (chapters, credited to their group, no account needed) and Nyaa (a public torrent index, for Manga only: it seeds, and follows your VPN settings) are off until you switch them on.'),
    },
    {
      kind: 'say',
      title: t('One series can differ'),
      text: t('On a series’ page, Sources for this series lets just that one take fan scans or Nyaa. With MangaDex chapters on, its Chapters tab follows new chapters or all of them, and picks the group when several translated a chapter.'),
    },
    {
      kind: 'say',
      title: t('Files you already have'),
      text: t('Under Files, Look through the folder now finds names like Series/Series v01.cbz without moving them (it also runs every night). Manga kept in Comics can be brought over too: you see every new name first, and Undo puts it all back.'),
    },
    { kind: 'done', title: t('Sources are set'), text: t('A copy on an indexer still isn’t a purchase: every series with a known publisher has a Buy it link.'), then: { id: 'get-manga', label: t('Get a manga series') } },
  ],
}

// ---- reading ----

const readerOptions = t('The sliders button (How you read this series) holds the reading direction and how pages show: one page, two, a long scroll, a Wide strip, or Panels (the page glides from panel to panel), Shift the pairs by one page for spreads that come out split, and Curl, Slide or None for the page turn. Also fit, trim white borders, brightness, dark pages, the progress bar (normal, a thin line or hidden, and where it goes), tap hints, greyscale, dim pages, the background, and hiding the header (M brings it back). Direction and layout are kept for each series; fit, trim, brightness, dark pages and the page turn for this device. Nothing changes until you choose.')

export const readManga: Walkthrough = {
  id: 'read-manga',
  group: 'read',
  needs: 'manga',
  title: t('Read manga'),
  blurb: t('Open a volume or chapter, right to left or as a long strip, and set the reader the way you like it.'),
  icon: 'book',
  words: 'manga read reader volume chapter right to left manhwa webtoon strip panels page turn curl progress bar greyscale settings options',
  simple: [
    { kind: 'go', to: '/manga', title: t('Your manga'), text: t('Continue reading is at the top when you’re part way through something.') },
    { kind: 'point', target: 'manga-grid', until: { path: '/manga/*' }, title: t('Pick a series'), text: t('Tap a series’ cover.') },
    {
      kind: 'point',
      target: 'manga-read',
      act: 'click',
      until: { seen: 'mg-reader' },
      optional: true,
      title: t('Read'),
      text: t('Opens the volume or chapter you’re on. No Read button? Nothing of this series is on disk yet.'),
    },
    {
      kind: 'say',
      when: () => has('mg-reader'),
      title: t('Turning pages'),
      text: t('Manga reads right to left, manhwa as one long scroll, remembered for each series. Tap or swipe, or use the arrow keys. Hold a finger on a page (or right-click) to save it. The last page offers whatever comes next.'),
    },
    { kind: 'point', target: 'mg-reader-opts', until: 'next', optional: true, title: t('How you read this series'), text: readerOptions },
    { kind: 'done', title: t('Happy reading'), text: t('Comics have the same reading options, under the sliders button in their reader. Your place follows you to every device.') },
  ],
}

export const browseManga: Walkthrough = {
  id: 'discover-manga',
  group: 'read',
  needs: 'manga',
  title: t('Find manga, and read it online'),
  blurb: t('Manga, Discover: AniList’s shelves, a search, new official volumes, and with MangaDex on, Read now without downloading.'),
  icon: 'compass',
  words: 'manga discover browse find search anilist mangadex read now online without downloading random filters tags new volumes',
  simple: [
    { kind: 'go', to: '/manga/discover', title: t('Manga, Discover'), text: t('The one place to find manga: trending, popular and new on AniList, by genre if you like.') },
    {
      kind: 'point',
      target: 'manga-browse-search',
      until: 'next',
      title: t('Search'),
      text: t('Type a title and press Search: AniList, and MangaDex when its source is on (a title both know shows once). With MangaDex on, More filters searches by tags, status, year and more, and Random title picks one.'),
    },
    {
      kind: 'point',
      target: 'manga-discover-want',
      until: 'next',
      title: t('What Add gets'),
      text: t('Add on any tile is one press: choose here what it gets, all volumes, new ones only, or nothing yet.'),
    },
    {
      kind: 'say',
      title: t('Read now'),
      text: t('With MangaDex chapters on (Settings, Manga), tiles have Read now: the chapter opens in Usherr’s own reader without downloading, with a chapter picker in the bar and the next chapter a press away. For a series in your library, an administrator can keep a chapter with Download this chapter, in the reader’s options.'),
    },
    {
      kind: 'say',
      title: t('Someone’s AniList list'),
      text: t('Further down, From an AniList list: type an AniList user name to see what’s on their public list, with Add on each.'),
    },
    {
      kind: 'say',
      title: t('18+ stays out'),
      text: t('Adult titles stay out of Discover unless the NSFW section is set up and your account may see it, and even then only when you tick the switch.'),
    },
    { kind: 'done', title: t('Happy hunting'), text: t('A series’ own page shows Related series and More like this, each with an Add button.'), then: { id: 'mangadex-account', label: t('Connect your MangaDex account') } },
  ],
}

// ---- a MangaDex account ----

export const mangaDexAccount: Walkthrough = {
  id: 'mangadex-account',
  group: 'read',
  needs: 'manga',
  title: t('Connect your MangaDex account'),
  blurb: t('Optional, just for you: your follows, read chapters, reading status and lists stay the same in Usherr and on MangaDex.'),
  icon: 'key',
  words: 'mangadex account sync follows read chapters lists reading status api client personal secret connect',
  simple: [
    {
      kind: 'say',
      title: t('A personal client first'),
      text: t('On a computer, sign in on mangadex.org. Open Settings, then API Clients, and make a new personal client (any name will do). Wait until MangaDex approves it, then copy its client id, and get its secret.'),
    },
    { kind: 'go', to: '/settings/manga-account', title: t('MangaDex account'), text: t('Under Settings, Library, or the MangaDex account button on the Manga page.') },
    {
      kind: 'point',
      target: 'manga-mangadex-account',
      until: 'next',
      title: t('Connect'),
      text: t('Type the client id, the secret, your MangaDex username and password, and press Connect. Usherr saves them encrypted, sends them only to MangaDex, and never shows them again. A personal client signs in without two-factor login.'),
    },
    {
      kind: 'say',
      title: t('What syncs'),
      text: t('Series you follow on MangaDex come into Manga, and the Manga page shows their new chapters, what you read lately on MangaDex, and your lists. Heavy syncing waits for the night, between 2 and 9; Sync now does a quick one.'),
    },
    { kind: 'done', title: t('MangaDex is connected'), text: t('Disconnect, on the same card, deletes everything Usherr kept from it. Everyone in the house connects their own.') },
  ],
}
