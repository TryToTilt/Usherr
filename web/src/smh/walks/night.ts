// Usherr SMH: movie night and TV. The Marquee and its remote, Live TV
// (watching it and making a channel), a sports team, and Play on TV.
import { t } from '../../i18n'
import { sportsApi } from '../../home/sportsApi'
import type { SmhCtx, SmhStep, Walkthrough } from '../types'
import { bc, bcTo } from '../look' // Usherr Broadcast: words for its own pages

const pick = (c: SmhCtx, key: string) => (c.answers[key] as { value: string } | undefined)?.value

// ---- the Marquee ----

const marqueeSteps = (advanced: boolean): SmhStep[] => [
  {
    kind: 'go',
    to: '/marquee/manage',
    title: t('The Marquee'),
    text: t('A lobby display for a tablet or a screen next to the TV, like the board outside a cinema: what’s playing now, new and coming films with trailers, tonight on Live TV, your own slides, and the pre-show before a film.'),
  },
  {
    kind: 'say',
    title: t('Open it on the screen'),
    text: t('On the tablet or the screen’s browser, open this address (scan the code, or type it) and make it full-screen. A kiosk browser such as Fully Kiosk keeps it there: set the address as its start page and switch on autoplay for videos and sound. Sign in once; it stays signed in.'),
    qr: () => `${window.location.origin}/marquee`,
  },
  {
    kind: 'point',
    target: 'mq-tabs',
    until: 'next',
    title: t('It shows up by itself'),
    text: t('The new screen appears under Screens on its own, with what it shows now and a remote. Look and scenes chooses how every screen looks; Slides and pre-show holds your own pictures, videos and words.'),
  },
  ...(advanced
    ? ([
        { kind: 'go', to: '/marquee/manage?tab=look', title: t('Look and scenes'), text: t('Seven looks (Usherr, Cinema, One-sheet, the Poster wall…), each with its own themes, fonts and frames, and which scenes play: now showing, coming soon, in theaters, tonight on Live TV, sports, your slides. A screen can keep a few settings of its own.') },
        { kind: 'go', to: '/marquee/manage?tab=media', title: t('Slides and pre-show'), text: t('Your own pictures, videos, web pages and words, shown between the scenes. The Marquee follows the pre-show by itself before a film.') },
      ] as SmhStep[])
    : []),
  {
    kind: 'done',
    title: t('Your Marquee is up'),
    text: t('It runs by itself. The phone remote steers it: next, pause, pin a scene, dim it, start the pre-show.'),
    then: { id: 'marquee-remote', label: t('The phone remote') },
  },
]

export const marquee: Walkthrough = {
  id: 'marquee',
  group: 'night',
  admin: true,
  title: t('Make a Marquee screen'),
  blurb: t('A lobby board for a tablet or a screen by the TV: what’s playing, coming soon, trailers, your slides.'),
  icon: 'monitor',
  words: 'marquee lobby display tablet screen board poster kiosk',
  simple: marqueeSteps(false),
  advanced: marqueeSteps(true),
}

export const marqueeRemote: Walkthrough = {
  id: 'marquee-remote',
  group: 'night',
  admin: true,
  title: t('Steer the Marquee from your phone'),
  blurb: t('The phone remote: next, pause, pin, dim, blank, info and the pre-show, for every screen.'),
  icon: 'play',
  words: 'marquee remote phone control next pause pin dim blank',
  simple: [
    { kind: 'go', to: '/marquee/remote', title: t('The phone remote'), text: t('Every screen, with what it shows now.') },
    {
      kind: 'say',
      title: t('The buttons'),
      text: t('Previous, pause and next move through the scenes. Pin plays this scene over and over. Dim and Blank are for movie time. Info puts the plot and cast over the film. Pre-show starts it now. Look changes this screen’s look; Show now jumps to a scene.'),
    },
    {
      kind: 'say',
      title: t('Keep it on your phone'),
      text: t('Scan this with your phone, then Add to Home Screen: the remote opens like an app.'),
      qr: () => `${window.location.origin}/marquee/remote`,
    },
    { kind: 'done', title: t('That’s the remote'), text: t('Home Assistant can steer the screens too: Marquee, Home Assistant and API.') },
  ],
}

