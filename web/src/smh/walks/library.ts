// Usherr SMH: looking after the library. A better copy, subtitles, moving a
// title, tags as collections, a download that didn't work, bringing in a
// library you have, and restyling posters.
import { api } from '../../api'
import { t } from '../../i18n'
import type { SmhChoice, SmhCtx, SmhStep, Walkthrough } from '../types'

const pick = (c: SmhCtx, key: string) => (c.answers[key] as { value: string; label: string } | undefined)?.value
const label = (c: SmhCtx) => (c.answers.film as { label: string } | undefined)?.label ?? ''
const inLib = (c: SmhCtx) => c.answers.inLib === true

// The library's films, read once per walkthrough page load.
let mineCache: Promise<{ tmdbId: number; title: string; year: number; posterUrl?: string }[]> | null = null
const libraryMovies = () => (mineCache ??= api.listMovies().catch(() => []))

// A film from the library: find it, look it up, open its page, and say so
// when it isn't in the library.
const libraryFilm = (title: string): SmhStep[] => [
  {
    kind: 'ask',
    key: 'film',
    title,
    text: t('Type its name and pick it from your library.'),
    search: async (q) => {
      // Your own films first: these are about a film you have.
      const want = q.toLowerCase()
      const mine = (await libraryMovies())
        .filter((m) => m.title.toLowerCase().includes(want))
        .slice(0, 8)
        .map((m): SmhChoice => ({ value: String(m.tmdbId), label: m.year ? `${m.title} (${m.year})` : m.title, image: m.posterUrl }))
      if (mine.length) return mine
      return (await api.tmdbSearch('movie', q)).slice(0, 8).map((f): SmhChoice => ({ value: String(f.tmdbId), label: f.year ? `${f.title} (${f.year})` : f.title, image: f.posterUrl }))
    },
  },
  {
    kind: 'do',
    title: t('Looking it up'),
    run: async (c) => {
      const m = await api.tmdbMovieDetail(Number(pick(c, 'film')))
      return { inLib: !!m.libraryId, downloaded: m.status === 'downloaded' }
    },
  },
  {
    kind: 'say',
    when: (c) => !inLib(c),
    title: (c) => t('{title} isn’t in your library', { title: label(c) }),
    text: t('This is for a film you already have. Press Back and pick another.'),
  },
  { kind: 'go', when: inLib, to: (c) => `/title/${pick(c, 'film')}`, title: label, text: t('Its page.') },
]

// ---- a better copy ----

export const betterCopy: Walkthrough = {
  id: 'better-copy',
  group: 'library',
  title: t('Swap a film for a better copy'),
  blurb: t('A better quality, another edition, or a smaller file: pick the release yourself.'),
  icon: 'refresh',
  words: 'better copy quality upgrade replace release 4k 1080p edition smaller',
  simple: [
    ...libraryFilm(t('Which film?')),
    {
      kind: 'point',
      target: 'title-choose',
      until: 'next',
      when: inLib,
      title: t('Choose a release'),
      text: t('Lists every release your indexers have for this film, best match first, with its quality and size and why Usherr would skip one. Pick the one you want: Usherr asks before replacing your file, and swaps it in only once the new one is complete. The old one goes to the recycle bin. (Each look counts against your indexers’ daily limits.)'),
    },
    {
      kind: 'say',
      when: inLib,
      title: t('Smaller instead?'),
      text: t('Moved the film to a lower quality profile, 4K to 1080p say? Replace with a smaller version, next to Choose a release, finds a smaller copy and only then removes the big file.'),
    },
    { kind: 'done', title: t('That’s how to swap a copy'), text: t('Automatic searches still upgrade by themselves, up to what the film’s quality profile allows.') },
  ],
}

// ---- subtitles ----

