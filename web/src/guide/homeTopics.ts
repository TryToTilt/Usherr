// Home fork: the in-app guide's topics for what the fork adds, from the pages
// in docs/home. Kept apart from upstream's list so merges stay clean.
import type { TopicDef } from './topicDefs.ts'

export const HOME_GROUP = 'Home fork'

export const HOME_TOPICS: TopicDef[] = [
  {
    id: 'home-start',
    title: 'Getting started with the home fork',
    summary: 'What the fork adds, what you need for it, and the quickest way to a working server.',
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
    summary: 'Play or Stream, on the TV, the Library, Live TV, Upcoming, comics and phones.',
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
    summary: 'The lobby display for a tablet or a screen by the TV: screens and the phone remote, seven looks, what it shows, your slides and pre-show, theme music, Home Assistant and the API.',
    group: HOME_GROUP,
    sources: [{ file: 'home/marquee.md' }],
  },
  {
    id: 'home-troubleshooting',
    title: 'When something goes wrong',
    summary: 'Docker stopped after a Windows update, slow streams, the AI, Jellyfin not showing new titles.',
    group: HOME_GROUP,
    sources: [{ file: 'home/troubleshooting.md' }],
  },
]