// ---- Live TV ----

export const watchLiveTv: Walkthrough = {
  id: 'watch-live-tv',
  group: 'night',
  title: t('Watch Live TV'),
  blurb: t('Channels like broadcast TV, made from your library and more: the guide, channel up and down, Multiview.'),
  icon: 'tv',
  words: 'live tv channels guide watch cable channel up down multiview favourites reminders',
  simple: [
    { kind: 'go', to: '/tv', title: t('Live TV'), text: t('Channels that play on a schedule, like broadcast TV: tune in and whatever is on plays, from the right second.') },
    {
      kind: 'say',
      title: t('The guide'),
      text: t('Channels down the side, time across. Tap a program to see what it is; tap it again to watch. A later one offers Remind me (a message to your phone five minutes before). Now on shows every channel as tiles.'),
    },
    {
      kind: 'say',
      title: t('Like a cable box'),
      text: t('While watching: Channel up and down (the ↑ ↓ keys, the buttons, or a swipe), type a number to jump, Backspace for the channel before, I for what’s on. Start over plays the program from its beginning.'),
    },
    {
      kind: 'say',
      title: t('Extras'),
      text: t('Multiview plays four channels at once: tap one for its sound. Star your favourite channels and show only those. Guide style changes the look, from a modern grid to a ’90s cable box or the scrolling Prevue guide.'),
    },
    { kind: 'done', title: t('Happy channel surfing'), text: t('The same channels are in Jellyfin’s Live TV too, once an administrator adds them.'), shortcut: t('Next time: Live TV, in the menu.') },
  ],
}

export const makeChannel: Walkthrough = {
  id: 'make-channel',
  group: 'night',
  admin: true,
  title: t('Make a Live TV channel'),
  blurb: t('A sitcom channel, Saturday-morning cartoons, a film night: from your library, streamed shows or a free live stream.'),
  icon: 'tv',
  words: 'live tv channel make create new pack ai schedule sitcom cartoons',
  simple: [
    { kind: 'go', to: '/tv', title: t('Live TV'), text: t('Channels play on a schedule; you choose what goes on each.') },
    {
      kind: 'ask',
      key: 'how',
      title: t('How do you want to start?'),
      card: {
        title: t('Three ways to make a channel'),
        options: [
          { name: t('Channel packs'), good: t('Ready-made channels that fill themselves from your library: sitcoms, cartoons, a film night. Quickest.'), bad: t('Less say in exactly what plays.') },
          { name: t('Build with AI'), good: t('Describe it (“90s sitcoms after school, cartoons on Saturday morning”) and it picks the shows and the schedule.'), bad: t('Needs the AI switched on.') },
          { name: t('New channel'), good: t('You pick every show, film, genre and decade, and the order.'), bad: t('The most choices to make.') },
        ],
        recommend: t('Channel packs for a first channel.'),
        later: t('Every channel can be edited or removed any time.'),
      },
      choices: [
        { value: 'tv-packs', label: t('Channel packs'), recommended: true },
        { value: 'tv-ai', label: t('Build with AI') },
        { value: 'tv-new', label: t('New channel') },
      ],
    },
    {
      kind: 'point',
      target: (c) => pick(c, 'how') ?? 'tv-packs',
      act: 'click',
      until: 'next',
      title: (c) => ({ 'tv-packs': t('Channel packs'), 'tv-ai': t('Build with AI'), 'tv-new': t('New channel') })[pick(c, 'how') ?? 'tv-packs'] ?? '',
      text: (c) =>
        ({
          'tv-packs': t('Pick a pack: it makes the channel and fills it from your library, now and as the library grows.'),
          'tv-ai': t('Describe the channel in your own words, check what it picked, and save it.'),
          'tv-new': t('Give it a name, a number and a logo. Under What plays: shows from your library (Find a show also finds ones you don’t have, streamed as they come on), genres and decades that fill themselves, films and how many a day, and the order: in order, or taking turns.'),
        })[pick(c, 'how') ?? 'tv-packs'] ?? '',
    },
    {
      kind: 'point',
      target: 'tv-actions',
      until: 'next',
      optional: true,
      title: t('In Jellyfin and Plex too'),
      text: t('Add to Jellyfin and Add to Plex put these channels, with their guide, in Jellyfin’s or Plex’s Live TV (Plex’s needs Plex Pass), so they play on every TV and phone.'),
    },
    { kind: 'done', title: t('Your channel is on the air'), text: t('Tune in from the guide.'), then: { id: 'watch-live-tv', label: t('Watch Live TV') } },
  ],
}