export const subtitles: Walkthrough = {
  id: 'subtitles',
  group: 'library',
  title: t('Get subtitles'),
  blurb: t('Subtitles in your language for a film, and how to have them come by themselves.'),
  icon: 'captions',
  words: 'subtitles captions srt language sdh forced out of sync timing',
  simple: [
    ...libraryFilm(t('Which film?')),
    {
      kind: 'point',
      target: 'title-subs',
      until: 'next',
      optional: true,
      when: inLib,
      title: t('Subtitles'),
      text: t('Search lists what the subtitle services have, in your languages. One marked made for this file was timed against your exact copy, so it’s in sync: it comes first. Out of time? Move it earlier or later right here.'),
    },
    {
      kind: 'say',
      when: (c) => inLib(c) && !document.querySelector('[data-walk="title-subs"]'),
      title: t('Subtitles are off'),
      text: t('An administrator switches them on under Settings, Subtitles, and picks the languages and services.'),
    },
    {
      kind: 'say',
      title: t('Let them come by themselves'),
      text: t('Under Settings, Subtitles, Languages, tick Download after each import: Usherr then gets subtitles for every new film and episode, within the services’ daily limits. In the player, the subtitle button picks the language.'),
    },
    { kind: 'done', title: t('Subtitles sorted'), text: t('The Dashboard says when downloaded titles have none yet, with a Get subtitles button.') },
  ],
}

// ---- moving a title ----

export const moveTitle: Walkthrough = {
  id: 'move-title',
  group: 'library',
  admin: true,
  title: t('Move a film to another folder'),
  blurb: t('Keep cartoons, documentaries or 4K copies in their own folder, files and all.'),
  icon: 'folder',
  words: 'move folder drive keep it in another disk relocate',
  simple: [
    ...libraryFilm(t('Which film?')),
    {
      kind: 'point',
      target: 'title-keepin',
      until: 'next',
      optional: true,
      when: inLib,
      title: t('Keep it in'),
      text: t('Pick the new folder. Usherr then asks: move the files there too, or leave them where they are. The window says from where to where, how much, and whether it’s instant (same drive) or a copy (another drive, each file checked first).'),
    },
    {
      kind: 'say',
      when: (c) => inLib(c) && !document.querySelector('[data-walk="title-keepin"]'),
      title: t('Only one folder so far'),
      text: t('Keep it in appears once your library has more than one folder: add one under Settings, Library, Folders, in More folders for other titles.'),
    },
    { kind: 'done', title: t('That’s moving'), text: t('A move never overwrites anything, and undoes itself if something goes wrong. Activity says when it’s done.') },
  ],
}

// ---- tags as collections ----

export const tags: Walkthrough = {
  id: 'tags',
  group: 'library',
  title: t('Make a Kids shelf (tags)'),
  blurb: t('Tag films and shows, and each tag becomes a collection in Jellyfin: a Kids shelf, a Christmas one.'),
  icon: 'bookmark',
  words: 'tags tag kids christmas collection shelf jellyfin group sort',
  simple: [
    ...libraryFilm(t('A film for the shelf')),
    {
      kind: 'point',
      target: 'title-tags',
      until: 'next',
      when: inLib,
      title: t('Tag it'),
      text: t('Type a tag (Kids, Christmas, Dad’s films) and press Enter. Tags already in use are suggested as you type.'),
    },
    {
      kind: 'say',
      title: t('It becomes a collection'),
      text: t('In Jellyfin, every tag is a collection of the same name, a few minutes after: a Kids collection to browse, or to share with a child’s account. In Usherr’s Library, the Tag filter shows just those titles.'),
    },
    {
      kind: 'say',
      title: t('Many at once'),
      text: t('In the Library, Select, tick several titles, then Tags adds a tag to all of them. The add window has a Tags box too, so a new film can start on the shelf.'),
    },
    { kind: 'done', title: t('Your shelf is started'), text: t('Taking a tag off takes the title out of that collection. Collections you made yourself in Jellyfin are never touched.') },
  ],
}

// ---- a download that didn't work ----

