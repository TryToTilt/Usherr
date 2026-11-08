// Home fork: the in-app guide's topics for what the fork adds, from the pages
// in docs/home. Kept apart from upstream's list so merges stay clean.
// Each summary stays under 140 characters (guide.test.ts checks it).
import type { TopicDef } from './topicDefs.ts'

export const HOME_GROUP = 'Usherr’s own'

export const HOME_TOPICS: TopicDef[] = [
  {
    id: 'home-start',
    title: 'Getting started with Usherr’s own parts',
    summary: 'What Usherr adds, what you need for it, the quickest way to a working server, and its looks: Neu, the new layout, TV size.',
    group: HOME_GROUP,
    sources: [{ file: 'home/getting-started.md' }],
  },
  {
    id: 'home-setup',
    title: 'Setting up each part',
    summary: 'TMDB, Usenet, Jellyfin, Plex, streaming, Live TV, AI, scores, comics, sports, linked Usherrs, phone apps: where each goes.',
    group: HOME_GROUP,
    sources: [{ file: 'home/integrations.md' }],
  },
  {
    id: 'home-finding',
    title: 'Finding something to watch',
    summary: 'The Dashboard and Continue watching, Discover and its rows, asking the AI, recipes, the family filter, picking seasons.',
    group: HOME_GROUP,
    sources: [{ file: 'home/finding.md' }],
  },
  {
    id: 'home-watching',
    title: 'Watching it',
    summary: 'Play or Stream, film and show pages, picking up where you left off, Showtimes and the lobby, the TV, Live TV, comics.',
    group: HOME_GROUP,
    sources: [{ file: 'home/watching.md' }],
  },
  {
    id: 'home-together',
    title: 'Watching together',
    summary: 'Linked Usherrs, screenings in step, the lobby, chat and reactions, intermission, the verdict, crews, film clubs, Crew Wrapped.',
    group: HOME_GROUP,
    sources: [{ file: 'home/watch-together.md' }],
  },
  {
    id: 'manga',
    title: 'Manga',
    summary: 'Add a series by its English title, official editions or fan scans, MangaDex chapters and account, Discover, the reader.',
    group: HOME_GROUP,
    sources: [{ file: 'manga.md' }],
  },
  {
    id: 'home-livetv',
    title: 'Live TV',
    summary: 'Channels from your library and streams, packs and the AI builder, Real TV, the encoder and watermark, Jellyfin, Plex, IPTV.',
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
    summary: 'The lobby display by the TV: screens and the phone remote, seven looks, sports scores, your slides, Home Assistant, the API.',
    group: HOME_GROUP,
    sources: [{ file: 'home/marquee.md' }],
  },
  {
    id: 'home-preshow',
    title: 'The pre-show',
    summary: 'A cinema’s pre-show before a film: themes, schedules, your clips, coming-soon trailers, and the pre-show in Jellyfin and Plex.',
    group: HOME_GROUP,
    sources: [{ file: 'home/preshow.md' }],
  },
  {
    id: 'home-community',
    title: 'Community (preview)',
    summary: 'Your own Discord bot for watch parties, the party chat and reviews; ticket stubs, the SEEN stamp and the badge cabinet.',
    group: HOME_GROUP,
    sources: [{ file: 'home/community.md' }],
  },
  {
    id: 'home-reposterr',
    title: 'Re-Posterr',
    summary: 'Posters, backgrounds and logos for Jellyfin and Plex, your own style, collections and poster badges, every change undoable.',
    group: HOME_GROUP,
    sources: [{ file: 'home/re-posterr.md' }],
  },
  {
    id: 'home-readtome',
    title: 'Read-to-Me',
    summary: 'Turn an ebook into an audiobook with a computer voice: Kokoro and Chatterbox on your graphics card, a full cast, the M4B.',
    group: HOME_GROUP,
    sources: [{ file: 'home/read-to-me.md' }],
  },
  {
    id: 'home-sports',
    title: 'Sports',
    summary: 'Leagues, teams and games on the Sports page: Usherr’s own engine or Sportarr, downloading and streaming games, scores.',
    group: HOME_GROUP,
    sources: [{ file: 'home/sports.md' }],
  },
  {
    id: 'home-server',
    title: 'Running the server',
    summary: 'Watch history, service checks, clean-up rule sets and Leaving soon, Simple and Advanced profiles, tags, Jellyfin sign-in.',
    group: HOME_GROUP,
    sources: [{ file: 'home/server.md' }],
  },
  {
    id: 'home-troubleshooting',
    title: 'When something goes wrong',
    summary: 'Docker stopped after a Windows update, slow streams, the AI, Jellyfin not showing new titles.',
    group: HOME_GROUP,
    sources: [{ file: 'home/troubleshooting.md' }],
  },
]