// ---- sports ----

export const followTeam: Walkthrough = {
  id: 'follow-team',
  group: 'night',
  title: t('Follow a sports team'),
  blurb: t('Your team’s games, listed and ready to stream or download, with the scores hidden.'),
  icon: 'trophy',
  words: 'sports team league follow game stream nfl football soccer scores',
  simple: [
    {
      kind: 'do',
      title: t('Checking Sports is set up'),
      run: async (c) => {
        if (!c.admin) return { sportsReady: true }
        const s = await sportsApi.settings().catch(() => null)
        return { sportsReady: !!s?.ready }
      },
    },
    {
      kind: 'done',
      when: (c) => c.answers.sportsReady !== true,
      title: t('Sports isn’t set up yet'),
      text: bc(t('Sports runs on Usherr’s own engine (TheSportsDB, no Sportarr needed) or on Sportarr. An administrator sets it up under Server, Kinds of media, Sports.'), t('Sports runs on Usherr’s own engine (TheSportsDB, no Sportarr needed) or on Sportarr. An administrator sets it up under Settings, Extras, Sports.')),
    },
    { kind: 'go', to: '/sports', title: t('Sports'), text: t('The games of the coming two weeks, the leagues in your library and the teams you follow.') },
    {
      kind: 'point',
      target: 'sp-search',
      until: { seen: 'sp-results' },
      title: t('Find your team'),
      text: t('Type a team or a league (Chiefs, Premier League, NFL, Formula 1) and press Enter.'),
    },
    {
      kind: 'point',
      target: 'sp-results',
      until: 'next',
      title: t('Follow, or Add'),
      text: t('Follow a team to get just its games, in every competition it plays. Add puts a whole league in your library. Nothing is downloaded until you ask, or until a league is set to get its new games.'),
    },
    {
      kind: 'point',
      target: 'sp-games',
      until: 'next',
      optional: true,
      title: t('The games'),
      text: t('By day, with the time, the channel that shows it, and a badge when it’s downloaded. Get downloads a game once it’s out; Stream plays one that has started or been played, without downloading.'),
    },
    {
      kind: 'point',
      target: 'sp-scores',
      until: 'next',
      optional: true,
      title: t('No spoilers'),
      text: t('Scores are hidden so they don’t spoil a game you’ll watch later. Show scores turns them on, on this device.'),
    },
    {
      kind: 'done',
      title: t('Go team'),
      text: t('The Marquee can show the day’s games too: a scoreboard, a ticker and a kick-off countdown.'),
      shortcut: t('Next time: Sports, in the menu.'),
      then: { id: 'sports-downloads', label: t('Get a league’s games by themselves'), when: (c) => c.admin },
    },
  ],
}