export const whyFailed: Walkthrough = {
  id: 'why-failed',
  group: 'library',
  title: t('Why didn’t my download work?'),
  blurb: t('Where Usherr says what went wrong, in plain words, and what to try.'),
  icon: 'warning',
  words: 'failed download error why stuck missing not downloading activity queue blocklist',
  simple: [
    { kind: 'go', to: '/queue', title: t('Activity'), text: t('Every download, and the ones that failed, with the reason.') },
    {
      kind: 'point',
      target: 'queue-why',
      until: 'next',
      optional: true,
      title: t('Why did this happen?'),
      text: t('Under a failed download, this explains the reason in plain words and what to try. The usual one: the release isn’t complete on your Usenet provider any more. Blocklist & search again tries the next release (Usherr also does it by itself).'),
    },
    {
      kind: 'say',
      title: t('Each title keeps a diary'),
      text: t('On every film’s and show’s page, What happened lists each search, what was found and why it was turned down (“12 releases, none acceptable: 8 CAM”), each download and its end. That’s the place to look when something just doesn’t come.'),
    },
    { kind: 'go', to: '/wanted', title: t('Still waiting'), text: t('Wanted lists what’s been added but isn’t here yet. Search now looks again straight away.') },
    {
      kind: 'done',
      title: t('Now you know where to look'),
      text: t('Often there’s simply no good release yet: Usherr keeps searching by itself. A quality profile that accepts more qualities, or a second Usenet provider, finds more.'),
    },
  ],
}

// ---- bringing in a library ----

export const importLibrary: Walkthrough = {
  id: 'import-library',
  group: 'setup',
  admin: true,
  title: t('Bring in the films and shows you already have'),
  blurb: t('Usherr matches your folders to the right titles; nothing is moved, renamed or downloaded again.'),
  icon: 'download',
  words: 'import existing library folder scan films shows already have',
  simple: [
    {
      kind: 'go',
      to: '/import',
      title: t('Import'),
      text: t('Point Usherr at a folder of films or shows. It reads the names, matches each to the right title, and lets you check before anything is added. Your files stay exactly where they are.'),
    },
    { kind: 'point', target: 'import-scan', until: 'next', title: t('Scan the folder'), text: t('Pick Movies or TV shows, check the folder, and press Scan folder.') },
    {
      kind: 'say',
      title: t('Check the matches'),
      text: t('Confident matches are ticked. Check match rows are worth a look: pick the right film from the list, or Search… by hand. Then Import: every title is in the Library at once, its poster and details arriving a moment later.'),
    },
    { kind: 'done', title: t('Your library is in'), text: t('Run it again any time: what’s already there is skipped.') },
  ],
}

// ---- Re-Posterr ----

export const restylePosters: Walkthrough = {
  id: 'restyle',
  group: 'library',
  admin: true,
  title: t('Restyle a poster'),
  blurb: t('Pick the poster, background and logo Jellyfin shows, or give them your own style.'),
  icon: 'image',
  words: 'reposterr re-posterr poster artwork logo background style font restyle',
  simple: [
    {
      kind: 'ask',
      key: 'film',
      title: t('Which film?'),
      text: t('Type its name and pick it.'),
      search: async (q) =>
        (await api.tmdbSearch('movie', q)).slice(0, 8).map((f): SmhChoice => ({ value: String(f.tmdbId), label: f.year ? `${f.title} (${f.year})` : f.title, image: f.posterUrl })),
    },
    { kind: 'go', to: (c) => `/title/${pick(c, 'film')}`, title: label, text: t('Its page.') },
    { kind: 'point', target: 'title-art', act: 'click', until: { seen: 'art-dialog' }, title: t('Re-Posterr'), text: t('Opens the artwork for this film.') },
    {
      kind: 'point',
      target: 'art-dialog',
      until: 'next',
      title: t('Pick, or restyle'),
      text: t('Pick another poster, background or logo from the choices, and Jellyfin and Usherr show it. Your style puts a clean picture together with the title in your own fonts and layout; Change the style opens the designer, on this film.'),
    },
    { kind: 'done', title: t('That’s Re-Posterr'), text: t('Settings, Re-Posterr can restyle the whole library at once, and put anything back as it was.') },
  ],
}
