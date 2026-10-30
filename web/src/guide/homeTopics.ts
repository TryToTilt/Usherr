// Home fork: the in-app guide's topics for what the fork adds, from the pages
// in docs/home. Kept apart from upstream's list so merges stay clean.
import type { TopicDef } from './topicDefs.ts'

export const HOME_GROUP = 'Home fork'

export const HOME_TOPICS: TopicDef[] = [
  {
    id: 'home-start',
    title: 'Getting started with the home fork',
    summary: 'What the fork adds, what you need for it, the quickest way to a working server, and Usherr’s look (Neu, main colours, the menu).',
    group: HOME_GROUP,
    sources: [{ file: 'home/getting-started.md' }],
  },
  {
    id: 'home-setup',
    title: 'Setting up each part',
    summary: 'TMDB, Usenet, Jellyfin, StreamNZB, Live TV, Play on TV, AI, scores, news and comics: where to get each, where it goes, how to test it.',
    group: HOME_GROUP,
    sources: [{ file: 'home/integrations.md' }],
  },
  {
    id: 'home-finding',
    title: 'Finding something to watch',
    summary: 'The Dashboard, Discover and its rows, asking the AI, recipes, the family filter, following people.',
    group: HOME_GROUP,
    sources: [{ file: 'home/finding.md' }],
  },
  {
    id: 'home-watching',
    title: 'Watching it',
    summary: 'Play or Stream, picking up where you left off, skip intro, Showtimes, on the TV, the Library, Live TV, Upcoming, comics and phones.',
    group: HOME_GROUP,
    sources: [{ file: 'home/watching.md' }],
  },
  {
    id: 'home-livetv',
    title: 'Live TV',
    summary: 'Channels from your library, streams and free live channels; packs and the AI builder; scheduling like a network; Real TV; Jellyfin, Plex and IPTV apps.',
    group: HOME_GROUP,
    sources: [{ file: 'home/live-tv.md' }],
  },
  {
    id: 'home-ai',
    title: 'The AI',
    summary: 'Ollama, Claude or another server; the Ask bar and every AI feature; the models and what they cost.',
    group: HOME_GROUP,
    sources: [{ file: 'home/ai.md' }],
  },
  {
    id: 'home-marquee',
    title: 'The Marquee',
    summary: 'The lobby display for a tablet or a screen by the TV: screens and the phone remote, seven looks, what it shows, sports scores, your slides and pre-show, theme music, Home Assistant and the API.',
    group: HOME_GROUP,
    sources: [{ file: 'home/marquee.md' }],
  },
  {
    id: 'home-preshow',
    title: 'The pre-show',
    summary: 'A cinema’s pre-show before a film: themes and their slots, Only when…, schedules, your clips by category, coming-soon trailers downloaded ahead, and the pre-show in the Jellyfin app.',
    group: HOME_GROUP,
    sources: [{ file: 'home/preshow.md' }],
  },
  {
    id: 'home-reposterr',
    title: 'Re-Posterr',
    summary: 'The posters, backgrounds and logos Jellyfin shows: Usherr’s picks from TMDB and fanart.tv, posters made in your own style (season posters, title cards, backgrounds, overlays, a style per library, your own picture and words), TheTVDB and Posterizarr, asking first, choosing by hand, putting one back, and when it looks.',
    group: HOME_GROUP,
    sources: [{ file: 'home/re-posterr.md' }],
  },
  {
    id: 'home-readtome',
    title: 'Read-to-Me',
    summary: 'Turn an ebook into an audiobook with a computer voice: Kokoro and Chatterbox on your graphics card, samples, a preview, night-only, and the finished M4B in your Audiobooks.',
    group: HOME_GROUP,
    sources: [{ file: 'home/read-to-me.md' }],
  },
  {
    id: 'home-sports',
    title: 'Sports',
    summary: 'Leagues, teams and games on the Sports page: Sportarr or Usherr’s own engine, streaming and getting games, scores, who can do what.',
    group: HOME_GROUP,
    sources: [{ file: 'home/sports.md' }],
  },
  {
    id: 'home-troubleshooting',
    title: 'When something goes wrong',
    summary: 'Docker stopped after a Windows update, slow streams, the AI, Jellyfin not showing new titles.',
    group: HOME_GROUP,
    sources: [{ file: 'home/troubleshooting.md' }],
  },
]