// Usherr's own engine downloads games: per league, which games, which parts
// of a card or race weekend, and in what quality.
export const getSports: Walkthrough = {
  id: 'sports-downloads',
  group: 'night',
  admin: true,
  title: t('Get a league’s games by themselves'),
  blurb: t('Usherr’s own sports engine downloads new games a few hours after they end: every game, or only your teams’.'),
  icon: 'download',
  words: 'sports download games league automatic new games fight card race weekend sessions quality profile wanted sportarr',
  simple: [
    {
      kind: 'do',
      title: t('Checking Sports is set up'),
      run: async () => {
        const s = await sportsApi.settings().catch(() => null)
        return { sportsReady: !!s?.ready, sportsOwn: s?.mode === 'usherr' }
      },
    },
    {
      kind: 'done',
      when: (c) => c.answers.sportsReady !== true || c.answers.sportsOwn !== true,
      title: t('This needs Usherr’s own engine'),
      text: bc(t('Under Server, Kinds of media, Sports, choose Usherr’s own engine and a Sports folder (where downloaded games go). With Sportarr, Sportarr does the downloading.'), t('Under Settings, Extras, Sports, choose Usherr’s own engine and a Sports folder (where downloaded games go). With Sportarr, Sportarr does the downloading.')),
    },
    { kind: 'go', to: '/sports', title: t('Sports'), text: t('Your leagues are listed under Leagues.') },
    {
      kind: 'point',
      target: 'sp-leagues',
      until: { path: '/sports/league/*' },
      optional: true,
      title: t('Open a league'),
      text: t('Tap the league. Not here yet? Search for it above and press Add.'),
    },
    {
      kind: 'point',
      target: 'sp-get',
      until: 'next',
      title: t('Downloads'),
      text: t('Switch on Get its new games. Then which games (every game, or only the teams you follow), which parts of a fight card or sessions of a race weekend, and the quality profile. Press Save.'),
    },
    {
      kind: 'say',
      title: t('Coming from Sportarr?'),
      text: bc(t('Under Server, Kinds of media, Sports, Bring my followed leagues from Sportarr adds its leagues and teams here, with whether each gets its new games. It only reads Sportarr.'), t('Under Settings, Extras, Sports, Bring my followed leagues from Sportarr adds its leagues and teams here, with whether each gets its new games. It only reads Sportarr.')),
    },
    {
      kind: 'say',
      title: t('How it looks for them'),
      text: t('Usherr looks for each new game on your indexers a few hours after it ends, and keeps looking for two weeks. A game already played: Search now, or Pick to see every release.'),
    },
    {
      kind: 'done',
      title: t('Games come by themselves'),
      text: t('Add the Sports folder to Plex or Jellyfin as a TV library: each league shows as a show, each game as an episode.'),
    },
  ],
}

// ---- Play on TV ----

export const playOnTv: Walkthrough = {
  id: 'play-on-tv',
  group: 'setup',
  admin: true,
  title: t('Set up Play on TV'),
  blurb: t('Send what you’re watching to an Android TV box (an NVIDIA Shield), with one tap in the player.'),
  icon: 'tv',
  words: 'play on tv android shield box cast send adb pair vlc stremio',
  simple: [
    {
      kind: 'say',
      title: t('First, on the TV box'),
      text: t('On the Android TV box: Settings, About, press Build seven times to turn on Developer options. Then in Developer options, turn on Network debugging (USB debugging on some boxes). Note the box’s address (Settings, Network); a fixed address for it in your router helps.'),
    },
    { kind: 'go', to: bcTo('/settings/play-on-tv', '/settings/extras#play-on-tv'), title: bc(t('Play on TV'), t('Extras')), text: bc(t('Server, Watching, Play on TV.'), t('Play on TV is one of the cards here.')) },
    { kind: 'point', target: 'tv-pair-form', until: 'next', title: t('The TV’s address and name'), text: t('Type the box’s address and a name (Living room TV), then Save.') },
    { kind: 'point', target: 'tv-pair', until: 'next', title: t('Pair'), text: t('Press Pair, then allow the prompt that appears on the TV. Test it starts something on the box.') },
    { kind: 'done', title: t('Play on TV is ready'), text: t('In the player, Play on Living room TV (your TV’s name) sends what you’re watching to the box, which plays it in VLC or opens it in Stremio.') },
  ],
}
